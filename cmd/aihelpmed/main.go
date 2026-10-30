// Command aihelpmed is the resident daemon of the aihelpme tool: it listens
// on a unix socket, keeps the queue and the finished answers in memory, and
// calls the AI as jobs arrive — the way dockerd backs the docker cli.
// Nothing is persisted: state lives and dies with this process.
package main

import (
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/HulnotHutu/dashscope-call/internal/aihelpme"
)

func main() {
	aihelpme.SetupLogging()

	cfg, err := aihelpme.LoadConfig()
	if err != nil {
		slog.Error("config load failed", "err", err)
		os.Exit(1)
	}
	// 当前用户空间的 /run 运行时 or /tmp 临时目录存放 Unix 域套接字
	sock := aihelpme.SocketPath()
	// 测试连接发现已经存在
	if conn, err := net.Dial("unix", sock); err == nil {
		conn.Close()
		slog.Warn("another daemon instance is running", "socket", sock)
		os.Exit(1)
	}
	_ = os.Remove(sock) // stale socket file left by a previous run
	// 监听 unix 管道
	ln, err := net.Listen("unix", sock)
	if err != nil {
		slog.Error("listen failed", "socket", sock, "err", err)
		os.Exit(1)
	}
	slog.Info("daemon started",
		"socket", sock,
		"pid", os.Getpid(),
		"workspace", cfg.WorkspaceID,
		"model", cfg.Model,
		"quota_db", cfg.QuotaDB,
		"call_timeout", cfg.CallTimeout.String())

	d := aihelpme.NewDaemon(cfg)
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		s := <-sig
		slog.Info("shutdown requested", "signal", s.String())
		ln.Close()
	}()
	// Serve 返回 ErrServerClosed 或 listener 关闭引发的 net.ErrClosed 都属正常退出
	if err := d.Serve(ln); err != nil &&
		!errors.Is(err, http.ErrServerClosed) &&
		!errors.Is(err, net.ErrClosed) {
		slog.Error("serve exited", "err", err)
		os.Exit(1)
	}
	slog.Info("daemon stopped")
}
