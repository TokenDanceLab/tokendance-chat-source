# Contributing

欢迎贡献！

## 开发流程

1. Fork 仓库，创建 feature 分支
2. 遵循现有代码风格（Go: `gofmt`，TSX: Prettier 默认）
3. 保持改动聚焦，避免无关重构
4. 为新功能补充测试
5. 确保构建和测试通过后再提交 PR

```bash
# 后端
cd backend && go build ./... && go test ./...

# 前端
cd frontend && pnpm run build && pnpm exec vitest run
```

## 提交规范

- `feat:` 新功能
- `fix:` 修复
- `docs:` 文档
- `refactor:` 重构
- `test:` 测试
- `chore:` 构建/工具

## 安全

- 不要在代码或文档中硬编码服务器 IP、内部端口、凭据
- 敏感配置使用环境变量（参考 `.env.example`）
- 发现安全漏洞请私下报告，不要公开提 Issue

## 贡献许可

新贡献以 AGPL-3.0-only 提交，除非文件已有需要保留的兼容第三方许可证。你保留自己的版权；提交贡献不代表版权转让，也不自动授权维护者将其改为专有许可证。当前未引入 CLA 或商业双许可协议。

请在每次提交中使用 `git commit -s`，按 [Developer Certificate of Origin 1.1](https://developercertificate.org/) 声明你有权提交该贡献。`Signed-off-by` 是来源声明，不是 GPG 签名或版权转让。建议使用公开的 noreply 邮箱；签署记录会进入公开 Git 历史。

复用第三方代码时，标明上游来源并保留原许可证、版权和修改声明；不要给第三方文件批量替换为项目 AGPL 头。原 MIT 项目代码的授权边界见 README 和 `LICENSES/MIT-legacy.txt`。

## PR 模板

```markdown
## Summary
<简要描述改动>

## Test plan
- [ ] 后端测试通过
- [ ] 前端测试通过
- [ ] 手动验证（截图/描述）
```
