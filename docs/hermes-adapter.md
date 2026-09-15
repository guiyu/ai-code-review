# Hermes review adapter

`scripts/hermes_reviewer.py` runs one isolated Hermes `AIAgent` conversation per invocation. It reads the controller's JSON request on stdin and writes exactly one JSON review object on stdout. Exit 0 means a structurally valid, complete review was returned; the explicit Chinese verdict and severity policy decide whether merge is allowed. Every exception, timeout, unsupported input, unread evidence range, invalid finding or incomplete response exits nonzero with `complete:false`.

## Runtime interface

Use an absolute Python interpreter from the Hermes virtual environment and Python isolated mode:

```text
/opt/hermes/venv/bin/python -I /app/scripts/hermes_reviewer.py
```

Install Hermes and its Python dependencies separately in the runtime image. The ordinary Go build image does not include them. `REVIEW_HERMES_PATH` points to the trusted Hermes source checkout containing `run_agent.py`; do not point it at PR-controlled code. Do not rely on `PYTHONPATH` or the interactive `hermes` CLI. `-I` prevents user site packages and inherited Python path settings from changing imports.

Required environment:

| Variable | Meaning |
| --- | --- |
| `REVIEW_HERMES_PATH` | Absolute trusted Hermes source directory |
| `REVIEW_MODEL` | Explicit model identifier |
| `REVIEW_BASE_URL` | Explicit OpenAI-compatible endpoint, including `/v1` when required |
| `REVIEW_API_KEY` | Model credential only; never a Gitea or Feishu credential |

Optional limits:

| Variable | Default | Accepted range |
| --- | --- | --- |
| `REVIEW_MAX_INPUT_BYTES` | 524288 | 1024–524288 |
| `REVIEW_MAX_TOKENS` | 8192 | 1024–16384 response tokens per model request |
| `REVIEW_MAX_ITERATIONS` | 16 | 2–64 Hermes iterations |
| `REVIEW_TIMEOUT_SECONDS` | 240 | 1–1800 seconds |

The controller should also enforce a subprocess deadline and bounded stdout. Input is rejected, never truncated, when the configured byte limit is exceeded. The response limit is 131072 bytes and 100 findings. Limits do not guarantee model context capacity: choose a model with enough context for the supplied diff, prompt, repeated tool messages and response budget. Runtime/model failures remain blocking.

## Isolation and evidence access

Before importing Hermes, the adapter replaces the entire environment with a small fixed set and creates a fresh private temporary `HOME`, `HERMES_HOME`, `HERMES_MANAGED_DIR`, XDG directories and working directory. Explicit model configuration is held in memory and passed to the constructor. The adapter disables Hermes's dotenv loader before importing `run_agent`: that loader otherwise reads the source checkout's `.env` and external/managed credential sources, even with a temporary home.

Personal memories, context files, SOUL identity, trajectory saving and checkpoints are disabled. No configured toolsets are enabled. The sole advertised tool is `read_diff(start_line, line_count)`, returning at most 200 immutable stdin diff lines per call. The dispatch boundary rejects every other tool name independently of model instructions. All supplied lines must have been returned through this tool before a complete result is accepted. Diff text and PR metadata are explicitly treated as untrusted evidence. No repository checkout, arbitrary file read, shell, network lookup, code execution, browser, skill or delegation tool is exposed to the model.

This is application-level tool isolation, not an operating-system sandbox for the trusted Hermes dependency itself. Deploy under a dedicated unprivileged account/container without personal home or Gitea/Feishu secret mounts, with read-only application/Hermes files, a writable temporary directory and network egress limited to the private model endpoint. Do not expose this service to PR-controlled command, environment or dependency changes. HTTP endpoints are accepted for explicitly configured private services; use TLS where required by the deployment.

The adapter suppresses third-party stdout/stderr at the file descriptor boundary, including exception paths, to avoid mixed JSON or accidental credential diagnostics. It emits a generic blocking error rather than raw model/library exceptions. Temporary Hermes state is deleted after each invocation.

## Review scope and validation

