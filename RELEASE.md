# Release Checklist
最后更新：2026-10-22 HKT

## 每次发布前

- [ ] 后端构建通过：`cd backend && go build ./...`
- [ ] 后端测试通过：`cd backend && go test ./...`
- [ ] 前端构建通过：`cd frontend && pnpm run build`
- [ ] 前端测试通过：`cd frontend && pnpm exec vitest run`
- [ ] 类型检查通过：`cd frontend && pnpm exec tsc --noEmit`
- [ ] E2E 测试通过：`cd frontend && E2E_BASE_URL="${E2E_BASE_URL:?Set an approved test origin}" pnpm exec playwright test src/e2e/ --project=chromium`
- [ ] 无敏感信息泄露：grep 服务器 IP / 内部端口 / SSH 配置
- [ ] `.gitignore` 覆盖所有生成文件
- [ ] `.env.example` 无真实凭据或内部 URL
- [ ] `LICENSE`、`NOTICE`、`LICENSES/` 随镜像分发；保留第三方原始许可证。
- [ ] 仓库变量 `CHAT_PUBLIC_SOURCE_URL` 是无登录可访问的 HTTPS source archive/tree，可用 `${SHA}` 占位实际 commit；CD 会匿名探测该 URL。
- [ ] 构建时 `SOURCE_REVISION` 使用实际 commit，`SOURCE_URL` 指向同一 commit 的完整对应源码；UI“源码与许可证”和 OCI source/revision 与该构建一致。
- [ ] 修改版/私有 fork 对外提供服务前，核对 AGPL 对应源码义务；不要将密钥或用户数据混入源码。

## 公开对应源码

首次发布前创建一次 public 空仓 `TokenDanceLab/tokendance-chat-source`，然后为
待发布的精确 commit 推送源码快照和 `source-<full-commit-sha>` tag：

```bash
scripts/publish-source-snapshot.sh <full-commit-sha>
```

`CHAT_PUBLIC_SOURCE_URL` 固定为：

```text
https://github.com/TokenDanceLab/tokendance-chat-source/tree/source-${SHA}
```

脚本使用 `git archive` 只发布该 commit 的 tracked files，执行凭据形态扫描，
并把快照 commit 打成 `source-<full-commit-sha>` tag。它只推送这个不可变 tag，
不修改公开默认分支；重复发布同一快照无变化，已存在的不同快照会被拒绝。
不要把可变的 `main` URL 配给 CD 充当版本级对应源码。

## 打 Tag

```bash
git tag -a v0.x.0 -m "v0.x.0: <简要描述>"
git push origin v0.x.0
```

GitHub Actions publishes `ghcr.io/tokendancelab/tokendance-chat` with `latest`, `YYYYMMDD`, short SHA, and semver tags (`X.Y.Z`, `X.Y`).

## 镜像与 Compose 部署

`scripts/deploy.sh` 的旧裸二进制 / systemd 流程已退役。该入口对所有参数都以状态码 `2` 退出，只提示本页，不连接服务器、不复制文件、不停止或启动服务。更新仍调用它的外部自动化，避免把退出当成部署成功。

根目录 `docker-compose.yml` 是本地构建 / 自托管示例，绑定 localhost；其中的 `latest` 和 `build` 不能替代已经审查的生产镜像 pin 与主机配置。维护 TokenDance 生产服务时，先从私有运维入口确认现役 Compose、运行镜像和恢复证据，再按以下顺序执行：

1. 完成本页发布门禁，公开与待发布 commit 一致的 Corresponding Source；确认 GHCR 镜像架构与目标主机一致，并使用不可变 digest。
2. 比对现役 Compose 与非密配置镜像；确认 Account issuer、精确 redirect / post-logout / back-channel URI、可信代理与 secure cookie 契约。凭据由目标环境提供，不写入代码或部署输出。
3. 按私有 runbook 保留当前镜像与一致的 SQLite / WAL 备份，确认本次版本的数据兼容性；只替换 Chat 应用服务，不因应用发布重建数据库或其它服务。
4. 按私有运维 preflight、变更验收和恢复 runbook 执行；nginx 有改动时先快照并验证语法。生产路径、SSH、环境值与回滚锚留在私有材料，公开源码不保存这些信息。
5. 完成下方验收后将已验证的非密 Compose / nginx 与运行记录回写到其运维所有者。测试通过或本地代码已更新不等于生产已部署。

## 生产部署后验证

- [ ] `/api/health` 返回 `{"status":"ok"}`，实际运行镜像、架构和源码版本与发布记录一致；容器健康、无新增重启或 OOM。
- [ ] WebSocket 连接、消息收发、受控测试账号的 `@TokenBot` 回复正常；授权模型调用预算和测试账号清理按私有验收计划执行。
- [ ] Account 登录后仅出现 Chat 的 HttpOnly / Secure / SameSite session；Account token 不进入浏览器存储或日志。
- [ ] 本地退出、Account 全局退出、callback replay 拒绝、back-channel 后 REST / WS 撤销均按 [OIDC 验收契约](docs/oidc-setup.md) 验证。
- [ ] 前端实际交互与更新缓存验证通过；记录 console 错误的来源，第三方统计 / CSP 噪声与产品错误分别判断，不能笼统声明零错误。
- [ ] 私有运维四层验收通过，非密配置镜像、运行状态与可用回滚锚已同步。
