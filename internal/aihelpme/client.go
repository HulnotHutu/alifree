package aihelpme

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// Client talks to the daemon over its unix socket.
type Client struct {
	http *http.Client // 短请求用（put/wait/get），带整体超时
	raw  *http.Client // 流式读用：无整体超时，时长由生成本身决定
}

// NewClient returns a client for SocketPath.
func NewClient() *Client {
	tr := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return net.Dial("unix", SocketPath())
		},
	}
	return &Client{
		http: &http.Client{Timeout: 10 * time.Second, Transport: tr},
		raw:  &http.Client{Transport: tr},
	}
}

// askWait is how long the cli blocks on a fresh question before giving up
// and leaving the answer for a later bare invocation.
const askWait = 5 * time.Second

// Ask submits one question and returns its outcome. In blocking mode (the
// daemon default) it waits up to askWait: short questions finish inside
// that window and come back immediately, longer ones return res == nil and
// keep processing daemon-side — a later Fetch returns them. In stream mode
// every delta is forwarded to onDelta as it arrives and the returned result
// carries the full answer. When the daemon is not running it is spawned
// detached and the submit is retried once.
func Ask(question string, onDelta func(string)) (*Result, error) {
	c := NewClient()
	id, stream, err := c.Put(question)
	if err != nil {
		if spawnErr := spawnDaemon(); spawnErr != nil {
			return nil, fmt.Errorf("aihelpmed 未运行且无法拉起（%v）", spawnErr)
		}
		if id, stream, err = c.Put(question); err != nil {
			return nil, err
		}
	}
	if stream {
		return c.Stream(id, onDelta)
	}
	return c.Wait(id)
}

// Stream follows the job's generation over the /stream SSE endpoint,
// forwarding each delta to onDelta, and returns the finished result. It
// uses the raw client: a long generation must not hit the short-request
// timeout.
func (c *Client) Stream(id string, onDelta func(string)) (*Result, error) {
	resp, err := c.raw.Get("http://aihelpme/stream?id=" + url.QueryEscape(id))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d：%s", resp.StatusCode, truncate(string(b), 200))
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var ev struct {
			T      string `json:"t"`
			Done   bool   `json:"done"`
			Answer string `json:"answer"`
			Err    string `json:"error"`
		}
		if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev) != nil {
			continue
		}
		if ev.T != "" && onDelta != nil {
			onDelta(ev.T)
		}
		if ev.Done {
			return &Result{ID: id, Answer: ev.Answer, Err: ev.Err}, nil
		}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return nil, fmt.Errorf("流式连接提前结束")
}

// Wait blocks up to askWait for the given job's result, or returns nil when
// it is not ready in time (state "pending").
func (c *Client) Wait(id string) (*Result, error) {
	q := url.Values{"id": {id}, "timeout": {askWait.String()}}.Encode()
	resp, err := c.http.Get("http://aihelpme/wait?" + q)
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

// Fetch returns the newest finished result from the daemon, or nil when
// nothing is ready yet (still processing, or no job at all). When the
// daemon is not running it is spawned detached and the fetch is retried
// once.
func Fetch() (*Result, error) {
	c := NewClient()
	res, err := c.Get()
	if err != nil {
		if spawnErr := spawnDaemon(); spawnErr != nil {
			return nil, fmt.Errorf("连接 aihelpmed 失败（%v）", err)
		}
		if res, err = c.Get(); err != nil {
			return nil, err
		}
	}
	return res, nil
}

// Put sends one question to the daemon and returns its job id plus whether
// the daemon runs in stream mode.
func (c *Client) Put(question string) (string, bool, error) {
	body, err := json.Marshal(map[string]string{"question": question})
	if err != nil {
		return "", false, err
	}
	resp, err := c.http.Post("http://aihelpme/put", "application/json", bytes.NewReader(body))
	if err != nil {
		return "", false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return "", false, fmt.Errorf("HTTP %d：%s", resp.StatusCode, truncate(string(b), 200))
	}
	var out struct {
		ID     string `json:"id"`
		Stream bool   `json:"stream"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", false, err
	}
	return out.ID, out.Stream, nil
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
		return "", fmt.Errorf("找不到 aihelpmed")
	}
	return p, nil
}
