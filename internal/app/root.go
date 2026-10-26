package app

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/olekukonko/tablewriter"
	"github.com/olekukonko/tablewriter/tw"
	"github.com/spf13/cobra"

	"github.com/HulnotHutu/dashscope-call/internal/bailian"
	"github.com/HulnotHutu/dashscope-call/internal/format"
	"github.com/HulnotHutu/dashscope-call/internal/store"
)

// Execute 运行 quota 命令行工具（fetch / serve）。
func Execute() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "错误："+err.Error())
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "quota",
		Short: "百炼（Model Studio）免费额度查询与看板",
		Long: `查询阿里云百炼各模型的免费额度，或启动本地看板网页。

抓取链路对齐官方 bailian-cli：AK/SK 换 CLI token → 匿名拉模型清单 →
Bearer token 查询免费额度（详见 status.md）。`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newFetchCmd(), newServeCmd())
	return root
}

func newFetchCmd() *cobra.Command {
	var (
		asJSON   bool
		archive  bool
		ak, sk   string
		envFile  string
		dataDir  string
		stsToken string
	)
	cmd := &cobra.Command{
		Use:   "fetch",
		Short: "抓取一次免费额度（表格或 JSON 输出）",
		Example: `  quota fetch                 # 文本表格，按剩余比例升序（最紧张在前）
  quota fetch --json          # JSON 输出
  quota fetch --archive       # 归档到数据目录（cron 用）`,
		RunE: func(cmd *cobra.Command, args []string) error {
			creds := resolveCreds(ak, sk, stsToken, envFile)
			if !creds.Complete() {
				return fmt.Errorf("缺少凭证：请设置 ALIBABA_CLOUD_ACCESS_KEY_ID / ALIBABA_CLOUD_ACCESS_KEY_SECRET（或用 --env-file 指定凭证文件）")
			}

			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			snap, err := bailian.NewClient(creds.AK, creds.SK, creds.STS).Snapshot(ctx)
			if err != nil {
				return err
			}

			if archive {
				path, err := store.New(dataDir).Save(snap)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "已归档 %d 个模型（%d 个有额度）→ %s\n",
					snap.Summary.TotalModels, snap.Summary.WithQuota, path)
				return nil
			}
			if asJSON {
				return encodeJSON(cmd.OutOrStdout(), snap)
			}
			printTable(cmd, snap)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "输出 JSON")
	cmd.Flags().BoolVar(&archive, "archive", false, "归档到数据目录并更新 latest.json")
	cmd.Flags().StringVar(&ak, "ak", "", "AccessKey ID（默认读环境变量）")
	cmd.Flags().StringVar(&sk, "sk", "", "AccessKey Secret（默认读环境变量）")
	cmd.Flags().StringVar(&stsToken, "sts", "", "STS 临时凭证（可选）")
	cmd.Flags().StringVar(&envFile, "env-file", DefaultEnvFile(), "AK/SK 凭证文件")
	cmd.Flags().StringVar(&dataDir, "data-dir", DefaultDataDir(), "归档目录")
	return cmd
}

// resolveCreds 组装凭证：显式 flag > 环境变量 > 凭证文件（与旧版一致，文件覆盖环境变量）。
func resolveCreds(ak, sk, sts, envFile string) Credentials {
	creds := LoadCredentials(envFile)
	if ak != "" {
		creds.AK = ak
	}
	if sk != "" {
		creds.SK = sk
	}
	if sts != "" {
		creds.STS = sts
	}
	return creds
}

func printTable(cmd *cobra.Command, snap *bailian.Snapshot) {
	out := cmd.OutOrStdout()
	fmt.Fprintf(out, "免费额度：%d 个模型有额度（共 %d 个模型）\n",
		snap.Summary.WithQuota, snap.Summary.TotalModels)

	table := tablewriter.NewTable(out,
		tablewriter.WithHeader([]string{"model", "type", "remaining", "total", "%", "expires"}),
		tablewriter.WithRowAlignment(tw.AlignRight),
	)
	for _, m := range snap.Models {
		table.Append([]string{
			m.Model, m.Type,
			format.Number(m.Remaining), format.Number(m.Total),
			fmt.Sprintf("%.1f", m.RemainingPercent), m.Expires,
		})
	}
	_ = table.Render()
}
