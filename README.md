# Hermes Gitea 自动评审门禁

基于 [guiyu/ai-code-review](https://github.com/guiyu/ai-code-review) 的本地派生版本。原始基线：`d830b3e87e007e1686073b5e205e30e617e295d5`。

开发者推送开发分支并创建 PR 后，内网服务主动领取开放 PR，使用 Hermes 和内网模型进行 diff 范围评审。结果先写入 Gitea，再发布必需状态检查；飞书应用机器人将结果发送给 PR 发起人。主分支由 Gitea 保护规则和受信任合并命令共同把关。

## 新增功能

- 出站轮询：无需向公网暴露内网 Webhook 或 AI API。
- Hermes 评审：受限 diff 读取工具、独立会话、结构化问题及证据；不执行提交代码。
- 确定性门限：默认 critical/high 阻断；不完整、超时和无效评审不能通过。
- 版本绑定：仓库、PR、head/base SHA、规则版本与门限共同标识任务。
- 持久化评审与通知记录：报告/通知失败可恢复，通知失败不重复消耗模型。
- 飞书私信：使用企业自建应用机器人及已验证的 Gitea ID → open_id 映射。
- 严格合并：唯一 bot 合并白名单，合并前核验受信任报告、状态与当前代码版本。

**新入口是 `cmd/review-gate`。原有 `cmd/server`、Dockerfile 和 docker-compose.yml 保留作为上游参考，不提供新门禁。** 上游使用说明见 [README.upstream.md](README.upstream.md)。

## 开始

要求 Go 1.24.2+，Linux/macOS，以及已安装的 Hermes Python 环境。

```sh
go build -o bin/review-gate ./cmd/review-gate
cp examples/review-gate.json review-gate.json
```

按 [部署说明](docs/deployment.md) 配置环境变量、状态目录、Hermes 路径和身份映射。示例默认指向 `qianshou/Gitea_code_review` 的 `main` 分支；实际启用状态见本地部署记录，不因存在示例配置而自动生效。

```sh
./bin/review-gate -config review-gate.json protect          # 查看保护计划
./bin/review-gate -config review-gate.json protect -apply   # 需仓库管理员权限
./bin/review-gate -config review-gate.json preflight
./bin/review-gate -config review-gate.json once
./bin/review-gate -config review-gate.json run
```

保护配置保留已有 CI 必需检查；`hermes-review` 是额外条件。通过后由受信任操作员使用 `merge -pr NUMBER -head FULL_SHA` 执行合并，不将 bot token 发给开发者。

## 文档

- [内网部署与服务管理](docs/deployment.md)
- [Hermes 适配器和评审范围](docs/hermes-adapter.md)
- [飞书应用权限及身份映射](docs/feishu.md)
- [设计约束](docs/superpowers/specs/2026-09-15-hermes-review-gate.md)
- [实现计划](docs/superpowers/plans/2026-09-15-hermes-review-gate.md)

## 验证

```sh
go test -race ./...
go vet ./...
python3 -m unittest discover -s scripts -p 'test_*.py'
```

详见各组件验证记录。单元测试通过不代表目标 Gitea 已配置，也不代表飞书已向真人送达。

## 当前边界

- 轮询模式通知 PR 发起人；识别每次 push 操作者需额外接入 Webhook。
- 仅评审完整、受支持的文本 diff，不能替代全仓库分析、单元测试和安全扫描。二进制或超限 diff 保持阻断。
- 初版管理入口为 CLI，无普通开发者自助申请合并页面；bot 账号必须专用。
- 单实例状态目录加锁，合并/管理命令与常驻进程不能同时占用它。
- 飞书发送成功代表平台接受，不代表用户已读；默认通知失败独立补发，不作为合并条件。

### 评论反馈自动复评

qianshou 评审后，在未合并 PR 的讨论区新增或编辑人工文字反馈，会自动触发复评、发布新结论并更新检查状态和飞书通知。机器人自己的报告不会重复触发。详见 [评论复评使用说明](docs/comment-rereview.md)。
