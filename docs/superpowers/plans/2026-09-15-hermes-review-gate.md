# Hermes Review Gate Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development or superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Private-network Hermes review enforcing Gitea main branch gate and Feishu DM.
**Architecture:** Outbound Go controller, single-process durable run state/outbox, isolated Hermes JSON subprocess, API-gated merge command.
**Tech Stack:** Go 1.24+, Python/Hermes, Gitea REST, Feishu REST, Docker.
**Spec:** docs/superpowers/specs/2026-09-15-hermes-review-gate.md

## Global Constraints
- Source workspace /Users/weiyi/workspace/ai-code-review, feature branch feat/hermes-review-gate.
- Default target halliday/Gitea_code_review main on provided Gitea instance.
- No secret output/commits. Fail closed. Report before success. No stale success. No execution of PR code on host.
- Agent owns assigned paths; other files may change concurrently. No git commits by workers.

## Task 1: Gitea controller, durable polling, policy and merge gate
- [x] Write tests using httptest for report-before-success, errors, version changes, severity policy, restart/outbox retry and merge stale-head refusal; run and record initial failures.
- [x] Implement in internal/gate and cmd/review-gate; keep legacy server separate. Use subprocess reviewer stdin/stdout contract from spec, explicit child env allowlist. Config JSON path flag; credentials referenced via environment variables. Support run, once, preflight, protect (dry-run default; --apply), merge (PR number + expected head). Merge whitelist and outdated protection, preserve existing checks/protection on updates.
- [x] Integrate internal/feishu API and persistent recipient mapping/outbox. Poll author DM, use stable UUID per run/recipient. No API error text leaking secrets.
- [x] Run go test -race ./... and go vet ./...; write tests/evidence to docs/controller-validation.md.

## Task 2: Hermes isolated reviewer adapter
- [x] Inspect installed Hermes AIAgent API read-only; write Python tests for config isolation, bounded input, output validation, token limits and error propagation.
- [x] Implement scripts/hermes_reviewer.py and scripts/test_hermes_reviewer.py against shared JSON contract. Configure private custom provider explicitly. Never inherit personal skills or use host terminal for review.
- [x] Verify tests and optional safe artificial diff smoke run against private AI only; record exact scope and limitations in docs/hermes-adapter.md.

## Task 3: Feishu direct-message client
- [x] Write httptest tests covering token cache, send success, HTTP200/business-code error, expired token refresh, context timeout, stable uuid and no credentials in errors.
- [x] Implement internal/feishu with shared interface; enforce HTTPS default, bounded HTTP timeout, parse business codes, use application bot auth and receive_id_type=open_id.
- [x] Run unit tests/race; write usage documentation docs/feishu.md (mapping, visibility, permissions).

## Task 4: Integration, deployment and live preflight
- [x] Configure systemd/launchd/README/examples and protected local runtime config without copying keys to tracked files; provide run instructions and missing configuration report.
- [x] Independently review integrated diff; fix verified findings and rerun relevant checks.
- [x] Inspect target live repository current protections using authorized credentials if available; save intended protection plan, apply only after runtime is reviewable and verify readback.
- [x] Report local changes, test evidence, live deployment/gate status separately; name any blocked authentication/network requirement.
