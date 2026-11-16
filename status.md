# 百炼免费额度抓取 — 状态

> 更新：2026-10-16
> 2026-10-27：项目已重写为 Go（`quota fetch` / `quota serve`，见 README），下文的 Python 脚本与 `run_free_quota.sh` 已删除；接口调研结论与测试记录仍然有效。

## 目标

轻量获取"本账号有哪些模型有免费额度"——不装 CLI、不用浏览器 MCP、不用 Playwright。

## 状态标记

- [x] 网页版接口已定位并现场验证（`queryFreeTierQuotaAsyn`，两步 taskId；cookie 认证）
- [x] 纯 HTTP + 页面可见 Cookie 不可行（关键票据 HttpOnly，`BailianGateway.Login.NotLogined`）
- [x] 官方 bl CLI 源码调研完成 → 找到裁减目标：`/cli/api.json` + Bearer token，AK/SK 可换 token
- [x] **裁减版脚本 `free_quota_lite.py` 完成**（纯 Python 标准库、零依赖，约 330 行）
- [x] 关键链路实测：
      - 匿名模型列表接口真实返回（135 个模型，无需登录）
      - 假 AK 的 OpenAPI 签名请求正确到达 ModelStudio 端点（`InvalidAccessKeyId.NotFound`）→ 签名/请求格式被受理
      - 无凭证 / 服务端错误路径均有清晰报错
- [x] **数据链路端到端实测成功（2026-10-17）**：用官方 console-login 流程获取真实 token，脚本取数函数实测通过 —— 365 个模型、82 个有额度，与网页版交叉一致（qwen3.7-flash 677.2K 至 10-23、qwen3.8-27b 至 11-17 等完全吻合）
- [x] **完整链路真实验证通过**（2026-10-17 00:26，exit 0）：365 个模型 / 82 个有额度 → `~/.local/share/bailian-quota/free-quota-2026-10-17.json`（JSON 含 fetched_at/summary 元信息）
- [ ] cron **未挂载**（2026-10-17 按要求撤回，已回滚到只有原 `cleanup.sh` 的状态）。包装脚本 `run_free_quota.sh` 就绪且验证过，手动可跑；需要时一条命令再加回：
      `( crontab -l; echo "15 9 * * * $HOME/note-space/dashscope-call/run_free_quota.sh" ) | crontab -`
- [ ] ⚠️ 凭证文件 `~/.config/bailian-quota.env` **已按要求撤回**（2026-10-17，shred 删除）；无凭证时 `run_free_quota.sh` 按预期 exit 2。账号侧提醒不变：那对主账号 AK（IdentityType=Account，且经过对话记录）建议禁用/轮换
- [ ] 后续启用（手动或 cron）：写入 `~/.config/bailian-quota.env` → `./run_free_quota.sh` 验证 → 需要时加 crontab（建议改用 RAM 用户 AK + `AliyunBailianReadOnlyAccess`，报权限错再补自定义策略）

## 裁减版脚本（首选）

`free_quota_lite.py` —— 只保留一条链路，只回答"哪些模型有免费额度"：

```bash
export ALIBABA_CLOUD_ACCESS_KEY_ID=LTAI...
export ALIBABA_CLOUD_ACCESS_KEY_SECRET=...
python3 free_quota_lite.py          # 文本表格（按剩余比例升序，最紧张在前）
python3 free_quota_lite.py --json   # JSON 输出
```

链路（从官方 bailian-cli v2.3.1 源码逐行裁减，https://github.com/modelstudioai/cli ）：

1. AK/SK → ACS3-HMAC-SHA256 签名 → OpenAPI `GenerateCLIAccessToken`（`modelstudio.cn-beijing.aliyuncs.com/modelstudio/cli/generateAccessToken`，version `2026-02-10`）→ CLI access token
2. 匿名调 `listFoundationModels` 拿全部模型清单（已实测无需登录）
3. `Authorization: Bearer <token>` 调 `/cli/api.json` 的**同步** `queryFreeTierQuota`（传模型名列表，一次调用，无轮询）
4. 过滤 `quotaStatus == "VALID"` 且 `quotaInitTotal > 0`，输出 model/type/剩余/总额/百分比/到期

