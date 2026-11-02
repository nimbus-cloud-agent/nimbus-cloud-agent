---
name: nimbus-dev-ops
description: 使用 Render、Neon 和 GitHub CLI 运维当前 Nimbus 项目的 dev 环境，查看配置、日志、实例与休眠状态，并执行已授权的发布、重启和启停。用于 Nimbus dev 排障和环境维护。
---

# Nimbus dev 运维

从项目根目录执行。使用官方 `render`、`neon` CLI，GitHub Actions 使用 `gh`。Render CLI 未覆盖的环境变量和 Web 挂起/恢复由本技能的补充终端脚本完成，不依赖浏览器操作平台。

## 环境定位

资源基线核实于 2026-11-02；运行状态、镜像、套餐、发布开关每次重新查询。

| 对象 | 定位 |
| --- | --- |
| GitHub 仓库 / 分支 | `nimbus-cloud-agent/nimbus-cloud-agent` / `dev` |
| Render workspace | `tea-dbjjrd142hec73d81l60` |
| Render Web | `nimbus-dev` / `srv-dbjl0pe7bikc73fplma0`，新加坡，基线 Free |
| 应用 | `https://nimbus-dev-8zy2.onrender.com` |
| Neon org / project | `org-dry-feather-16104029` / `silent-field-76474041`，项目名 `nimbus` |
| Neon branch | **`production`** / `br-morning-firefly-aolcg06f`，对应应用 **dev**，不是 Git 分支名 |
| Neon compute | `ep-royal-sound-aobqyzhu`，`aws-ap-southeast-1`，基线 Free / PostgreSQL 18 |
| 数据库 | `neondb`；旧 Render `nimbus-postgres` 已删除，不再连接或重建 |

## 按任务读取

- Render 配置、日志、实例、发布、重启、挂起/恢复：读 [references/render.md](references/render.md)。
- Neon 配置、连接、迁移、日志能力、数据库诊断：读 [references/neon.md](references/neon.md)。
- 休眠和跨平台排查：先读下面共同约束，再读相关平台参考。
- 仓库声明与实现：`docs/deployment-render.md`、`render.yaml`、`neon.ts`、`.github/workflows/ci.yml`。声明文件不能替代线上实际配置。

## 开始检查

```sh
render --version
neon --version
render workspace current -o json
neon config status --current-branch
env -u GITHUB_TOKEN gh variable list --json name,value
```

基线 CLI 为 Render 2.34.0、Neon 8.11.4；命令随版本变化时先读 `--help`。定位或认证失败按参考文档处理，不另建项目。

## 共同约束

- 本技能不增加线上写权限。查看状态/配置/日志用只读命令；用户明确要求的发布、重启、启停直接按范围执行。删除、重置、改套餐需被当前请求覆盖，过去“测试数据可删”不作为未来删除授权。
- 不保存或回显 API key、完整 DSN、密码、Deploy Hook、认证头。复用已有凭据，缺失才请用户登录。`.env`、`.env.local`、`.neon` 被忽略；凭据文件仅当前用户可读。
- 架构是 Go/Gin + pgx/GORM，Render 运行 GHCR 预构建镜像，Neon 只承载 PostgreSQL；不为了运维额外部署 Functions、Auth、Data API。
- 启动执行嵌入式迁移，使用会话 advisory lock 和 `LISTEN/NOTIFY`，必须使用 **直连** `DATABASE_URL_UNPOOLED`，保留 TLS 参数，不能用 `-pooler` 事务池。
- 本地优先级：`--postgres`、`NIMBUS_POSTGRES`、`DATABASE_URL_UNPOOLED`、`DATABASE_URL`。`npm start` 加载 `.env.local`、`.env`，已导出的变量优先。Render 的 `NIMBUS_POSTGRES` 保存 Neon 直连值；本地 env pull 不会更新 Render。
- 部分 UI 设置保存在数据库 `settings`，如模型密钥可能覆盖环境变量回退值。检查有效配置要区分仓库声明、Render 环境变量和数据库设置，勿输出设置的秘密值。
- 应用 slog JSON 到 stdout/stderr，dev 为 debug。关联 `request_id`、`session_id`、`turn_id`、`sandbox_id`；应用 `X-Request-ID` 不等于 Render `Rndr-Id`。不为排障输出请求体、凭据或 SQL 参数。

## 休眠检查

Render Free Web 约 15 分钟无入站 HTTP/WS 活动后休眠；Neon Free 在最后数据库活动约 5 分钟后休眠。Nimbus 运行时交接监测每秒查询、沙箱回收每 30 秒查询，另有 LISTEN 连接。因此关闭页面、终端及其他客户端后，通常先 Web 休眠再 Neon 休眠，合计约 20 分钟，平台调度可能有延迟。

使用 `render services instances` 与 `neon api .../endpoints` 观察。不访问公开健康接口、不跑 SQL、不添加 uptime ping，以免重置计时或唤醒。部署 `live` 不等于仍有运行实例；Neon `suspend_timeout_seconds=0` 使用套餐默认值，**不是禁用休眠**，`-1` 才表示禁用。

SSE 心跳不查询数据库；但浏览器自动重连、401/404、WS 输入/resize 都可能产生入站活动。HTTP 完成日志需等 SSE/WS 断开才出现，不能单凭缺少完成日志判断没有客户端。不要移除 ownership 安全检查来强迫数据库休眠。

报告实际镜像/部署 ID、实例和数据库状态、日志区间及是否截断，区分观测、推断和未验证事项。变更失败后先读取状态和日志，保留仍可用版本；不循环重启或盲目重发写请求。
