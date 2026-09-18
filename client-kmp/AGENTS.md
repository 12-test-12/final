# KMP Client Agent Rules

- `commonMain` 只保存真正可共享的客户端业务逻辑。
- Android UI 与 MiniApp UI 不得放入 `commonMain`。
- 平台能力必须通过清晰的接口或 Adapter 隔离。
- 后续 `miniappMain` 用于微信/JavaScript Runtime 适配，不承载微信 UI。
- 微信 WXML/WXSS 属于 Host UI，不属于共享 Kotlin 代码。
- 不把共享 SDK 演进成自研跨平台 UI Framework。
- 目标是共享业务行为，而不是强制共享所有代码。
- 当前阶段保留已有模板，不新增业务页面或复杂架构。
- 两套客户端共同遵守 `../docs/api/openapi.yaml`，不得各自发明字段、告警等级或阈值单位。
- 目标能力包括实时仪表盘、温湿度/气体趋势、远程静音、阈值设置和历史告警；当前只记录契约，不实现页面。
- 负责 KMP 的成员/Agent 必须依照根规则使用 `multica` CLI，同步 source set、平台适配、API 契约消费、测试结果和阻塞。
- KMP 修改遵守根 Git/PR 流程，分支使用 `feat/kmp-...`、`fix/kmp-...` 等完整名称；PR 应列出受影响 source set 与 Android/MiniApp 分别验证的结果。