退出码：0 成功；1 服务/网络错误（原样透出服务端错误码）；2 缺凭证。

测试记录（2026-10-17）：

- 同步接口返回字段为 `{model, quotaTotal, quotaInitTotal, quotaValidityPeriod, quotaStatus}`，**没有** `quotaTotalPercentage`/`freeTierOnly`（前者脚本自行计算；后者需另调 `queryFreeTierOnlyStatus`）；数值可能是 `999668.0` 这类整数浮点，脚本已兼容
- 无 AK/SK 复测数据链路的方法：本地起回调服务，浏览器打开官方授权页
  `https://bailian.console.aliyun.com/console-login?notice=127.0.0.1:<端口>?state=<随机串>`
  点「同意授权 CLI 登录」→ access_token 回传到本地（与官方 `bl auth login --console` 同款流程）

## 备选：官方 bl CLI（功能更全，需要安装）

```bash
npm install -g bailian-cli   # 或 curl -fsSL https://bailian.aliyun.com/cli/install.sh | bash
bl auth login --open-api --access-key-id <AK> --access-key-secret <SK>   # 一次性，非交互
bl usage free --output json
```

优势：token 过期自动用 AK/SK 续期（源码 `refresh-token.ts` + `client.ts` 重试逻辑）；命令覆盖 usage stats 等。

## 兜底：Playwright 脚本（重，但不需要 AK/SK）

`fetch_free_quota.py` —— 自持持久化 Profile（`~/.cache/bailian-quota/profile`）：

```bash
python3 fetch_free_quota.py --login   # 打开浏览器人工登录一次
python3 fetch_free_quota.py           # 之后 headless 抓取
```

## 已验证接口（参考）

CLI 版（裁减脚本采用，Bearer 认证）：

```
POST https://bailian-cs.console.aliyun.com/cli/api.json
  ?action=BroadScopeAspnGateway&product=sfm_bailian&api=<URL编码的api名>
  Authorization: Bearer <cli_access_token>
  body: params=<URL编码JSON> + region=cn-beijing
  api=zeldaEasy.bailian-commerce.freeTrial.queryFreeTierQuota
  Data.queryFreeTierQuotaRequest = { models: ["<模型名>", ...] }
模型列表: zeldaHttp.dashscopeModel./zelda/api/v1/modelCenter/listFoundationModels（分页 pageNo/pageSize，匿名可用）
响应条目: model / quotaTotal(剩余) / quotaInitTotal(总额) / quotaTotalPercentage /
          quotaValidityPeriod(毫秒时间戳) / freeTierOnly / quotaStatus(VALID=有额度, UNKNOWN=无)
```

网页版（cookie 认证，仅作参考）：`/data/api.json` + `queryFreeTierQuotaAsyn`（modelType → taskId → 轮询）。

## 当前额度快照（2026-10-16，来自网页）

- 汇总（页面口径，5 类合计）：有额度模型 81 个；用尽或过期 278 个
- Text 类型：共 112 个，12 个有额度
- 示例：qwen3.8-27b 剩 1M/1M，到期 2026-11-17；qwen3.7-flash-2026-07-15 剩 677K/1M，到期 2026-10-23（最紧）

## 凭证获取：官方文档路径（2026-10-17 调研）

官方 CLI 页面（`https://bailian.console.aliyun.com/cli`，控制台内）明确推荐"方式一：快捷登录"`bl auth login --console`；文档站：`docs.bailian.console.aliyun.com/zh/model-studio/cli/`。

| 途径 | 命令 | 说明 |
|---|---|---|
| 控制台快捷登录（官方推荐） | `bl auth login --console` | 拉起浏览器完成控制台登录授权；本次测试的手工回调即此流程的复刻 |
| 非交互登录（AK/SK） | `bl auth login --open-api --access-key-id <AK> --access-key-secret <SK>` | 保存 AK/SK 并生成 CLI token；适合服务器/CI |
| 临时令牌 | `bl auth generate-access-token --access-key-id ... --access-key-secret ... [--security-token <STS>]` | 官方命令，支持 STS 临时凭证 |
| 验证 | `bl auth status --output json` | masked 输出 |

