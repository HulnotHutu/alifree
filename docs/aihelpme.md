# aihelpme：终端 AI 异步问答

docker 式的 cli/daemon 结构：`aihelpme` 是薄客户端，`aihelpmed` 是常驻 daemon，队列和结果全部保存在 daemon 内存里，不落盘。

```bash
aihelpme "Go 里怎么原子地交换两个 int？"   # put：提问后阻塞至多 5s，短问题当场得到回答
cat big_doc.txt | aihelpme "总结这份文档"  # 管道内容拼在问题后
aihelpme                                   # get：无输入即取最新回答，未就绪提示稍等
```

- `aihelpme`（cli）：**有输入即提问**（位置参数或管道 stdin），经 unix socket 发给 daemon 后**阻塞至多 5s**——短问题当场打印回答（stdout 纯净，可继续管道），超时则提示"仍在处理中"，答案稍后用裸命令取回；**无输入即取回答**。daemon 未运行时 cli 会自动拉起一个。
- `aihelpmed`（daemon）：监听 socket，内存里维护问题队列与已完成结果，逐个调用百炼 compatible-mode 接口。等待 AI 的过程完全发生在 daemon 内，不占用任何终端。不落盘、无通知机制；Ctrl-C 退出即状态清空，重复启动会被单实例检查拦下。

## 配置

调用配置在 `~/.config/aihelpme/config.yaml`（`AIHELPME_CONFIG` 可覆盖），daemon 启动时加载，cli 不读配置：

```yaml
workspace_id: llm-p317n4rywd1joyu3
model: deepseek-v4-flash-0731
call_timeout: 10m              # 可选，默认 10m
quota_db: /home/hulut/note-space/dashscope-call/data/quota.db   # 可选，quota 工具 SQLite 路径，启用 /model 接口
stream: false                  # 可选，true 时提问改为流式输出（重启生效）
system_prompt: <回答风格提示词，可留空>
```

缺文件或缺 `workspace_id`/`model` 时 daemon 启动即报错并打印示例；改完配置重启 daemon 生效。

## 调试与运行时接口

daemon 只监听 unix socket，以下接口仅 curl 可用（cli 不封装）：

```bash
curl --unix-socket $XDG_RUNTIME_DIR/aihelpme.sock http://localhost/list   # 队列状态（排队 id/working/ready）
curl --unix-socket $XDG_RUNTIME_DIR/aihelpme.sock http://localhost/model  # 当前模型 + 可用 Text 模型
curl -X PUT --unix-socket $XDG_RUNTIME_DIR/aihelpme.sock \
     -d '{"model":"qwen3.8-max"}' http://localhost/model                  # 切换模型，即时生效于后续任务
curl --unix-socket $XDG_RUNTIME_DIR/aihelpme.sock http://localhost/prompt # 当前 system prompt
curl -X PUT --unix-socket $XDG_RUNTIME_DIR/aihelpme.sock \
     -d '{"prompt":"新的 system prompt"}' http://localhost/prompt         # 修改 system prompt；传 "" 为清空
```

`/model` 的可用列表来自 quota 工具的 SQLite（`main.model_quotas` 最新快照，路径由 `quota_db` 配置；`quota fetch --save` 刷新后即生效），只含剩余额度 > 0 的 Text 模型，按剩余降序；PUT 会校验目标在列表内，否则 400。`/prompt` 与 `/model` 同语义：修改保存在 daemon 内存，即时生效于后续任务，重启后回到 config.yaml 的 `system_prompt`；PUT 传 `""` 即清空（此后请求不携带 system 消息）。

## 流式模式

**流式模式**（`stream: true`，重启生效；默认关闭，保持阻塞异步语义）：提问后回答**逐字打印**到终端，不再有 5s 等待与"稍后取回"。实现为同一队列架构的另一种交付方式——worker 调 API（`stream: true`）边生成边把增量推进 per-job channel，新增 `GET /stream?id=` SSE 端点转发；Ctrl-C 只是停止观看，worker 继续算完，完整回答仍入结果队列（裸命令可再取回）；journald 对话日志照记。curl 体验：

```bash
ID=$(curl -s --unix-socket $XDG_RUNTIME_DIR/aihelpme.sock -d '{"question":"你好"}' http://localhost/put | jq -r .id)
curl -N --unix-socket $XDG_RUNTIME_DIR/aihelpme.sock "http://localhost/stream?id=$ID"   # SSE 逐块
```

## 日志

daemon 走 slog，JSON 输出到 stderr，含时间与来源（文件：行号）；级别由 `AIHELPME_LOG_LEVEL` 控制（debug/info/warn/error，默认 info）。字段约定：`job_id`/`model`/`status`/`duration_ms` 标识上下文与结果，`err` 承载异常。请求入口/出口（`request start`/`request done`）、业务节点（`job started`/`job finished`）、外部调用（`api call start`/`done`/`failed`）、错误处理均有对应事件；每次调用成功后另有一条 `conversation` 记录完整对话（system、各轮、回答），多轮提示词测试可在 journald 里回溯。

## systemd 用户服务部署

aihelpmed 是**用户级**常驻进程（socket 在 `$XDG_RUNTIME_DIR`、状态在内存），用 systemd --user 管理，而不是 nginx 那类系统级服务。与系统服务模板的差异：`Type=simple`（Go 程序前台运行，无需 PIDFile）、`WantedBy=default.target`、密钥经 `EnvironmentFile` 注入而非写进 unit，另加 `Restart=on-failure` 崩溃自愈。

**1. 安装二进制与凭证文件**（config.yaml 见上节）：

```bash
install -m755 bin/aihelpme bin/aihelpmed ~/.local/bin/
echo 'DASHSCOPE_API_KEY=sk-xxx' > ~/.config/aihelpme/env
chmod 600 ~/.config/aihelpme/env
```

**2. unit 文件** `~/.config/systemd/user/aihelpmed.service`（用户手写 unit 的规范位置；软件包式安装则用 `~/.local/share/systemd/user/`，两者都被 systemd --user 搜索）：

```ini
[Unit]
Description=aihelpmed：aihelpme 的常驻 daemon（内存队列 + unix socket）

[Service]
Type=simple
EnvironmentFile=%h/.config/aihelpme/env
ExecStart=%h/.local/bin/aihelpmed
Restart=on-failure
RestartSec=3

[Install]
WantedBy=default.target
```

**3. 启用与运维**：

```bash
systemctl --user daemon-reload
systemctl --user enable --now aihelpmed   # 开机自启 + 立即运行
systemctl --user status aihelpmed
journalctl --user -u aihelpmed -f         # slog JSON 日志直达 journald
```

说明：

- `AIHELPME_LOG_LEVEL=debug` 等可加进 env 文件调整日志级别。
- 重新构建后需重新 `install` 到 `~/.local/bin/`，再 `systemctl --user restart aihelpmed`。
- 服务停止时 cli 会经 PATH 自动拉起 `aihelpmed` 兜底；想让 daemon 未登录也常驻：`loginctl enable-linger $USER`。
