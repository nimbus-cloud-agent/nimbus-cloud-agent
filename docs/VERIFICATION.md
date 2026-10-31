# 实际验收记录

本记录对应 2026-10-31 在这台 macOS 机器上的运行结果。使用当前目录提供的真实 DeepSeek / E2B 凭据；凭据未写入代码或测试输出。

| 项目 | 实际结果 |
| --- | --- |
| 编译与静态检查 | 前端 TypeScript / Vite 构建、Go 构建、`go test ./...`、`go vet ./...` 通过 |
| Wasm 隔离 | 9 项自动检查通过：TypeScript 与 SDK、无限循环中断、禁止 Node import、异步错误、悬空 Promise 截止时间、256 MiB 内存上限、ToolCallError 类型，以及等待评审不占执行时间、等待期间 CPU 限制与取消仍有效 |
| 普通对话 | 真实模型返回、增量持久化；没有创建沙箱 |
| E2B / 网页终端 | 真实模型写入 `/workspace/nimbus-proof.txt`；WebSocket PTY 读取到 `NIMBUS_E2B_OK` |
| 暂停和恢复 | 暂停后历史可读且不唤醒；恢复后 E2B ID、文件内容保持不变 |
| 无效模板 | E2B 明确返回失败，页面 API 显示异常；没有写入占位沙箱或伪造 E2B ID |
| PTC | 原生 Harness PTC 展示模式调用 `run_code`，Wasm 内通过 SDK 读取真实 E2B 文件；检查了 `tool/ptc-dispatch`，不是只检查模型文字 |
| 崩溃恢复 | 输出中途强制杀掉 Go 进程；重启后已持久化的部分输出不变、旧轮中断、会话空闲；“继续”建立新轮且带上已显示的内容 |
| Outbox | 5 个并发请求使用同一 client_msg_id，只产生并返回同一条 outbox；排队两轮串行执行，事件序号连续 |
| 数据库不可变历史 | 直接 UPDATE / DELETE 事件被数据库拒绝；删除所属会话可以正常级联清理 |
| Skill | 页面保存；真实模型读取 PostgreSQL 中的验证字符串；停用后新会话无法读取；全程不需要沙箱 |
| Skill 校验 | 非法路径、二进制、101 个文件、单个文件超过 5 MiB 均拒绝 |
| 模型 / Plan / 思考 | 已切换为 deepseek-flash；真实 max + 原生 Plan + Wasm PTC 通过 E2B 写入 `/workspace/plan-proof.txt` |
| Plan 评审全流程 | 真实 Flash + DSH：PTC 调用原生 `exit_plan_mode`，Chrome 刷新保留待评审卡片，点击批准后原生 `plan/mode` 关闭 Plan 并继续回复；Standard 下提交修改意见后出现修订版计划，暂不决定结束本轮并保留 Plan；Stop 中断待评审，不会批准 |
| Plan 等待与答案边界 | PTC 的 `timeoutMs=10000`，等待人工确认 12 秒仍可批准继续；数据库集成测试覆盖跨会话回答、无效选项、重复回答幂等、多标签页冲突、取消 / 重启后的失效评审、仅原生事件更新 Plan 默认值 |
| 会话隔离 | 第二个会话获得不同 E2B ID，无法读到第一个沙箱内的验证文件 |
| 停止 | 有增量输出时停止，保留部分文本、旧轮标记 interrupted、会话回到 idle |
| 自动暂停 | 将专用测试会话的活动时间回拨 16 分钟，实际 reaper 调用 E2B 暂停成功；没有在这里额外等待完整 15 分钟 |
| MCP / OAuth | 本地真实 HTTP 协议测试服务完成 discovery、动态注册、PKCE、回调、401 后 refresh；真实 DeepSeek 调用 echo 工具成功且没有启动沙箱 |
| Notion MCP | 使用用户已有 OAuth 授权，完整发现 68 个工具；真实 Flash + Wasm PTC 调用 `notion-get-self` 成功，没有启动沙箱、没有读写 Notion 页面；测试会话验证后删除 |
| 大体积 MCP 响应 | 修复进程退出前未等待 stdout 写完的问题；本地真实 HTTP MCP 自动测试覆盖分页工具目录（约 300 KB）和约 1.6 MB 工具结果，字段逐字校验完整，无需真实凭据 |
| 凭据 / 请求边界 | API 仅返回遮罩，不返回 OAuth token；跨 Origin 请求与无 capability 的控制请求被拒绝 |
| 浏览器 | 本机 Chrome 桌面与 390px 手机视口检查；对话、设置、Skill 保存及响应式布局可用，无页面 JavaScript 异常 |
| 工具展示与折叠 | 参考 DSH Web 的工具摘要和调用树，用 HeroUI 实现整轮 / 单工具折叠；Chrome 验证 PTC 子调用去重与嵌套、命令失败和退出码、文件内容复制、Enter / Space 操作、SSE 更新保留用户折叠选择、已中断调用及 390px 布局 |
| Preset 生命周期 | PostgreSQL 隔离 schema 集成测试及 race 检查通过：空会话可选、首条消息锁定、并发异模式首次发送仅一个成功、PATCH / 部分消息参数 / SQL 均不能绕过、失败消息重试保留锁定、旧混合会话迁移保留原生历史 |
| 模型验证 | 官方更新博客、模型文档及真实 `/models` 均确认 Flash 的 API 名称为 `deepseek-flash`；产品仅提供 Flash，后端拒绝旧模型名 |
| 会话列表推送 | 浏览器持续订阅 `/api/sessions/stream`，观察期间没有 GET `/api/sessions` 轮询；标题修改实时到达，重新连接收到最新列表快照 |
| SSE 心跳 | 列表流与对话流实测相邻心跳分别为 5000 ms、5001 ms；数据推送不受 5 秒心跳间隔影响 |
| 图片 | Chrome 文件选择、粘贴、缩略图、大图预览、移除通过；Flash 真实识别测试图文字 `NIMBUS 4729`，原生 user/message 包含 image 附件；刷新后图片可见，后续轮次恢复附件并回答正确，未创建 E2B 沙箱 |
| 图片边界 | 数据库集成测试覆盖跨会话图片读取与引用、伪造格式、无效 / 重复图片 ID、数量上限、纯图片消息及删除会话级联清理图片 |
| 图片重启恢复 | 第一轮仅让模型回复“收到”；停止后端并删除测试会话的本地轮次缓存，重新启动后仍可读取图片、preset 仍锁定，第二轮 Flash 正确读出此前未转写的 `ORBIT 5938`；证明图片从 PostgreSQL 恢复 |