⚠️ `--api-key` 登录的边界（2026-10-17 源码核实）：usage 命令族（`free`/`freetier`/`stats`/`summary`/`token-plan`/`coding-plan`）**全部声明 `auth: "console"`**，鉴权阶段只认配置文件里的 `access_token`、无 API Key 回退；`--api-key` 登录解锁的是**模型调用类命令**（text chat、生图等，`auth: "apiKey"`）。即：远程 SSH/CI 环境查免费额度应使用 `bl auth login --open-api`（AK/SK，同样免浏览器），而不是 `--api-key`。另：控制台 API Key 创建后不可再查看明文（页面注明不可逆加密存储）。**实测（2026-10-17）**：真实 sk- Key 在模型域（DashScope `/compatible-mode/v1/models` 与业务空间专属域名）均 HTTP 200 有效；作为 Bearer 调控制台网关的免费额度接口返回 `BailianGateway.Login.NotLogined`——网关层同样拒绝 API Key，与源码结论一致。

AK/SK 的合规获取（阿里云 RAM 文档）：

- 为程序化访问创建 **RAM 用户**，在 RAM 控制台 → 用户 → 凭证管理 → 创建 AccessKey；官方"强烈不建议创建和使用主账号 AccessKey"
- 安全实践：Secret 仅创建时显示一次；每账号最多 2 个 AK 用于轮转；可设闲置自动禁用（如 90 天）；支持 AccessKey 网络访问限制；官方建议优先用 **STS 临时凭证**（lite 脚本已支持 `ALIBABA_CLOUD_SECURITY_TOKEN`）
- 百炼侧授权：系统策略 `AliyunBailianReadOnlyAccess`（查额度够用）或自定义策略最小授权；RamCode = `modelstudio`，自定义 Action 前缀 `sfm:`
- 文档：`help.aliyun.com/zh/ram/user-guide/create-an-accesskey-pair`、`alibabacloud.com/help/zh/model-studio/bailian-ram-permission`

⚠️ 合规发现：`GenerateCLIAccessToken`（AK/SK 换 CLI token 的底层 OpenAPI）**未收录在公开 API 目录**（api.aliyun.com 元数据 `ModelStudio/2026-02-10` 版本 38 个 API 中无此动作）——属内部接口，官方仅通过 CLI 命令暴露。lite 脚本对它（及 `/cli/api.json` 网关）的调用 = 复刻官方 CLI 的实现细节、不绕过任何鉴权；如需严格"文档化路径"，凭证应通过上表的官方 CLI 命令获取。

## 相关与备用

- 裁减依据：官方 modelstudioai/cli v2.3.1 源码（https://github.com/modelstudioai/cli ，本地副本已删除）
- 官方文档：https://help.aliyun.com/zh/model-studio/cli/installation
- 用完即停状态：`...freeTrial.queryFreeTierInterceptStatus` / CLI `bl usage freetier`
- 余额/优惠券：`https://bailian.console.aliyun.com/data/v2/multiApi.json?_fetcher_=BssOpenAPI-V3__...QueryFreetierInstanceInfo`

## 页面状态

- IAB 标签页保留且已登录，免费额度表格在第 1 页

## RAM 用户路径实测（2026-10-27）

主账号 AK 换成专用 RAM 用户 `quota-reader` 后，链路需要三块拼图（缺一不可）：

1. **系统策略** `AliyunBailianReadOnlyAccess` —— 覆盖百炼业务读操作，但**不含** `GenerateCLIAccessToken`；
2. **自定义策略** `quota-cli-token` —— 仅允许 `modelstudio:GenerateCLIAccessToken` + `sfm:GenerateCLIAccessToken`（两个命名空间都写，RAM 自行匹配）；缺它时 OpenAPI 返回 `NoPermission`；
3. **百炼业务空间成员** —— 百炼控制台「管理 → 账号管理 → 添加用户」把 RAM 用户加入业务空间，页面权限勾「模型用量&免费额度-操作」（本次直接全选 38 项）；缺它时网关返回 `BailianGateway.Team.NotAuthorised`（token 能换到，但网关拒绝）。

