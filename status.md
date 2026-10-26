# 百炼免费额度抓取 — 状态

> 更新：2026-10-16

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
