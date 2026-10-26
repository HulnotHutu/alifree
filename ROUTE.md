下面按 **HTTP/1.1 协议报文格式** 重新整理。核心原则：

- 以 `方法 + 路径 + 请求头 + 请求体 + 响应` 为主；
- 不再以 `curl` 为主；
- 完整 URL = `Base URL + 请求行路径`；
- Workspace 专属 Endpoint 只替换 `Host`，路径通常不变；
- 动态拼接路径以 SDK 中 `SUB_PATH`、`task`、`function`、`base_address` 为准。

---

# DashScope HTTP API 文档

## 1. 通用约定

### 1.1 Base URL 与 Host

| 类型 | Base URL | 请求行 Host | 说明 |
|---|---|---|---|
| 原生公共 API | `https://dashscope.aliyuncs.com/api/v1` | `dashscope.aliyuncs.com` | 路径以 `/api/v1` 开头 |
| OpenAI-compatible 公共 API | `https://dashscope.aliyuncs.com/compatible-mode/v1` | `dashscope.aliyuncs.com` | 路径以 `/compatible-mode/v1` 开头 |
| Workspace 专属原生 API | `https://<workspace-id>.<region>.maas.aliyuncs.com/api/v1` | `<workspace-id>.<region>.maas.aliyuncs.com` | 路径仍以 `/api/v1` 开头 |
| Workspace 专属兼容 API | `https://<workspace-id>.<region>.maas.aliyuncs.com/compatible-mode/v1` | `<workspace-id>.<region>.maas.aliyuncs.com` | 路径仍以 `/compatible-mode/v1` 开头 |

支持区域：

```text
cn-beijing
cn-hongkong
ap-southeast-1
ap-northeast-1
eu-central-1
us-east-1
```

不同区域的 API Key 通常不能混用。

---

### 1.2 通用请求头

原生 API：

```http
Authorization: Bearer <DASHSCOPE_API_KEY>
Content-Type: application/json
X-DashScope-WorkSpace: <workspace-id>
```

OpenAI-compatible API：

```http
Authorization: Bearer <DASHSCOPE_API_KEY>
Content-Type: application/json
```

流式 SSE：

```http
X-DashScope-SSE: enable
Accept: text/event-stream
```

非流式：

```http
X-DashScope-SSE: disable
```

文件上传：

```http
Content-Type: multipart/form-data; boundary=<boundary>
```

---

### 1.3 通用响应结构

DashScope 原生接口通常返回：

```json
{
  "status_code": 200,
  "request_id": "xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx",
  "code": "",
  "message": "",
  "output": {},
  "usage": {}
}
```

说明：

- `status_code`：业务状态码，不一定等于 HTTP 状态码；
- `request_id`：请求追踪 ID；
- `code` / `message`：错误码和错误信息；
- `output`：业务结果；
- `usage`：本次请求用量，不是账户总余额。

---

### 1.4 异步任务模式

图像、视频、部分批量任务通常使用异步任务：

```http
POST /api/v1/services/aigc/...       创建任务
GET  /api/v1/tasks/<task_id>         查询任务
POST /api/v1/tasks/<task_id>/cancel  取消任务
GET  /api/v1/tasks                   查询任务列表
```

部分基础任务 API 也可能使用：

```http
DELETE /api/v1/tasks/<task_id>
```

具体以对应模块和服务端版本为准。

---

### 1.5 WebSocket

实时语音识别、实时语音合成、实时多模态对话可能使用 WebSocket：

```text
wss://<host>/api-ws/v1/inference
```

WebSocket 不是 HTTP API，不能直接用普通 HTTP 客户端或 `curl` 测试。

---

## 2. 模型管理 API

源码：`dashscope/models.py`

### 2.1 查询模型列表

```http
GET /api/v1/models?page_no=1&page_size=100 HTTP/1.1
Host: dashscope.aliyuncs.com
Authorization: Bearer <DASHSCOPE_API_KEY>
X-DashScope-WorkSpace: <workspace-id>
Accept: application/json
```

查询参数：

| 参数 | 类型 | 说明 |
|---|---|---|
| `page_no` | integer | 页码 |
| `page_size` | integer | 每页数量 |
| `model` | string | 可选，指定模型 |

响应示例：

```json
{
  "output": {
    "models": [
      {
        "model": "qwen-plus",
        "name": "通义千问 Plus"
      }
    ],
    "total": 100,
    "page_no": 1,
    "page_size": 100
  }
}
```

