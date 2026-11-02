# Neon CLI 运维

官方：[CLI](https://neon.com/docs/cli)、[连接池](https://neon.com/docs/connect/connection-pooling)、[休眠](https://neon.com/docs/introduction/scale-to-zero)。本环境仅使用 PostgreSQL。

## 认证与定位

```sh
neon --version
# 缺少 CLI 时安装；失败则用 npx neon@latest 替代 neon
npm i -g neon@latest
neon profile list -o json
```

复用现有 profile 或 NEON_API_KEY；`account: "-"`、`file: "missing"` 不算有效认证。缺失/过期时请用户执行 `neon login`，完成浏览器授权后重试，不另建 Claimable 项目绕过认证。

已经关联时不重跑 init；未关联时链接现有项目，避免误建项目或分支：

```sh
neon link -y --org-id org-dry-feather-16104029 --project-id silent-field-76474041 --branch production
neon config status --current-branch
```

`.neon` 是被忽略的本地定位文件。本项目 `production` 对应应用 dev；不要因为 Git 分支叫 dev 而另外创建数据库。下列命令显式指定 project/branch，避免上下文被别的任务切换。

## 实际配置与休眠状态（无 SQL）

```sh
neon status --project-id silent-field-76474041 --branch production
neon config plan --project-id silent-field-76474041 --branch production
neon api /projects/silent-field-76474041/endpoints -o json
```

endpoint 关注 `id`、`branch_id`、`current_state`、region、autoscaling limits、`suspend_timeout_seconds`，报告仅选择需要的字段。`active` 为运行、`idle` 为闲置/休眠；0 使用套餐默认值，Free 基线默认 5 分钟，不是关闭休眠；-1 才是禁用。

`neon.ts` 是声明，status 是实际状态，plan 是待执行差异。纯查看不用 `neon deploy`。

## 拉取连接与应用配置

```sh
neon env pull --project-id silent-field-76474041 --branch production
neon config plan --project-id silent-field-76474041 --branch production
# 已要求应用配置且核对差异后才执行
neon deploy --project-id silent-field-76474041 --branch production
```

CLI 把 DATABASE_URL、DATABASE_URL_UNPOOLED、NEON_BRANCH 写入已有 `.env`，否则 `.env.local`。不输出完整连接值；确认被 Git 忽略并限制文件权限。旧的已导出 NIMBUS_POSTGRES 会优先于新拉取值。

本应用迁移、会话锁、LISTEN 都要求直连，不是 Go 不支持 Neon。保留 pgx/GORM，选 DATABASE_URL_UNPOOLED，保留 TLS 参数，不引入 JS serverless 驱动。

更换 branch/连接/密码后，env pull 不更新 Render。按 Render 参考文档在进程内加载本地 env，交给 `render_extra.py env-set NIMBUS_POSTGRES --from-env DATABASE_URL_UNPOOLED --confirm`，然后发布。用应用 API 写入并在 Neon 核对同一测试记录，结束后清理。不要把 DSN 放进镜像或 CI 构建参数。

`--update-existing` 只在用户要求覆盖现有配置、且已核对差异时用。Free 保持默认休眠即可。确需 init 时加 `--agent codex`：基线 CLI 自动安装检测到的 Zed 技能曾失败。Neon 8.11.4 不支持 `neon skills status`，不要循环重试该命令。

## 连接与迁移（会唤醒 compute）

使用已安装的 psql，Neon CLI 默认直连，不加 `--pooled`：

```sh
neon psql production --project-id silent-field-76474041 --database-name neondb
neon psql production --project-id silent-field-76474041 --database-name neondb -- -X -c 'SELECT version();'
neon psql production --project-id silent-field-76474041 --database-name neondb -- -X -c 'SELECT version FROM schema_migrations ORDER BY version;'
```

启动自动执行 `internal/store/migrations/*.sql`。schema_migrations 在基线日期有 8 个版本，以后与仓库实际文件比较，不永远断言必须是 8。结构变更写进版本化迁移；按用户具体要求可通过 psql 跑临时 DDL，但不以手工建表替代迁移。

仅检查数据库设置是否配置，不输出值：

```sql
SELECT key, CASE WHEN value = '' THEN 'empty' ELSE 'configured' END AS state
FROM settings ORDER BY key;
```

TLS 用 psql `\conninfo` 核对客户端连接；Neon proxy 可终止 TLS，不能单凭 pg_stat_ssl.ssl=false 判断公网未加密。

## 日志与 SQL 诊断

Go/GORM/连接错误主要看 Render slog；Neon telemetry 是数据库平台侧，不含应用 request_id 日志。

```sh
neon logs query --help
neon logs query --project-id silent-field-76474041 --branch production --source pg_endpoint --since 1h --limit 100 -o json
```

能力边界：CLI 8.11.4 接受 pg_endpoint，本项目 2026-11-02 实测 logs 为空，安装的 Neon 指南说明 Postgres 来源尚未保证产生日志。**不能用空列表证明数据库没有错误**，也不以 function/storage 日志代替，本环境未部署那两类服务。能力变化时重新核对 help 和官方指南。

如 minimum-severity 过滤不支持，先用 `neon logs field-values severity_text`（同样指定 project/branch）获取真实值，再选择 severity-text。区分无记录、权限失败、telemetry 未启用。响应有 next_cursor/is_truncated 时，保持同一时间窗口分页并报告截断。

按症状选择只读诊断；会查询数据库并重置空闲计时，休眠观察时不执行：

```sh
neon inspect db long-running-queries --project-id silent-field-76474041 --branch production --database-name neondb
neon inspect db locks --project-id silent-field-76474041 --branch production --database-name neondb
neon inspect db table-sizes --project-id silent-field-76474041 --branch production --database-name neondb
```

outliers/calls 需要 pg_stat_statements，缓存诊断可能需要 neon 扩展，不能为只读排查自动安装。统计可能在重启/休眠后重置。SQL 诊断可能带业务内容，报告前脱敏。

## 停止与恢复

日常依靠自动休眠，不删除项目、branch 或 compute。先停 Render Web，结束本地 npm start、psql 等客户端，等 Neon idle，再用 endpoint 管理查询观察。恢复 Web 后新连接自动唤醒 Neon。不要循环 SELECT 1 检测休眠。

另有明确删除、重置、手动停机请求时，先用相应 CLI help / `neon api ... --describe` 核对目标和能力，再执行具体变更，本技能不提供一键清库。
