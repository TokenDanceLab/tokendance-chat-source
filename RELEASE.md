# Release Checklist

## 每次发布前

- [ ] 后端构建通过：`cd backend && go build ./...`
- [ ] 后端测试通过：`cd backend && go test ./...`
- [ ] 前端构建通过：`cd frontend && pnpm run build`
- [ ] 前端测试通过：`cd frontend && pnpm exec vitest run`
- [ ] 类型检查通过：`cd frontend && pnpm exec tsc --noEmit`
- [ ] E2E 测试通过：`cd frontend && E2E_BASE_URL=https://chat.tokendancelab.com pnpm exec playwright test src/e2e/ --project=chromium`
- [ ] 无敏感信息泄露：grep 服务器 IP / 内部端口 / SSH 配置
- [ ] `.gitignore` 覆盖所有生成文件
- [ ] `.env.example` 无真实凭据或内部 URL
- [ ] `LICENSE`、`NOTICE`、`LICENSES/` 随镜像分发；保留第三方原始许可证。
- [ ] 仓库变量 `CHAT_PUBLIC_SOURCE_URL` 是无登录可访问的 HTTPS source archive/tree，可用 `${SHA}` 占位实际 commit；CD 会匿名探测该 URL。
- [ ] 构建时 `SOURCE_REVISION` 使用实际 commit，`SOURCE_URL` 指向同一 commit 的完整对应源码；UI“源码与许可证”和 OCI source/revision 与该构建一致。
- [ ] 修改版/私有 fork 对外提供服务前，核对 AGPL 对应源码义务；不要将密钥或用户数据混入源码。

## 公开对应源码

首次发布前创建一次 public 空仓 `TokenDanceLab/tokendance-chat-source`，然后为
待发布的精确 commit 推送源码快照和同名 tag：

```bash
scripts/publish-source-snapshot.sh <full-commit-sha>
```

`CHAT_PUBLIC_SOURCE_URL` 固定为：

```text
https://github.com/TokenDanceLab/tokendance-chat-source/tree/${SHA}
```

脚本使用 `git archive` 只发布该 commit 的 tracked files，执行凭据形态扫描，
并把快照 commit 打成与开发 commit 同名的 tag。不要把可变的 `main` URL 配给
CD 充当版本级对应源码。

## 打 Tag

```bash
git tag -a v0.x.0 -m "v0.x.0: <简要描述>"
git push origin v0.x.0
```

GitHub Actions publishes `ghcr.io/tokendancelab/tokendance-chat` with `latest`, `YYYYMMDD`, short SHA, and semver tags (`X.Y.Z`, `X.Y`).

## 生产部署后验证

- [ ] `/api/health` 返回 `{"status":"ok"}`
- [ ] WebSocket 连接 + 消息收发正常
- [ ] `@TokenBot` 正常回复
- [ ] 前端页面无 JS 错误（打开浏览器控制台检查）
