# 实际验收记录

本记录覆盖 2026-10-31 至 2026-11-01 在这台 macOS 机器上的运行结果。使用当前目录提供的真实 DeepSeek / E2B 凭据；凭据未写入代码或测试输出。

| 项目 | 实际结果 |
| --- | --- |
| 独立 runtime 包 | `make pack-runtime` 生成 0.3.0 tarball；核对产物使用协议 v3，包含生成后的 SDK/schema，没有 E2B npm 依赖或残留 e2b.js / terminal.js；本地服务与真实常驻进程、图片端到端验证通过 |
| 按需生成 | 删除两端生成目录与 TS 编译产物后直接 `make` 成功；`make test` 自动生成并运行 19 项 JS/TS 测试和 Go race / PostgreSQL 集成测试；Git 未跟踪生成代码、dist 或 tarball |
| 编译与静态检查 | 前端 TypeScript / Vite 构建、Go 构建、`go test ./...`、`go vet ./...` 通过 |
| Wasm 隔离 | 9 项自动检查通过：TypeScript 与 SDK、无限循环中断、禁止 Node import、异步错误、悬空 Promise 截止时间、256 MiB 内存上限、ToolCallError 类型，以及等待评审不占执行时间、等待期间 CPU 限制与取消仍有效 |
| 前端类型边界 | HTTP / SSE / WebSocket 使用明确的事件、帧、设置、MCP 模型；工具参数与结果在入口归一化，组件不使用 `any` 或二次 JSON 解析；自动测试覆盖原生 PTC 关联、失败结果和异常字段 |
| 类型与 DAO 边界 | GORM / SQL 只在 DAO；独立持久化 / 业务模型。隔离 schema 测试覆盖 typed JSONB、false 更新、错误类型在 DAO 拒绝、事务回滚、preset 约束，以及原生事件 / OAuth 扩展字段无损保存；架构测试防止业务层重新引入 DB 访问和晚期解码 |
| 常驻 Node / RPC | 真实多轮保持相同 PID；MCP、图片复用 Node；E2B 和 PTY 已迁入 Go；工作线程复用、两端 SDK 的 Content-Length 协议、schema 校验、请求取消、崩溃替换、大体积结果与嵌套回调自动测试通过 |
| Node 故障恢复 | 对真实输出中的 Node 子进程 SIGKILL，Go 自动拉起新 PID；流式输出逐项保持，活动轮次与待评审中断，新 turn 恢复；空闲 Node 被杀也主动重启。测试只创建并删除自己的会话 |
| 并发与停止 | 一个 PTC 会话停在 Plan 评审时，另一会话完成对话并恢复历史；停止评审会话保持 Node PID，不影响其他请求。工作线程测试验证卡死线程单独回收 |
| 普通对话 | 真实模型返回、增量持久化；没有创建沙箱 |
| Go E2B 适配层 | 真实 E2B：创建时 timeout-to-pause、文件写读、中文命令输出、stdout/stderr、非零退出码、取消、PTY resize/input/output/close、暂停查询不唤醒、恢复后文件保留；测试不启动 Node |
| 终端独立于 runtime | Chrome 与 WebSocket 保持连接期间，对空闲 Node 先 SIGSTOP 再 SIGKILL；期间仍可输入输出，自动重启后同一 shell 环境变量保留；中文、resize、Ctrl-C 均通过 |
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
| 大体积 MCP 响应 | 常驻 JSON-RPC 保留了等待 stdout 写完的修复；本地真实 HTTP MCP 自动测试覆盖分页工具目录（约 300 KB）和约 1.6 MB 工具结果，字段逐字校验完整，无需真实凭据 |
| 凭据 / 请求边界 | API 仅返回遮罩，不返回 OAuth token；跨 Origin 请求被拒绝；已移除 HTTP 内部工具入口，RPC 回调绑定所属请求 / 会话 |
| 浏览器 | 本机 Chrome 桌面与 390px 手机视口检查；对话、设置、Skill 保存及响应式布局可用，无页面 JavaScript 异常 |
| 工具展示与折叠 | 参考 DSH Web 的工具摘要和调用树，用 HeroUI 实现整轮 / 单工具折叠；Chrome 验证 PTC 子调用去重与嵌套、命令失败和退出码、文件内容复制、Enter / Space 操作、SSE 更新保留用户折叠选择、已中断调用及 390px 布局 |
| Preset 生命周期 | PostgreSQL 隔离 schema 集成测试及 race 检查通过：空会话可选、首条消息锁定、并发异模式首次发送仅一个成功、PATCH / 部分消息参数 / SQL 均不能绕过、失败消息重试保留锁定、旧混合会话迁移保留原生历史 |
| 模型验证 | 官方更新博客、模型文档及真实 `/models` 均确认 Flash 的 API 名称为 `deepseek-flash`；产品仅提供 Flash，后端拒绝旧模型名 |
| PG 通知与 Go 迭代器 | Go 基线 1.25；DAO `iter.Seq2` 消费通知，业务 `iter.Seq` 消费合并后的唤醒。集成测试覆盖提交前不可见、回滚无通知、六类表与删除级联、schema 隔离、终止监听连接后的重连和补读 |
| SSE 空闲查询 | 同时打开列表和会话流，并运行 outbox dispatcher；跨过一次 5 秒心跳，独立测试 schema 的所有数据连接 `pg_stat_activity.query_start` 保持不变，验证没有空闲轮询查询 |
| SSE 重放与背压 | 单事务写入超过 1000 条事件和帧，仅靠合并通知完整读完全部页；验证 Last-Event-ID 重连、25 ms 通知合并、跨会话过滤、迭代器退出清理、内存 provisioning 推送、并发帧提交不跳游标 |
| 会话列表推送 | 浏览器持续订阅 `/api/sessions/stream`，观察期间没有 GET `/api/sessions` 轮询；标题修改实时到达，重新连接收到最新列表快照 |
| SSE 心跳 | LISTEN/NOTIFY 改造后，列表流与对话流实测相邻心跳分别为 4999 ms、5000 ms；数据推送不受 5 秒心跳间隔影响 |
| 图片 | Chrome 文件选择、粘贴、缩略图、大图预览、移除通过；Flash 真实识别测试图文字 `NIMBUS 4729`，原生 user/message 包含 image 附件；刷新后图片可见，后续轮次恢复附件并回答正确，未创建 E2B 沙箱 |
| 图片边界 | 数据库集成测试覆盖跨会话图片读取与引用、伪造格式、无效 / 重复图片 ID、数量上限、纯图片消息及删除会话级联清理图片 |
| 图片重启恢复 | 第一轮仅让模型回复“收到”；停止后端并删除测试会话的本地轮次缓存，重新启动后仍可读取图片、preset 仍锁定，第二轮 Flash 正确读出此前未转写的 `ORBIT 5938`；证明图片从 PostgreSQL 恢复 |

