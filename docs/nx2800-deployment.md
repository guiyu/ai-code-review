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
