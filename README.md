# DashScope / 百炼工具集

Go 实现的工具集：**百炼免费额度 CLI 与本地看板**，以及通过 `curl` 测试 DashScope / Model Studio API 的参考文档。

免费额度部分（`quota fetch` / `quota serve`）从官方 bailian-cli 源码裁减而来，用 AK/SK 查询账户级免费额度，详见[免费额度 CLI](#免费额度-cliquota-fetch)。

## 目录

- [目录结构](#目录结构)
- [构建与测试](#构建与测试)
- [免费额度 CLI（quota fetch）](#免费额度-cliquota-fetch)
- [额度看板（quota serve）](#额度看板quota-serve)
- [准备凭证](#准备凭证)
- [定时抓取（cron）](#定时抓取cron)
- [环境要求](#环境要求)
- [配置环境变量](#配置环境变量)
- [Endpoint 配置](#endpoint-配置)
- [查询模型列表](#查询模型列表)
- [查询指定模型](#查询指定模型)
- [过滤 Chat Model](#过滤-chat-model)
- [分页查询全部模型](#分页查询全部模型)
- [测试 Chat Model 是否可调用](#测试-chat-model-是否可调用)
- [查看单次请求用量](#查看单次请求用量)
- [完整模型可用性检查脚本](#完整模型可用性检查脚本)
- [使用原生 DashScope Generation API](#使用原生-dashscope-generation-api)
- [Workspace 配置](#workspace-配置)
- [HTTP 状态码](#http-状态码)
- [关于额度查询](#关于额度查询)
- [相关 Python SDK API](#相关-python-sdk-api)
- [License](#license)

## 目录结构

```text
dashscope-call/
├── cmd/quota/            # 入口：quota fetch / serve / history
├── internal/
│   ├── bailian/          # ACS3 签名、CLI token、控制台网关、额度快照
│   ├── store/            # SQLite 存储（snapshots + model_quotas 两表）
│   ├── format/           # K/M 数字与到期日格式化
│   └── app/              # cobra 命令、HTTP 看板服务、内嵌页面
├── data/                 # SQLite 库（运行时生成，已 gitignore）
├── ROUTE.md              # DashScope API 路由调研
├── status.md             # 免费额度接口调研记录（含网页版 cookie 路线）
└── Makefile
```

## 构建与测试

需要 Go 1.24+（模块代理不通时可设 `HTTPS_PROXY=socks5://127.0.0.1:10808`）：

```bash
make build     # 构建 bin/quota
make test      # 单元测试
make vet fmt   # 静态检查与格式化
```

## 免费额度 CLI（quota fetch）

查询当前账号有哪些模型有免费额度、还剩多少、何时到期：

```bash
./bin/quota fetch            # 文本表格，按剩余比例升序（最紧张在前）
./bin/quota fetch --json     # JSON 输出
./bin/quota fetch --save     # 保存到 SQLite 库（cron / 看板刷新用）
./bin/quota fetch --ak <ID> --sk <Secret>   # 也可直接传参（不推荐，会进 shell 历史）
./bin/quota history          # 查看最近的抓取记录
```

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

## 环境要求

`quota` 工具构建时需要 [Go](https://go.dev)；以下 curl 文档章节需要：

- `curl`
- `jq`

检查是否已安装：

```bash
curl --version
jq --version
```

Ubuntu / Debian：

```bash
sudo apt-get update
sudo apt-get install -y curl jq
```

macOS：

```bash
brew install curl jq
```

## 配置环境变量

```bash
export DASHSCOPE_API_KEY="sk-你的API-Key"
export DASHSCOPE_WORKSPACE="你的Workspace-ID"
export MODEL="qwen3.8-max"
```

检查变量是否已设置：

```bash
test -n "${DASHSCOPE_API_KEY}" && echo "DASHSCOPE_API_KEY is set"
test -n "${DASHSCOPE_WORKSPACE}" && echo "DASHSCOPE_WORKSPACE is set"
test -n "${MODEL}" && echo "MODEL=${MODEL}"
```

不要把真实 API Key 提交到 Git 仓库。

## Endpoint 配置

### 北京公共 Endpoint

```bash
export DASHSCOPE_API_URL="https://dashscope.aliyuncs.com/api/v1"
export DASHSCOPE_COMPATIBLE_URL="https://dashscope.aliyuncs.com/compatible-mode/v1"
```

### 新加坡 Workspace Endpoint

如果 Workspace 位于新加坡，可以使用：

```bash
export DASHSCOPE_REGION="ap-southeast-1"

export DASHSCOPE_API_URL="https://${DASHSCOPE_WORKSPACE}.${DASHSCOPE_REGION}.maas.aliyuncs.com/api/v1"

export DASHSCOPE_COMPATIBLE_URL="https://${DASHSCOPE_WORKSPACE}.${DASHSCOPE_REGION}.maas.aliyuncs.com/compatible-mode/v1"
```

其他区域示例：

```text
cn-beijing
cn-hongkong
ap-southeast-1
ap-northeast-1
eu-central-1
us-east-1
```

区域 Endpoint、Workspace 和 API Key 必须匹配。

## 查询模型列表

查询第一页，每页 100 个模型：

```bash
curl -sS -G "${DASHSCOPE_API_URL}/models" \
  -H "Authorization: Bearer ${DASHSCOPE_API_KEY}" \
  -H "X-DashScope-WorkSpace: ${DASHSCOPE_WORKSPACE}" \
  --data-urlencode "page_no=1" \
  --data-urlencode "page_size=100"
```

只输出 `model` 和 `name`：

```bash
curl -sS -G "${DASHSCOPE_API_URL}/models" \
  -H "Authorization: Bearer ${DASHSCOPE_API_KEY}" \
  -H "X-DashScope-WorkSpace: ${DASHSCOPE_WORKSPACE}" \
  --data-urlencode "page_no=1" \
  --data-urlencode "page_size=100" \
| jq -r '
    .output.models[]?
    | [.model, .name]
    | @tsv
'
```

输出示例：

```text
qwen3.8-max        Qwen3.8-Max
qwen3.8-flash      Qwen3.8-Flash
qwen3.7-plus       Qwen3.7-Plus
```

查看分页信息：

```bash
curl -sS -G "${DASHSCOPE_API_URL}/models" \
  -H "Authorization: Bearer ${DASHSCOPE_API_KEY}" \
  -H "X-DashScope-WorkSpace: ${DASHSCOPE_WORKSPACE}" \
  --data-urlencode "page_no=1" \
  --data-urlencode "page_size=100" \
| jq '.output | {
    total,
    page_no,
    page_size
  }'
```

模型查询接口：

```text
GET /api/v1/models
```

请求参数：

| 参数 | 说明 |
|---|---|
| `page_no` | 页码 |
| `page_size` | 每页数量 |
| `model` | 按模型 ID 查询 |

## 查询指定模型

例如查询 `qwen3.8-max`：

```bash
export MODEL="qwen3.8-max"

curl -sS -G "${DASHSCOPE_API_URL}/models" \
  -H "Authorization: Bearer ${DASHSCOPE_API_KEY}" \
  -H "X-DashScope-WorkSpace: ${DASHSCOPE_WORKSPACE}" \
  --data-urlencode "model=${MODEL}" \
  --data-urlencode "page_no=1" \
  --data-urlencode "page_size=1" \
| jq '{
    success,
    code,
    message,
    model: (.output.models[0].model // null),
    name: (.output.models[0].name // null)
  }'
```

等价请求：

```text
GET /api/v1/models?model=qwen3.8-max&page_no=1&page_size=1
```

模型信息查询成功，只能说明当前 API 能查询到该模型，不一定代表该 Workspace 有权限调用该模型。

## 过滤 Chat Model

模型列表接口不一定提供统一的 `model_type=chatmodel` 服务端过滤参数。建议先请求模型列表，再通过 `jq` 过滤。

### 如果返回结果有 `model_type`

先查看一个完整的模型对象：

```bash
curl -sS -G "${DASHSCOPE_API_URL}/models" \
  -H "Authorization: Bearer ${DASHSCOPE_API_KEY}" \
  -H "X-DashScope-WorkSpace: ${DASHSCOPE_WORKSPACE}" \
  --data-urlencode "page_no=1" \
  --data-urlencode "page_size=1" \
| jq '.output.models[0]'
```

如果返回字段中存在：

```json
{
  "model": "qwen3.8-max",
  "name": "Qwen3.8-Max",
  "model_type": "chatmodel"
}
```

可以使用：

```bash
curl -sS -G "${DASHSCOPE_API_URL}/models" \
  -H "Authorization: Bearer ${DASHSCOPE_API_KEY}" \
  -H "X-DashScope-WorkSpace: ${DASHSCOPE_WORKSPACE}" \
  --data-urlencode "page_no=1" \
  --data-urlencode "page_size=100" \
| jq -r '
    .output.models[]?
    | select(.model_type == "chatmodel")
    | [.model, .name]
    | @tsv
'
```

如果字段名称是 `type`，改为：

```bash
jq -r '
  .output.models[]?
  | select(.type == "chatmodel")
  | [.model, .name]
  | @tsv
'
```

### 根据模型 ID 过滤

如果模型对象只有 `model` 和 `name`，可以排除明显的非 Chat 模型：

```bash
curl -sS -G "${DASHSCOPE_API_URL}/models" \
  -H "Authorization: Bearer ${DASHSCOPE_API_KEY}" \
  -H "X-DashScope-WorkSpace: ${DASHSCOPE_WORKSPACE}" \
  --data-urlencode "page_no=1" \
  --data-urlencode "page_size=100" \
| jq -r '
    .output.models[]?
    | select(
        (.model | test(
          "image|video|audio|tts|asr|embedding|rerank|realtime|speech";
          "i"
        ) | not)
      )
    | [.model, .name]
    | @tsv
'
```

也可以只保留常见聊天模型系列：

```bash
curl -sS -G "${DASHSCOPE_API_URL}/models" \
  -H "Authorization: Bearer ${DASHSCOPE_API_KEY}" \
  -H "X-DashScope-WorkSpace: ${DASHSCOPE_WORKSPACE}" \
  --data-urlencode "page_no=1" \
  --data-urlencode "page_size=100" \
| jq -r '
    .output.models[]?
    | select(
        (.model | test("^(qwen|qwq|qvq|deepseek)"))
        and
        (.model | test(
          "image|video|audio|tts|asr|embedding|rerank|realtime";
          "i"
        ) | not)
      )
    | [.model, .name]
    | @tsv
'
```

> 以上基于模型 ID 的过滤是名称规则匹配，不是服务端返回的正式模型类型判断。最终是否支持 Chat API，应通过实际请求验证。

## 分页查询全部模型

如果返回结果中的 `total` 大于 `page_size`，需要查询多个页面。

```bash
#!/usr/bin/env bash

set -euo pipefail

: "${DASHSCOPE_API_KEY:?请设置 DASHSCOPE_API_KEY}"
: "${DASHSCOPE_WORKSPACE:?请设置 DASHSCOPE_WORKSPACE}"
: "${DASHSCOPE_API_URL:?请设置 DASHSCOPE_API_URL}"

page_size=100

first_page="$(
  curl -fsSG "${DASHSCOPE_API_URL}/models" \
    -H "Authorization: Bearer ${DASHSCOPE_API_KEY}" \
    -H "X-DashScope-WorkSpace: ${DASHSCOPE_WORKSPACE}" \
    --data-urlencode "page_no=1" \
    --data-urlencode "page_size=${page_size}"
)"

total="$(jq -r '.output.total // 0' <<< "${first_page}")"
pages=$(( (total + page_size - 1) / page_size ))

for page in $(seq 1 "${pages}"); do
  if [[ "${page}" -eq 1 ]]; then
    response="${first_page}"
  else
    response="$(
      curl -fsSG "${DASHSCOPE_API_URL}/models" \
        -H "Authorization: Bearer ${DASHSCOPE_API_KEY}" \
        -H "X-DashScope-WorkSpace: ${DASHSCOPE_WORKSPACE}" \
        --data-urlencode "page_no=${page}" \
        --data-urlencode "page_size=${page_size}"
    )"
  fi

  jq -r '
    .output.models[]?
    | [.model, .name]
    | @tsv
  ' <<< "${response}"
done
```

只获取所有模型的 JSON：

```bash
#!/usr/bin/env bash

set -euo pipefail

page_size=100
total="$(
  curl -fsSG "${DASHSCOPE_API_URL}/models" \
    -H "Authorization: Bearer ${DASHSCOPE_API_KEY}" \
    -H "X-DashScope-WorkSpace: ${DASHSCOPE_WORKSPACE}" \
    --data-urlencode "page_no=1" \
    --data-urlencode "page_size=${page_size}" \
  | jq -r '.output.total // 0'
)"

pages=$(( (total + page_size - 1) / page_size ))

for page in $(seq 1 "${pages}"); do
  curl -fsSG "${DASHSCOPE_API_URL}/models" \
    -H "Authorization: Bearer ${DASHSCOPE_API_KEY}" \
    -H "X-DashScope-WorkSpace: ${DASHSCOPE_WORKSPACE}" \
    --data-urlencode "page_no=${page}" \
    --data-urlencode "page_size=${page_size}"
done \
| jq -s '
    map(.output.models // [])
    | add
    | map({model, name})
'
```

## 测试 Chat Model 是否可调用

模型列表查询成功并不代表模型一定可以调用。验证 Chat Model 最可靠的方式是实际发起一次最小请求。

> 该请求是真实调用，可能消耗免费额度或产生费用。使用 `max_tokens: 1` 可以减少消耗，但不能保证完全不计费。

```bash
export MODEL="qwen3.8-max"

curl -sS \
  "${DASHSCOPE_COMPATIBLE_URL}/chat/completions" \
  -H "Authorization: Bearer ${DASHSCOPE_API_KEY}" \
  -H "X-DashScope-WorkSpace: ${DASHSCOPE_WORKSPACE}" \
  -H "Content-Type: application/json" \
  -d "$(jq -n \
    --arg model "${MODEL}" \
    '{
      model: $model,
      messages: [
        {
          role: "user",
          content: "请只回复 OK"
        }
      ],
      max_tokens: 1,
      stream: false
    }'
  )" \
| jq '{
    id,
    model,
    choices,
    usage,
    error,
    request_id
  }'
```

成功响应通常包含：

```json
{
  "id": "xxx",
  "model": "qwen3.8-max",
  "choices": [
    {
      "message": {
        "role": "assistant",
        "content": "OK"
      }
    }
  ],
  "usage": {
    "prompt_tokens": 10,
    "completion_tokens": 1,
    "total_tokens": 11
  }
}
```

## 查看单次请求用量

Chat Completion 返回的 `usage` 是当前请求的用量：

```bash
curl -sS \
  "${DASHSCOPE_COMPATIBLE_URL}/chat/completions" \
  -H "Authorization: Bearer ${DASHSCOPE_API_KEY}" \
  -H "X-DashScope-WorkSpace: ${DASHSCOPE_WORKSPACE}" \
  -H "Content-Type: application/json" \
  -d "$(jq -n \
    --arg model "${MODEL}" \
    '{
      model: $model,
      messages: [
        {
          role: "user",
          content: "OK"
        }
      ],
      max_tokens: 1,
      stream: false
    }'
  )" \
| jq '.usage'
```

典型字段：

```json
{
  "prompt_tokens": 10,
  "completion_tokens": 1,
  "total_tokens": 11
}
```

`usage` 表示本次请求消耗，不表示账户剩余额度。

## 完整模型可用性检查脚本

保存为 `check-model.sh`：

```bash
#!/usr/bin/env bash

set -u

: "${DASHSCOPE_API_KEY:?请设置 DASHSCOPE_API_KEY}"
: "${DASHSCOPE_WORKSPACE:?请设置 DASHSCOPE_WORKSPACE}"
: "${DASHSCOPE_COMPATIBLE_URL:?请设置 DASHSCOPE_COMPATIBLE_URL}"

MODEL="${1:?用法: $0 <model>}"
URL="${DASHSCOPE_COMPATIBLE_URL}/chat/completions"

response="$(
  curl -sS \
    -w $'\n%{http_code}' \
    "${URL}" \
    -H "Authorization: Bearer ${DASHSCOPE_API_KEY}" \
    -H "X-DashScope-WorkSpace: ${DASHSCOPE_WORKSPACE}" \
    -H "Content-Type: application/json" \
    -d "$(jq -n \
      --arg model "${MODEL}" \
      '{
        model: $model,
        messages: [
          {
            role: "user",
            content: "OK"
          }
        ],
        max_tokens: 1,
        stream: false
      }'
    )"
)"

http_code="$(tail -n 1 <<< "${response}")"
body="$(sed '$d' <<< "${response}")"

echo "HTTP status: ${http_code}"

if [[ "${http_code}" == "200" ]]; then
  echo "模型可以调用: ${MODEL}"

  jq '{
    model,
    usage,
    text: (.choices[0].message.content // null)
  }' <<< "${body}"
else
  echo "模型调用失败: ${MODEL}"

  jq '{
    code: (.error.code // .code // null),
    message: (.error.message // .message // null),
    request_id: (.request_id // null)
  }' <<< "${body}"
fi
```

运行：

```bash
chmod +x check-model.sh
./check-model.sh qwen3.8-max
```

## 使用原生 DashScope Generation API

除 OpenAI-compatible API 外，也可以使用 DashScope 原生 Generation API：

```bash
curl -sS \
  "${DASHSCOPE_API_URL}/services/aigc/text-generation/generation" \
  -H "Authorization: Bearer ${DASHSCOPE_API_KEY}" \
  -H "X-DashScope-WorkSpace: ${DASHSCOPE_WORKSPACE}" \
  -H "Content-Type: application/json" \
  -H "X-DashScope-SSE: disable" \
  -d "$(jq -n \
    --arg model "${MODEL}" \
    '{
      model: $model,
      input: {
        messages: [
          {
            role: "user",
            content: "请只回复 OK"
          }
        ]
      },
      parameters: {
        result_format: "message",
        max_tokens: 1
      }
    }'
  )" \
| jq '{
    output,
    usage,
    code,
    message,
    request_id
  }'
```

原生 Generation API 路径：

```text
POST /api/v1/services/aigc/text-generation/generation
```

OpenAI-compatible Chat Completion 路径：

```text
POST /compatible-mode/v1/chat/completions
```

## Workspace 配置

对于公共北京 Endpoint，一般使用：

```bash
-H "X-DashScope-WorkSpace: ${DASHSCOPE_WORKSPACE}"
```

对于 Workspace 专属 MaaS Endpoint，Workspace ID 位于域名中：

```text
https://<workspace-id>.<region>.maas.aliyuncs.com/api/v1
https://<workspace-id>.<region>.maas.aliyuncs.com/compatible-mode/v1
```

例如：

```bash
export DASHSCOPE_WORKSPACE="ws-abc123"
export DASHSCOPE_REGION="ap-southeast-1"

export DASHSCOPE_API_URL="https://${DASHSCOPE_WORKSPACE}.${DASHSCOPE_REGION}.maas.aliyuncs.com/api/v1"

export DASHSCOPE_COMPATIBLE_URL="https://${DASHSCOPE_WORKSPACE}.${DASHSCOPE_REGION}.maas.aliyuncs.com/compatible-mode/v1"
```

不同区域的 API Key 通常不能混用。API Key、Workspace、Endpoint 和模型权限必须属于匹配的区域和资源范围。

## HTTP 状态码

| HTTP 状态码 | 说明 |
|---:|---|
| `200` | 请求成功，当前请求可以执行 |
| `400` | 请求参数、模型名称或协议不正确 |
| `401` | API Key 无效、过期或区域不匹配 |
| `403` | Workspace、模型或区域权限不足 |
| `404` | Endpoint、路径或模型不存在 |
| `429` | 触发限流，或者可能存在配额问题 |
| `500` | 服务端内部错误 |
| `502` / `503` / `504` | 网关或服务暂时不可用 |

`429` 不一定代表额度用尽，也可能是请求频率过高。应结合返回的 `code` 和 `message` 判断。

## 关于额度查询

模型查询接口：

```text
GET /api/v1/models
```

主要用于查询模型信息和分页列表。

项目中的 `Models.list()` 和 `Models.get()` 没有提供账户级别的余额或剩余额度查询能力。可以通过以下方式判断：

1. 查看百炼 / Model Studio 控制台中的余额、配额和账单；
2. 发起一次最小真实请求；
3. 读取当前请求返回的 `usage`；
4. 在业务侧保存每次请求的 `usage`，自行统计累计用量。

注意：

- `Models.list()` 成功不代表一定有推理权限；
- 查询模型成功不代表当前 Workspace 一定有额度；
- 最终是否可调用，应以实际模型调用结果为准；
- 测试调用本身可能消耗额度。

查询**账户级**免费额度用本仓库的 `quota` 工具，见[免费额度 CLI](#免费额度-cliquota-fetch)。

## 相关 Python SDK API

该项目中的对应 Python API：

```python
import dashscope

# 查询模型列表
response = dashscope.Models.list(
    page=1,
    page_size=100,
)

# 查询指定模型
response = dashscope.Models.get(
    name="qwen3.8-max",
)

# 调用文本生成
response = dashscope.Generation.call(
    model="qwen3.8-max",
    messages=[
        {
            "role": "user",
            "content": "你好",
        }
    ],
)
```

相关源码：

- `dashscope/models.py`
- `dashscope/aigc/generation.py`
- `dashscope/aigc/chat_completion.py`
- `dashscope/__init__.py`

## License

本项目遵循 Apache License 2.0。
