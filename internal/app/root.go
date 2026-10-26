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

// Execute 运行 quota 命令行工具（fetch / serve / history）。
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

数据保存在项目内 SQLite 库（默认 data/quota.db，--db / BAILIAN_DB 可改）；
抓取链路对齐官方 bailian-cli：AK/SK 换 CLI token → 匿名拉模型清单 →
Bearer token 查询免费额度（详见 status.md）。`,
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newFetchCmd(), newServeCmd(), newHistoryCmd())
	return root
}

func newFetchCmd() *cobra.Command {
	var (
		asJSON   bool
		save     bool
		ak, sk   string
		envFile  string
		dbPath   string
		stsToken string
	)
	cmd := &cobra.Command{
		Use:   "fetch",
		Short: "抓取一次免费额度（表格或 JSON 输出）",
		Example: `  quota fetch                 # 文本表格，按剩余比例升序（最紧张在前）
  quota fetch --json          # JSON 输出
  quota fetch --save          # 保存到 SQLite 库（cron / 看板刷新用）`,
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

			if save {
				st, err := store.Open(dbPath)
				if err != nil {
					return err
				}
				defer st.Close()
				id, err := st.Save(snap)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "已保存 %d 个模型（%d 个有额度）→ %s 快照 #%d\n",
					snap.Summary.TotalModels, snap.Summary.WithQuota, dbPath, id)
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
	cmd.Flags().BoolVar(&save, "save", false, "保存到 SQLite 库")
	cmd.Flags().StringVar(&ak, "ak", "", "AccessKey ID（默认读环境变量）")
	cmd.Flags().StringVar(&sk, "sk", "", "AccessKey Secret（默认读环境变量）")
	cmd.Flags().StringVar(&stsToken, "sts", "", "STS 临时凭证（可选）")
	cmd.Flags().StringVar(&envFile, "env-file", DefaultEnvFile(), "AK/SK 凭证文件")
	cmd.Flags().StringVar(&dbPath, "db", DefaultDBPath(), "SQLite 库文件路径")
	return cmd
}

func newHistoryCmd() *cobra.Command {
	var (
		limit  int
		dbPath string
	)
	cmd := &cobra.Command{
		Use:   "history",
		Short: "查看最近的抓取记录",
		Example: `  quota history               # 最近 20 次
  quota history --limit 50`,
		RunE: func(cmd *cobra.Command, args []string) error {
			st, err := store.Open(dbPath)
			if err != nil {
				return err
			}
			defer st.Close()

			runs, err := st.History(limit)
			if err != nil {
				return err
			}
			if len(runs) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "还没有抓取记录。先运行 quota fetch --save。")
				return nil
			}
			table := tablewriter.NewTable(cmd.OutOrStdout(),
				tablewriter.WithHeader([]string{"id", "fetched_at", "models", "with_quota"}),
				tablewriter.WithRowAlignment(tw.AlignRight),
			)
			for _, r := range runs {
				table.Append([]string{
					fmt.Sprint(r.ID), r.FetchedAt,
					fmt.Sprint(r.TotalModels), fmt.Sprint(r.WithQuota),
				})
			}
			return table.Render()
		},
	}
	cmd.Flags().IntVar(&limit, "limit", 20, "显示条数")
	cmd.Flags().StringVar(&dbPath, "db", DefaultDBPath(), "SQLite 库文件路径")
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