### 2.2 查询指定模型

```http
GET /api/v1/models?model=qwen3.8-max&page_no=1&page_size=1 HTTP/1.1
Host: dashscope.aliyuncs.com
Authorization: Bearer <DASHSCOPE_API_KEY>
X-DashScope-WorkSpace: <workspace-id>
Accept: application/json
```

注意：查询到模型不等于当前 Workspace 一定有调用权限或额度。

---

## 3. 文本生成与对话 API

### 3.1 OpenAI-compatible Chat Completions

源码：`dashscope/aigc/chat_completion.py`

```http
POST /compatible-mode/v1/chat/completions HTTP/1.1
Host: dashscope.aliyuncs.com
Authorization: Bearer <DASHSCOPE_API_KEY>
Content-Type: application/json
X-DashScope-WorkSpace: <workspace-id>
```

请求体：

```json
{
  "model": "qwen-plus",
  "messages": [
    {
      "role": "user",
      "content": "你好"
    }
  ],
  "stream": false,
  "temperature": 0.7,
  "top_p": 0.8,
  "top_k": 50,
  "max_tokens": 128,
  "stop": null,
  "tools": [],
  "tool_choice": "auto",
  "response_format": {
    "type": "text"
  }
}
```

支持能力：

- 普通对话
- 流式对话
- `temperature`
- `top_p`
- `top_k`
- `max_tokens`
- `stop`
- `tools`
- `tool_choice`
- `response_format`
- 多模态消息，取决于模型

流式请求：

```http
POST /compatible-mode/v1/chat/completions HTTP/1.1
Host: dashscope.aliyuncs.com
Authorization: Bearer <DASHSCOPE_API_KEY>
Content-Type: application/json
X-DashScope-WorkSpace: <workspace-id>
Accept: text/event-stream
```

```json
{
  "model": "qwen-plus",
  "messages": [
    {
      "role": "user",
      "content": "写一首短诗"
    }
  ],
  "stream": true
}
```

---

### 3.2 原生文本生成

源码：`dashscope/aigc/generation.py`

```http
POST /api/v1/services/aigc/text-generation/generation HTTP/1.1
Host: dashscope.aliyuncs.com
Authorization: Bearer <DASHSCOPE_API_KEY>
Content-Type: application/json
X-DashScope-WorkSpace: <workspace-id>
X-DashScope-SSE: disable
```

请求体：

```json
{
  "model": "qwen-plus",
  "input": {
    "messages": [
      {
        "role": "user",
        "content": "你好，请介绍一下自己"
      }
    ]
  },
  "parameters": {
    "result_format": "message",
    "max_tokens": 128,
    "temperature": 0.7
  }
}
```

流式：

```http
POST /api/v1/services/aigc/text-generation/generation HTTP/1.1
Host: dashscope.aliyuncs.com
Authorization: Bearer <DASHSCOPE_API_KEY>
Content-Type: application/json
X-DashScope-WorkSpace: <workspace-id>
X-DashScope-SSE: enable
Accept: text/event-stream
```

```json
{
  "model": "qwen-plus",
  "input": {
    "messages": [
      {
        "role": "user",
        "content": "讲一个简短的故事"
      }
    ]
  },
  "parameters": {
    "result_format": "message",
    "incremental_output": true
  }
}
```

---

### 3.3 原生多模态生成

源码：`dashscope/aigc/multimodal_conversation.py`

```http
POST /api/v1/services/aigc/multimodal-generation/generation HTTP/1.1
Host: dashscope.aliyuncs.com
Authorization: Bearer <DASHSCOPE_API_KEY>
Content-Type: application/json
X-DashScope-WorkSpace: <workspace-id>
```

请求体：

```json
{
  "model": "qwen3.5-omni-plus",
  "input": {
    "messages": [
      {
        "role": "user",
        "content": [
          {
            "image": "https://example.com/example.png"
          },
          {
            "text": "请描述这张图片"
          }
        ]
      }
    ]
  },
  "parameters": {
    "result_format": "message"
  }
}
```

可用于：

- 图片理解
- 视频理解
- 音频理解
- 文本和图片混合输入
- 全模态对话

---

## 4. Embedding、Rerank、Tokenization

### 4.1 文本 Embedding

源码：`dashscope/embeddings/text_embedding.py`

