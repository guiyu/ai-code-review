> 最新策略 `oasis-static-v3`：提示词进一步压缩，三节正文各一句、总计目标 150 字；缺陷详情仅在 findings 中展示，合并重复问题且保留全部阻断问题。其余静态分析范围与门禁规则沿用 v2。

> 当前策略：`oasis-static-v2`。仅基于 PR 修改范围进行静态分析，报告正文为“修改概述 / 代码问题 / 待确认项”，目标 600 字以内。取消跨仓全量追踪、版本映射、历史故障清单、编译/构建/真机分析及验证矩阵。下文 `oasis-v1`、七章节与 16384 token 的记录属于此前部署历史，以本节为准。
>
> 根据全部 commit log 和完整 diff 推断修改目标；未提供关联仓库或编译结果不自动阻断，仅与改动判断直接相关的关键代码证据缺失时使用“证据不足”。已证实 P0/P1、无效报告、证据未读完仍禁止合入。只读工具、精确源码行号校验和飞书通知保留。Oasis 输出预算调整为 8192 token，评审超时上限暂保留，以免把缩短报告误实现为截断报告。

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

## 新增生产仓库：qingyun/oasis_glasses

- 仓库 ID 4，默认目标分支 `dev_oasis`。当前接入面向此分支的 PR；其他目标分支未接入此实例。
- 已核验 qianshou 管理员权限，应用并读回严格 `hermes-review` 门禁：禁直接/强制推送、仅服务账号合入、阻止过期基线和管理员绕过；保留既有 1 名人工审批及原审核人白名单。
- 原有 `oasis_pvt*`、`oasis_dvt*`、`oasis_nps*` 规则不在本次变更范围。
- 独立本机配置 `.runtime/config-oasis.json`，状态 `.runtime/state-oasis`，日志 `.runtime/oasis-service.log`，launchd 服务 `com.halliday.hermes-review-gate-oasis`。共享同一版本评审程序、Oasis Prompt、内网模型和飞书群通知配置。
- 原测试仓库实例继续运行。两实例状态目录独立，均每 30 秒轮询并随当前用户登录自启动。
- 已通过 preflight 和首次空 PR 轮询；接入时无开放 PR，尚无此仓库的实际 PR 评审验收。飞书既有发送故障仍需修复，不影响 Gitea 报告发布。

只读检查新增实例：

```sh
python3 .runtime/run.py -config .runtime/config-oasis.json preflight
launchctl print gui/$(id -u)/com.halliday.hermes-review-gate-oasis
```

对该仓库运行 `once`、`retry` 或 `merge` 前，先停止新增实例以释放 `.runtime/state-oasis` 的锁；随后使用 `-config .runtime/config-oasis.json` 指向该仓库，操作后恢复该实例。不要误停原测试仓库的服务。

## 飞书关键词故障定位与修复

截图确认机器人输入框内实际关键词为 `1. codereview`，左侧编辑器行号之外的 `1.` 也是关键词内容。此前客户端仅发送 `codereview`，所以飞书返回 `19024 / Key Words Not Found`。使用完整关键词发送诊断消息，飞书返回 `code:0`、`StatusCode:0`，验证该差异就是失败原因。

群客户端现支持 `FEISHU_WEBHOOK_KEYWORD`，默认值仍为 `codereview`。本机两个实例共享环境配置，设为准确值 `1. codereview`；无需修改飞书机器人设置。此前的失败记录是修复前的历史状态。

修复后验收：两个 launchd 服务均运行正常；原队列中的 2 份 PR #1 报告（3264、3265）均收到飞书成功确认，Notified=true、NotifyError 为空、待发送数为 0。Oasis 仓库当前无待发送报告。

通知更新：两个实例均已切换到用户后续提供的新 Webhook（仅保存在私有配置中），连接测试返回 code=0；关键词沿用 `1. codereview`。新增 PR 提交人 Gitea 用户名与数字 ID，位于报告正文前，长消息截断仍保留。两个服务已重启并确认为 running；既有已发送报告未重发。相关 Go 回归测试及静态检查通过。

## PR #588 执行失败诊断

原评审输入：11 文件、400 行 diff、2 提交，输入解析通过。三次自动执行均约 240 秒终止。对相同 head/base 的隔离诊断扩大到 600 秒后，模型用时 503.73 秒返回，但消息历史含 finish_reason=length，完整性校验拒绝该结果。小请求在 1.28 秒返回 HTTP 200，确认认证/接口可用。

修复：错误报告保留白名单阶段码，区分执行超时、输入错误、模型调用失败和输出截断等；Oasis 实例提高适配器期限至 1200 秒、外层至 1260 秒，输出限额从 8192 提高到 16384 token，仍严格阻止截断/不完整结果合入。通过正常轮询触发新的评审键，未手工改写评审结论或绕过保护。


后续诊断捕获到另一类执行失败：模型正常结束（无 length 标记），却返回英文前言，并在已闭合 JSON 对象后继续追加 findings，严格 JSON 解析失败。另一次输出含重复键；这些响应均未用于批准合入。适配器现通过 Hermes `request_overrides` 请求 JSON 模式；现有模型端点用无项目代码的小请求验证 HTTP 200、1.25 秒返回合法 JSON。报告协议增加受控阶段码、重试日志原因、默认关闭的私有诊断文件，以及额外报告首尾章节的保守规范化。

修复验证：34 项 Python 测试通过，含实际已安装 Hermes 对本机模拟端点发送 JSON 模式并调用只读证据工具；Go gate/feishu 测试、go vet 和 Darwin arm64 构建通过。否定/条件通过表述、前言、尾随 JSON、重复键、截断输出仍拒绝。真实 PR 复评结果以 Gitea 新报告和本地持久状态为准；以上测试不代表该 PR 获得合入批准。


第二次正常结束的模型响应因带书名号的报告总标题被旧校验拒绝；标题兼容已修复。JSON 模式下的一次后续响应返回合法 JSON、verdict=证据不足，但章节名附有“（待执行）”，且一个 finding 引用了差异范围之外的源码行号。前者已兼容且保留附注，后者继续阻断并通过 allowed_finding_lines 提供明确定位依据。36 项 Python 测试（含真实 Hermes 本机模拟集成）通过。失败报告 #3275 已写回 PR，持久状态确认飞书通知成功；随后通过控制器 retry 命令发起复评。临时诊断已关闭，合入保护保留。
