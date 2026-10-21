# AgentHub 历史验证说明（非现役参考）

最后更新：2026-10-21 HKT。

TokenDanceChat 早期曾验证 AgentHub 的聊天渲染、类型化实时事件、SQLite 持久化与 Agent 入口。**AgentHub 已退出当前参考范围**；本文件仅说明这段沿革，不作为 Chat 的设计、架构、实现、迁移目标或发布条件。

早期映射与验证表保留在本文件的 Git 历史中。它们描述当时的实验，不能据此推断当前仍有 Desktop/Edge 依赖、真人 DM、群组、Webhook、上传或旧工具面板，也不应作为恢复这些能力的依据。

当前工作分别以以下入口为准：

- [AGENTS.md](../AGENTS.md)：项目身份、运行边界与验证命令。
- [ROADMAP.md](../ROADMAP.md)：Chat + Account 当前目标与实现顺序。
- [能力矩阵](./capability-matrix.md)：实际存在、实验和退役的运行面。
- [Bot 架构](./bot-architecture.md)：TokenBot 当前实现及 Grok Bot / Dots / Muse 的已核验产品借鉴。
- [DESIGN.md](../DESIGN.md) 与 [视觉验收](./visual-acceptance.md)：仓库当前组件与 TokenDance 品牌约定。
- [Account 平台设计](../../docs/identity/account-platform-design.md)：平台用户、内部运营成员、IAM 与邮箱方案。

不维护新的 AgentHub 对照表；后续 Chat 能力应依据本项目的用户场景、代码与验证结果设计。
