# Repository Agent Rules

1. 本项目是团队软硬件综合实训项目，不得擅自改变既定技术路线。
2. 优先采用能够完成当前任务的最小改动。
3. 未经需求确认，不得进行大规模重构。
4. 不得为了所谓“最佳实践”引入当前阶段不需要的框架、目录、抽象或依赖。
5. `hardware`、`backend`、`client-kmp` 与 `client-wx-native` 必须保持清晰边界。
6. 跨一级目录修改前必须说明原因、影响和涉及的事实源。
7. API、设备协议与 Domain Model 是跨模块事实源；当前草案分别记录在 `docs/api/openapi.yaml` 与 `docs/device-protocol.md`，冻结前不得自行假定稳定契约。
8. `hardware` 现有代码默认视为高复用资产，优先保留和验证。
9. `client-kmp` 与 `client-wx-native` 必须保持独立，不得互相复制内部实现形成隐式耦合。
10. 当前阶段为方案与接口契约设计阶段；允许建立最小可测试路由骨架和文档，不实现数据库、MQTT Broker 接入、WebSocket 推送或客户端业务页面。
11. 项目主题为智慧机房/实验室微环境动环监控与早期火情预警，目标链路为 Hardware → ESP8266/MQTT → EMQX → Go Backend → Client。
12. 必须区分仓库现状与目标态：现有硬件使用 STM32F10x Standard Peripheral Library、DHT11 和 ESP8266 TCP 文本帧；HAL、MQTT、远程阈值持久化仍是待实施目标。

## Multica Collaboration

- 所有团队成员及其 Agent 必须使用 `multica` CLI 跟进自己负责的内容；不得只在本地文件、聊天或口头沟通中保留进度。
- 开始工作前，应在 Multica 中查找或创建对应 issue，将状态更新为 `in_progress`，并确认负责人和模块范围。
- 工作过程中，应通过 `multica issue comment add` 同步关键决策、接口变化、验证结果、风险和阻塞；发生阻塞时将 issue 状态设为 `blocked` 并说明解除条件。
- 提交评审前，将 commit、涉及文件、已运行验证及剩余风险同步到对应 issue，并将状态设为 `in_review`；验收后才可设为 `done`。
- Hardware、Backend、KMP、微信端之间的协议/API 变更必须在各自 issue 中交叉引用，不能只更新单个模块。
- 若 Multica 服务、认证或网络不可用，应在当前工作记录中明确说明，保留待同步摘要，并在连接恢复后补录；不得把同步失败视为任务已完成。
- 不在 AGENTS.md 中硬编码 workspace、project 或 issue ID；执行时通过 `multica workspace`、`multica project` 和 `multica issue` 查询当前上下文。

## Git and Pull Request Workflow

1. 首次基线建立后，所有成员从远程仓库重新克隆或更新本地 `main`；禁止长期在过期分支上开发。
2. 开工前根据实际内容新建分支，格式统一为 `<type>/<module>-<complete-description>`，使用小写英文和连字符。
3. `type` 只能从 `feat`、`fix`、`docs`、`test`、`refactor`、`chore` 中选择；`module` 使用 `hardware`、`backend`、`kmp`、`wx`、`repo` 或 `docs`。
4. 分支名必须完整表达工作内容，例如 `feat/backend-telemetry-query`、`fix/hardware-dht11-timeout`、`docs/repo-mqtt-contract`；禁止 `dev`、`test1`、姓名或只有模块名的含糊命名。
5. 一个分支只处理一个可评审目标。开始、关键进展、阻塞和评审状态必须同步到对应 Multica issue。
6. 修改完成后先运行模块 README 要求的格式化、静态检查和测试，再提交语义清楚的 commit；不得提交密码、本机配置、缓存或生成物。
7. 将分支推送到远程并创建 Pull Request。PR 必须说明关联 Multica issue、背景、变更范围、接口/协议影响、验证证据、风险和回退方式。
8. 跨模块 API/协议变更必须请求受影响模块负责人审核。不得未经审核直接合并到 `main`。
9. 审核意见处理完成、必需检查通过且获得批准后方可合并；合并后删除远程功能分支，并在 Multica issue 中同步结果和最终 commit/PR。
10. 紧急修复同样使用 `fix/...` 分支和 PR，不以紧急为由跳过审计；确需特殊处理时必须在 PR 和 Multica 中记录原因。
