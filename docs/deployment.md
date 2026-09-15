# 内网部署与仓库接入

## 部署边界

新门禁入口是 `cmd/review-gate`，不是原项目 `cmd/server`。原项目 Docker Compose 运行的是旧 Webhook 服务，不能提供新门禁。第一版通过内网主动查询 Gitea 开放 PR，不开放内网接收端口。

出站连通性：Gitea REST/代码下载、飞书 OpenAPI、内网模型 API。Hermes 只接触当前评审的不可变 diff、PR 元数据和完整提交日志，不能执行被评审代码。第一版是 diff 范围的 Agent 评审，不宣称拥有全仓库上下文或完成动态测试；既有 CI 必须继续独立执行。

## 安装

1. 使用 Go 1.24.2 或以上版本：`go build -o bin/review-gate ./cmd/review-gate`。
2. 在 `/opt/hermes` 安装并固定经过验证的 Hermes 版本及 Python venv。参考 `docs/hermes-adapter.md` 的兼容性约束。
3. 将本项目安装到 `/opt/ai-code-review`，包含 `scripts/` 和 `prompts/oasis-review.md`；复制 `examples/review-gate.json` 到 `/etc/ai-code-review/config.json`。
4. 将 `examples/review.env.example` 复制为 `/etc/ai-code-review/review.env`，填入专用凭据并设置 0600。HTTP 是目标实例现有配置，迁移 HTTPS 后关闭 `allow_insecure_http`。
5. 在 config 的 `identities` 中配置 `"Gitea数字用户ID": "飞书open_id"`。映射必须属于当前 Gitea 实例及当前飞书应用；不能按 commit author 邮箱盲猜。
6. 创建无登录权限的 `code-review` 系统账号，创建 `/var/lib/ai-code-review` 并赋予其独占读写权限。其他安装文件保持只读。

凭据可由企业密钥管理系统注入；环境示例只是最低部署方式。绝不将 Gitea 或飞书写入凭据传给 Hermes。

## 配置与启用

先加载环境文件，再运行（以下命令均从项目目录执行）：

```sh
./bin/review-gate -config /etc/ai-code-review/config.json preflight
./bin/review-gate -config /etc/ai-code-review/config.json protect
./bin/review-gate -config /etc/ai-code-review/config.json once
```

`protect` 默认仅展示计划；确认 worker 配置和授权后执行 `protect -apply` 并读回规则。管理员权限只能由仓库管理员授予；普通写权限不能设置保护分支。

保护 main：禁直接推送、强推，必须 `hermes-review` 成功，禁止落后分支合并、旧审批失效、禁止管理员合并绕过，合并白名单仅专用 bot。现有 CI 必需检查保留。权限模型要求 bot 凭据只归此服务；如果同一账号也被人直接用于合并，便无法保证只能经 Controller 放行。

安装 `deploy/review-gate.service` 后启动服务。命令行运行也支持 `run`，但终端关闭后的保活应交给 systemd 等进程管理器。服务启动与门禁配置是两个独立步骤。

## 合并

评审通过只赋予合并资格，不自动执行合并。受信任的操作员通过 gate 的 `merge -pr NUMBER -head FULL_SHA` 发起；服务核对当前 PR、版本、持久化评审及状态，再调用 Gitea。不能直接以 bot 身份点击网页合并。普通开发者不持有 bot token。

第一版 merge 是管理命令，未提供对普通用户开放的申请合并 Web UI/API。为团队开放入口时应在身份鉴权后调用同一门禁逻辑。

## 通知语义与运维

轮询模式发送给 PR 发起人；轮询无法可靠识别每次 push 的操作者。需要同时通知实际推送者时，增加已验签的 Webhook 事件入口及持久化 pusher 身份，不能用最新 commit 作者代替。

通知使用独立持久化状态，失败后重试，不重新调用模型。飞书接收成功不代表用户已读。通知失败不改变评审结论；默认先将完整报告发布到 Gitea 即可进入合并检查。

单实例 state 目录加锁，禁止多个 poller/merge 进程同时修改同一状态。需要运行一次管理命令时先停止常驻服务，操作完成后恢复。不要直接编辑状态文件制造“通过”结果。

备份 state、规则和身份映射；备份文件包含代码评审内容，需要访问控制。修改规则后升级 policy_version 触发重新评审。模型超时、无效输出、超出 diff 大小限制均保持阻断。告警关注日志中的身份映射缺失、API 失败、长时间无成功轮询与模型失败。

## 多仓库实例

每个仓库/目标分支运行一个独立进程，共享二进制和可信 Prompt，分别配置 `repository`、`base_branch`、`state_dir`。不要让两个进程使用同一状态目录。`examples/review-gate-oasis.json` 为 `qingyun/oasis_glasses → dev_oasis` 的无密钥配置示例；复制、配置并分别执行 protect/preflight 后，用独立服务启动。

本机新增实例通过已有私有启动器覆盖配置路径：`python3 .runtime/run.py -config .runtime/config-oasis.json run`。它保留原测试仓库的常驻进程。现有用户级 launchd 服务依赖当前 Mac 在线和用户登录，未改为服务器系统级部署。