## 重跑

服务启动后，顺序执行，避免崩溃恢复测试影响其他测试：

```sh
node scripts/e2e.mjs
node scripts/features-e2e.mjs
node scripts/invariants-e2e.mjs
node scripts/recovery-e2e.mjs
node scripts/browser-check.mjs
node scripts/session-updates-e2e.mjs
node scripts/plan-e2e.mjs
node scripts/tool-ui-check.mjs
```

Preset、模型迁移、图片归属和计划评审的数据库集成测试使用独立临时 schema，不调用模型、不改动现有会话：

```sh
NIMBUS_TEST_DATABASE_URL='postgres://nimbus@127.0.0.1:55432/nimbus?sslmode=disable' go test -race ./internal/server
npm test
```

`npm test` 共 16 项，还验证运行时在加载 Harness 前拒绝缺失 / 不一致的会话 preset 与旧模型名、超过管道缓冲区的大体积 MCP 工具目录和结果传输，以及工具调用树关联、重放、退出码与待完成状态。`scripts/plan-e2e.mjs` 使用本机 Chrome，只回答其自身创建的测试会话，并在退出时删除这些会话。`scripts/tool-ui-check.mjs` 使用浏览器内模拟事件，不调用真实模型，也不修改现有会话。

MCP 需要另一个终端先运行 `node scripts/mcp-fixture.mjs`，再执行 `node scripts/mcp-e2e.mjs`。这些检查会调用真实模型 / 沙箱，产生相应使用量。恢复测试会主动结束并重新启动本地服务。测试脚本应在专用开发实例运行。

## 明确的边界

- 这是单用户、仅绑定本机回环地址的应用；不包含公网部署、登录、多租户或多实例调度。
- Go 二进制内嵌前端和 Nimbus 监督脚本；仍需要 Node.js 24.11+ 与独立安装的指定 Harness / npm 依赖，尚不是一个完全独立的可执行文件。
- DSH RC.5 的公共持久化事件不包含每个 token。增量帧单独存入 PostgreSQL，恢复时提供明确标识的中断上下文。服务端 SSE 使用 250 ms 数据库变更检查，客户端不轮询；心跳独立为 5 秒。尚未切换为 LISTEN 驱动。详见 ARCHITECTURE.md。
- E2B 单次同步命令当前最多执行 55 秒、输出上限 1 MiB；长任务可以在沙箱内启动后台进程，再轮询日志。沙箱内容不备份到 Mac，销毁后不能恢复。
- OAuth 已通过协议测试，也使用用户已有 Notion 授权完成了真实只读调用；没有自动执行第三方登录。第三方服务是否要求预注册 OAuth 客户端、是否接受本机 HTTP 回调，取决于该服务策略。
- 没有压测 20 个真实并发沙箱或等待完整一小时来测试云端暂停。容量锁、等待、近到期暂停与 E2B onTimeout=pause 已实现。
- 依赖审计：前端 0 项；运行时 0 high / critical，剩余 3 moderate 报告均来自 E2B 的 glob / minimatch 依赖链。Nimbus 不使用其模板构建或本机 glob API。没有为了消除报告而降级 E2B 或强制替换不兼容的大版本。

测试截图和运行日志保存在被 Git 忽略的 `.local/` 目录。正常交付时测试用 MCP 配置和临时 Skill 会删除，保留一段可查看的 E2B 验证对话及其已暂停沙箱。