Input fields: `repository`, `number`, `head_sha`, `base_sha`, `title`, `diff`, `description`, `head_ref`, `base_ref`, `merge_base`, `commits` (SHA/message pairs). Output fields: `complete`, `verdict`, `summary`, `findings`. Each finding has `severity`, `file`, `line`, `title`, `evidence`, `suggestion`; severity is one of `critical`, `high`, `medium`, `low`, `info`.

A successful adapter response always produces a report starting with:

> # Oasis 嵌入式代码评审报告

The report includes an explicit Chinese evidence-scope statement. `complete:true` means the supplied evidence was processed; it does not mean full repository coverage or approval. `verdict` is mandatory: `通过`, `有条件通过`, `不通过`, or `证据不足`. Only `通过` can pass the gate, and findings must also satisfy the severity threshold. Evidence gaps retain a full report instead of being treated as an adapter execution failure.

Tool coverage proves which evidence was presented; it does not prove model understanding or absence of defects. Findings must name a supplied file and a positive line represented in its textual hunks (old line for deletion, new line for addition/context). Duplicate JSON keys, missing/extra schema fields, invalid finding types/severities/locations and empty text fail closed. Hermes must report `completed:true`, no error, and no message with truncated/filtered finish reasons; its final response must itself be strict JSON with `complete:true`.

The parser validates declared old/new hunk line counts. Binary patches, combined diffs, quoted paths, malformed/incomplete hunks and any individual file without a textual hunk are unsupported and block. This conservative limitation can require a separate manual review path for metadata-only changes or empty files. A structurally valid response with no findings is a limited diff review, not a full repository audit.

## Compatibility and verification

Inspected installed Hermes commit: `7c9d05267c550dd6b0db2bdcdecda9d06a73baea` (2026-09-15 local verification). The adapter uses `run_agent.AIAgent(...)`, `AIAgent.run_conversation(user_message=..., system_message=...)`, and the returned `completed`, `final_response`, `messages` and `error` fields. It replaces the module's `handle_function_call` dispatch binding and the instance's `tools` / `valid_tool_names`. These integration points must be retested when upgrading Hermes; no CLI text scraping is used.

Run the self-contained boundary suite with any Python 3.9+ interpreter:

```sh
python3 -m unittest discover -s scripts -p test_hermes_reviewer.py
```

Run the optional real installed-Hermes integration using its Python interpreter (the test opens a localhost mock OpenAI server and uses dummy credentials only):

```sh
TEST_HERMES_SOURCE=/opt/hermes /opt/hermes/venv/bin/python -m unittest discover -s scripts -p test_hermes_reviewer.py
```

Verification on 2026-09-15: all 19 tests passed with installed Hermes, including an actual streamed model tool-call loop against the localhost mock. The default suite passed 18 boundary tests and skipped that opt-in integration. Tests cover isolated environment/working directory, only the evidence tool being advertised, dispatch refusal, strict JSON, secret diagnostic suppression, input/output size bounds, configuration limits and deadline, exception diagnostic suppression, malformed/truncated diffs (including an incomplete trailing file), valid multiple-file diffs, completeness, unread evidence, invalid findings and completion truncation. The mock integration observed `read_diff`, its supplied diff result, and a valid final JSON response.

An authorized live smoke also passed against the configured private model API using only an artificial six-line diff: adapter exit 0, valid JSON, `complete:true`, zero findings, diff-only scope present and empty stderr. Credentials were loaded from existing local Hermes configuration into dedicated child configuration without printing them. No real repository content, Gitea writes or Feishu messages were involved. This verifies basic private-model compatibility, not model review quality.

## 所有自动 PR 的 Oasis 默认规则

可信部署文件 `prompts/oasis-review.md` 固定加载到每次 Hermes 系统提示词；无需 PR 作者提供需求描述。需求优先从 PR 说明、全部提交日志和差异推断，报告必须说明推断依据。PR 内容不能覆盖系统提示词。