## 重跑

服务启动后，顺序执行，避免崩溃恢复测试影响其他测试：

```sh
node scripts/e2e.mjs
node scripts/features-e2e.mjs
node scripts/invariants-e2e.mjs
node scripts/runtime-e2e.ts
node scripts/terminal-e2e.ts
node scripts/recovery-e2e.mjs
node scripts/browser-check.mjs
node scripts/session-updates-e2e.mjs
node scripts/plan-e2e.mjs
node scripts/tool-ui-check.mjs
```

Preset、模型迁移、图片归属、计划评审，以及 PG 通知 / SSE 空闲查询与重连的数据库集成测试使用独立临时 schema，不调用模型、不改动现有会话：

```sh
NIMBUS_TEST_DATABASE_URL='postgres://nimbus@127.0.0.1:55432/nimbus?sslmode=disable' make test
go vet ./...
```

`npm test` 共 19 项，还验证运行时在加载 Harness 前拒绝不一致的会话 preset 与旧模型名、超过管道缓冲区的大体积 MCP 工具目录和结果传输，以及工具调用树关联、重放、退出码与待完成状态。`scripts/plan-e2e.mjs` 使用本机 Chrome，只回答其自身创建的测试会话，并在退出时删除这些会话。`scripts/tool-ui-check.mjs` 使用浏览器内模拟事件，不调用真实模型，也不修改现有会话。

