# Hermes review adapter

`scripts/hermes_reviewer.py` runs one isolated Hermes `AIAgent` conversation per invocation. It reads the controller's JSON request on stdin and writes exactly one JSON review object on stdout. Exit 0 means a structurally valid, complete review was returned; severity policy still decides whether merge is allowed. Every exception, timeout, unsupported input, unread evidence range, invalid finding or incomplete response exits nonzero with `complete:false`.

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

Input fields: `repository`, `number`, `head_sha`, `base_sha`, `title`, `diff`. Output fields: `complete`, `summary`, `findings`. Each finding has `severity`, `file`, `line`, `title`, `evidence`, `suggestion`; severity is one of `critical`, `high`, `medium`, `low`, `info`.

A successful summary always starts with:

> Coverage: diff-only; all supplied lines made available via evidence tool; no full repository, dependencies, builds or runtime tests.

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
