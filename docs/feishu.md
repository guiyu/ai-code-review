# Feishu bot notifications

The review controller uses an **enterprise self-built application bot**, with its own app ID and secret, to send a private message to the PR author. A group custom webhook bot cannot implement this app-authenticated, `open_id` direct-message flow. Polling identifies the PR author; it does not establish who pushed the latest commit. Accurate pusher notifications require trusted push events and additional identity mapping.

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