`internal/sandbox` 的 Go 集成测试通过 `NIMBUS_TEST_E2B_KEY` 选择性启用真实 E2B，创建并删除自己的沙箱。`scripts/terminal-e2e.ts` 在现有用户会话都空闲时才中断 Node，使用本机 Chrome，并清理自己的会话和沙箱。

`internal/runtimeprotocol` 通过真实 SDK 测试请求 / 返回 schema、缺失字段和断连回调清理；`internal/executor` 的 Go race 测试覆盖 12 个并发大消息、反向回调内再次请求 Node、输出确认顺序、单请求与单工具取消、活动 / 空闲崩溃后的主动重启，以及关闭后不重启。`scripts/runtime-e2e.ts` 使用真实 Flash / DSH / E2B 检查常驻 PID、多会话 Plan 等待、Stop、Node SIGKILL 恢复、持久化输出、Wasm 工具调用和 PTY。

MCP 需要另一个终端先运行 `node scripts/mcp-fixture.mjs`，再执行 `node scripts/mcp-e2e.mjs`。这些检查会调用真实模型 / 沙箱，产生相应使用量。恢复测试会主动结束并重新启动本地服务。测试脚本应在专用开发实例运行。

## 明确的边界

- 这是单用户、仅绑定本机回环地址的应用；不包含公网部署、登录、多租户或多实例调度。
- 常驻服务复用 Node 进程和工作线程；每轮仍新建 DSH 上下文和从数据库恢复工作目录。崩溃后不自动重放已接收的工具调用，外部服务已开始的副作用无法回滚。
- Go 二进制仅内嵌前端；`nimbus-agent-runtime` 单独作为 npm 包安装，明确依赖 Harness，运行时代码为 TypeScript。Go 只检查 Nimbus 协议并调用 runtime，不再接收 Harness 路径。需要 Node.js 24.11+；npm 包尚未发布到公共 registry，可安装构建出的 tarball。
- DSH RC.5 的公共持久化事件不包含每个 token。增量帧单独存入 PostgreSQL，恢复时提供明确标识的中断上下文。服务端 SSE 由 PostgreSQL LISTEN/NOTIFY 驱动，空闲和心跳不查库；监听重连后按游标补读。客户端不轮询，心跳独立为 5 秒。E2B reaper 仍每 30 秒检查云端生命周期。详见 ARCHITECTURE.md。
- E2B 单次同步命令当前最多执行 55 秒、输出上限 1 MiB；长任务可以在沙箱内启动后台进程，再轮询日志。沙箱内容不备份到 Mac，销毁后不能恢复。
- OAuth 已通过协议测试，也使用用户已有 Notion 授权完成了真实只读调用；没有自动执行第三方登录。第三方服务是否要求预注册 OAuth 客户端、是否接受本机 HTTP 回调，取决于该服务策略。
- 没有压测 20 个真实并发沙箱或等待完整一小时来测试云端暂停。容量锁、等待、近到期暂停与 E2B onTimeout=pause 已实现。
- E2B 使用固定版本的社区 Go SDK，隔离在 `internal/sandbox`；没有官方 Go SDK 的假设。SDK 不提供按 ID 的无唤醒生命周期接口，因此在同一适配层补充有类型的 info/pause/delete HTTP 调用。Node 已移除 E2B npm 依赖及其相关依赖链；当前 `npm audit --omit=dev` 为 0 项，前端安装审计也为 0 项。

测试截图和运行日志保存在被 Git 忽略的 `.local/` 目录。正常交付时测试用 MCP 配置和临时 Skill 会删除，保留一段可查看的 E2B 验证对话及其已暂停沙箱。
