---
name: nimbus-coolify-ops
description: 通过 Coolify CLI 检查和维护 Nimbus 的备用 dev 部署，查看资源配置、部署与运行日志，并在用户明确指定时发布或重启。当前 Render 仍是主要 dev 环境。
---

# Nimbus Coolify 备用运维

## 平台定位

- 截至 2026-11-03，Render + Neon 仍是 Nimbus dev 的主要环境；Coolify 是部署在阿里云的备用环境。普通的“部署 dev”默认沿用 [nimbus-dev-ops](../nimbus-dev-ops/SKILL.md) 的 Render 流程。
- 只有用户明确指定 Coolify、阿里云备用环境或 `nimbus-aliyun` 时，才在 Coolify 上发布或操作资源。不要自动故障切换，也不要因 Coolify 可用而改变 Render 的主用地位。
- 使用已安装的 `coolify` CLI。所有命令指定 `--context nimbus-coolify`；不要显示或传递 token，也不要使用 `--show-sensitive`。

## Coolify 资源定位

以下 ID 是定位信息，状态和配置可能变化；每次操作前重新读取：

| 资源 | 当前定位 |
| --- | --- |
| 项目 | `nimbus`，UUID `xksx3cfpjfi4p90v2jsucnbf` |
| 环境 | `dev`，UUID `xswngey34azvjmchoqykxdgc` |
| 应用 | `nimbus-aliyun`，UUID `3liiq80eey7yqwsk23u80sty` |
| 部署服务器 | `aliyun`，UUID `w2ndumy8tfc9h51f88ccebep` |
| 镜像 | `ghcr.io/nimbus-cloud-agent/nimbus-cloud-agent:dev` |
| Coolify 当前返回的应用域名 | `https://3liiq80eey7yqwsk23u80sty.apps.aliyun.lambda1107.icu` |
| 数据库资源 | `nimbus-postgres-aliyun`，UUID `lplpeyidmfslz6st8bicfcr7` |

应用当前为预构建 Docker 镜像类型，暴露端口 `10000`，健康检查为 `GET /api/health`。域名、镜像、端口、健康检查及资源状态都以实时 Coolify 配置为准，不把表中的快照当作当前状态。

## 连接和查配置

先验证默认 Coolify context，再检查项目、环境和资源：

```sh
coolify --context nimbus-coolify context verify
coolify --context nimbus-coolify project list --format json
coolify --context nimbus-coolify project environments get xksx3cfpjfi4p90v2jsucnbf dev --format json \
  | jq '{uuid,name,applications:[.applications[]? | {uuid,name,status}],postgresqls:[.postgresqls[]? | {uuid,name,status}],services:[.services[]? | {uuid,name,status}]}'
coolify --context nimbus-coolify resource list --format json
```

查看应用的镜像、域名、端口和健康检查等配置，只输出所需字段：

```sh
coolify --context nimbus-coolify app get 3liiq80eey7yqwsk23u80sty --format json \
  | jq '{uuid,name,status,fqdn,docker_registry_image_name,docker_registry_image_tag,ports_exposes,health_check_enabled,health_check_path,health_check_method,health_check_scheme,health_check_port}'
```

需要确认环境变量是否存在时，只输出 key 和非敏感标记，不输出 `value` 或 `real_value`：

```sh
coolify --context nimbus-coolify app env list 3liiq80eey7yqwsk23u80sty --format json \
  | jq 'map({key,is_buildtime,is_runtime,is_preview,is_shared})'
```

Postgres 配置或状态只在任务涉及该数据库时检查：

```sh
coolify --context nimbus-coolify database get lplpeyidmfslz6st8bicfcr7 --format json \
  | jq '{uuid,name,status,image,is_public}'
```

`app get`、资源列表和数据库详情不应加 `--show-sensitive`。不要把环境变量值、连接串、密码、webhook 或认证头写进报告、日志摘录或仓库文件。

## 发布和确认结果

Coolify 只拉取已发布到 GHCR 的镜像，不在 Coolify 上构建 Nimbus 镜像。发布前确认用户选择了 Coolify，并确认对应 GitHub Actions 已把预期版本推送到 GHCR；不要在镜像尚未更新时部署 `dev` 标签。

按请求发布已有的 `dev` 镜像：

```sh
coolify --context nimbus-coolify deploy uuid 3liiq80eey7yqwsk23u80sty --docker-tag dev
```

默认不加 `--force`。提交部署请求后，读取部署列表和具体部署状态；不要因为日志暂时没有新行就重复触发或取消仍在运行的部署：

```sh
coolify --context nimbus-coolify app deployments list 3liiq80eey7yqwsk23u80sty --format json
coolify --context nimbus-coolify deploy get <deployment_uuid> --format json
```

只在用户明确要求时执行重启、停止、启动、回滚或取消部署。失败后先收集部署状态和日志，再判断是否需要新的写操作。

## 查日志

部署过程日志与容器运行日志是两类信息：

```sh
# 某次部署的构建/拉取/切换日志
coolify --context nimbus-coolify app deployments logs 3liiq80eey7yqwsk23u80sty <deployment_uuid> --lines 200

# 应用容器运行日志
coolify --context nimbus-coolify app logs 3liiq80eey7yqwsk23u80sty --lines 100 --show-timestamps
```

汇报部署 UUID、状态、时间范围、日志是否截断及最后阶段。日志可能含敏感值；分享前先检查并遮盖凭证。对状态保持 `in_progress` 的部署先继续查询状态，不凭静态日志推断已失败。
