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
	mu      sync.Mutex
	queue   []*job
	results []*Result
	working bool
}

// NewDaemon returns an empty daemon that calls the API with cfg.
func NewDaemon(cfg *Config) *Daemon { return &Daemon{cfg: cfg} }

// Put appends a question to the queue and returns its job id, which the cli
// prints docker-run -d style. A worker goroutine starts on the spot if the
// daemon is idle; otherwise the question waits for the running one.
func (d *Daemon) Put(question string) string {
	j := &job{ID: newID(), Question: question}
	d.mu.Lock()
	d.queue = append(d.queue, j)
	idle := !d.working
	d.working = true
	d.mu.Unlock()
	if idle {
		go d.work()
	}
	return j.ID
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

		slog.Info("job started", "job_id", j.ID, "question_chars", utf8.RuneCountInString(j.Question))
		start := time.Now()
		res := &Result{ID: j.ID}
		answer, err := ChatCompletion(context.Background(), d.cfg, j.Question)
		if err != nil {
			res.Err = err.Error()
			slog.Error("job failed",
				"job_id", j.ID,
				"duration_ms", time.Since(start).Milliseconds(),
				"err", err)
		} else {
			res.Answer = answer
			slog.Info("job finished",
				"job_id", j.ID,
				"duration_ms", time.Since(start).Milliseconds(),
				"answer_chars", utf8.RuneCountInString(answer))
		}

		d.mu.Lock()
		d.results = append(d.results, res)
		d.mu.Unlock()
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

// Serve handles cli connections on ln until it is closed: POST /put enqueues
// a question, GET /get fetches the newest finished answer, and GET /list
// reports the queue state (intended for curl, not wrapped by the cli).
// Every request passes through the logging middleware.
func (d *Daemon) Serve(ln net.Listener) error {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /put", d.handlePut)
	mux.HandleFunc("GET /get", d.handleGet)
	mux.HandleFunc("GET /list", d.handleList)
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