Controller 分页收集 PR 全部提交 SHA 和完整消息，获取失败、重复记录、超过 1000 条/200 KB、缺少目标 HEAD 均阻断，不静默截断。采集前后再次校验 head/base；不是只取最后一条 commit。Python 仍只开放 read_diff，提交日志与 PR 元数据作为不可信证据输入。未提供本机工作区、关联仓库或真实固件组合时必须标出，不会自动把本机其他分支混进远端 PR。

模型返回七个中文正文章节；适配器验证章节顺序及中文内容，再确定性生成总标题、评审结论、最高已证实风险等级和最终合入建议，形成完整九章报告。P0/P1/P2/P3 分别对应 critical/high/medium/low。有条件通过保持阻断，需指定验证完成后更新证据并重新评审。仅普通文档变更可基于局部证据通过；涉及跨仓/跨核逻辑但缺必要证据时不能伪装通过。

提示词与适配器均是部署文件，必须一起安装；调整提示词时同步升级 `policy_version`，旧结论不会作为新策略下的合入凭据。当前默认策略为 `oasis-v1`。修改 PR 说明本身不自动触发重新评审；需新提交或升级策略版本。

## 安全失败诊断

适配器失败输出附带受控 `error_code`：`CONFIG_INVALID`、`INPUT_INVALID`、`MODEL_EXECUTION_FAILED`、`OUTPUT_INVALID`、`OUTPUT_TRUNCATED`、`REVIEW_TIMEOUT`。控制器仅接受此白名单及自身子进程失败码，将其转换为中文错误报告；不展示模型响应、URL、密钥或第三方异常原文。内部超时和外层子进程超时均显示 `REVIEW_TIMEOUT`。有效报告中的“证据不足”仍是评审结论，不与执行失败混淆。

超时有两层：适配器由 `REVIEW_TIMEOUT_SECONDS` 控制，默认 240 秒；控制器由 `review_timeout_seconds` 控制，默认 300 秒。提高适配器期限时必须在 `reviewer_env` 映射该变量，并让外层期限留有余量；只修改控制器期限不能消除适配器的 240 秒终止。

Oasis 生产实例现显式配置 1200 秒适配器期限、1260 秒控制器期限、16384 输出 token。此为上限，不代表每次执行耗时；其他未映射这些环境变量的实例仍用适配器默认值。曾发生任一消息 length/content_filter 截断的结果仍拒绝放行，不因最终补写文本而忽略中间截断。


模型调用通过 Hermes 的 `request_overrides` 设置 `response_format: {"type":"json_object"}`。部署模型端点必须支持该参数；本机端点已用独立小请求验证 HTTP 200 和合法 JSON。适配器仍严格解析单一 JSON，不截取前言后的对象、不修补重复字段，也不忽略尾随内容。

输出失败进一步区分 `OUTPUT_JSON_SYNTAX`、`OUTPUT_DUPLICATE_KEYS`、`OUTPUT_SECTIONS`、`OUTPUT_LOCATION`、`OUTPUT_SUMMARY`、`OUTPUT_FINDING_SCHEMA`、`OUTPUT_LANGUAGE`、`EVIDENCE_INCOMPLETE`、`OUTPUT_SCHEMA`、`OUTPUT_SIZE` 和 `OUTPUT_VERDICT_CONFLICT`。自动重试日志保留这些受控原因。

兼容模型额外生成的正式报告标题、首部评审结论和末尾合入建议，但七个正文章节仍必须完整有序。结构化 verdict 为“通过”时，额外结论/建议必须分别完整等于“通过”/“可以合入”；否定或附条件文本一律拒绝，禁止用子串匹配将“不能通过”误判为通过。

可选诊断变量 `REVIEW_DIAGNOSTICS_DIR` 必须映射到 `reviewer_env` 且为绝对路径。默认关闭；开启时仅保存完成标记、结束原因和有界模型最终输出，不保存认证配置或完整对话。目录权限 0700、文件权限 0600。最终输出可能包含被评审代码，应仅写入本机忽略目录；诊断后清空该变量可关闭，保留映射以免改变评审键。
