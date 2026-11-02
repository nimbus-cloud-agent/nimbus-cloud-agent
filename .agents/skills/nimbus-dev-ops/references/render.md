# Render CLI 运维

官方：[CLI](https://render.com/docs/cli)、[命令参考](https://render.com/docs/cli-reference)、[日志](https://render.com/docs/logging)、[Free 休眠](https://render.com/docs/free)。

## 认证与定位

```sh
brew install render
render --version
# 已有 RENDER_API_KEY 时复用，否则请用户完成首次授权
render login
render workspace set tea-dbjjrd142hec73d81l60 -o json --confirm
render workspace current -o json
```

日常从终端操作；只有缺少凭据时的登录需要用户浏览器授权。自动化安全注入 `RENDER_API_KEY`，不将其值写进技能/脚本。`-o json` 避免交互菜单。配置存在 `~/.render/cli.yaml`，不要输出它。需项目隔离时，先设置 `RENDER_CLI_CONFIG_DIR="$PWD/.local/render-cli"`，再完成该目录对应的认证与 workspace 选择。

## 只读状态和配置

```sh
render services -o json
render services instances srv-dbjl0pe7bikc73fplma0 -o json
render deploys list srv-dbjl0pe7bikc73fplma0 -o json
python3 .agents/skills/nimbus-dev-ops/scripts/render_extra.py env
```

先检查实际 JSON 结构，定位 `name/id`，核对镜像、`serviceDetails.plan`、region、healthCheckPath、suspended。实例列表为空且非手动挂起，结合停止日志判断自动休眠；历史部署 live 不等于实例运行。历史的 `image.ref` 应对应 Git SHA，不能只看 `:dev` 或仓库 HEAD。

仓库配置来源是 `render.yaml`、`Dockerfile`、`docker-entrypoint.sh`。运行时注入 NIMBUS_POSTGRES、模型/E2B key 和 Basic Auth；`PORT` 映射监听地址。使用 `env` 补充命令查看变量存在性、少数非敏感值和数据库 host，不输出秘密值。

**官方 CLI 2.34.0 缺口**：没有 Web suspend/resume、环境变量读取/更新命令，`services update` 也没有 `--env-var`。不要虚构 `render env`、`render services get`、`render services suspend`。本技能的 `render_extra.py` 只覆盖这些缺口，内部使用 Render API，要求 `RENDER_API_KEY`，不读取或展示 CLI token；其他操作继续用官方 CLI。

## 日志

```sh
render logs -r srv-dbjl0pe7bikc73fplma0 --limit 100 -o json
render logs -r srv-dbjl0pe7bikc73fplma0 --level error,warning --limit 100 -o json
render logs -r srv-dbjl0pe7bikc73fplma0 --text 'sandbox' --limit 100 -o json
# 时间为示例，替换成事件的 RFC3339 UTC 区间
render logs -r srv-dbjl0pe7bikc73fplma0 --start 2026-11-02T06:50:30Z --end 2026-11-02T07:00:00Z --direction forward -o json
# 确有观察新事件的需要时使用，观察结束终止该进程
render logs -r srv-dbjl0pe7bikc73fplma0 --tail -o json
```

按新部署开始时间、image.ref、instance 分组，别把旧失败当作当前错误。2.34.0 的 `logs -o json` 返回多个 JSON 对象组成的流，不是单个数组；需要聚合时使用 `jq -s '.'` 或逐个解码，不能直接把整个 stdout 交给一次 `json.loads`。先检查排序，必要时按 timestamp 排序。CLI limit 100 是有界结果，不代表整个时间窗口；缩小区间/分段查询后才能报告完整覆盖。不要把敏感业务日志原样放进公开报告。

Hobby 基线保留 7 天应用日志；平台 HTTP request logs 是较高套餐能力，不以其存在为前提。Nimbus 自己的 `msg="http request"` JSON 含 method/path/status/duration/request_id。

判读要点：

- `/` 401 通常是 Basic Auth 未通过，仍是入站请求。`/api/health` 不认证，debug 可见。
- `draining:true` / 短暂 503 先查 ownership handoff，不将健康 200 直接等同业务就绪。
- `database is already owned...` 查本地进程或另一部署是否占锁、两端是否兼容 handoff；不能随意杀连接或禁用锁。
- E2B x509 / `tls_unknown_authority` 先查镜像 `ca-certificates`，再看 DNS/TLS/超时诊断，不用跳过证书校验解决。
- GORM 记录 duration/rows/SQLSTATE，不记录 SQL 文本/参数。正常取消/终端关闭的 DEBUG 与 ERROR 区分。

## 发布和重启

当前发布逻辑见 `.github/workflows/ci.yml`。`RENDER_DEPLOY_ENABLED` 基线 false，每次重新检查。此机器的环境 GITHUB_TOKEN 曾受组织策略限制，`env -u GITHUB_TOKEN gh` 使用已有 gh 登录凭据。

```sh
env -u GITHUB_TOKEN gh variable list --json name,value
env -u GITHUB_TOKEN gh workflow run ci.yml --ref dev -f deploy_render=true
env -u GITHUB_TOKEN gh run list --workflow ci.yml --branch dev --limit 5
env -u GITHUB_TOKEN gh run view RUN_ID --log-failed
```

workflow_dispatch 测试、发布 SHA 镜像并通过 hook 部署，不同时再发第二个 Render deploy。只有部署已存在的镜像、回退或 hook 未触发时才直接执行（替换 COMMIT_SHA）：

```sh
render deploys create srv-dbjl0pe7bikc73fplma0 \
  --image ghcr.io/nimbus-cloud-agent/nimbus-cloud-agent:COMMIT_SHA \
  --wait -o json --confirm
render restart srv-dbjl0pe7bikc73fplma0 -o json --confirm
```

image-backed 服务不传 `--commit`。部署后核对 image.ref、live、就绪实例；有必要时单次请求健康接口检查 `draining:false`。健康接口不查数据库，连接验证需要业务 API 读写。休眠检查时不发健康请求。

手动发布不用重新开启持续部署；用户要求恢复自动发布时才修改 GitHub 变量。若 npm 安装 ENOTFOUND，检查 lock 是否混入内网 registry URL，本项目曾因此 CI 失败。

## 改变量、挂起和恢复

以下是项目补充 CLI，只有已有相应操作授权时执行。省略 `--confirm` 仅显示计划。

```sh
export NIMBUS_LOG_LEVEL=debug
python3 .agents/skills/nimbus-dev-ops/scripts/render_extra.py env-set NIMBUS_LOG_LEVEL --from-env NIMBUS_LOG_LEVEL --confirm
python3 .agents/skills/nimbus-dev-ops/scripts/render_extra.py suspend --confirm
python3 .agents/skills/nimbus-dev-ops/scripts/render_extra.py resume --confirm
```

`env-set` 值从进程环境读取，不通过命令参数传密钥；只更新一个 key 并读回核对，不替换整个变量列表。更换数据库时，在不输出密钥的 Node/Python 进程中加载本地 env，再执行 `env-set NIMBUS_POSTGRES --from-env DATABASE_URL_UNPOOLED --confirm`。保存变量不等于当前容器已更新，要发布并验证。

手动 suspend 后需要显式 resume，普通请求不解除手动挂起。停止环境只挂起 Render Web，让 Neon 自行 idle；恢复 Web 连接将唤醒 Neon。返回成功后继续核对实例、部署和必要的业务检查。

Free Web 没有常规 SSH/Shell，不要为看环境变量尝试 `render ssh`，也不默默创建收费 ephemeral shell。
