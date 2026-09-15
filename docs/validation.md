# 集成验证记录

- 上游基线包构建通过（上游无测试）；新组件按失败测试→实现→通过的流程开发。
- Go 全仓库 `go test -race ./...` 通过；`go vet ./...` 通过。
- 最终审查修复后的 `go test -race ./internal/gate ./internal/feishu` 通过，`go vet ./internal/gate ./cmd/review-gate` 通过。
- 已构建 macOS arm64 和 Linux amd64 的 `review-gate` 二进制。
- Python 适配器 19 项测试通过，包含真实安装的 Hermes 对接 localhost 模拟模型、工具调用及完整结果验证。
- 内网实际模型使用人工构造的 diff 冒烟通过，未使用真实提交数据、未写 Gitea、未发飞书。
- 独立全量代码审查发现共享 head 状态恢复和 Gitea 故障时通知重试两个问题；均补充回归测试修复，并通过限定范围复审。
- 适配器独立检查发现第二文件截断被误接受的问题；已验证失败复现并修复，所有文件必须含完整文本 hunk。
- 飞书单元测试覆盖 HTTP 200 业务失败、HTTP 400/401 Token 失效后的单次刷新、UUID 保留、超时、并发缓存和响应大小限制。
- Gitea 实际保护应用与读回见 `deployment-status.md`；真实 PR 和真人消息尚未端到端验收。
