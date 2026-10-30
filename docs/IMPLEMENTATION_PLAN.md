# Nimbus Cloud Agent 实现计划

> 依据：技术方案 v12（PG 唯一真相源、发件箱送达、Sandbox 工具化、E2B 沙箱 0..1 懒创建、Wasm PTC 运行时、
> Go + Gin + GORM + Viper + Cobra、React 19 + HeroUI v3 + Tailwind v4）。
>
> v11 改动要点：执行层不再是自维护的 QEMU/KVM 虚拟机，改为直接接入 E2B API（Hobby 档）。
> v12 改动要点：PTC 运行时仍在宿主机上，程序跑在 Wasm 引擎内（第一期 TypeScript 经 Wasm 内 QuickJS，Wasm 内存上限 256 MiB / 实例，超时默认 120 秒、上限 600 秒），不原样使用 DSH 自带 Node PTC 运行时；Sandbox 与 MCP 只是 PTC 经 SDK 同层级调用的两套工具。
> 本计划中凡写 VM 处均指 E2B 沙箱，不再有 QEMU/KVM、后端 Hub + VM relay、本地 overlay 磁盘。
>
> 本文件只是一份实现计划，**不包含任何业务代码**。此前在本地搭过的
> `web/` 前端 mock 原型也不属于本次交付，**不需要提交**；前端在第 6 阶段
> 按 HeroUI v3 + 真实 API 重新实现。
>
> 总原则：**后端先行、前端后行；每一步都可验证、可演示，验证通过才进入下一步。**
> 每一阶段末尾都有「本阶段看得见的效果」和「交付标准（验收清单）」，
> 验收清单有一项未通过，就不进入下一阶段。

---

## 总览：阶段与依赖

| 阶段 | 目标 | 前置 | 本阶段看得见的效果 |
| --- | --- | --- | --- |
| 0 | 工程骨架与配置 | — | 一个能跑、能打印版本、能连 PG 的二进制 |
| 1 | Session + 发件箱 + 事件 + SSE | 0 | 不用前端，只用 curl 就能建会话、发消息、看流式事件 |
| 2 | DSH 接入与 PG 同步 | 1 | 真实模型跑起来，历史落 PG，关机重开历史还在 |
| 3 | Wasm PTC 运行时 + Sandbox 工具化与宿主机能力剥离 | 2 | 宿主机上跑不出 `rm`/`bash`，PTC 程序在 Wasm 里跑，E2B 沙箱里才能改文件、跑命令 |
| 4 | Skill（PG）+ MCP（streamable-http + OAuth） | 2 | 页面/接口能配 Skill 和 MCP，Agent 能调 MCP 工具 |
| 5 | E2B 沙箱生命周期：懒创建、暂停、恢复、销毁 | 3 | 只调 MCP 的会话全程 0 个沙箱；一调 Sandbox 工具就建 E2B 沙箱 |
| 6 | 前端：React 19 + HeroUI v3，接真实 API | 1–5 | 浏览器里能完整走一遍聊天、发件箱、终端、设置 |
| 7 | 端到端收尾：单二进制、测试、发布 | 6 | 一台干净机器上只装 PG、装好 DSH、配好 E2B key 就能跑起来 |

依赖规则：

- 第 1 阶段不依赖 DSH、E2B 沙箱、前端，先用桩（stub）Agent 循环把链路跑通。
- 第 3 阶段不依赖第 4 阶段；Skill / MCP 可以在 Sandbox 工具化之后再接。
- 第 6 阶段开始前，后端接口必须已经按第 9 节（技术方案）冻结；前端不对未冻结的接口做适配。

---

## 第 0 阶段：工程骨架与配置

**目标**：先有一个能编译、能启动、能配、能连库的后端骨架，后面所有阶段都往这个骨架里加东西，不返工。

**做：**

1. Go module、目录结构按技术方案第 11 节落位：
   `cmd/nimbus`（Cobra 入口）、`internal/config`、`internal/server`（Gin）、
   `internal/store`（GORM）、`internal/session`、`internal/e2b`（E2B API 客户端）、`internal/executor`。
2. Cobra 命令：根命令 + `serve` + `version` 子命令；裸跑等价于 `serve`。
3. Viper 配置，优先级写死：flag > `NIMBUS_` 环境变量 > `config.yaml` > 默认值。
   必填值只有三项：`--postgres`、`--harness-path`、`--e2b-api-key`；缺任何一项启动即报错退出，
   不以默认值悄悄跑起来。