```http
POST /api/v1/services/embeddings/text-embedding/text-embedding HTTP/1.1
Host: dashscope.aliyuncs.com
Authorization: Bearer <DASHSCOPE_API_KEY>
Content-Type: application/json
X-DashScope-WorkSpace: <workspace-id>
```

```json
{
  "model": "text-embedding-v4",
  "input": {
    "texts": [
      "第一段文本",
      "第二段文本"
    ]
  },
  "parameters": {
    "text_type": "document",
    "dimension": 1024,
    "output_type": "dense"
  }
}
```

Query 类型：

```json
{
  "model": "text-embedding-v4",
  "input": {
    "texts": [
      "如何查询订单？"
    ]
  },
  "parameters": {
    "text_type": "query",
    "dimension": 1024,
    "output_type": "dense"
  }
}
```

---

### 4.2 多模态 Embedding

源码：`dashscope/embeddings/multimodal_embedding.py`

```http
POST /api/v1/services/embeddings/multimodal-embedding/multimodal-embedding HTTP/1.1
Host: dashscope.aliyuncs.com
Authorization: Bearer <DASHSCOPE_API_KEY>
Content-Type: application/json
X-DashScope-WorkSpace: <workspace-id>
```

```json
{
  "model": "qwen3-vl-embedding",
  "input": {
    "contents": [
      {
        "text": "一只猫坐在窗边",
        "factor": 1
      },
      {
        "image": "https://example.com/cat.png",
        "factor": 1
      }
    ]
  },
  "parameters": {
    "dimension": 1024,
    "output_type": "dense"
  }
}
```

---

### 4.3 文本重排 Rerank

源码：`dashscope/rerank/text_rerank.py`

```http
POST /api/v1/services/rerank/text-rerank/text-rerank HTTP/1.1
Host: dashscope.aliyuncs.com
Authorization: Bearer <DASHSCOPE_API_KEY>
Content-Type: application/json
X-DashScope-WorkSpace: <workspace-id>
```

```json
{
  "model": "qwen3-rerank",
  "input": {
    "query": "什么是向量数据库？",
    "documents": [
      "向量数据库用于存储和检索向量。",
      "今天天气很好。",
      "Embedding 可以把文本转换成向量。"
    ]
  },
  "parameters": {
    "top_n": 2,
    "return_documents": true
  }
}
```

---

### 4.4 Tokenization

源码：`dashscope/tokenizers/tokenization.py`

常见路径：

```http
POST /api/v1/services/aigc/tokenizer HTTP/1.1
Host: dashscope.aliyuncs.com
Authorization: Bearer <DASHSCOPE_API_KEY>
Content-Type: application/json
X-DashScope-WorkSpace: <workspace-id>
```

部分封装可能使用：

```http
POST /api/v1/services/tokenizers/tokenizer
```

以实际服务端路由为准。

```json
{
  "model": "qwen-plus",
  "input": {
    "messages": [
      {
        "role": "user",
        "content": "请计算这段文本的 token 数量"
      }
    ]
  }
}
```

本地 Tokenizer 不一定访问远程 HTTP 服务。

---

## 5. 图像生成 API

源码：`dashscope/aigc/image_synthesis.py`

### 5.1 创建图像生成任务

```http
POST /api/v1/services/aigc/text2image/image-synthesis HTTP/1.1
Host: dashscope.aliyuncs.com
Authorization: Bearer <DASHSCOPE_API_KEY>
Content-Type: application/json
X-DashScope-WorkSpace: <workspace-id>
```

```json
{
  "model": "wanx-v1",
  "input": {
    "prompt": "一只猫在月光下的湖边散步",
    "negative_prompt": "低质量、模糊"
  },
  "parameters": {
    "size": "1024*1024",
    "n": 1
  }
}
```

响应中通常包含：

```json
{
  "output": {
    "task_id": "xxxxxxxx"
  }
}
```

### 5.2 查询图像任务

```http
GET /api/v1/tasks/<task_id> HTTP/1.1
Host: dashscope.aliyuncs.com
Authorization: Bearer <DASHSCOPE_API_KEY>
X-DashScope-WorkSpace: <workspace-id>
```

### 5.3 取消图像任务

```http
POST /api/v1/tasks/<task_id>/cancel HTTP/1.1
Host: dashscope.aliyuncs.com
Authorization: Bearer <DASHSCOPE_API_KEY>
X-DashScope-WorkSpace: <workspace-id>
```

### 5.4 查询任务列表

