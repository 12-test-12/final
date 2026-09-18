# Backend Agent Rules

- Go 是 Backend 的主体语言，遵循标准 Go 风格并保持可执行、可测试。
- 当前优先使用标准库；已确认需要 HTTP 路由契约，但在实现阶段到来前不添加 Web Framework、数据库驱动或 MQTT 客户端依赖。
- 不在需求出现前创建 Controller、Handler、DTO、Repository、Service 或复杂分层。
- 后续 C 能力必须通过清晰、可记录的边界接入；不得提前加入 CGO。
- 业务代码不得直接依赖具体传感器、GPIO 或硬件驱动实现。
- Backend 是客户端与设备之间的系统边界；任何协议变化都必须与 `hardware` 显式协调和记录。
- 路由事实源为 `../docs/api/openapi.yaml`，设备主题与 Payload 事实源为 `../docs/device-protocol.md`；实现与文档变更必须同步。
- Backend 目标职责包括遥测入库、设备在线判定、复合预警、REST 查询、控制指令发布和 WebSocket 秒级推送。
- 复合预警必须同时考虑气体突增与温升速率，并记录触发证据；不得用单次采样直接产生复合火警。
- 当前路由 Handler 返回明确的 `501 not_implemented` 是有意的契约占位，不得伪造数据库或 Broker 结果。
- 负责 Backend 的成员/Agent 必须依照根规则使用 `multica` CLI，同步路由/Schema 变更、MQTT 与数据库决策、测试结果和跨模块依赖。
- Backend 修改遵守根 Git/PR 流程，分支使用 `feat/backend-...`、`fix/backend-...` 等完整名称；API 或 MQTT Schema PR 必须同步契约文档并请求客户端/硬件负责人审核。