4. GORM 接 PostgreSQL：连接池、连接失败重试一次后明确报错、迁移（migration）
   机制（只用迁移改表，**生产表不用 AutoMigrate**）。
5. 健康检查：`GET /healthz`（只返回进程与 PG 连通性，不暴露敏感信息）。
6. 日志与错误格式统一：请求级 `request_id`、错误码与可读信息分开，
   敏感字段（API key、token）一律不打日志。

**验证：**

```sh
go build -o nimbus ./cmd/nimbus
./nimbus version
./nimbus serve --postgres "$PG_DSN" --harness-path /path/to/dsh --e2b-api-key "$E2B_API_KEY"
curl -s http://127.0.0.1:8080/healthz
```

**交付标准：**

- [ ] `go build` 通过，`go vet` 无新增告警。
- [ ] 缺 `--postgres`、`--harness-path` 或 `--e2b-api-key` 时启动失败且报错指明缺哪一项。
- [ ] PG 连不上时启动失败，报错可定位到连接环节（不是空指针或超时悬着）。
- [ ] `/healthz` 在 PG 正常时返回 200，PG 断开时返回非 200。
- [ ] 迁移可重复执行（up 后再 up 不报错），可回滚到上一版。

---

## 第 1 阶段：Session + 发件箱 + 事件 + SSE（无 DSH、无 E2B 沙箱、无前端）

**目标**：把“一个会话是怎么活着的”这条主链路先跑通，而且**不用前端、不用模型、不用 E2B 沙箱**就能验证。

**做：**

1. 数据模型按技术方案第 5 节落表：
   - `sessions`：只存会话字段 + `agent_state`，**不含任何沙箱字段**。
   - `session_events`：append-only 事件流，PG 为唯一真相源。
   - `outbox_messages`：发件箱，`turn_id` 由后端在写入发件箱时生成，
     一个 turn 严格对应一条发件箱消息，`client_msg_id` 做幂等。
   - `sandboxes`：独立表，存 `e2b_sandbox_id` 与 `session_id` UNIQUE，本阶段可以先建表但不建行
     （沙箱行要到第 5 阶段才懒创建）。
2. 接口（Gin 路由组，先做最小集）：
   - `POST /api/sessions`：建会话。
   - `GET /api/sessions`：列表（含派生的沙箱状态：无 `sandboxes` 行即 `not_started`）。
   - `POST /api/sessions/:id/messages`：只写发件箱，返回 202，不直接跑模型。
   - `GET /api/sessions/:id/events`：只读 PG，不唤醒任何东西。
   - `GET /api/sessions/:id/events/stream`：SSE，支持 Last-Event-ID 续传，
     推送 `session_events` 与 `outbox` 事件。
   - `POST /api/sessions/:id/messages/:msgId/retry`：发件箱重发，沿用同一 `turn_id`。
3. 发件箱状态机写死：`queued → starting → delivered / failed`。
   送达的定义写死：消息写入 session 文件**并且**同步回 PG 的那一刻才算 `delivered`，
   之前一律是未送达；`starting` 的定义是后端取出消息、开始 turn 启动时置位，
   SSE 同步推送 `starting` 事件。
4. 先用一个桩 Agent 循环代替 DSH：收到发件箱消息后按顺序走
   `queued → starting → delivered`，再回一条固定的流式文本（例如
   “stub reply”），只用来验证发件箱、事件、SSE 这条链路，不冒充真实模型。
5. 删除会话：删 PG 记录，`session_events` / `outbox` / `executors` 靠
   `ON DELETE CASCADE` 连带删掉；本阶段无沙箱行可删，删除语义先按无沙箱走通。

**验证（全部只用 curl / psql，不开浏览器）：**

```sh
# 建会话、发消息、看 SSE、看 PG 事件数、重发一条失败消息
curl -s -X POST localhost:8080/api/sessions -d '{}'
curl -s -X POST localhost:8080/api/sessions/<id>/messages -d '{"text":"hi"}'
curl -N localhost:8080/api/sessions/<id>/events/stream
psql "$PG_DSN" -c "select count(*) from session_events where session_id='<id>';"
```