```http
GET /api/v1/tasks?page_no=1&page_size=20&model_name=wanx-v1&status=SUCCEEDED HTTP/1.1
Host: dashscope.aliyuncs.com
Authorization: Bearer <DASHSCOPE_API_KEY>
X-DashScope-WorkSpace: <workspace-id>
```

---

## 6. 视频生成 API

源码：`dashscope/aigc/video_synthesis.py`

### 6.1 创建视频任务

常见路径：

```http
POST /api/v1/services/aigc/video-generation/video-synthesis HTTP/1.1
Host: dashscope.aliyuncs.com
Authorization: Bearer <DASHSCOPE_API_KEY>
Content-Type: application/json
X-DashScope-WorkSpace: <workspace-id>
```

文生视频：

```json
{
  "model": "wan2.7-t2v",
  "input": {
    "prompt": "海边的日落，海浪缓慢拍打沙滩"
  },
  "parameters": {
    "duration": 5,
    "resolution": "720P",
    "ratio": "16:9"
  }
}
```

图生视频：

```json
{
  "model": "wan2.7-i2v",
  "input": {
    "prompt": "镜头缓慢推进",
    "img_url": "https://example.com/input.png"
  },
  "parameters": {
    "duration": 5,
    "resolution": "720P"
  }
}
```

### 6.2 查询视频任务

```http
GET /api/v1/tasks/<task_id> HTTP/1.1
Host: dashscope.aliyuncs.com
Authorization: Bearer <DASHSCOPE_API_KEY>
X-DashScope-WorkSpace: <workspace-id>
```

### 6.3 取消视频任务

```http
POST /api/v1/tasks/<task_id>/cancel HTTP/1.1
Host: dashscope.aliyuncs.com
Authorization: Bearer <DASHSCOPE_API_KEY>
X-DashScope-WorkSpace: <workspace-id>
```

---

## 7. 文件管理 API

源码：`dashscope/files.py`

| 操作 | 方法 | 路径 |
|---|---|---|
| 上传文件 | `POST` | `/api/v1/files` |
| 查询文件列表 | `GET` | `/api/v1/files` |
| 查询文件详情 | `GET` | `/api/v1/files/<file_id>` |
| 删除文件 | `DELETE` | `/api/v1/files/<file_id>` |

### 7.1 上传文件

```http
POST /api/v1/files HTTP/1.1
Host: dashscope.aliyuncs.com
Authorization: Bearer <DASHSCOPE_API_KEY>
X-DashScope-WorkSpace: <workspace-id>
Content-Type: multipart/form-data; boundary=----Boundary
```

```text
------Boundary
Content-Disposition: form-data; name="files"; filename="training.jsonl"
Content-Type: application/json

{...}
------Boundary
Content-Disposition: form-data; name="descriptions"

["训练文件"]
------Boundary--
```

### 7.2 查询文件列表

```http
GET /api/v1/files?page_no=1&page_size=20 HTTP/1.1
Host: dashscope.aliyuncs.com
Authorization: Bearer <DASHSCOPE_API_KEY>
X-DashScope-WorkSpace: <workspace-id>
```

### 7.3 查询文件详情

```http
GET /api/v1/files/<file_id> HTTP/1.1
Host: dashscope.aliyuncs.com
Authorization: Bearer <DASHSCOPE_API_KEY>
X-DashScope-WorkSpace: <workspace-id>
```

### 7.4 删除文件

```http
DELETE /api/v1/files/<file_id> HTTP/1.1
Host: dashscope.aliyuncs.com
Authorization: Bearer <DASHSCOPE_API_KEY>
X-DashScope-WorkSpace: <workspace-id>
```

---

## 8. Fine-tune 微调 API

源码：`dashscope/finetune/finetunes.py`

| 操作 | 方法 | 路径 |
|---|---|---|
| 创建微调任务 | `POST` | `/api/v1/fine-tunes` |
| 查询微调列表 | `GET` | `/api/v1/fine-tunes` |
| 查询微调详情 | `GET` | `/api/v1/fine-tunes/<job_id>` |
| 取消微调 | `POST` | `/api/v1/fine-tunes/<job_id>/cancel` |
| 删除微调 | `DELETE` | `/api/v1/fine-tunes/<job_id>` |
| 查询日志 | `GET` | `/api/v1/fine-tunes/<job_id>/logs` |
| 查询事件 | `GET` | `/api/v1/fine-tunes/<job_id>/events` |

### 8.1 创建微调任务

