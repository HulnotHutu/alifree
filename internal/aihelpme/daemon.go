package aihelpme

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
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
			return
		}
		j := d.queue[0]
		d.queue = d.queue[1:]
		d.mu.Unlock()

		res := &Result{ID: j.ID}
		answer, err := ChatCompletion(context.Background(), d.cfg, j.Question)
		if err != nil {
			res.Err = err.Error()
		} else {
			res.Answer = answer
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
// a question, GET /get fetches the newest finished answer.
func (d *Daemon) Serve(ln net.Listener) error {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /put", d.handlePut)
	mux.HandleFunc("GET /get", d.handleGet)
	return (&http.Server{Handler: mux}).Serve(ln)
}

func (d *Daemon) handlePut(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Question string `json:"question"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Question) == "" {
		http.Error(w, `question empty, body should be {"question":"..."}`, http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"id": d.Put(req.Question)})
}

func (d *Daemon) handleGet(w http.ResponseWriter, r *http.Request) {
	res, ok := d.Get()
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

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
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
