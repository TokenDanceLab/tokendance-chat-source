# TokenDanceChat source and exposure boundary

最后更新：2026-10-20 HKT

## 当前事实

- TokenDanceChat 是 AGPL-3.0-only 的网络应用，工作仓当前为 private。
- 部署或提供修改版网络服务时，AGPL 的 Corresponding Source 义务不会因为仓库
  改为 private 而消失。
- 因此 Chat 不能通过“界面开源、运行代码私有”来规避许可证；如果不希望公开
  某段代码，它必须从 AGPL 覆盖的程序边界中真正拆出去，而不是只在仓里隐藏。

## 推荐边界

Chat 采用 **公开 AGPL 产品源码 + 私有运行配置/数据**，而不是半私有的同一程序：

| 层 | 建议 |
|---|---|
| 可公开 | 聊天 UI、通用 Go Hub/WS/Store、Account OIDC BFF 参考实现、构建脚本、测试、安全文档、对应版本的完整源码 |
| 仅运行环境 | `CHAT_OIDC_CLIENT_SECRET`、LLM/API key、数据库、模型 base URL 的私有值、Cloudflare/nginx/SSH、生产配额、备份、日志 |
| 私有数据 | 用户消息、profile、违规证据、运营工单、真实 IP/UA、访问管理记录 |
| 需单独法律/架构评审 | TokenBot prompt、工具清单、内容安全策略、业务运营规则、未公开的 Agent 核心 |

Prompt 或策略放在运行时 secret/config store 中可以作为私有的运营材料，但这
本身不自动解决 AGPL 对程序源码的要求。若这些组件与公开程序构成一个作品，发布
方式仍需法务确认，不能仅凭“文件没在 Git 里”推断合规。

运行配置遵循 fail-closed：安全布尔值必须可解析，OIDC 开启时必须有稳定的
`CHAT_SESSION_SECRET`，且 `CHAT_COOKIE_SECURE=false` 只能用于 issuer 与 callback
都指向 loopback 的本地开发。配置错误会在数据库打开前终止启动。

## 发布硬门禁

每一条公开或生产 Release 都必须同时具备：

1. 通过 digest 固定实际运行镜像；
2. 从同一 commit 生成完整 source archive；
3. archive 可在无需 GitHub 组织权限的公开 URL 下载；
4. `/` 的“Source and license”入口指向该版本的 archive/tree，而不是不可访问的
   private repo；
5. 检查 archive 不含 `.env`、secret、数据库、用户内容和个人数据。

当前 Dockerfile 强制要求显式 `SOURCE_URL`，并使用同一值生成 UI“Source and
license”入口及 OCI source label；私仓 URL 会被拒绝。公开快照镜像为
`https://github.com/TokenDanceLab/tokendance-chat-source`。GitHub 不允许把
40 位十六进制 commit ID 直接用作 tag，因此每个发布 commit 对应
`source-${SHA}` tag。CD 要求仓库变量 `CHAT_PUBLIC_SOURCE_URL`，在构建前匿名
探测替换了 `${SHA}` 的完整源码 tree；快照不存在时 CD 失败，不得把新的 Chat
Release 描述为合规公开发布。

## 半开源的正确方式

若未来确实需要“半开源”：

- Chat 的 AGPL 程序整体继续提供对应源码；私有部分应是独立服务、部署配置或
  不构成同一作品的数据。
- 不从公开仓删除历史来制造闭源假象，也不把第三方贡献擅自改许可。
- 若要从早期 MIT 基线另建闭源产品，必须建立独立清洁仓、保留许可证/NOTICE、
  做版权与贡献归属审计，并验证没有混入 AGPL 代码。
- AgentHub 已是 public/Apache-2.0，不能追溯关闭既有代码。后续半开源应新增
  独立私有扩展模块，而不是反向锁住现有公开核心。

## 与 Account 的边界

Account 是“开放 OIDC 契约、私有账号控制面”。Chat 只需依赖 Account discovery、
OpenAPI 和审核后的 confidential client，不需要 Account 源码或内部 API。
