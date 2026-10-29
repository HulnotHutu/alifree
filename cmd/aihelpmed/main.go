// Command aihelpmed is the resident daemon of the aihelpme tool: it listens
// on a unix socket, keeps the queue and the finished answers in memory, and
// calls the AI as jobs arrive — the way dockerd backs the docker cli.
// Nothing is persisted: state lives and dies with this process.
package main

import (
	"fmt"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/HulnotHutu/dashscope-call/internal/aihelpme"
)

func main() {
	cfg, err := aihelpme.LoadConfig()
	if err != nil {
		fmt.Fprintln(os.Stderr, "aihelpmed："+err.Error())
		os.Exit(1)
	}

	sock := aihelpme.SocketPath()
	if conn, err := net.Dial("unix", sock); err == nil {
		conn.Close()
		fmt.Fprintln(os.Stderr, "aihelpmed：已有实例在运行")
		os.Exit(1)
	}
	_ = os.Remove(sock) // stale socket file left by a previous run

	ln, err := net.Listen("unix", sock)
	if err != nil {
		fmt.Fprintln(os.Stderr, "aihelpmed 监听失败："+err.Error())
		os.Exit(1)
	}
	fmt.Println("aihelpmed：监听", sock, "（Ctrl-C 退出，内存数据随之清空）")

	d := aihelpme.NewDaemon(cfg)
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		<-sig
		ln.Close()
	}()
	if err := d.Serve(ln); err != nil {
		fmt.Fprintln(os.Stderr, "aihelpmed 退出："+err.Error())
		os.Exit(1)
	}
}