**本阶段看得见的效果**：终端里能看到一条消息从排队、启动中变成已送达，
SSE 里能看到事件一条条流出来，PG 里能查到对应的事件行。

**交付标准：**

- [ ] 发消息只写发件箱、返回 202，不会因为模型没接好就 500。
- [ ] `queued → starting → delivered` 的顺序在 SSE 与 PG 里一致，无跳号、无乱序。
- [ ] 同一条 `client_msg_id` 重复提交只产生一条发件箱消息（幂等）。
- [ ] 失败消息能重发，重发沿用同一 `turn_id`，不产生新 turn。
- [ ] 断开 SSE 再用 Last-Event-ID 重连，能从断点续传，不丢、不重。
- [ ] 删会话后，PG 里该会话的事件、发件箱、执行器记录全部消失。
- [ ] 历史查询全程不唤醒、不创建任何沙箱（查 `sandboxes` 表行数为 0）。

---

## 第 2 阶段：DSH 接入与 PG 同步（真实模型跑起来）

**目标**：把第 1 阶段的桩换成真正的 DeepSeek Harness，跑通“模型真的在说话、
历史真的在 PG 里”。

**做：**

1. 启动时校验 `--harness-path` 指向的 DSH 版本，钉死 **0.2.1-rc.5**
   （以实测过的版本为准，升级要单独走一次验证，不悄悄跟最新版）。
2. 按发件箱串行取消息：一次只取一条，取到后置 `starting`，再准备 turn。
3. Turn 的 session 文件是**工作文件**，不是真相源：
   - turn 开始前：从 PG 事件重建一份新的 session 文件
     （用 DSH 自己的 session API：create + 按顺序 append 事件 / fork-seed，
     **不由 Go 代码手写 zstd 文件**）。
   - turn 活跃时：DSH 把文件写到本地，同步插件实时流式同步进 PG。
   - turn 结束时：必须等 PG 同步追平（PG 的 max seq = 文件的 max seq）
     才算真正结束；追平前不把 turn 标为结束。
   - turn 结束后：本地工作文件封存不删（第一期不做删除与归档），
     路径固定为 `<work-dir>/<session_id>/<turn_id>/`，每 turn 一个新目录，
     永不复用旧目录。
4. 崩溃语义写死：崩溃 = 该 turn 结束、标为“已中断”，历史只保留到
   PG 已同步的位置，未同步的尾巴按“从未发生”处理；用户发新消息
   = 在同一会话里开一个新 turn，从 PG 重建，不复活已崩溃的 turn，
   不提供“恢复”按钮。
5. 模型、思考档位、Plan 开关按 turn 生效：
   模型（DeepSeek Chat / Reasoner）、思考档位（`off / low / high / max`，
   其他值直接 400，不发明 `medium`）、Plan 开关（DSH 原生行为，不做后端额外拦截）。
6. 桩循环下线：同一套发件箱 + SSE + 事件代码路径上跑真实 DSH，
   不留两套并行逻辑。

**验证：**

```sh
# 真实发两条消息，第二条在第一条结束后发；中途 kill 后端再重启
curl -s -X POST localhost:8080/api/sessions/<id>/messages -d '{"text":"你好"}'
# 观察：流式回复完整、PG 事件数与文件 seq 一致、重启后历史可读且 turn 标为已中断
psql "$PG_DSN" -c "select seq, type from session_events where session_id='<id>' order by seq;"
```

**本阶段看得见的效果**：用 curl 发消息能收到真实模型的流式回复，
后端重启后，不用起沙箱也能从 PG 读回完整历史。

**交付标准：**

- [ ] 真实模型回复能流式到达 SSE，流式内容与最终保存内容一致（不跳、不丢、不重排）。
- [ ] turn 结束时 PG 已追平；故意在追平前 kill 进程，重启后该 turn 标为已中断，
      且 PG 里没有追平之后才出现的内容。
- [ ] 崩溃后发新消息能开新 turn，历史连续，旧 turn 不被复活。
- [ ] 思考档位只接受 `off / low / high / max`，传 `medium` 返回 400。
- [ ]  session 文件重建走 DSH 自身 API，不出现手写 zstd 文件的代码路径。
- [ ] DSH 版本不是 0.2.1-rc.5 时启动即报错，不静默兼容。

---

## 第 3 阶段：Wasm PTC 运行时 + Sandbox 工具化与宿主机能力剥离（安全边界）

