package aihelpme

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

// Client talks to the daemon over its unix socket.
type Client struct {
	http *http.Client
}

// NewClient returns a client for SocketPath.
func NewClient() *Client {
	return &Client{http: &http.Client{
		Timeout: 10 * time.Second, // put/get are instant; the AI wait happens daemon-side
		Transport: &http.Transport{
			DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
				return net.Dial("unix", SocketPath())
			},
		},
	}}
}

// Submit sends one question to the daemon and returns its job id. When the
// daemon is not running it is spawned detached and the put is retried once.
func Submit(question string) (string, error) {
	c := NewClient()
	id, err := c.Put(question)
	if err != nil {
		if spawnErr := spawnDaemon(); spawnErr != nil {
			return "", fmt.Errorf("aihelpmed 未运行且无法拉起（%v）：请先手动运行 aihelpmed", spawnErr)
		}
		if id, err = c.Put(question); err != nil {
			return "", err
		}
	}
	return id, nil
}

// Fetch returns the newest finished result from the daemon, or nil when
// nothing is ready yet (still processing, or no job at all). When the
// daemon is not running it is spawned detached and the fetch is retried
// once.
func Fetch() (*Result, error) {
	c := NewClient()
	res, err := c.Get()
	if err != nil {
		if spawnErr := spawnDaemon(); spawnErr != nil {
			return nil, fmt.Errorf("连接 aihelpmed 失败（%v）：请先手动运行 aihelpmed", err)
		}
		if res, err = c.Get(); err != nil {
			return nil, err
		}
	}
	return res, nil
}

// Put sends one question to the daemon and returns its job id.
func (c *Client) Put(question string) (string, error) {
	body, err := json.Marshal(map[string]string{"question": question})
	if err != nil {
		return "", err
	}
	resp, err := c.http.Post("http://aihelpme/put", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("HTTP %d：%s", resp.StatusCode, truncate(string(b), 200))
	}
	var out struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.ID, nil
}

// Get returns the newest finished result, or nil when the daemon has
// nothing ready yet (state "pending").
func (c *Client) Get() (*Result, error) {
	resp, err := c.http.Get("http://aihelpme/get")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out struct {
		State  string `json:"state"`
		ID     string `json:"id"`
		Answer string `json:"answer"`
		Err    string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if out.State != "ready" {
		return nil, nil
	}
	return &Result{ID: out.ID, Answer: out.Answer, Err: out.Err}, nil
}

// spawnDaemon starts aihelpmed as a detached process and waits for its
// socket to accept connections. The binary is looked up next to the cli
// first (the make build layout), then on PATH (covers go run, where the cli
// itself is a temporary binary). It is a fallback: running aihelpmed by
// hand always works too.
func spawnDaemon() error {
	daemon, err := daemonBinary()
	if err != nil {
		return err
	}
	cmd := exec.Command(daemon)
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // new session: survives the terminal closing
	if err := cmd.Start(); err != nil {
		return err
	}
	_ = cmd.Process.Release()
	for i := 0; i < 50; i++ { // wait until the socket actually accepts
		conn, err := net.Dial("unix", SocketPath())
		if err == nil {
			conn.Close()
			return nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return fmt.Errorf("socket 未就绪：%s", SocketPath())
}

// daemonBinary locates aihelpmed: next to the running cli binary, else on
// PATH.
func daemonBinary() (string, error) {
	if exe, err := os.Executable(); err == nil {
		p := filepath.Join(filepath.Dir(exe), "aihelpmed")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	p, err := exec.LookPath("aihelpmed")
	if err != nil {
		return "", fmt.Errorf("找不到 aihelpmed（make build 生成 bin/aihelpmed，或放入 PATH）")
	}
	return p, nil
}
