// Command aihelpme is the cli of the terminal AI Q&A tool: with input (a
// question argument or piped stdin) it sends the question to aihelpmed;
// without input it prints the newest answer. The AI calls happen in the
// daemon (aihelpmed), which is spawned on demand. This file owns the whole
// cli surface: input detection, question assembly, output formatting, and
// exit codes.
package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/HulnotHutu/dashscope-call/internal/aihelpme"
)

func main() {
	// 有输入（位置参数或管道 stdin）即向 ai 提问；无输入即取最新回答。
	if len(os.Args) > 1 || hasPipedStdin() {
		exit(runPut(os.Args[1:]))
		return
	}
	exit(runGet())
}

// hasPipedStdin reports whether stdin is a pipe or file rather than a
// terminal, i.e. whether the caller piped a document into us.
func hasPipedStdin() bool {
	stat, err := os.Stdin.Stat()
	if err != nil {
		return false
	}
	return stat.Mode()&os.ModeCharDevice == 0
}

// exit reports an error with status 1; nil returns quietly with status 0.
func exit(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "错误："+err.Error())
		os.Exit(1)
	}
}

// runPut validates and assembles the question and hands it to the daemon,
// then blocks up to five seconds: short questions print their answer right
// in this invocation, longer ones leave a wait hint on stderr and the
// answer is fetched by a later bare invocation.
func runPut(args []string) error {
	question, err := readQuestion(args)
	if err != nil {
		return err
	}
	res, err := aihelpme.Ask(question)
	if err != nil {
		return err
	}
	if res == nil {
		fmt.Fprintln(os.Stderr, "仍在处理中，稍后运行 aihelpme 取回答")
		return nil
	}
	return printResult(res)
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
		return fmt.Errorf("稍等片刻再取")
	}
	return printResult(res)
}

// printResult writes the outcome for the user: the failure reason as an
// error, or the answer alone on stdout so it can be piped on.
func printResult(res *aihelpme.Result) error {
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
		return "", fmt.Errorf("问题为空（输入为空管道或仅空白字符）")
	}
	return question, nil
}