**目标**：这是 v9 推翻旧设计后的核心一节，也是 v11 之后 Wasm PTC 层的落地点——
**PTC 运行时仍在宿主机上，但程序跑在 Wasm 里；先把宿主机能力拿掉，
再给 Agent 一套只能经 SDK 调用的工具**。这一阶段不过，后面接什么工具都不安全。

**做：**

1. 剥离（fail-closed，启动时断言；这是用户要的唯一防护，不再加别的）：
   - DSH 自带的本地 fs / bash / 子进程 / sandbox provider 一律不注册。
   - 宿主机文件既不能改、也不能读、也不能执行命令；启动时检查工具目录：
     只要出现本地文件读写或本地命令执行能力，启动即失败，不降级跑起来。
   - DSH 自带的 Node PTC 运行时不原样使用：若检测到它可直接 import fs / child_process，
     启动即失败；改用自定义 Wasm PTC 运行时，经 tool-presentation mode ptc 组合接入。
2. Wasm PTC 运行时（宿主机上，程序跑在 Wasm 引擎内，不搬进 E2B 沙箱）：
   - 引擎为 wasmtime 类 Wasm 引擎，第一期语言为 TypeScript，经跑在 Wasm 内的 QuickJS 执行；
     Python 经 Pyodide 列为后续扩展，不在本期实现。
   - WASI 能力默认全禁、白名单放行：不授予宿主机目录、不授予任意网络、
     不授予进程创建、不授予环境变量读取；程序的直接文件读写、直接联网、spawn/exec
     在 Wasm 层即不存在，不靠事后检查拦截。
   - 程序产生副作用的唯一通道是 SDK 宿主函数，由宿主机上的受信任监督进程接收并校验后执行；
     Sandbox 工具与 MCP 工具在同一层级：前者背后是 E2B 沙箱，后者背后是宿主机上的 MCP 客户端，
     OAuth token 只存在监督进程与后端一侧，永不进入 Wasm 程序环境；Wasm 程序不得直连 MCP 服务器，
     不得从沙箱内发起 MCP 请求。
   - Wasm 程序超时与 PTC 原有 120 秒默认、600 秒上限一致（这是 Wasm 执行超时，不是 E2B 沙箱超时）。
   - 后端进程本身不直接替 Agent 跑宿主机命令；所有执行请求只走 Sandbox 工具。
2. Sandbox 工具集（PTC 经 SDK、standard 直接调用，共用同一隔离边界）：
   - `sandbox_status`：查当前会话的 E2B 沙箱状态（无行即 `not_started`）。
   - `sandbox_start`：手动拉起 E2B 沙箱（不调它也不会自动起，见第 5 阶段）。
   - `sandbox_exec`：在 E2B 沙箱里执行命令，返回 stdout / stderr / 退出码。
   - `sandbox_read_file` / `sandbox_write_file`：读写 E2B 沙箱里的文件，
     沙箱内任何路径都允许，不做工作区子目录的额外限制；唯一边界就是沙箱本身，
     出沙箱才拒绝。
3. 执行通道：后端直接调 E2B API（创建 / 命令 / 文件 / 暂停 / 恢复 / 销毁），
   不再有后端 Hub + 沙箱 relay；E2B 沙箱内的执行环境永远不能路由到宿主机的 Harness / 后端控制面，
   E2B API key 不进沙箱。
4. Plan 模式：保持 DSH 原生行为，不由后端额外拦截——Plan 开着时 Agent
   要写文档、写文件就正常调 Sandbox 工具写进 E2B 沙箱，不做执行层拦截。
5. 固定数值（不再写“建议”）：
   - 单次 `sandbox_exec` 输出截断上限 **1 MiB**（超限截断并标注）。
   - E2B API 调用超时 **60 秒**，超时即失败，不无限等。

**验证：**

```sh
# 1) 宿主机逃逸测试：让模型/工具尝试在宿主机上 touch / rm / bash，必须全部被拒绝
# 1b) Wasm 逃逸测试：让 PTC 程序在 Wasm 内尝试 import fs、读写宿主机文件、spawn、直连网络、直连 MCP，
#     必须全部在 Wasm 层即失败；经 SDK 调 sandbox_* 与 mcp__* 则必须成功
# 2) 沙箱内测试：sandbox_exec 在 E2B 沙箱里写文件、读文件、跑命令，全部成功
# 3) Plan 开启时，sandbox_write_file / sandbox_exec 仍可正常写进 E2B 沙箱（DSH 原生行为，不拦截）
```