**端到端真实验证通过**：Go 工具（ACS3 签名 → GenerateCLIAccessToken → listFoundationModels → queryFreeTierQuota → SQLite）取回 370 个模型、79 个有额度，快照 #2 入库，看板显示「刚刚更新」。与 10 天前主账号快照（82 个有额度）交叉一致：差额来自期间到期的额度。

## 与官方控制台页面交叉验证（2026-11-14）

官方「费用用量 → 免费额度」页面显示有额度模型 **72** 个，lite/Go 工具同期抓到 **74** 个。全量翻页比对（72 行逐页收集）+ 官方页内搜索证实，多出的 2 个是：

- `qwen-audio-3.1-realtime-flash`（1M/1M，2027-01-26 到期）
- `qwen-audio-3.1-realtime-plus`（1M/1M，2026-12-18 到期）

二者在 API 里 quotaStatus=VALID、额度真实存在，但官方页面**搜不到**（返回"暂无数据"）。原因：网页版接口按 modelType（Text/Vision/Audio/Embedding/Multimodal）分类查询，这两个 realtime 模型的能力标签无法归入任何类型（在工具数据里 type 同样是 "-"），官方页面按类型遍历时把它们漏掉了。

结论：**按模型名逐个查额度的工具链路比官方页面更全**；官方数字可作为下界交叉验证。同日其余 72 个模型与官方页面逐一吻合。

## 「用完即停」实现机制（2026-11-14 页面现场捕获）

官方页 notice：开启后免费额度用尽即停止服务（403 错误码 `AllocationQuota.FreeTierOnly.`），不转计费；关闭则用尽后按量付费。免费额度为中国内地所有业务空间共享。

**查询**：网页版用 `queryFreeTierQuotaAsyn`（cookie 认证）按 modelType 五类各发一次，请求带
`{modelType, needOverviewInfo: true, needFreeTierOnlyStatus: true, returnUnissuedModels: false}`，
再按 taskId 轮询取结果 —— 开关状态随额度条目内联返回，无需单独查询。
（CLI 侧另有同步接口 `queryFreeTierInterceptStatus`，见 `bl usage freetier`。）

**写入**（浏览器 fetch/XHR 钩子现场捕获，qwen3.8-flash 开→关→开）：
- 开启：`zeldaEasy.bailian-commerce.freeTrial.batchActivateFreeTierOnly`
  body: `{"BatchActivateFreeTierOnlyRequest":{"models":["qwen3.8-flash"]}}` → taskId → 轮询
- 关闭：`zeldaEasy.bailian-commerce.freeTrial.batchDeactivateFreeTierOnly`
  body: `{"BatchDeactivateFreeTierOnlyRequest":{"models":["qwen3.8-flash"]}}` → taskId → 轮询
- 均为批量异步任务模式（页面「批量设置」多选同理）；关闭方向强制弹确认框
  「关闭后，您将为免费额度用尽后产生的用量付费」。

**生效层**：开关存 commerce 侧，推理网关在计量时检查 —— 开启且 `quotaTotal=0` 时直接拒绝
（403 `AllocationQuota.FreeTierOnly.`），不透传到计费。客户端/SDK 无感知，仅靠错误码区分。

**对照**：我们的同步查询（`queryFreeTierQuota`，CLI 链路）响应不含 freeTierOnly 字段；
看板如需展示该列，可调 `queryFreeTierInterceptStatus` 或改用带 `needFreeTierOnlyStatus` 的查询。

## 测试台对齐官方 SDK 文档：7 类模型调用形状（2026-11-17）

