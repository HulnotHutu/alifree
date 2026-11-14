# DashScope / 百炼工具集

Go 实现的工具集：**终端 AI 异步问答（aihelpme）** 与 **百炼免费额度 CLI 与本地看板（quota）**，以及通过 `curl` 测试 DashScope / Model Studio API 的参考文档。

详细文档：

- [docs/aihelpme.md](docs/aihelpme.md) —— aihelpme 用法、配置、调试接口、流式模式、日志与 systemd 用户服务部署
- [docs/dashscope-api.md](docs/dashscope-api.md) —— DashScope / Model Studio API 的 curl 参考（模型列表、可用性检查、原生 Generation API 等）

## 目录

- [目录结构](#目录结构)
- [构建与测试](#构建与测试)
- [终端 AI 异步问答（aihelpme）](#终端-ai-异步问答aihelpme)
- [免费额度 CLI（quota fetch）](#免费额度-cliquota-fetch)
- [额度看板（quota serve）](#额度看板quota-serve)
- [准备凭证](#准备凭证)
- [定时抓取（cron）](#定时抓取cron)
- [License](#license)

## 目录结构

```text
dashscope-call/
├── cmd/quota/            # 入口：quota fetch / serve / history
├── cmd/aihelpme/         # cli：有输入即提问 / 无输入取回答
├── cmd/aihelpmed/        # daemon：内存队列 + unix socket + AI 调用
├── internal/
│   ├── aihelpme/         # cli/daemon 共用：daemon 内存队列、socket RPC、配置加载、compatible-mode 调用
│   ├── bailian/          # ACS3 签名、CLI token、控制台网关、额度快照
│   ├── store/            # SQLite 存储（snapshots + model_quotas 两表）
│   ├── format/           # K/M 数字与到期日格式化
│   └── app/              # cobra 命令、HTTP 看板服务、内嵌页面
├── docs/                 # 详细文档（aihelpme、DashScope API curl 参考）
├── data/                 # SQLite 库（运行时生成，已 gitignore）
├── ROUTE.md              # DashScope API 路由调研
├── status.md             # 免费额度接口调研记录（含网页版 cookie 路线）
└── Makefile
```

## 构建与测试

需要 Go 1.24+（模块代理不通时可设 `HTTPS_PROXY=socks5://127.0.0.1:10808`）：

```bash
make build     # 构建 bin/quota、bin/aihelpme 与 bin/aihelpmed
make test      # 单元测试
make vet fmt   # 静态检查与格式化
```

## 终端 AI 异步问答（aihelpme）

docker 式的 cli/daemon 结构：`aihelpme` 是薄客户端，`aihelpmed` 是常驻 daemon，队列和结果全部保存在 daemon 内存里，不落盘。

```bash
aihelpme "Go 里怎么原子地交换两个 int？"   # put：提问后阻塞至多 5s，短问题当场得到回答
cat big_doc.txt | aihelpme "总结这份文档"  # 管道内容拼在问题后
aihelpme                                   # get：无输入即取最新回答，未就绪提示稍等
```

**详细文档见 [docs/aihelpme.md](docs/aihelpme.md)**：config.yaml 配置、调试接口（`/list`、`/model`、`/prompt`）、流式模式、slog 日志、systemd 用户服务部署。

## 免费额度 CLI（quota fetch）

查询当前账号有哪些模型有免费额度、还剩多少、何时到期：

```bash
./bin/quota fetch            # 文本表格，按剩余比例升序（最紧张在前）
./bin/quota fetch --json     # JSON 输出
./bin/quota fetch --save     # 保存到 SQLite 库（cron / 看板刷新用）
./bin/quota fetch --ak <ID> --sk <Secret>   # 也可直接传参（不推荐，会进 shell 历史）
./bin/quota history          # 查看最近的抓取记录
```

### 用完即停（freetier）

控制模型的「用完即停」开关：开启后该模型免费额度用尽时推理网关直接返回 403（`AllocationQuota.FreeTierOnly.`），**不会转按量计费**——防止超额调用的第一道闸门。写入为批量异步任务（提交 models → taskId → 自动轮询确认）：

```bash
./bin/quota freetier                       # 查询账户级拦截标志
./bin/quota freetier qwen3.8-flash         # 查询指定模型状态
./bin/quota freetier --enable qwen3.8-max  # 开启（防超额，推荐）
./bin/quota freetier --disable kimi-k3     # 关闭（额度用尽后转按量计费）
```

注意：`--disable` 后免费额度用尽即产生真实费用；开关状态也可在官方控制台「费用用量 → 免费额度」页核对。

文本输出示例：

```text
免费额度：82 个模型有额度（共 365 个模型）
model                               type      remaining  total    %  expires
qwen3.7-flash-2026-07-15            Text          677.2K  1M   67.7  2026-10-23
qwen3.8-max                         Text          999.7K  1M  100.0  2026-11-01
qwen3.8-flash                       Text               1M  1M  100.0  2026-11-25
```

JSON 输出结构（`--json`，与旧版 Python 工具的归档格式完全一致）：

```json
{
  "fetched_at": "2026-10-17T00:29:30+08:00",
  "source": "https://bailian-cs.console.aliyun.com/cli/api.json?action=...",
  "summary": { "totalModels": 365, "withQuota": 82 },
  "models": [
    {
      "model": "qwen3.7-flash-2026-07-15",
      "type": "Text",
      "remaining": 677194.0,
      "total": 1000000.0,
      "remainingPercent": 67.7,
      "expires": "2026-10-23"
    }
  ]
}
```

退出码：`0` 成功；`1` 服务/网络错误或缺少凭证（服务端错误码原样透出）。

### 原理

`internal/bailian` 组合三条接口（对齐官方 [modelstudioai/cli](https://github.com/modelstudioai/cli) 的实现）：

1. 用 AK/SK 做 ACS3-HMAC-SHA256 签名，调用 OpenAPI `GenerateCLIAccessToken` 换取 CLI access token；
2. 匿名调用控制台网关的 `listFoundationModels` 获取模型清单；
3. 带 `Authorization: Bearer <token>` 调用 `queryFreeTierQuota` 获取额度，过滤出 `quotaStatus=VALID` 且额度大于 0 的模型。

更完整的接口调研记录（含网页版 cookie 接口、官方 CLI 源码结论、凭证合规路径）见 `status.md`。

## 额度看板（quota serve）

本地起一个小服务，把 SQLite 里的最新额度快照渲染成可搜索、筛选、排序的网页表格：

```bash
./bin/quota serve            # 默认 http://127.0.0.1:8377
./bin/quota serve --port 9000 --open
```

- 数据源是项目内的 SQLite 库 `data/quota.db`（`--db` / `BAILIAN_DB` 可改，WAL 模式）；
- 首次打开库时，会把旧版 JSON 归档（`~/.local/share/bailian-quota/free-quota-*.json`）一次性迁入，旧文件保留不动；
- 页面右上角的「刷新」按钮抓取一次并写入库，需要凭证（见下节）；
- **可视化面板**（ECharts 5.6，已 vendor 进二进制、离线可用）：各类型剩余额度、到期时间分布、剩余比例分布、快照历史趋势（随 cron 每日抓取自然生长）；前三张图跟随当前筛选视图，标题处显示可视化范围；
- 列表支持搜索、类型筛选、点表头排序与分页（每页 20/50/100 可选），筛选或排序变化自动回到第一页；
- 服务只绑定 `127.0.0.1`，凭证只在服务端进程内使用，不会发送到浏览器；
- HTTP API：`GET /api/quota`（最新快照 JSON）、`GET /api/history`（抓取历史）、`POST /api/refresh`（抓取并入库）；
- 页面顶部显示快照时间并提示是否过期；「即将到期 / 已过期」按本机日期实时计算，统计条可直接点击筛选。

## 准备凭证

需要一对阿里云 AK/SK（AccessKey ID / Secret），建议在 [RAM 控制台](https://ram.console.aliyun.com) 创建**专用 RAM 用户**并挂 `AliyunBailianReadOnlyAccess`，不要使用主账号 AK（官方强烈不建议）。API Key（`sk-`）**不能**用于查询免费额度——控制台网关会拒绝（返回 `BailianGateway.Login.NotLogined`）。

> **RAM 用户两条额外配置**（缺一不可，2026-10-27 实测）：
> 1. `AliyunBailianReadOnlyAccess` **不含**换 CLI token 的内部动作 `GenerateCLIAccessToken`，需另建自定义策略允许 `modelstudio:GenerateCLIAccessToken` 与 `sfm:GenerateCLIAccessToken`（本账号已建 `quota-cli-token`）；
> 2. 在百炼控制台「右上角 管理 → 账号管理 → 添加用户」把该 RAM 用户加入业务空间，并勾选页面权限（至少含「模型用量&免费额度-操作」），否则网关返回 `BailianGateway.Team.NotAuthorised`。

凭证读取顺序：`--ak/--sk` 参数 > 环境变量 `ALIBABA_CLOUD_ACCESS_KEY_ID/SECRET`（及可选 `ALIBABA_CLOUD_SECURITY_TOKEN`）> 凭证文件中的同名键。凭证文件默认 `~/.config/bailian-quota.env`（可用 `--env-file` 或 `BAILIAN_ENV_FILE` 覆盖）：

```bash
mkdir -p ~/.config
cat > ~/.config/bailian-quota.env <<'EOF'
ALIBABA_CLOUD_ACCESS_KEY_ID=LTAI...
ALIBABA_CLOUD_ACCESS_KEY_SECRET=...
EOF
chmod 600 ~/.config/bailian-quota.env
```

## 定时抓取（cron）

```bash
( crontab -l; echo "15 9 * * * cd $HOME/note-space/dashscope-call && ./bin/quota fetch --save" ) | crontab -
```

数据落在 `data/quota.db`（含 `-wal` / `-shm` 两个 WAL 伴生文件），每次抓取新增一份快照，历史可用 `quota history` 或直接 SQL 查询：

```sql
sqlite3 data/quota.db 'SELECT id, fetched_at, with_quota FROM snapshots ORDER BY id DESC LIMIT 5;'
```

旧版 JSON 归档目录 `~/.local/share/bailian-quota/` 仅作为迁移来源，迁移完成后可自行处理。

### 注意事项

- AK/SK 是唯一能无人值守查询免费额度的凭证；浏览器授权（官方 `bl auth login --console` 流程）也可查询，但 token 会过期、需人工重新授权；
- 不要把真实 AK/SK 提交到 Git 仓库（`.gitignore` 已排除 `*.env`）；
- `GenerateCLIAccessToken` 是控制台内部接口（未收录在公开 API 目录），本工具复刻的是官方 CLI 的实现；如需完全文档化的路径，可安装官方 CLI 使用 `bl usage free`（详见 `status.md`）。

## License

本项目遵循 Apache License 2.0。