**本阶段看得见的效果**：同一条“写文件”指令，在宿主机侧被挡下来，
在 E2B 沙箱侧能正常写进去；Plan 开着时写操作也不被后端拦截，按 DSH 原生行为走。

**交付标准：**

- [ ] Wasm PTC 程序的直接文件读写、直接联网、spawn/exec 全部在 Wasm 层失败，
      经 SDK 的 Sandbox 与 MCP 调用全部成功且 token 不进 Wasm 程序环境。
- [ ] 宿主机文件读写、宿主机命令执行、宿主机子进程三条路全部被堵死，
      且启动时断言能发现其中任何一条被重新注册。
- [ ] `sandbox_exec` / `sandbox_read_file` / `sandbox_write_file` 只在 E2B 沙箱内生效，
      返回结果与沙箱内实际状态一致。
- [ ] 路径越界（写到沙箱工作区之外）直接拒绝，不做静默改写。
- [ ] Plan 开启时写操作不被后端拦截，Agent 可正常写进 E2B 沙箱。
- [ ] E2B 沙箱不能用来访问宿主机控制面（用例：沙箱内尝试连宿主机端口，全部失败）。
- [ ] 输出超 1 MiB 时被截断并标注，不会把整段输出塞爆事件流。

---

## 第 4 阶段：Skill（PG）+ MCP（streamable-http + OAuth）

**目标**：Agent 除了聊天和跑命令，还能用上页面里配的 Skill 和外部 MCP 服务，
而且凭证不进 E2B 沙箱。

**做：**

1. Skill：
   - 新写一个 PG Skill Provider（不走 DSH 自带的文件系统 provider，
     不把 Skill 文件物化到磁盘、不放进 E2B 沙箱）。
   - `skills` / `skill_files` 表，硬限制写死：每 Skill 最多 **100 个文件**、
     仅文本文件、每文件最大 **5 MB**；超限整单拒绝，不静默截断/改名/丢弃。
   - Skill 管理接口：创建 / 编辑 / 启用 / 禁用 / 删除，改动从下一个 turn 生效。
2. MCP：
   - 只支持 streamable-http，STDIO 配置直接拒绝并提示“仅支持 HTTP Stream 地址”。
   - `mcp_servers` 表存配置；OAuth 由后端跑流程、存 token、按 turn 刷新，
     并把 `Authorization` 头注入 DSH 的 MCP 配置，DSH 不感知 OAuth。
   - OAuth 状态机：`none | pending | ok | expired`，过期有重新授权入口，
     header / key 类凭证在界面上掩码显示。
   - MCP 客户端位置写清楚：它在**宿主机 DSH 进程内，不在 E2B 沙箱内**，
     也不经 Hub / relay 转发；E2B 沙箱的网络隔离不覆盖 MCP 出站流量，
     如需限制 MCP 出站目标，要在宿主机侧另外做 allowlist（本阶段先把
     位置与风险写进文档与接口注释，allowlist 本身可列为后续项，不阻塞本阶段）。
3. MCP 工具在模型侧的呈现：按技术方案第 7 / 9 节走——PTC 经 SDK、
   standard 直接调用，与 Sandbox 工具共用同一套“工具可感知”的模型，
   但 MCP 调用本身不进 E2B 沙箱、不经过 Sandbox 工具。

**验证：**

```sh
# Skill：建一个 101 个文件的 Skill、一个二进制文件、一个 6MB 文件，三个都应被拒绝
# MCP：配一个 streamable-http 的 MCP、一个 STDIO 的 MCP，前者可用、后者被拒绝
# OAuth：走一遍授权 → ok → 故意让 token 过期 → expired → 重新授权 → ok
```

**本阶段看得见的效果**：不改代码，只在接口/数据库层面就能配好一个 Skill
和一个 MCP，然后让模型在一次对话里真的调到 MCP 工具并返回结果。

**交付标准：**

- [ ] Skill 超限三类（101 文件、二进制、超 5MB）全部整单拒绝，无静默处理。
- [ ] Skill 改动只从下一个 turn 生效，当前 turn 不中途换 Skill。
- [ ] STDIO 的 MCP 配置被拒绝且提示文案明确。
- [ ] OAuth 从授权、ok、过期、重新授权全链路走通，token 不出现在日志与 E2B 沙箱里。
- [ ] MCP 工具调用能返回结果并记入事件流；MCP 挂掉时 turn 正常标错，不悬着。

