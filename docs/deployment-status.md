# 当前目标接入记录

## 已部署

- 目标：`http://120.26.178.131:3000/qianshou/Gitea_code_review`，仓库 ID 18，默认分支 `main`。
- 用户已将仓库从 `halliday` 转移到 `qianshou`；qianshou 的 API 身份 ID 37，已核验仓库管理员权限。
- 实例版本接口返回 `25.4.2`，按该实例 Swagger 验证接口。
- `main` 的实际生效规则名为 `main`：禁止直接 push、force push；必需 `hermes-review`；阻止过期分支和管理员合并绕过；合并白名单仅 `qianshou`。
- 已执行 protection apply、独立读回、preflight 和一次空 PR 轮询，均成功。检查时没有开放 PR，尚未进行真实 PR 的提交→评审→合并验收。
- 本机 launchd 服务 `com.halliday.hermes-review-gate` 已启动，进程状态 `running`，每 30 秒轮询。服务运行于当前 Mac；电脑休眠或离线时不能评审，必需检查仍会阻止未评审代码合并。

## 本机配置

- `.runtime/config.json`：本机运行路径及身份映射。
- `.runtime/environment.json`：现有内网模型和飞书应用配置。
- `.runtime/gitea-token.json`：经用户明确批准创建的专用 Token，范围为仓库/评论读写和用户读取。
- `.runtime/run.py`：从私有配置加载环境并启动门禁命令。
- `.runtime/com.halliday.hermes-review-gate.plist`：本机服务定义。

私有文件为 0600、目录为 0700，均已被 Git 忽略。用于初次认证的账号密码文件已删除；服务只使用专用 Token。没有将密钥写入代码或示例。

## 飞书剩余配置

飞书应用认证成功。按账号邮箱查询用户 ID 返回业务码 `99991672`，缺少 `contact:user.id:readonly` 权限。需要用户开通权限及成员可见范围，并确认实际提交者邮箱；或直接提供当前飞书应用的已验证 open_id 映射。当前身份映射为空，通知会记录待处理错误，不会假装已发送。

未发送真人测试消息。默认通知结果不作为合并条件，Gitea 报告成功发布是放行前置条件。

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