```http
POST /api/v1/fine-tunes HTTP/1.1
Host: dashscope.aliyuncs.com
Authorization: Bearer <DASHSCOPE_API_KEY>
Content-Type: application/json
X-DashScope-WorkSpace: <workspace-id>
```

```json
{
  "model": "qwen-plus",
  "training_file_ids": [
    "file-training-xxxxxxxx"
  ],
  "validation_file_ids": [
    "file-validation-xxxxxxxx"
  ],
  "hyper_parameters": {
    "n_epochs": 2,
    "batch_size": 1,
    "learning_rate": 0.00001
  }
}
```

### 8.2 查询微调任务列表

```http
GET /api/v1/fine-tunes?page_no=1&page_size=20 HTTP/1.1
Host: dashscope.aliyuncs.com
Authorization: Bearer <DASHSCOPE_API_KEY>
X-DashScope-WorkSpace: <workspace-id>
```

### 8.3 查询微调任务详情

```http
GET /api/v1/fine-tunes/<job_id> HTTP/1.1
Host: dashscope.aliyuncs.com
Authorization: Bearer <DASHSCOPE_API_KEY>
X-DashScope-WorkSpace: <workspace-id>
```

### 8.4 取消微调任务

```http
POST /api/v1/fine-tunes/<job_id>/cancel HTTP/1.1
Host: dashscope.aliyuncs.com
Authorization: Bearer <DASHSCOPE_API_KEY>
X-DashScope-WorkSpace: <workspace-id>
```

### 8.5 删除微调任务

```http
DELETE /api/v1/fine-tunes/<job_id> HTTP/1.1
Host: dashscope.aliyuncs.com
Authorization: Bearer <DASHSCOPE_API_KEY>
X-DashScope-WorkSpace: <workspace-id>
```

---

## 9. Deployment 模型部署 API

源码：`dashscope/finetune/deployments.py`

| 操作 | 方法 | 路径 |
|---|---|---|
| 创建部署 | `POST` | `/api/v1/deployments` |
| 查询部署列表 | `GET` | `/api/v1/deployments` |
| 查询部署详情 | `GET` | `/api/v1/deployments/<deployed_model>` |
| 扩缩容 | `PUT` | `/api/v1/deployments/<deployed_model>` |
| 删除部署 | `DELETE` | `/api/v1/deployments/<deployed_model>` |

### 9.1 创建部署

```http
POST /api/v1/deployments HTTP/1.1
Host: dashscope.aliyuncs.com
Authorization: Bearer <DASHSCOPE_API_KEY>
Content-Type: application/json
X-DashScope-WorkSpace: <workspace-id>
```

```json
{
  "model_name": "qwen-plus",
  "capacity": 1,
  "model_version": "v1",
  "suffix": "demo"
}
```

### 9.2 扩缩容

```http
PUT /api/v1/deployments/<deployed_model> HTTP/1.1
Host: dashscope.aliyuncs.com
Authorization: Bearer <DASHSCOPE_API_KEY>
Content-Type: application/json
X-DashScope-WorkSpace: <workspace-id>
```

```json
{
  "capacity": 2
}
```

---

## 10. Assistant / Thread / Message / Run API

> 项目当前代码已标记 Assistants API 为 deprecated，建议迁移到 Responses API。

### 10.1 Assistant

| 操作 | 方法 | 路径 |
|---|---|---|
| 创建 Assistant | `POST` | `/api/v1/assistants` |
| 查询列表 | `GET` | `/api/v1/assistants` |
| 查询详情 | `GET` | `/api/v1/assistants/<assistant_id>` |
| 更新 | `POST` | `/api/v1/assistants/<assistant_id>` |
| 删除 | `DELETE` | `/api/v1/assistants/<assistant_id>` |
| 添加文件 | `POST` | `/api/v1/assistants/<assistant_id>/files` |
| 文件列表 | `GET` | `/api/v1/assistants/<assistant_id>/files` |
| 文件详情 | `GET` | `/api/v1/assistants/<assistant_id>/files/<file_id>` |
| 删除文件 | `DELETE` | `/api/v1/assistants/<assistant_id>/files/<file_id>` |

创建示例：

```http
POST /api/v1/assistants HTTP/1.1
Host: dashscope.aliyuncs.com
Authorization: Bearer <DASHSCOPE_API_KEY>
Content-Type: application/json
X-DashScope-WorkSpace: <workspace-id>
```

