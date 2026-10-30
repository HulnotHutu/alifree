package aihelpme

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode/utf8"
)

// job is one question waiting to be processed.
type job struct {
	ID       string
	Question string
}

// Result is the outcome of one question. It lives in daemon memory only and
// dies with the process.
type Result struct {
	ID     string
	Answer string
	Err    string
}

// Daemon keeps the queue and the finished answers in memory and processes
// them one at a time. Nothing is persisted: state lives and dies with the
// daemon process, the way container state dies with dockerd.
type Daemon struct {
	cfg     *Config
	model   string // 当前生效的模型，PUT /model 可切换
	mu      sync.Mutex
	queue   []*job
	results []*Result
	working bool
	notify  map[string]chan *Result // waiters blocked in Wait, by job id
}

// NewDaemon returns an empty daemon that calls the API with cfg; the
// effective model starts at cfg.Model and can be switched via PUT /model.
func NewDaemon(cfg *Config) *Daemon {
	return &Daemon{cfg: cfg, model: cfg.Model, notify: map[string]chan *Result{}}
}

// currentModel returns the model used for the next job.
func (d *Daemon) currentModel() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.model
}

// setModel switches the model used for subsequent jobs.
func (d *Daemon) setModel(m string) {
	d.mu.Lock()
	d.model = m
	d.mu.Unlock()
}

// Put appends a question to the queue and returns its job id. A worker
// goroutine starts on the spot if the daemon is idle; otherwise the
// question waits for the running one. Waiters can await the outcome through
// Wait.
func (d *Daemon) Put(question string) string {
	j := &job{ID: newID(), Question: question}
	ch := make(chan *Result, 1) // buffered: the worker never blocks on send
	d.mu.Lock()
	d.notify[j.ID] = ch
	d.queue = append(d.queue, j)
	idle := !d.working
	d.working = true
	d.mu.Unlock()
	if idle {
		go d.work()
	}
	return j.ID
}

// Wait blocks until the job's result is available or the timeout elapses. A
// result delivered through Wait is removed from the fetch queue, so it is
// returned exactly once no matter which path picks it up. ok=false means
// the timeout hit first, or the id is unknown (e.g. the daemon restarted
// and lost its in-memory state).
func (d *Daemon) Wait(id string, timeout time.Duration) (*Result, bool) {
	d.mu.Lock()
	ch := d.notify[id]
	d.mu.Unlock()
	if ch == nil {
		return nil, false
	}
	select {
	case res := <-ch:
		d.removeResult(id)
		return res, true
	case <-time.After(timeout):
		return nil, false
	}
}

// removeResult drops the result with the given id from the fetch queue, so
// a result delivered through Wait is not handed out a second time by Get.
func (d *Daemon) removeResult(id string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	for i, res := range d.results {
		if res.ID == id {
			d.results = append(d.results[:i], d.results[i+1:]...)
			return
		}
	}
}

// work drains the queue serially, waiting on each AI call inside the daemon
// so no terminal ever blocks, and stores every outcome in memory. Exactly
// one worker goroutine exists at a time; it exits when the queue runs dry.
func (d *Daemon) work() {
	for {
		d.mu.Lock()
		if len(d.queue) == 0 {
			d.working = false
			d.mu.Unlock()
			slog.Debug("worker idle, exit")
			return
		}
		j := d.queue[0]
		d.queue = d.queue[1:]
		d.mu.Unlock()

		model := d.currentModel()
		slog.Info("job started", "job_id", j.ID, "model", model, "question_chars", utf8.RuneCountInString(j.Question))
		start := time.Now()
		res := &Result{ID: j.ID}
		answer, err := ChatCompletion(context.Background(), d.cfg, model, j.Question)
		if err != nil {
			res.Err = err.Error()
			slog.Error("job failed",
				"job_id", j.ID,
				"model", model,
				"duration_ms", time.Since(start).Milliseconds(),
				"err", err)
		} else {
			res.Answer = answer
			slog.Info("job finished",
				"job_id", j.ID,
				"model", model,
				"duration_ms", time.Since(start).Milliseconds(),
				"answer_chars", utf8.RuneCountInString(answer))
		}

		d.mu.Lock()
		d.results = append(d.results, res)
		ch := d.notify[res.ID]
		delete(d.notify, res.ID)
		d.mu.Unlock()
		if ch != nil {
			ch <- res // buffered(1): never blocks the worker
		}
	}
}

// Get pops the newest finished result. ok=false means nothing is ready yet:
// the queue is still being worked or the daemon is simply empty. The cli
// turns that into a wait hint.
func (d *Daemon) Get() (*Result, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.results) == 0 {
		return nil, false
	}
	res := d.results[len(d.results)-1]
	d.results = d.results[:len(d.results)-1]
	return res, true
}

// waitMax caps how long a single /wait request may block the handler.
const waitMax = time.Minute

// Serve handles cli connections on ln until it is closed: POST /put enqueues
// a question, GET /wait blocks for one job's result, GET /get fetches the
// newest finished answer, GET /list reports the queue state, and GET/PUT
// /model views or switches the effective model (/list and /model are
// curl-only, not wrapped by the cli). Every request passes through the
// logging middleware.
func (d *Daemon) Serve(ln net.Listener) error {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /put", d.handlePut)
	mux.HandleFunc("GET /wait", d.handleWait)
	mux.HandleFunc("GET /get", d.handleGet)
	mux.HandleFunc("GET /list", d.handleList)
	mux.HandleFunc("GET /model", d.handleGetModel)
	mux.HandleFunc("PUT /model", d.handlePutModel)
	return (&http.Server{Handler: d.logRequests(mux)}).Serve(ln)
}

