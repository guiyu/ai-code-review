# Controller validation and operating contract

## Commands

`review-gate -config /absolute/private/config.json once|run|preflight`

`review-gate -config /absolute/private/config.json protect` prints a protection plan; adding `-apply` writes it and verifies the effective rule afterward. `merge -pr 1 -head <full-SHA>` is the only supported merge path. `retry -pr 1` resets an exhausted reviewer execution for the current version; it refuses completed reviews. The next polling cycle performs the review.

Default repository is `qianshou/Gitea_code_review`, following its authorized ownership transfer from `halliday`. Default target is `main`. Plain HTTP requires explicit `allow_insecure_http: true`; use a trusted private transport or TLS for credentials. Secret configuration values are ENVIRONMENT VARIABLE NAMES, except nonsecret numeric-ID/open_id mappings. Changing model endpoint/model/prompt behavior requires incrementing `policy_version`, since environment variable values are intentionally not persisted or hashed into review identity.

## Enforcement

The run identity includes instance, repository, PR number, target name, head/base SHA, policy version, severity threshold, reviewer command, and environment-name mapping. Unknown or incomplete reviewer JSON blocks. Default critical/high findings block; medium/low/info remain in the report.

The controller verifies PR/head/base before diff retrieval, after retrieval, and before publishing results. Reports are persisted before success status. A failed report or notification does not repeat a completed review. Model failures retry with persisted 60/120-second backoff, up to three attempts, then publish a blocking error report. The explicit retry command allows an operator to try again after resolving the cause.

The fixed Gitea required context is `hermes-review`. Status contexts are commit-scoped, so they cannot alone distinguish PRs or targets. The controller refuses an open head shared by multiple PRs. Safety additionally depends on **only the controller account being allowed to merge**, and using its `merge` command: it verifies exact durable identity, strict result schema, original published report content/author, current bot-owned success status/run identifier, expected head, current base, and an up-to-date merge base. It asks Gitea to merge with `head_commit_id`, with force and deferred merge disabled. Gitea's outdated-branch protection must enforce the last race between the final read and merge. Administrators who change protections or use the controller credentials outside this command can bypass this trust boundary.

Preflight verifies authenticated bot identity, repository admin permission, effective branch-rule name, required status, no direct/force pushes, no file bypass patterns, stale-review rules, outdated-base blocking, admin override blocking, and a bot-only merge whitelist. Protection updates preserve unrelated approval settings and existing required checks. If another protection rule remains effective, readback fails; resolve priority before enabling the service.

## Durability and notifications

State uses a private directory, exclusive OS file lock, atomic JSON replacement, file fsync and directory fsync. Keep one persistent state directory per deployment. State belongs to a trusted service account; it is the merge controller's trust database. No concurrent controller/merge processes may hold the same store. Stop the poller before a manual merge invocation or use a separately scheduled one-shot poll loop.

Report recovery scans paginated issue comments for exact content and bot ownership, avoiding a duplicate after a crash between comment creation and state persistence. Delivery is at least once across uncertain network outcomes; Gitea itself offers no idempotency token for comments. Feishu retries use a stable UUID per run/recipient. Bot-side deduplication windows remain a platform constraint.

The durable outbox retries independently from review and status publication and is scoped to the configured instance/repository/target. It reports the reviewed commits, summary, severity counts, first three findings and report link. Missing numeric-author-ID mappings or credentials remain delivery errors. Polling identifies the PR author, not the actual latest pusher; true-pusher attribution requires event history/webhooks. No external notifications are sent by the unit tests.

## Reviewer boundary

The subprocess receives only bounded JSON stdin plus an explicit HOME/HERMES_HOME/PATH/language environment and the allowed `REVIEW_*` mappings. Gitea and Feishu credentials are not inherited. Stdout is capped at 1 MiB and parsed strictly; stderr is discarded to avoid secret-bearing model errors. The controller imposes a process timeout. The Hermes adapter separately controls its read tools and temporary personal context. The configured reviewer executable itself is trusted deployment code.

## Recorded checks

- Initial tests failed to compile with undefined controller symbols before implementation (RED).
- `go test ./internal/gate` passed after implementation, including policy failures, locking/restart, report-before-success, report/outbox retry, PR/base/policy identity, stale diff refusal, protection preservation/bypass refusal, stale-head merge refusal, trusted merge/tampered report refusal, subprocess credential isolation/timeout/schema rejection, automatic model retry, and report recovery.
- `go vet ./internal/gate ./cmd/review-gate` passed after correcting a malformed status JSON tag found by vet.
- `go build -o /tmp/review-gate ./cmd/review-gate` passed in `golang:1.24-alpine`.
- Whole-repository race tests and deployment/live verification are recorded by the integrating task separately. Controller unit tests use local `httptest` servers only; they neither change live Gitea nor send Feishu messages.

### Final review regressions

Added tests first and observed both failures: a cleared shared-head conflict left the final status at `error`, and a failed pull-list request skipped pending Feishu delivery. The fixes persist a publication-invalid flag before writing ambiguity errors (without discarding the review), restore the result status when the conflict clears, and drain the scoped durable outbox even when listing PRs fails. `go test ./internal/gate -count=1` and `go vet ./internal/gate ./cmd/review-gate` passed after the fixes.