`install-sdk` 页「后续步骤」给出 7 类模型文档入口（文本/图像/视频/语音合成/语音识别/向量/排序），
测试台按此补齐为 7 个标签页。逐页核对结论：

- **文本**：OpenAI 兼容 `/compatible-mode/v1/chat/completions`，temperature/top_p/max_tokens/
  enable_thinking 等官方参数直接放请求体顶层（前端透传）。
- **图像**：qwen-image 2.x 为**同步** `/api/v1/services/aigc/multimodal-generation/generation`，
  图片在 `output.choices[0].message.content[].image`；parameters：size/negative_prompt/watermark/prompt_extend/seed。
- **视频**：wan3.0 **异步** `/api/v1/services/aigc/video-generation/video-synthesis`（X-DashScope-Async: enable）+
  轮询 `/api/v1/tasks/{id}`；parameters：resolution/ratio/duration/audio/prompt_extend/watermark/seed。
- **语音识别**：qwen3-asr 走 chat/completions，`input_audio.data` 必须是**完整 Data URI**（无 format 字段）；
  `asr_options: {language, enable_itn}` 顶层传递，context 需 System Message。
- **向量**：`/compatible-mode/v1/embeddings`，`dimensions` 放顶层（flash 系列 1024/768/512/256）；
  text_type/instruct 仅 DashScope 原生接口支持。
- **语音合成（新增标签页）**：CosyVoice 官方 SDK 走 WebSocket（`wss://…/api-ws/v1/inference`），
  但存在 **HTTP 非流式接口**：`POST /api/v1/services/audio/tts/SpeechSynthesizer`，
  body `{model, input:{text, voice, format, sample_rate, instruction?}}` → `output.audio.url`。
  支持 qwen-audio-3.x-tts / cosyvoice-v2/v3(.5) / qwen3-tts-flash。音色需与模型匹配
  （qwen3-tts 用英文名 Cherry/Ethan…，cosyvoice 用 longxiaochun_v2 等）。
- **排序（新增标签页）**：DashScope 原生 `POST /api/v1/services/rerank/text-rerank/text-rerank`，
  body `{model, input:{query, documents}, parameters:{top_n, return_documents, instruct}}` →
  `output.results[]` 按 relevance_score 降序（每项 index/relevance_score/document）。
  另有 OpenAI 兼容扁平版 `/compatible-api/v1/reranks`。qwen3.7-text-rerank / qwen3-rerank 在列，
  gte-rerank-v2 将于 2026-05-30 弃用。

**Base URL**：经典域名 `dashscope.aliyuncs.com`（compatible-mode/v1 与 /api/v1）仍是文档主推；
`{WorkspaceId}.{region}.maas.aliyuncs.com` 为可选的生产环境业务空间级域名，仅限对应业务空间的 API Key。

实现：`internal/dashscope/client.go` 新增 `SynthesizeSpeech`/`Rerank`；`/api/infer/tts`、`/api/infer/rerank`
两个服务端代理路由；Playground 7 标签页 + 各自高级参数；空输入守卫与音色-模型联动。

**补充（同日实测）**：qwen3-tts-flash 不走 SpeechSynthesizer 端点（报 InvalidParameter: url error），
它用 `POST /api/v1/services/aigc/multimodal-generation/generation`，input 仅接受
text/voice/language_type（无 format/sample_rate，固定输出 24kHz wav，URL 24h 有效）。
`SynthesizeSpeech` 已按模型家族路由；qwen-audio-3.1-tts-flash、cosyvoice-v3-flash、qwen3-tts-flash
三族均真机验证返回音频 URL。

## serve HTTP 层迁移到 Gin（2026-11-17）

按用户要求把 `quota serve` 的 API 层从 net/http ServeMux 迁到 gin-gonic/gin（注意：aihelpme
守护进程本身仍是 net/http，"类似"指的是其代码组织风格——struct 方法 handler、统一 writeJSON、
请求日志中间件）。要点：