// logRequests logs each request at its entry (debug) and its exit (info,
// with status and duration) — the entry/exit pair that answers when, what,
// and with what result. It relies on handlers writing a status via
// WriteHeader; unwritten ones count as 200.
func (d *Daemon) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		slog.Debug("request start", "method", r.Method, "path", r.URL.Path)
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		slog.Info("request done",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	})
}

// statusRecorder remembers the status code handed to the client so the
// request-exit log can report the outcome.
type statusRecorder struct {
	http.ResponseWriter
	status   int
	wroteHdr bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.wroteHdr {
		s.status = code
		s.wroteHdr = true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (d *Daemon) handlePut(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Question string `json:"question"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		slog.Warn("invalid put request", "reason", "bad json body", "err", err)
		http.Error(w, `question empty, body should be {"question":"..."}`, http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(req.Question) == "" {
		slog.Warn("invalid put request", "reason", "empty question")
		http.Error(w, `question empty, body should be {"question":"..."}`, http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": d.Put(req.Question)})
}

func (d *Daemon) handleGet(w http.ResponseWriter, r *http.Request) {
	res, ok := d.Get()
	if !ok {
		slog.Debug("get has nothing ready")
		writeJSON(w, http.StatusOK, map[string]string{"state": "pending"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"state":  "ready",
		"id":     res.ID,
		"answer": res.Answer,
		"error":  res.Err,
	})
}

// handleWait backs GET /wait?id=...&timeout=...: it blocks up to the
// requested timeout (capped at waitMax) so the asking cli invocation can
// deliver short-question answers inline.
func (d *Daemon) handleWait(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		http.Error(w, "missing id", http.StatusBadRequest)
		return
	}
	timeout := waitMax
	if t, err := time.ParseDuration(r.URL.Query().Get("timeout")); err == nil && t > 0 && t < waitMax {
		timeout = t
	}
	res, ok := d.Wait(id, timeout)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]string{"state": "pending"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"state":  "ready",
		"id":     res.ID,
		"answer": res.Answer,
		"error":  res.Err,
	})
}

// handleGetModel backs GET /model: the effective model plus the Text models
// with quota left, read live from the quota database. curl-only, like /list.
func (d *Daemon) handleGetModel(w http.ResponseWriter, r *http.Request) {
	available, err := AvailableTextModels(d.cfg.QuotaDB)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{
			"current":   d.currentModel(),
			"available": []ModelInfo{},
			"error":     err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"current":   d.currentModel(),
		"available": available,
	})
}

// handlePutModel backs PUT /model: switch the model used for subsequent
// jobs. The target must be one of the available Text models from the quota
// database; the response mirrors GET /model.
func (d *Daemon) handlePutModel(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Model string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Model) == "" {
		slog.Warn("invalid put model request", "reason", "bad json body")
		http.Error(w, `body should be {"model":"..."}`, http.StatusBadRequest)
		return
	}
	available, err := AvailableTextModels(d.cfg.QuotaDB)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"error": err.Error()})
		return
	}
	usable := false
	for _, m := range available {
		if m.Model == req.Model {
			usable = true
			break
		}
	}
	if !usable {
		slog.Warn("model switch rejected", "model", req.Model)
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"error":     "模型不在可用 Text 列表中：" + req.Model,
			"available": available,
		})
		return
	}
	d.setModel(req.Model)
	slog.Info("model switched", "model", req.Model)
	writeJSON(w, http.StatusOK, map[string]any{
		"current":   d.currentModel(),
		"available": available,
	})
}

// Status snapshots the queue: the ids waiting in submission order, whether
// a job is being processed right now, and how many finished results are
// waiting to be fetched.
func (d *Daemon) Status() (queue []string, working bool, ready int) {
	d.mu.Lock()
	defer d.mu.Unlock()
	queue = make([]string, 0, len(d.queue))
	for _, j := range d.queue {
		queue = append(queue, j.ID)
	}
	return queue, d.working, len(d.results)
}

// handleList backs GET /list: a JSON view of the queue for debugging via
// curl --unix-socket; the cli has no wrapper for it.
func (d *Daemon) handleList(w http.ResponseWriter, r *http.Request) {
	queue, working, ready := d.Status()
	writeJSON(w, http.StatusOK, map[string]any{
		"queue":   queue,
		"working": working,
		"ready":   ready,
	})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}

// SocketPath returns the unix socket the daemon listens on:
// $XDG_RUNTIME_DIR/aihelpme.sock, falling back to a per-user file in the
// system temp dir. The runtime location is per-boot and per-user, matching
// the in-memory lifetime of the daemon.
func SocketPath() string {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "aihelpme.sock")
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("aihelpme-%d.sock", os.Getuid()))
}

func newID() string {
	now := time.Now().Format("20060102-150405")
	var b [2]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(err) // crypto/rand being unavailable is a system-level failure
	}
	return now + "-" + hex.EncodeToString(b[:])
}
