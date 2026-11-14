package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"sort"
	"strings"
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
	root.AddCommand(newFetchCmd(), newServeCmd(), newHistoryCmd(), newFreetierCmd())
	return root
}

func newFreetierCmd() *cobra.Command {
	var (
		enable    bool
		disable   bool
		allModels bool
		asJSON    bool
		ak, sk    string
		stsToken  string
		envFile   string
		dbPath    string
	)
	cmd := &cobra.Command{
		Use:   "freetier [模型名...]",
		Short: "查询或设置模型的「用完即停」开关",
		Long: `「用完即停」开启后，该模型免费额度用尽时推理网关直接返回 403
（AllocationQuota.FreeTierOnly.），不会转按量计费——适合防止超额调用。

不带参数时查询指定模型（或全部有额度模型）的开关状态；
--enable / --disable 批量设置（写入为异步批量任务，提交后自动轮询）。`,
		Example: `  quota freetier                          # 查询全部有额度模型的状态
  quota freetier qwen3.8-flash            # 查询指定模型
  quota freetier --enable qwen3.8-max     # 开启（防超额）
  quota freetier --disable qwen3.8-flash  # 关闭
  quota freetier --enable --all           # 一次开启全部有额度模型（读最新快照）`,
		RunE: func(cmd *cobra.Command, args []string) error {
			creds := resolveCreds(ak, sk, stsToken, envFile)
			if !creds.Complete() {
				return fmt.Errorf("缺少凭证：请设置 ALIBABA_CLOUD_ACCESS_KEY_ID / ALIBABA_CLOUD_ACCESS_KEY_SECRET（或用 --env-file 指定凭证文件）")
			}
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			client := bailian.NewClient(creds.AK, creds.SK, creds.STS)

			switch {
			case enable && disable:
				return fmt.Errorf("--enable 与 --disable 不能同时使用")
			case enable || disable:
				models := args
				if allModels {
					if len(models) > 0 {
						return fmt.Errorf("--all 不能与模型名同时使用")
					}
					st, err := store.Open(dbPath)
					if err != nil {
						return err
					}
					defer st.Close()
					_, snap, err := st.Latest()
					if err != nil {
						return fmt.Errorf("读取最新快照失败：%w（先运行 quota fetch --save）", err)
					}
					for _, m := range snap.Models {
						models = append(models, m.Model)
					}
					if len(models) == 0 {
						return fmt.Errorf("最新快照里没有模型")
					}
				}
				if len(models) == 0 {
					return fmt.Errorf("请指定要设置的模型名，或使用 --all 作用于全部有额度模型")
				}

				// 分批提交（每批 50 个），聚合任务结果
				out := cmd.OutOrStdout()
				action := map[bool]string{true: "开启", false: "关闭"}[enable]
				var success, failed []string
				const batch = 50
				for i := 0; i < len(models); i += batch {
					end := i + batch
					if end > len(models) {
						end = len(models)
					}
					raw, err := client.SetFreeTierOnly(ctx, models[i:end], enable)
					if err != nil {
						return err
					}
					var result struct {
						SuccessModels []string `json:"successModels"`
						FailureModels []string `json:"failureModels"`
					}
					if err := json.Unmarshal(raw, &result); err == nil {
						success = append(success, result.SuccessModels...)
						failed = append(failed, result.FailureModels...)
					}
					fmt.Fprintf(cmd.ErrOrStderr(), "已提交 %d/%d…\n", end, len(models))
				}

				if asJSON {
					return encodeJSON(out, map[string]any{
						"action": action, "success": success, "failure": failed,
					})
				}
				fmt.Fprintf(out, "已%s %d/%d 个模型的用完即停：成功 %s",
					action, len(success), len(models), strings.Join(success, ", "))
				if len(failed) > 0 {
					fmt.Fprintf(out, "；失败 %s", strings.Join(failed, ", "))
				}
				fmt.Fprintln(out)
				return nil
			default:
				// 按模型的用完即停状态（异步接口，五大类型各查一次）
				status, err := client.QueryFreeTierOnly(ctx, bailian.DefaultModelTypes)
				if err != nil {
					return fmt.Errorf("查询按模型状态失败：%w", err)
				}
				// 指定模型时只显示这些模型
				if len(args) > 0 {
					filtered := map[string]bool{}
					for _, a := range args {
						v, ok := status[a]
						if !ok {
							return fmt.Errorf("模型 %s 没有免费额度记录，无法查询其用完即停状态", a)
						}
						filtered[a] = v
					}
					status = filtered
				}
				type row struct {
					model   string
					enabled bool
				}
				rows := make([]row, 0, len(status))
				on := 0
				for model, enabled := range status {
					rows = append(rows, row{model, enabled})
					if enabled {
						on++
					}
				}
				sort.Slice(rows, func(i, j int) bool {
					if rows[i].enabled != rows[j].enabled {
						return rows[i].enabled
					}
					return rows[i].model < rows[j].model
				})
				fmt.Fprintf(cmd.OutOrStdout(), "用完即停：开启 %d / 未开启 %d（共 %d 个模型）\n\n",
					on, len(status)-on, len(status))
				table := tablewriter.NewTable(cmd.OutOrStdout(),
					tablewriter.WithHeader([]string{"model", "用完即停"}),
				)
				for _, r := range rows {
					state := "未开启"
					if r.enabled {
						state = "开启"
					}
					table.Append([]string{r.model, state})
				}
				return table.Render()
			}
		},
	}
	cmd.Flags().BoolVar(&enable, "enable", false, "开启用完即停（防超额）")
	cmd.Flags().BoolVar(&disable, "disable", false, "关闭用完即停")
	cmd.Flags().BoolVar(&allModels, "all", false, "作用于最新快照里的全部有额度模型")
	cmd.Flags().BoolVar(&asJSON, "json", false, "附带原始 JSON 输出")
	cmd.Flags().StringVar(&ak, "ak", "", "AccessKey ID（默认读环境变量）")
	cmd.Flags().StringVar(&sk, "sk", "", "AccessKey Secret（默认读环境变量）")
	cmd.Flags().StringVar(&stsToken, "sts", "", "STS 临时凭证（可选）")
	cmd.Flags().StringVar(&envFile, "env-file", DefaultEnvFile(), "AK/SK 凭证文件")
	cmd.Flags().StringVar(&dbPath, "db", DefaultDBPath(), "SQLite 库路径（--all 读取模型清单用）")
	return cmd
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