- `server.router()`：ReleaseMode + slog 请求日志中间件（入口 debug / 出口 info，含 status 与
  duration_ms，与 aihelpme 的 logRequests 同风格）+ gin.Recovery()；页面与静态资源直挂，
  /api 分组，/api/infer 子分组五个推理代理；NoRoute 返回 JSON 404（与原行为一致）。
- handler 全部迁移 `*gin.Context`；`bindJSON` 保留 16MB MaxBytesReader 限制。
- JSON 线格式刻意保持不变（缩进 + SetEscapeHTML(false)，不经 gin 默认 render），前端零改动。
- 冒烟全绿：quota/history/404/坏 JSON 友好提示/rerank 真机调用/playground HTML；go test ./... 通过。

坑（再次）：pkill 与启动命令合并在一条 shell 里时，启动命令文本 `./bin/quota serve` 会被
pkill 的 `-f "bin/quota [s]erve"` 模式命中，把自己整条命令杀掉——kill 与 start 必须分两次调用。

## 框架化瘦身四件套（2026-11-17）

按讨论结论落地 1+2+3+4：

1. **TanStack Query（前端）**：main.tsx 挂 QueryClientProvider；Dashboard 三个数据源换
   useQuery/useMutation（quota/history + 刷新 mutate → setQueryData/invalidateQueries），
   Playground 七个 Tab 统一走新 hook `useInfer`（useMutation 包装，ok:false 归一成 Error，
   校验失败 fail() 注入同一信封）。删掉 ~15 组手写 loading/out 状态与 .then/.finally 链。
2. **Air 热重载**：`.air.toml`（build→bin/quota，full_bin 带 serve 参数，排除 web-ui/data/tmp），
   `~/go/bin/air -c .air.toml` 接管 serve 进程，真机验证两次改动触发两次重建、服务不断。
   注意 touch 只改 mtime 不触发 fsnotify，需真实内容改动。
3. **gin binding tag**：embeddings/imagegen/tts/rerank 请求体加 `binding:"required"`（集合加
   min=1），删除 5 处手写空值检查；校验错误经 bindJSON 统一返回信封。
4. **openai-go/v3**：ChatCompletion 改走官方 SDK 低层 `Client.Post`（body 原样透传，保留
   asr_options 等 DashScope 扩展）；Embeddings 改用类型化 EmbeddingNewParams（OfArrayOfStrings
   + Dimensions param.Opt）。错误经 convertErr 映射回 *APIError，inferErr/IsFreeTierExhausted
   无感。图像/视频/TTS/rerank 为 DashScope 原生接口，保持手写 do()。

冒烟全绿：chat（SDK 透传，reasoning_content 正常）、embeddings（dimensions=256 生效）、
rerank（原生路径）、binding 校验（空 model 拒绝）、浏览器看板/测试台；go test 通过。

## 视频生成升级为 SSE 进度推送（2026-11-17）

把「提交后同步挂起轮询 ≤5 分钟」改为「提交立即返回 + SSE 推送轮询状态」：

- **POST /api/infer/imagegen**（kind=video）：创建任务后立即返回 `{taskId}`（实测 220ms）；
  图像（qwen-image 同步接口）路径不变。
- **GET /api/infer/tasks/{id}/events**（新增）：SSE 流式推送任务状态。服务端仍查官方
  GET /tasks/{id}（限流 20QPS），但轮询间隔 2s 起指数退避至 15s；每个状态变化发
  `data:{status, urls, message, elapsed}`，终态（SUCCEEDED/FAILED/CANCELED/UNKNOWN/TIMEOUT
  ≤10min）后结束流。客户端断开（r.Context().Done）即停止轮询。
- **前端 VisionTab**：提交后渲染 Steps（已提交→排队中→生成中→完成）+ 进度条（按已耗时/240s
  估算，完成 100%）+ 状态 Tag + 已等待秒数 + taskId；终态渲染视频/错误。`subscribeTask`
  （EventSource）在 api.ts；终态/断开时 close 防自动重连；重新订阅已完成任务会立即收到
  一次终态事件（幂等）。
- **旧版 playground.html**：视频路径同步改造为 EventSource + 状态行文本。