---

## 第 5 阶段：E2B 沙箱生命周期：懒创建、暂停、恢复、销毁

**目标**：把沙箱从“会话一建就起”改成“需要时才起”，并且把
“一个会话最多一个沙箱”用数据库约束钉死；宿主机不再养虚拟机。

**做：**

1. 懒创建：
   - 建会话、发消息、turn 启动都不碰 E2B。
   - agent 第一次调 Sandbox 工具（或手动 `sandbox/start`）时才调用 E2B API 创建沙箱，
     再 INSERT 一行 `sandboxes`（存 `e2b_sandbox_id`），`sandboxes.session_id` UNIQUE，DB 层落实至多 1 个。
   - 无 `sandboxes` 行时，列表/详情/SSE 统一派生 `sandbox:{state:"not_started"}`，
     前端显示“未启动”，不再有扁平的沙箱状态字段。
2. 生命周期接口：`sandbox/start`（幂等、只唤醒/恢复）、`sandbox/pause`（agent 非 idle 时
   返回 409）、沙箱状态机：`not_started（派生）/ provisioning / running /
   paused / killed / error`，与 agent 状态机组合成 LEFT JOIN 派生视图。
3. 暂停与唤醒（E2B pause/resume，不再是宿主机挂起）：
   - 闲置自动暂停 **15 分钟**（固定值），调 E2B pause；paused 沙箱不计费，文件与内存保留。
   - Hobby 单沙箱连续运行上限 **1 小时**：创建时 `timeout=3600` 且 `onTimeout=pause`，
     后端在到期前 pause，不靠超时杀掉；暂停后恢复会重置 1 小时窗口。
   - 同时运行沙箱上限 **20 台**（Hobby 账号上限，固定值），超出时新 turn 等待空位，不静默超开。
4. 模板与磁盘：
   - 沙箱用 E2B 默认 base 模板；页面里的模板配置只影响之后新建的沙箱
     （**包括存量 session 之后懒创建的沙箱**），不影响存量沙箱；无效模板要报错，不静默用默认顶替。
   - 单沙箱磁盘 **10 GiB**（Hobby 固定值），不再有旧的本地 overlay 磁盘与本地 copy-on-write 用量统计；
     默认 2 vCPU（上限 8 vCPU，内存上限 8 GiB），默认内存数值以运行中沙箱 `get_info()` 的
     `cpu_count` / `memory_mb` 为准。
   - 默认区域 **US-WEST-1**；APAC（含新加坡 ap-southeast-1）需 Pro 及以上且 support 开通，Hobby 不承诺可换区。
5. 清理与回收：
   - turn 目录只增不减，本阶段不做删除；外部过期清理逻辑单独列出
     （保留 **30 天**、删除前按目录名 `session_id/turn_id` 对照 PG、第一期不做归档），
     服务本身只保证每次用新目录。
   - 删会话：先调 E2B kill 销毁沙箱，再删 sessions 行连带删 `sandboxes` 行；已 kill 的沙箱内文件即丢失，
     这一点写进文档与错误提示。
   - 回收器以 `sandboxes` 表为驱动表做 JOIN，不以 sessions 表为驱动表。

**验证：**

```sh
# 只调 MCP 的会话：全程查 sandboxes 表行数为 0，前端显示未启动
# 第一次调 sandbox_exec 的会话：sandboxes 表出现 1 行，E2B 沙箱建起来，文件写进沙箱
# 闲置 15 分钟：沙箱自动暂停；再发消息/手动 sandbox/start：沙箱恢复，文件还在
# 第 21 个沙箱：新 turn 等待空位，不超开
```

**本阶段看得见的效果**：一个只查 MCP 的会话全程没有沙箱；
一个要改文件的会话在第一次需要时才“长出”一个 E2B 沙箱，暂停后再恢复文件还在。

**交付标准：**

