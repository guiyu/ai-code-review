# NX2800 自动代码评审接入

2026-09-16 已部署仓库 `liangyou/halliday_nx2800`，当前仅评审目标分支为 `main` 的 PR。

- 策略：`oasis-static-v11`。复用现有内网模型、简短静态评审、完整 diff 首次交付及飞书群通知（含提交人、`codereview` 关键词）。
- 门禁：必须完成 `hermes-review` 并通过；保留原有 1 名人工审批，禁止直接/强制推送、过期基线合并和管理员绕过。
- 合并名单为原有 9 名写权限协作者：alvin、huichen、leonard、qingye、scorttin、tom、wangkw、zhangyupeng、zichuan。只读协作者及评审机器人 qianshou 未加入。
- Gitea 在规则写入时未接受 owner `liangyou` 作为合并白名单成员，因此配置使用读回的实际有效名单；未修改仓库协作者权限。
- `oasis_pvt*`、`oasis_dvt*`、`oasis_nps*`、`oasis_mp*` 原有保护规则均保持不变。

独立实例：

- 配置：`.runtime/config-nx2800.json`
- 状态：`.runtime/state-nx2800`
- 日志：`.runtime/nx2800-service.log`
- launchd：`com.halliday.hermes-review-gate-nx2800`，每 30 秒轮询。
- 可移植示例：`examples/review-gate-nx2800.json`

已验证保护规则写入和独立读回、preflight、首次空 PR 轮询及 launchd running 状态。接入时没有开放 PR，因此尚未完成此仓库的真实模型评审及飞书通知验收。

```sh
python3 .runtime/run.py -config .runtime/config-nx2800.json preflight
launchctl print gui/$(id -u)/com.halliday.hermes-review-gate-nx2800
```

`once`、`retry` 等需要状态锁的手动命令，必须先停止此实例再执行，并在完成后恢复服务。不要操作另外两个实例的状态目录。

2026-09-16 通知地址已独立配置：本实例读取 `FEISHU_WEBHOOK_URL_NX2800`（仅保存在本机私有环境文件），其他实例继续使用原变量。已发送包含 `codereview` 的测试消息，飞书返回 code=0；服务已重启生效。

2026-09-16 通知统一修正：Oasis PR #593 的通知因旧地址投递失败；共享 `FEISHU_WEBHOOK_URL` 已更新为用户指定的新群机器人地址，与 NX2800 当前目的地一致。服务自动补发后 `Notified=true`、`NotifyError` 为空。飞书接受记录代表接口确认接收，不代表群成员已读。独立变量仍保留，便于后续按仓库调整。

## 2026-09-17 PR #335 未触发的启动故障

主机已重启，图形控制台用户是 halliday，而服务属于 weiyi。原 gui/502 域不可用；NX2800 的 plist 也没有安装到 weiyi 的 Library/LaunchAgents。原 plist 缺少 Background 会话支持，直接 bootstrap user/502 返回错误 5。

修复：三个服务的 LaunchAgent 均设置 `LimitLoadToSessionType=[Aqua, Background]`，安装到用户 Library/LaunchAgents，并启动到 user/502 域。读回三个服务均为 running，PR #335 自动生成评审运行记录（无需手动模拟 PR 或更改门禁）。

可重复安装：

```sh
python3 scripts/install_macos_review_services.py
```

用户级服务仍需要该用户的登录/后台会话；若要求开机且无人登录时运行，使用系统级 LaunchDaemon。脚本会以工作区所属用户运行服务，不让模型以 root 执行；会禁用同名用户 LaunchAgent，避免重复启动。

```sh
sudo /usr/bin/python3 scripts/install_macos_review_services.py --system
```

本次 sudo 非交互检查返回“a password is required”，因此没有安装系统级服务。管理员可先用 `--system --dry-run` 核对三个安装目标，再执行上述命令。凭据继续留在忽略的 .runtime 文件中，不写入 plist。

恢复后的线上验证：PR #335 自动发布报告 #3342，`hermes-review=success`，`Attempts=1`，飞书 `Notified=true` 且无通知错误。该记录证明恢复后自动扫描、模型评审、报告发布和通知链路均已运行。

后续用户已执行系统级安装。读回确认三个 `/Library/LaunchDaemons/com.halliday.hermes-review-gate*.plist` 均由 root 持有、权限 0644，`UserName=weiyi`、`RunAtLoad=true`、`KeepAlive=true`；三个 system 域服务均 running，同名用户 LaunchAgent 已禁用。现已配置为系统开机启动，不依赖图形登录用户；本次没有实际重启主机测试。