**真机端到端**：wan3.0-video-prime 5s/1080P 视频提交 → RUNNING×4（间隔 2.1/4.1/8.1/15.1s，
退避生效）→ 52 秒 SUCCEEDED，mp4 URL 直接推送。bogus taskId → UNKNOWN 终态事件。
注意：看板额度数字来自快照，不实时反映这次消耗（wan3.0 快照 30 条，实际已用 1）。

epoll vs 轮询的结论（讨论记录）：epoll 只解决「本地 fd 就绪」，远端业务事件必须有对端推送通道
（WebSocket/SSE/EventBridge 回调）才可感知；百炼视频任务对本机工具唯一可行通道是轮询，
EventBridge 回调需公网可达 URL（AsyncTaskFinish 事件），规模化后可升级为集中轮询器 + SSE。

## 旧版 HTML 页面退役，React SPA 单二进制托管（2026-11-17）

internal/app/static 的手写 dashboard.html / playground.html / echarts.min.js（共 ~1000 行
HTML+CSS+JS）全部删除，功能已由 web-ui React 应用完整承接。`quota serve` 现在直接托管 React：

- **webui.go**（模块根包）：`//go:embed all:web-ui/dist` + `Dist()` 返回 fs.FS；
  embed 无法跨目录上溯，所以放在模块根。dist 不入库（.gitignore），构建顺序由
  Makefile 保证：`build` 依赖 `web-ui/dist/index.html` 文件规则，fresh clone 首次
  `make build` 自动 npm install + 前端构建；`make web` 强制重建前端。
- **serve.go**：删除 handlePage/handlePlayground/handleEcharts 与 /static/echarts.min.js
  路由；NoRoute 分流——/api 前缀或非 GET 保持 JSON 404，其余走 spa()：
  dist 中存在的文件经 FileFromFS 直出，缺失路径回退 index.html（react-router 历史路由）。
- 开发循环不变：Vite dev（5173，HMR）+ air（8377 Go 热重载）；改 React 后入库需
  `make web && make build` 让 embed 拿到新 dist。
- 验证：/ 与 /playground 返回 React 页面、/assets/*.js 200（2.3MB）、/favicon.svg 200、
  /api/nope JSON 404、/nonexistent SPA 回退；浏览器在 8377 生产模式渲染看板（快照 #23）
  与测试台（7 标签页），SPA 内部导航正常。README/Makefile 已同步。

## 测试台模型下拉改为「本类优先 + 全量可选」（2026-11-17）

用户指出测试台可选拼起来远不到 73 个——原 PICK 过滤太苛刻（vision/multimodal/audio 都是写死
名单，只露出 ~19 个）。快照类型分布：Audio 55（ASR/TTS/realtime/translate 混在一起）、
Text 7、Vision 4、Multimodal 3、Embedding 2、无类型 realtime 2。

- PICK 改为按快照类型：text=Text、vision=Vision、multimodal=Multimodal、
  audio=Audio∧/asr/i、tts=/tts|cosyvoice/i、embedding=Embedding∧¬rerank、rerank=/rerank/i。
- ModelSelect 改分组下拉：本类模型（N）+ 其他有额度模型（M），每页 N+M=73 全部可选；
  showSearch 按模型名过滤；默认选中 FALLBACK 推荐型号（排最前），其余按名称排序。
- 实测：文本 7+66、语音识别 4+69，分组与搜索正常。

**矫正（同日）**：「其他有额度模型」分组不合理——排序页里能选 TTS 模型是错误设计。已回归
严格类型过滤：每个 tab 只出现功能匹配的模型（asr 4 / tts 3 / embedding 1 / rerank 1 /
text 7 / vision 4 / multimodal 3 = 23 个），保留按名搜索。其余 50 个不可测试的原因：
realtime WebSocket 族（qwen-audio-realtime、omni-realtime、paraformer-realtime、
livetranslate-realtime 等约 45 个）与 paraformer 录音文件识别 REST 的调用协议
测试台尚未实现——它们本来就不属于这 7 个 REST 形态的标签页。
