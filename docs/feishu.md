# Feishu bot notifications

In `feishu_dm` mode (the default), the review controller uses an **enterprise self-built application bot**, with its own app ID and secret, to send a private message to the PR author. A group custom webhook bot cannot implement this app-authenticated, `open_id` direct-message flow. Polling identifies the PR author; it does not establish who pushed the latest commit. Accurate pusher notifications require trusted push events and additional identity mapping.

## 群评审结果通知（当前部署方式）

在私有配置中设置：

```json
{
  "notification_mode": "feishu_group",
  "feishu_webhook_url_env": "FEISHU_WEBHOOK_URL"
}
```

将群自定义机器人的完整 Webhook URL 放入 `FEISHU_WEBHOOK_URL` 环境变量；本机部署由 `.runtime/environment.json` 加载。默认关键词为 `codereview`，客户端会自动加到每条消息开头。可通过 `FEISHU_WEBHOOK_KEYWORD` 环境变量覆盖，或用配置项 `feishu_webhook_keyword_env` 指定环境变量名。关键词按原样发送，必须与飞书安全设置一致，包括空格、标点和编号。本机截图确认实际关键词为 `1. codereview`，已经按此配置。无需飞书应用 App ID/Secret、通讯录权限或个人身份映射。当前实现支持关键词验证，未实现机器人签名验证。

控制器在 Gitea 评审报告与状态发布后发送结果摘要、问题统计及报告链接。仅发送评审结果，不转发普通 Issue/PR 评论。服务所在内网需要能出站访问 Gitea 和 `open.feishu.cn:443`，不需要公网回调入口或 Gitea Webhook。

发送失败留在持久化 outbox，后续轮询重试；已确认成功的结果不重复发送。群机器人没有私信 API 的 UUID 去重能力，网络超时或发送成功后本地保存失败可能产生重复消息。保存的 `group-accepted:...` 是本地确认标记，不是飞书消息 ID。切换通知模式不会补发此前已经成功通知的结果。通知失败不会撤销已通过的评审门禁。

## Application setup and identity

1. Enable the application's bot capability, publish/install the application in the intended tenant, and have the administrator grant the required messaging permission. The send-message API accepts the bot-send scope `im:message:send_as_bot` (or the applicable broader messaging scope in your application console).
2. Configure application availability/visibility so intended recipients can use the bot. Permission approval and application visibility are separate requirements; an API token alone does not make every tenant user reachable.
3. Maintain an explicitly verified mapping from the **numeric Gitea account ID** on the configured Gitea instance to the recipient's **open_id for this Feishu application**. Do not infer identities from mutable usernames/display names or reuse an open_id obtained for another application. Verify the Gitea account and Feishu identity belong to the same person through your approved identity process. Add directory lookup scopes only if your provisioning workflow actually needs them; the send client does not query the directory.
4. Configure app credentials through the controller's environment-variable references. Never put credentials, bearer tokens or identity exports in source control. Restrict access to runtime configuration/state.

A missing identity mapping must remain a failed/pending outbox item. A successful send means Feishu accepted the message and returned a message ID; it does **not** establish that the person received a device notification or read the message.

## Client contract

```go
client := feishu.NewClient(appID, appSecret)
messageID, err := client.Send(ctx, verifiedOpenID, reportText, stableUUID)
```

`BaseURL` defaults to `https://open.feishu.cn`; it is the API origin, without `/open-apis`. `HTTPClient` and `BaseURL` can be configured before concurrent use for local `httptest` integration. Use HTTPS in deployment. Each `Send` has a 20-second overall deadline, including token acquisition and the one permitted authentication retry; an earlier caller deadline wins.

The client caches the tenant access token with an expiry margin, serializes refreshes, posts JSON-string text content to `/open-apis/im/v1/messages?receive_id_type=open_id`, checks HTTP and API business codes, and requires a nonempty message ID. Invalid/expired-token codes 99991663/99991664/99991671 permit one refresh/retry. Redirects are rejected to avoid forwarding app credentials. Response sizes are bounded. Returned errors contain operation/status/code, never remote error bodies or credential values.

Persist a nonempty ASCII UUID of at most 50 bytes with the outbox entry, derived deterministically from the review run and recipient. Reuse it for every attempt, including retries after restart and uncertain network outcomes. The client never invents a new key on retry. Feishu's server-side deduplication window is finite; persist acknowledged message IDs and do not assume a UUID provides permanent exactly-once delivery. Other failures are returned to the durable outbox for bounded-backoff retry or operator correction.

## Validation

All tests use local HTTP servers and fake credentials; no actual recipient was messaged. The initial test run failed because `Client`/`NewClient` were absent. Tests cover concurrent token reuse, exact endpoint/recipient/text/UUID, refresh exactly once with unchanged UUID, HTTP 200 business errors, missing response fields, malformed JSON, HTTP errors, secret-safe errors, caller cancellation, invalid inputs and credential-bearing redirects.

API references: [tenant access token](https://open.feishu.cn/document/server-docs/authentication-management/access-token/tenant_access_token_internal), [send message, permissions and UUID semantics](https://open.feishu.cn/document/server-docs/im-v1/message/create).

Verified with Go 1.24 Alpine in Docker: `go test -timeout 30s ./internal/feishu` passed (0.822s), and `go vet ./internal/feishu` completed cleanly. Additional tests cover timed cache renewal, cancellation while waiting for another token refresh, and oversized responses. Repository-wide race validation is recorded separately in the controller validation report.

HTTP 400/401 authentication responses are parsed within the same response-size limit, permitting a single token refresh with the original UUID. HTTP 5xx, malformed bodies, and non-authentication errors remain failures. Retry codes follow the [official Go SDK constants](https://github.com/larksuite/oapi-sdk-go/blob/v3_main/core/constants.go); user-token error 99991668 does not trigger a tenant-token refresh. Regression tests were observed failing for HTTP 400/401 before this fix.

## 提交人展示及 Webhook 切换

通知在评审正文前显示 `PR 提交人：用户名（Gitea ID：数字）`，使用 Gitea PR API 的 `user.login` / `user.id`。身份缺失时明确显示缺失，长报告截断后仍保留此信息。这代表 PR 发起人；轮询不能将其认定为每次 push 的实际操作者。

替换私有环境中的 `FEISHU_WEBHOOK_URL` 并重启各实例即可切换机器人。已确认发送成功的历史报告不会因换群而自动重发；未发送成功的队列项将发送至新目标。

## 代码提交人（2026-09-17）

群通知使用评审所覆盖提交的 Git `commit.author.name`，显示为“代码提交人”，多人去重汇总；不再把 PR 创建者或其 Gitea ID 当作代码作者。作者快照随评审结果持久保存，通知重试不读取可能已经变化的 PR。作者数据只用于通知，不扩展模型输入协议。旧结果没有作者快照或提交缺少作者名时明确显示“未提供作者信息”，不回退为 PR 创建者。已发送通知不重发。