- [ ] 只调 MCP 的会话全程 0 个沙箱，`sandboxes` 表无行，UI 显示未启动。
- [ ] 第一次 Sandbox 调用必然创建且仅创建 1 行 `sandboxes`，重复调用不重复建。
- [ ] 闲置 15 分钟自动暂停，恢复后文件、工作目录、已装软件都在。
- [ ] 暂停时 agent 非 idle，`sandbox/pause` 返回 409，不强行暂停。
- [ ] 同时运行沙箱到 20 台上限时，第 21 个 turn 等待空位且状态可查。
- [ ] E2B API 失败或额度耗尽时新沙箱创建失败且报错可定位。
- [ ] 删会话后 E2B 沙箱已 kill、`sandboxes` 行全部清理干净。

---

## 第 6 阶段：前端：React 19 + HeroUI v3，接真实 API

**目标**：把浏览器里的页面从“看效果”变成“真的能用”，
而且**不复用此前本地 mock 原型的代码**——按 v8 技术方案重做。

**做：**

1. 前端栈钉死：React **19.3.1**、@heroui/react **3.2.7**、
   Tailwind CSS **4.3.3**、Vite **8.3.5**、TypeScript **7.0.2**。
   HeroUI v3 写法：**不再需要 HeroUIProvider**；
   样式只按 `@import "tailwindcss"` 再 `@import "@heroui/styles"`；
   Tailwind v4 只走 @tailwindcss/vite 插件。
   组件按 v8 对应表：Button、Textarea、Input、Select、Switch、Chip、
   Spinner、Accordion、Modal（复合组件写法）、Tabs（仅在确实需要时用）。
2. 布局：桌面优先、ChatGPT 式——左侧可收起窄边栏（session 列表）、
   中间居中聊天主列（最大宽 720px）、顶部极简、输入框底部居中；
   **不做手机 tab 适配**。
3. 接真实 API，替换掉 mock 层：
   - session 列表、建会话、发消息（只写发件箱、返回 202）、SSE 流式事件、
     Last-Event-ID 续传、重发（沿用同一 `turn_id`）。
   - 发件箱展示：PG 记录与发件箱未送达消息合并展示，未送达转圈、
     失败红色感叹号 + 一键重发；`starting` 状态 SSE 同步推送。
   - 模型（Chat / Reasoner）、思考档位（`off / low / high / max`）、
     Plan 开关、全局设置面板（API key 掩码显示、基础镜像、Skill、MCP）。
   - 沙箱展示：无 `sandboxes` 行即“未启动”，有行则显示嵌套 `sandbox:{state}`；
     沙箱信息、PG 同步 seq、Skills / MCP、终端收进默认隐藏的详情抽屉，
     各节可折叠。
   - Web 终端：xterm.js 经 WebSocket 连 E2B 沙箱，终端改动不记入对话历史，
     且要提示“终端与 Agent 共用同一个 E2B 沙箱”。
4. 前端不直接连 PG、不直接连 DSH、不直接连 MCP；所有数据只走后端 Gin 接口。

**验证：**

```sh
cd web && npm install && npm run build && npm run preview
# 浏览器里手动走一遍：建会话 → 发消息（看 queued → starting → delivered）
# → 流式回复 → 失败重发 → 暂停/恢复 → 终端输入 → 设置页改模型/思考档位
```

**本阶段看得见的效果**：浏览器里能完整走一遍真实流程，
不再是 mock 数据在自娱自乐；关掉前端重开，历史还能从 PG 读回来。

**交付标准：**

- [ ] 前端构建产物能嵌进 Go 单二进制（见第 7 阶段），不依赖本地 dev 服务器。
- [ ] 发消息、流式回复、失败重发、续传四条主链路在浏览器里全部走通。
- [ ] 思考档位下拉只有 `off / low / high / max`，没有 `medium`。
- [ ] 无沙箱的会话显示“未启动”，不是空白或报错；有沙箱的会话显示真实状态。
- [ ] 终端能输入并看到 E2B 沙箱里的回显，且终端改动不出现在对话历史里。
- [ ] API key 在界面上掩码显示，不以明文出现在页面源码或前端日志里。

---

## 第 7 阶段：端到端收尾：单二进制、测试、发布

**目标**：把前面六个阶段拼成一个能交给别人的东西——
一台干净机器、一个二进制、两项外部依赖，就能跑。

**做：**

1. 单二进制：前端构建产物嵌进 Go 二进制（`web/dist/` 经 Makefile 的
   `web` 目标构建），只暴露一个服务端口；DSH **不打包**，
   用户自己装在宿主机上并用 `--harness-path` 传入（默认构建不捆绑 Harness，
   显式 `make bundle-harness` 才算 opt-in，且须保留 Harness 的 MIT LICENSE）。
