# 当前目标接入记录

## 已部署

- 目标：`http://120.26.178.131:3000/qianshou/Gitea_code_review`，仓库 ID 18，默认分支 `main`。
- 用户已将仓库从 `halliday` 转移到 `qianshou`；qianshou 的 API 身份 ID 37，已核验仓库管理员权限。
- 实例版本接口返回 `25.4.2`，按该实例 Swagger 验证接口。
- `main` 的实际生效规则名为 `main`：禁止直接 push、force push；必需 `hermes-review`；阻止过期分支和管理员合并绕过；合并白名单仅 `qianshou`。
- 已执行 protection apply、独立读回、preflight 和一次空 PR 轮询，均成功。PR #1 已完成旧策略评审并发布报告；尚未执行实际合并。当前已启用 Oasis 新策略，PR #1 重新评审通过，中文正式报告为 issuecomment-3265。
- 本机 launchd 服务 `com.halliday.hermes-review-gate` 已启动，进程状态 `running`，每 30 秒轮询。服务运行于当前 Mac；电脑休眠或离线时不能评审，必需检查仍会阻止未评审代码合并。

## 本机配置

- `.runtime/config.json`：本机运行路径及身份映射。
- `.runtime/environment.json`：现有内网模型和飞书应用配置。
- `.runtime/gitea-token.json`：经用户明确批准创建的专用 Token，范围为仓库/评论读写和用户读取。
- `.runtime/run.py`：从私有配置加载环境并启动门禁命令。
- `.runtime/com.halliday.hermes-review-gate.plist`：本机服务定义。

私有文件为 0600、目录为 0700，均已被 Git 忽略。用于初次认证的账号密码文件已删除；服务只使用专用 Token。没有将密钥写入代码或示例。

## 飞书群通知

已切换 `notification_mode=feishu_group`，由评审控制器直接向指定群机器人发送评审结果摘要和报告链接，自动加上 `codereview`。Webhook 仅保存在忽略的私有运行配置中，不需要个人 open_id 映射或通讯录权限。通知失败记录在持久化 outbox，后续轮询重试。

2026-09-15 实际接入测试：请求含 `codereview`，飞书返回 `19024`（关键词校验失败），测试消息未被接受。用户确认关键词后，使用正式 Go 群通知客户端再次实测仍返回 19024。需核对该 Webhook 对应机器人的安全设置，重新保存关键词后再测试。目前不能宣称群通知链路已验收成功。

未创建转发全部 Issue/PR 评论的 Gitea Webhook；该方案因范围超过评审结果通知被自动审批拒绝，改用控制器仅发送评审结果。通知发送成功默认不作为合并条件，Gitea 报告成功发布是放行前置条件。

## 本机管理

```sh
cd /Users/weiyi/workspace/ai-code-review
python3 .runtime/run.py preflight
launchctl print gui/$(id -u)/com.halliday.hermes-review-gate
```

合并命令需要独占状态目录，先停止轮询，合并后恢复（无论合并成功或失败都应恢复服务）：

```sh
launchctl bootout gui/$(id -u)/com.halliday.hermes-review-gate
python3 .runtime/run.py merge -pr NUMBER -head FULL_SHA
launchctl bootstrap gui/$(id -u) .runtime/com.halliday.hermes-review-gate.plist
```

不要将服务账号的凭据提供给开发者，也不要直接使用该账号在网页合并；管理员绕过控制层使用同一账号的行为不在本系统的信任边界内。

## Oasis 默认评审策略

本地服务已启用 `policy_version=oasis-v1`。每次自动 PR 评审固定加载 `prompts/oasis-review.md`，采集完整提交日志及 PR 说明；缺需求描述时根据 commit log 和 diff 推断并明确标注。输出正式中文九章报告。仅“通过”且缺陷等级低于门限可进入合入检查；“有条件通过／不通过／证据不足”均阻断。

当前取证仍限远端 PR diff、提交日志及元数据，不具备全仓 git show/blame 或跨仓源码读取能力；缺少必要证据必须在报告中明确说明。没有将本机 Oasis 未提交修改混入测试仓库的 PR。

验证：26 项 Python 测试（含真实已安装 Hermes 对本机模拟模型）、Go gate/feishu 测试与竞态检查、相关 Go 静态检查通过。未执行 Oasis 固件构建或真机测试。

实际新策略验收：PR #1、head `00ba73befdbac61ee58415f6ec8e764052d25674`，`hermes-review` 为 success，`verdict=通过`，报告已发布到 `http://120.26.178.131:3000/qianshou/Gitea_code_review/pulls/1#issuecomment-3265`。评审明确基于提交日志和 README 差异推断修改目标；跨仓/真机证据限制已列出。飞书通知仍失败并等待重试，未执行合并。