```json
{
  "model": "qwen-plus",
  "name": "demo-assistant",
  "instructions": "你是一个有帮助的助手",
  "tools": [],
  "metadata": {}
}
```

### 10.2 Thread

| 操作 | 方法 | 路径 |
|---|---|---|
| 创建 Thread | `POST` | `/api/v1/threads` |
| 查询 Thread | `GET` | `/api/v1/threads/<thread_id>` |
| 更新 Thread | `POST` | `/api/v1/threads/<thread_id>` |
| 删除 Thread | `DELETE` | `/api/v1/threads/<thread_id>` |

### 10.3 Message

| 操作 | 方法 | 路径 |
|---|---|---|
| 创建 Message | `POST` | `/api/v1/threads/<thread_id>/messages` |
| 查询列表 | `GET` | `/api/v1/threads/<thread_id>/messages` |
| 查询详情 | `GET` | `/api/v1/threads/<thread_id>/messages/<message_id>` |

### 10.4 Run

| 操作 | 方法 | 路径 |
|---|---|---|
| 创建 Run | `POST` | `/api/v1/threads/<thread_id>/runs` |
| 查询列表 | `GET` | `/api/v1/threads/<thread_id>/runs` |
| 查询详情 | `GET` | `/api/v1/threads/<thread_id>/runs/<run_id>` |
| 取消 Run | `POST` | `/api/v1/threads/<thread_id>/runs/<run_id>/cancel` |
| 提交工具输出 | `POST` | `/api/v1/threads/<thread_id>/runs/<run_id>/submit_tool_outputs` |

### 10.5 Run Steps

```http
GET /api/v1/threads/<thread_id>/runs/<run_id>/steps
GET /api/v1/threads/<thread_id>/runs/<run_id>/steps/<step_id>
```

---

## 11. Agentic RL HTTP API

源码：

- `dashscope/finetune/agentic_rl.py`
- `dashscope/finetune/reinforcement/common/constants.py`

默认基础路径：

```text
/api/v1/fine-tunes
```

| 操作 | 方法 | 路径 |
|---|---|---|
| 上传文件 URL | `POST` | `/api/v1/fine-tunes/generate/faas/upload/url` |
| 创建 Rollout | `POST` | `/api/v1/fine-tunes/create/rollout` |
| 创建 Reward | `POST` | `/api/v1/fine-tunes/create/reward` |
| 创建 Group Reward | `POST` | `/api/v1/fine-tunes/create/group-reward` |
| 加载 FaaS | `POST` | `/api/v1/fine-tunes/online/faas` |
| 查询 FaaS | `GET` | `/api/v1/fine-tunes/query/faas` |
| 创建 FaaS Layer | `POST` | `/api/v1/fine-tunes/create/faas/layer` |
| 查询 FaaS Layer 状态 | `GET` | `/api/v1/fine-tunes/query/faas/layer/status` |

这些接口用于：

- 上传 Agentic RL 相关文件；
- 注册 Rollout 函数；
- 注册 Reward 函数；
- 注册 Group Reward 函数；
- 加载 Function Compute 服务；
- 查询 Function Compute 服务；
- 创建和查询函数依赖层。

---

## 12. OSS 上传辅助 API

源码：`dashscope/utils/oss_utils.py`

流程：

1. 向 DashScope 请求上传凭证；
2. 服务端返回 `upload_host`；
3. 客户端再上传文件到返回的 OSS 地址。

DashScope 侧资源路径：

```http
POST /api/v1/uploads HTTP/1.1
Host: dashscope.aliyuncs.com
Authorization: Bearer <DASHSCOPE_API_KEY>
Content-Type: application/json
X-DashScope-WorkSpace: <workspace-id>
```

实际文件上传地址由服务端返回的 `upload_host` 决定，不是固定 DashScope API URL。

---

## 13. Agent Studio / Managed Agent API

源码目录：

```text
dashscope/agentstudio/
```

主要文件：

- `client.py`
- `transport.py`
- `streaming.py`
- `types/`

该模块封装多个 Agent Studio HTTP 资源，包括：

- Agent
- Session
- Deployment
- Webhook
- Webhook delivery
- Runs
- Events
- Resources
- Streaming

建议通过：

```python
from dashscope.agentstudio import Client
```

以及 `client.py` 中定义的资源方法调用，不要手工拼接所有路径。

Agent Studio 的接口路径与传统 `/api/v1/services/aigc/...` 推理接口是两套不同的资源体系。