2. 一键验证脚本：写一个 `scripts/smoke.sh`（或等价物），在一台机器上按顺序跑：
   建会话 → 发消息 → 流式回复 → 调 MCP → 调 Sandbox 工具建 E2B 沙箱 →
   暂停 → 恢复 → 删会话，每一步断言返回码与 PG 状态，任何一步失败即停。
3. 测试补齐：后端单元测试覆盖发件箱状态机、PG 追平门、Sandbox 越界拒绝、
   Skill/MCP 超限拒绝；前端构建测试（`npm run build`）进 CI。
4. 发布：tag 触发 release 工作流，产出 Linux 单二进制 + sha256，
   发布说明里写清外部依赖只有 PostgreSQL 与 E2B API、DSH 需自装、
   以及本版不支持的项（见下）。
5. 文档收尾：README、ARCHITECTURE、SECURITY、CONTRIBUTING 与技术方案
   v12 的版本号、固定数值表、决策表保持一致，不留“建议/待定”措辞。

**本阶段不支持的项（明确写进发布说明，不假装已实现）：**

- 多用户与鉴权（本期单用户 = 管理员，无登录、无权限检查）。
- 一个 session 多个沙箱、多个 session 共享沙箱（架构留了扩展点，本期软限制 0..1 个）。
- 沙箱内文件不经终端的浏览（workdir 文件浏览列为第 2 期）。
- 内存快照归档、turn 目录归档（第一期只做 30 天外部过期清理）。
- DSH 升级到稳定版（本期钉死 0.2.1-rc.5）。

**验证（干净机器验收，缺一不可）：**

```sh
# 在一台只装了 PostgreSQL 16+ + 自装 DSH 0.2.1-rc.5、配好 E2B API key 的机器上：
./nimbus serve --postgres "$PG_DSN" --harness-path /path/to/dsh --e2b-api-key "$E2B_API_KEY" --e2b-api-key "$E2B_API_KEY"
./scripts/smoke.sh   # 全绿才算通过
```

**交付标准：**

- [ ] 干净机器验收全绿：建会话、发消息、流式回复、MCP、Sandbox 建 E2B 沙箱、
      暂停、恢复、删会话全部通过。
- [ ] 单二进制里不含 Harness（默认构建），`--harness-path` 校验版本，
      版本不对即报错。
- [ ] `scripts/smoke.sh` 任一步失败即停，且失败信息能定位到阶段。
- [ ] 发布产物有 sha256，发布说明里的“不支持项”与上表一致。
- [ ] 技术方案、README、ARCHITECTURE 三处的版本号与固定数值表一致，
      全文无“建议/推荐/待定/待验证”残留。

---

## 风险与回退

| 风险 | 影响 | 应对 |
| --- | --- | --- |
| DSH 0.2.1-rc.5 是 RC 版，API 可能变 | 第 2、3 阶段返工 | 只用通用、稳定的插件/Provider API；升级 RC 时单独走一次第 2 阶段验证 |
| PG 同步追平门拖慢长会话 | 每 turn 都要完整水化 + 追平 | 先接受该代价（技术方案已定），后续再优化增量水化，不在本计划内偷换语义 |
| E2B 沙箱懒创建后首次 Sandbox 调用变慢 | 用户体验：第一次改文件要等沙箱起 | 前端在 `starting` 状态明确提示“正在启动沙箱”，不假装已执行 |
| HeroUI v3 与 React 19 的 API 变动 | 第 6 阶段返工 | 以 v8 文档里的复合组件写法为准，不混用 v2 写法；构建失败先查 v3 迁移说明 |
| 宿主机能力剥离漏网 | 安全边界失效 | 第 3 阶段的启动断言 + 逃逸用例是硬门槛，不过就不进入第 4 阶段 |

---

## 附：每阶段的“停下来检查”清单（给执行人用）

每进入下一阶段前，先回答这三个问题，答不上来就停下来：

1. 上一阶段的交付标准是不是**全部**打勾了？（不是“差不多都过了”。）
2. 上一阶段看得见的效果，能不能在**不依赖下一阶段**的情况下演示出来？
3. 有没有为了赶进度，把“建议/待定”写进了代码或文档？有就先清掉再往下走。
