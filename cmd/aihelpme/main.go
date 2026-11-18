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
	// --help/-h 优先于「有输入即提问」规则
	if len(os.Args) > 1 && (os.Args[1] == "-h" || os.Args[1] == "--help") {
		usage()
		return
	}
	// 有输入（位置参数或管道 stdin）即向 ai 提问；无输入即取最新回答。
	if len(os.Args) > 1 || hasPipedStdin() {
		exit(runPut(os.Args[1:]))
		return
	}
	exit(runGet())
}

// usage 打印用法说明（--help/-h 触发）。
func usage() {
	fmt.Fprint(os.Stdout, `用法：
  aihelpme "问题"              交付方式跟随 config.yaml 的 stream 配置
  aihelpme --stream "问题"     流式输出，逐字打印
  aihelpme --sync "问题"       阻塞至多 5s，短问题当场得到回答
  aihelpme --async "问题"      仅入队立即返回，稍后运行 aihelpme 取回答
  cat 文档 | aihelpme "总结"   管道内容拼在问题后（可与上述参数组合）
  aihelpme                     取最新一次的回答（未就绪会提示稍等）
  aihelpme --help              本说明
`)
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

// runPut validates and assembles the question and hands it to the daemon
// according to the selected mode: --stream prints the answer as it is
// generated, --sync blocks up to five seconds, --async returns right after
// the submit, and no flag follows the daemon's config.yaml stream default.
func runPut(args []string) error {
	mode := aihelpme.ModeDefault
	var rest []string
	for _, a := range args {
		switch a {
		case "--stream":
			if mode != aihelpme.ModeDefault {
				return fmt.Errorf("模式参数冲突：--stream/--sync/--async 只能用一个")
			}
			mode = aihelpme.ModeStream
		case "--sync":
			if mode != aihelpme.ModeDefault {
				return fmt.Errorf("模式参数冲突：--stream/--sync/--async 只能用一个")
			}
			mode = aihelpme.ModeSync
		case "--async":
			if mode != aihelpme.ModeDefault {
				return fmt.Errorf("模式参数冲突：--stream/--sync/--async 只能用一个")
			}
			mode = aihelpme.ModeAsync
		default:
			rest = append(rest, a)
		}
	}
	question, err := readQuestion(rest)
	if err != nil {
		return err
	}
	streamed := false
	res, err := aihelpme.Ask(question, mode, func(delta string) {
		streamed = true
		fmt.Print(delta)
	})
	if err != nil {
		return err
	}
	if res == nil {
		hint := "仍在处理中，稍后运行 aihelpme 取回答"
		if mode == aihelpme.ModeAsync {
			hint = "已入队，稍后运行 aihelpme 取回答"
		}
		fmt.Fprintln(os.Stderr, hint)
		return nil
	}
	if res.Err != "" {
		return fmt.Errorf("任务失败：%s", res.Err)
	}
	if streamed {
		if !strings.HasSuffix(res.Answer, "\n") {
			fmt.Println()
		}
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
