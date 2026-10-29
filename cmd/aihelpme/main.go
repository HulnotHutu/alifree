// Command aihelpme is the cli of the terminal AI Q&A tool: -a sends a
// question to aihelpmed, no arguments fetches the newest answer. The AI
// calls happen in the daemon (aihelpmed), which is spawned on demand. This
// file owns the whole cli surface: argument validation, question assembly,
// output formatting, and exit codes.
package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/HulnotHutu/dashscope-call/internal/aihelpme"
)

func main() {
	if len(os.Args) < 2 {
		exit(runGet())
		return
	}
	switch os.Args[1] {
	case "-a":
		exit(runPut(os.Args[2:]))
	default:
		usage()
		os.Exit(2)
	}
}

// exit reports an error with status 1; nil returns quietly with status 0.
func exit(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "错误："+err.Error())
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `用法：aihelpme -a "问题"   向ai提交问题
      aihelpme             等待并打印最新一次的回答
`)
}

// runPut validates and assembles the question, hands it to the daemon, and
// prints only its job id, the way docker run -d prints a container id.
func runPut(args []string) error {
	question, err := readQuestion(args)
	if err != nil {
		return err
	}
	id, err := aihelpme.Submit(question)
	if err != nil {
		return err
	}
	fmt.Println(id)
	return nil
}

// runGet fetches the newest finished answer: a wait hint when nothing is
// ready yet, the failure reason when the job failed, and otherwise the
// answer alone on stdout so it can be piped on.
func runGet() error {
	res, err := aihelpme.Fetch()
	if err != nil {
		return err
	}
	if res == nil {
		return fmt.Errorf("还没有就绪的回答，稍等片刻再取")
	}
	if res.Err != "" {
		return fmt.Errorf("任务失败：%s", res.Err)
	}
	if !strings.HasSuffix(res.Answer, "\n") {
		res.Answer += "\n"
	}
	fmt.Print(res.Answer)
	return nil
}

// readQuestion assembles the question text: the words following -a, plus
// the piped stdin when present (appended between --- markers, matching the
// retired shell script).
func readQuestion(args []string) (string, error) {
	stat, err := os.Stdin.Stat()
	if err != nil {
		return "", err
	}
	piped := stat.Mode()&os.ModeCharDevice == 0
	question := strings.Join(args, " ")
	if piped {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", err
		}
		if question == "" {
			question = string(data)
		} else {
			question = question + "\n---\n" + string(data) + "\n---"
		}
	}
	if strings.TrimSpace(question) == "" {
		return "", fmt.Errorf(`问题为空，用法：aihelpme -a "问题"`)
	}
	return question, nil
}