---

## 14. HTTP 状态码

| 状态码 | 说明 |
|---:|---|
| `200` | 请求成功 |
| `201` | 资源创建成功 |
| `400` | 参数、模型或协议错误 |
| `401` | API Key 无效、过期或区域不匹配 |
| `403` | Workspace、模型或区域权限不足 |
| `404` | 路径、资源或模型不存在 |
| `429` | 限流或可能存在配额问题 |
| `500` | 服务端错误 |
| `502` | 网关错误 |
| `503` | 服务暂时不可用 |
| `504` | 网关超时 |

`429` 不一定表示额度耗尽，也可能是请求频率过高，需要结合 `code` 和 `message` 判断。

---

## 15. HTTP API 总览

| 能力 | 方法 | 路径 |
|---|---|---|
| 模型列表 | `GET` | `/api/v1/models` |
| 指定模型 | `GET` | `/api/v1/models?model=...` |
| 原生文本生成 | `POST` | `/api/v1/services/aigc/text-generation/generation` |
| 原生多模态生成 | `POST` | `/api/v1/services/aigc/multimodal-generation/generation` |
| Chat Completion | `POST` | `/compatible-mode/v1/chat/completions` |
| 文本 Embedding | `POST` | `/api/v1/services/embeddings/text-embedding/text-embedding` |
| 多模态 Embedding | `POST` | `/api/v1/services/embeddings/multimodal-embedding/multimodal-embedding` |
| 文本重排 | `POST` | `/api/v1/services/rerank/text-rerank/text-rerank` |
| Tokenization | `POST` | `/api/v1/services/aigc/tokenizer` |
| 图像生成 | `POST` | `/api/v1/services/aigc/text2image/image-synthesis` |
| 视频生成 | `POST` | `/api/v1/services/aigc/video-generation/video-synthesis` |
| 异步任务查询 | `GET` | `/api/v1/tasks/<task_id>` |
| 取消异步任务 | `POST` | `/api/v1/tasks/<task_id>/cancel` |
| 异步任务列表 | `GET` | `/api/v1/tasks` |
| 文件上传 | `POST` | `/api/v1/files` |
| 文件列表 | `GET` | `/api/v1/files` |
| 文件详情 | `GET` | `/api/v1/files/<file_id>` |
| 删除文件 | `DELETE` | `/api/v1/files/<file_id>` |
| 创建微调任务 | `POST` | `/api/v1/fine-tunes` |
| 微调任务列表 | `GET` | `/api/v1/fine-tunes` |
| 微调任务详情 | `GET` | `/api/v1/fine-tunes/<job_id>` |
| 取消微调 | `POST` | `/api/v1/fine-tunes/<job_id>/cancel` |
| 删除微调 | `DELETE` | `/api/v1/fine-tunes/<job_id>` |
| 创建模型部署 | `POST` | `/api/v1/deployments` |
| 部署列表 | `GET` | `/api/v1/deployments` |
| 部署详情 | `GET` | `/api/v1/deployments/<model>` |
| 扩缩容 | `PUT` | `/api/v1/deployments/<model>` |
| 删除部署 | `DELETE` | `/api/v1/deployments/<model>` |
| 创建 Assistant | `POST` | `/api/v1/assistants` |
| 创建 Thread | `POST` | `/api/v1/threads` |
| 创建 Message | `POST` | `/api/v1/threads/<thread_id>/messages` |
| 创建 Run | `POST` | `/api/v1/threads/<thread_id>/runs` |
| 实时 WebSocket | `WSS` | `wss://<host>/api-ws/v1/inference` |

---

## 16. 重要说明

- `Models.list()` 查询到模型，不等于该模型一定有调用权限。
- 实际调用权限受 API Key、Workspace、区域、模型状态和额度影响。
- SDK 中的 `usage` 通常表示当前请求用量，不是账户总余额。
- Assistants、Threads、Messages、Runs 在项目中已经标记为 deprecated，应优先关注 Responses API。
- 图像和视频通常采用异步任务模式，需要提交任务后再查询状态。
- 部分实时语音和多模态接口使用 WebSocket，而不是 HTTP。
- 大量接口路径由 `BaseApi`、`CreateMixin`、`GetMixin`、`ListMixin`、`TaskMixin` 等基础类动态拼接。
- 最终路径应以对应模块的 `task`、`function`、`SUB_PATH` 和 `base_address` 配置为准。
