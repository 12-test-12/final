# Backend API Detailed Contract

状态：`draft-v1`。本文是 Go Backend 的开发者接口说明；机器可读事实源为 [`../../docs/api/openapi.yaml`](../../docs/api/openapi.yaml)。除 `GET /healthz` 外，当前 Handler 仅注册路由并返回 `501 not_implemented`。

## 1. Conventions

### 1.1 Base URLs

```text
REST:      /api/v1
WebSocket: /ws/v1
Health:    /healthz
```

本地默认地址为 `http://localhost:8080`。生产域名、TLS 终止和反向代理尚未确定。

### 1.2 Media Type and Encoding

- REST 请求与响应使用 `application/json; charset=utf-8`。
- 时间使用 UTC RFC 3339，例如 `2026-09-18T11:20:30Z`。
- MQTT 设备时间使用 Unix 毫秒，进入 Backend 后转换并同时保存 `receivedAt`。
- 温度单位为摄氏度 `°C`，湿度为 `%RH`，气体 ADC 为 0–4095，估算浓度为 `ppm`。
- JSON 字段使用 lower camel case。未知请求字段默认拒绝，避免客户端拼写错误被静默忽略。

### 1.3 Authentication and Authorization

当前骨架尚未实现鉴权。正式实现前必须确定：

- 客户端身份认证方式；
- 用户可访问的设备集合；
- 查看、静音和修改阈值的独立权限；
- 控制操作审计日志。

在鉴权完成前，不得将控制类路由暴露到公网。目标请求格式预留：

```http
Authorization: Bearer <access-token>
```

### 1.4 Common Headers

| Header | Direction | Required | Meaning |
| --- | --- | --- | --- |
| `Authorization` | Request | Production required | Bearer access token |
| `Content-Type` | Both | JSON body required | `application/json` |
| `X-Request-ID` | Both | Recommended | Client trace ID or server-generated ID |
| `Idempotency-Key` | Control request | Required when implemented | Prevent duplicate commands |

`Idempotency-Key` 建议使用 UUID/ULID。相同用户、设备、路由和 key 的重复请求必须返回同一命令结果，不能重复向设备发布控制命令。

### 1.5 Device ID

路径参数 `deviceId` 必须匹配：

```regex
^[A-Za-z0-9_-]{1,32}$
```

示例：`MCU001`。路径中的 ID 必须与数据库和 MQTT Payload 中的 `deviceId` 一致。

### 1.6 Error Envelope

所有业务错误采用同一结构：

```json
{
  "error": {
    "code": "invalid_threshold",
    "message": "gasHighPpm must be between 1 and 999",
    "requestId": "01K5H2YRG92V0V6A3EJ8VQPW03",
    "details": {
      "field": "gasHighPpm",
      "minimum": 1,
      "maximum": 999
    }
  }
}
```

建议错误码：

| HTTP | Code | Scenario |
| --- | --- | --- |
| 400 | `invalid_request` | JSON、参数或时间范围无效 |
| 401 | `unauthenticated` | 缺少或无效凭据 |
| 403 | `forbidden` | 无设备或控制权限 |
| 404 | `device_not_found` | 设备不存在或不可见 |
| 409 | `version_conflict` | 阈值版本或幂等键冲突 |
| 422 | `invalid_threshold` | 阈值超出设备允许范围 |
| 429 | `rate_limited` | 查询或控制请求过多 |
| 500 | `internal_error` | 未预期服务端错误 |
| 503 | `broker_unavailable` | 控制命令无法发布到 Broker |
| 504 | `device_ack_timeout` | 等待设备确认超时 |

当前占位实现统一返回：

```json
{
  "error": {
    "code": "not_implemented",
    "message": "route contract exists; implementation is scheduled for a later phase"
  }
}
```

## 2. Route Summary

| Method | Route | Purpose | Current |
| --- | --- | --- | --- |
| GET | `/healthz` | 进程存活检查 | Implemented |
| GET | `/api/v1/devices/{deviceId}/status` | 设备在线与告警状态 | 501 |
| GET | `/api/v1/devices/{deviceId}/telemetry/latest` | 最新有效遥测 | 501 |
| GET | `/api/v1/devices/{deviceId}/telemetry` | 历史遥测分页 | 501 |
| GET | `/api/v1/devices/{deviceId}/alerts` | 历史告警分页 | 501 |
| GET | `/api/v1/devices/{deviceId}/thresholds` | 期望与设备确认阈值 | 501 |
| PUT | `/api/v1/devices/{deviceId}/thresholds` | 校验并下发阈值 | 501 |
| POST | `/api/v1/devices/{deviceId}/commands/mute` | 静音或恢复蜂鸣器 | 501 |
| GET | `/ws/v1/devices/{deviceId}/telemetry` | 实时 WebSocket 流 | 501 |

## 3. Health

### GET `/healthz`

用于进程级 liveness。它不检查数据库、EMQX 或设备状态，因此依赖故障时仍可返回 200。后续如需 readiness，应新增独立 `/readyz`，不能改变 `/healthz` 语义。

Response `200 OK`：

```json
{
  "status": "ok"
}
```

## 4. Device Status

### GET `/api/v1/devices/{deviceId}/status`

返回 Backend 根据最后有效遥测计算的在线状态、复合预警状态及设备最近确认配置。

Response `200 OK`：

```json
{
  "deviceId": "MCU001",
  "connectivity": "online",
  "alarmState": "suspect",
  "localAlarm": true,
  "buzzerMuted": false,
  "lastSeenAt": "2026-09-18T11:20:30Z",
  "offlineAfterSeconds": 15,
  "thresholdVersion": {
    "desired": 4,
    "confirmed": 4
  }
}
```

`connectivity`：`online | offline | unknown`。

`alarmState`：

- `normal`：无复合预警；
- `suspect`：部分条件成立，等待确认窗口；
- `fire_warning`：气体突增与温升速率同时满足；
- `acknowledged`：人员已确认，但环境尚未恢复；
- `recovered`：指标恢复，事件等待归档或已结束。

`localAlarm` 来自设备本地判断，与 Backend 复合状态不是同一概念。

Errors：`400 invalid_request`、`401`、`403`、`404 device_not_found`。

## 5. Latest Telemetry

### GET `/api/v1/devices/{deviceId}/telemetry/latest`

返回最近一条通过 Schema、范围和设备权限校验的遥测。没有有效遥测时返回 404，而不是使用全零对象。

Response `200 OK`：

```json
{
  "deviceId": "MCU001",
  "sequence": 42,
  "timestamp": "2026-09-18T11:20:29Z",
  "receivedAt": "2026-09-18T11:20:30Z",
  "temperatureC": 28.0,
  "humidityRh": 61.0,
  "gasAdcRaw": 1350,
  "gasAdcFiltered": 1328,
  "gasPpm": 25.0,
  "localAlarm": true,
  "alarmCauses": ["gas_high"],
  "buzzerMuted": false,
  "network": "online"
}
```

`timestamp` 在设备未同步时间时允许为 `null`；`receivedAt` 永远由 Backend 填充。

## 6. Historical Telemetry

### GET `/api/v1/devices/{deviceId}/telemetry`

Query parameters：

| Name | Type | Required | Rules |
| --- | --- | --- | --- |
| `from` | RFC 3339 | No | Inclusive; default `now-1h` |
| `to` | RFC 3339 | No | Exclusive; default `now` |
| `limit` | integer | No | 1–1000; default 200 |
| `cursor` | string | No | Opaque cursor returned by previous page |
| `order` | enum | No | `asc` or `desc`; default `asc` |

`cursor` 出现时，`from/to/order` 必须与第一页一致。最大查询跨度建议限制为 31 天；更长区间应采用聚合接口或导出任务，而不是一次返回全部原始数据。

Example：

```http
GET /api/v1/devices/MCU001/telemetry?from=2026-09-18T10:00:00Z&to=2026-09-18T11:00:00Z&limit=200&order=asc
```

Response `200 OK`：

```json
{
  "items": [
    {
      "deviceId": "MCU001",
      "sequence": 41,
      "timestamp": "2026-09-18T11:20:24Z",
      "receivedAt": "2026-09-18T11:20:25Z",
      "temperatureC": 27.8,
      "humidityRh": 61.0,
      "gasAdcRaw": 1331,
      "gasAdcFiltered": 1310,
      "gasPpm": 24.1,
      "localAlarm": false,
      "alarmCauses": [],
      "buzzerMuted": false
    }
  ],
  "nextCursor": null
}
```

索引与排序必须使用稳定组合键，例如 `(device_id, event_time, sequence, id)`，避免相同时间戳造成跳页或重复。

## 7. Alert Events

### GET `/api/v1/devices/{deviceId}/alerts`

Query parameters：`from`、`to`、`limit`、`cursor` 与历史遥测一致，另支持：

| Name | Type | Meaning |
| --- | --- | --- |
| `state` | enum | 按 `suspect/fire_warning/acknowledged/recovered` 过滤 |
| `active` | boolean | 仅返回尚未结束的事件 |

Response `200 OK`：

```json
{
  "items": [
    {
      "id": "01K5H7T7T4J2MYE7Y0BR1ZBQ0Q",
      "deviceId": "MCU001",
      "state": "fire_warning",
      "startedAt": "2026-09-18T11:18:00Z",
      "acknowledgedAt": null,
      "endedAt": null,
      "evidence": {
        "gasRise": 187.0,
        "gasRiseThreshold": 150.0,
        "temperatureRateCPerMinute": 4.2,
        "temperatureRateThresholdCPerMinute": 3.0,
        "sampleCount": 8
      }
    }
  ],
  "nextCursor": null
}
```

事件必须保存触发证据，客户端不能仅凭展示时的最新值反推历史告警原因。

## 8. Thresholds

### GET `/api/v1/devices/{deviceId}/thresholds`

区分 Backend 期望配置与设备确认配置。两者版本不一致表示控制命令仍在等待、失败或设备离线。

Response `200 OK`：

```json
{
  "desiredVersion": 4,
  "confirmedVersion": 3,
  "temperatureHighC": 30.0,
  "gasHighPpm": 80.0,
  "updatedAt": "2026-09-18T11:10:00Z",
  "confirmationState": "pending"
}
```

`confirmationState`：`confirmed | pending | rejected | timed_out`。

### PUT `/api/v1/devices/{deviceId}/thresholds`

校验阈值，生成新版本并异步发布 `set_thresholds` MQTT 命令。HTTP 202 只表示 Backend 接受命令，不表示设备已写入 Flash。

Headers：

```http
Content-Type: application/json
Idempotency-Key: 01K5H0PN0M1N9NB8B7RBTVWT8P
```

Request：

```json
{
  "temperatureHighC": 30.0,
  "gasHighPpm": 80.0
}
```

初始允许范围：

- `temperatureHighC`: 0–80 °C；
- `gasHighPpm`: 1–999 ppm。

最终范围必须与固件能力一致。若气体尚未完成 ppm 标定，应在协议评审后改用明确命名的 ADC 阈值，不能混用单位。

Response `202 Accepted`：

```json
{
  "requestId": "01K5H0PN0M1N9NB8B7RBTVWT8P",
  "status": "pending",
  "desiredVersion": 4,
  "expiresAt": "2026-09-18T11:21:30Z"
}
```

Special errors：

- `409 version_conflict`：同一幂等键对应不同 Payload；
- `422 invalid_threshold`：范围或单位无效；
- `503 broker_unavailable`：没有成功发布命令。

设备 ack 为 `applied` 后才更新 `confirmedVersion`。超时不得回滚 `desiredVersion`，而是标记 `timed_out`，供用户重试或诊断。

## 9. Buzzer Mute

### POST `/api/v1/devices/{deviceId}/commands/mute`

静音只抑制蜂鸣器，不清除 `localAlarm`、不关闭 LED/OLED、不停止采样和上报。

Headers：必须包含 `Idempotency-Key`。

Request：

```json
{
  "muted": true
}
```

Response `202 Accepted`：

```json
{
  "requestId": "01K5H0M8YH1F4H4X6B62R9JB5A",
  "status": "pending",
  "expiresAt": "2026-09-18T11:21:30Z"
}
```

设备离线时是否允许排队必须在产品规则中冻结。安全默认建议为：命令短期排队但带 `expiresAt`，过期后绝不在设备重连时执行。

## 10. WebSocket Telemetry

### GET `/ws/v1/devices/{deviceId}/telemetry`

客户端发送标准 WebSocket Upgrade。鉴权失败在 Upgrade 前返回 HTTP 错误；成功返回 `101 Switching Protocols`。

服务端事件 Envelope：

```json
{
  "type": "telemetry.updated",
  "eventId": "01K5H8E4SXGPHCQQY6H11XK9RQ",
  "occurredAt": "2026-09-18T11:20:30Z",
  "deviceId": "MCU001",
  "data": {
    "sequence": 42,
    "temperatureC": 28.0,
    "humidityRh": 61.0,
    "gasAdcFiltered": 1328,
    "gasPpm": 25.0,
    "localAlarm": true,
    "buzzerMuted": false
  }
}
```

事件类型：

| Type | Meaning |
| --- | --- |
| `telemetry.updated` | 新的有效遥测 |
| `device.status_changed` | online/offline/unknown 变化 |
| `alert.state_changed` | 复合预警状态变化 |
| `command.status_changed` | 控制命令确认、拒绝或超时 |
| `thresholds.confirmed` | 设备确认阈值版本 |

连接要求：

- 服务端发送 ping，客户端响应 pong；具体周期在实现时固定并文档化。
- 慢客户端采用有界队列；超过上限应断开并要求客户端 REST 补数，不能无限占用内存。
- WebSocket 仅用于实时增量，不保证历史补发。重连后客户端先请求 latest/history，再订阅实时流。
- 同一 `eventId` 可用于客户端去重；客户端不能假定事件绝不重复。

## 11. Consistency and Command Lifecycle

控制命令状态建议为：

```text
accepted → published → applied
                    ↘ rejected
                    ↘ timed_out
          ↘ publish_failed
```

REST 202 对应 `accepted` 或已成功进入可靠发布流程。Backend 必须保存 requestId、用户、设备、期望 Payload、幂等键、发布时间、设备 ack 与失败原因。

遥测、设备状态、告警和控制结果可能存在短暂最终一致性。API 不得把 Backend 期望值冒充设备确认值。

## 12. Test Requirements Per Route

每条路由至少具备：

1. 方法与路径匹配测试；
2. 合法请求/响应 Schema 测试；
3. 非法 deviceId、JSON、Query 和范围测试；
4. 401/403/404 等权限与资源边界测试；
5. 数据层或 Broker 失败映射测试；
6. 幂等、重复、超时和并发测试（控制路由）；
7. MQTT/数据库真实集成测试（接入后）；
8. WebSocket Upgrade、消息、心跳、慢消费者与重连测试（实现后）。

Backend 统一质量命令：

```sh
go fmt ./...
go vet ./...
go test -race -coverprofile=coverage.out ./...
go tool cover -func=coverage.out
```

可测试 Backend 包总行覆盖率至少 80%，新增/修改核心逻辑目标至少 90%。覆盖率只是门槛；错误语义、并发安全和关键集成链路仍需独立验证。

## 13. Change Process

修改路由或字段时必须在同一 PR 中：

1. 更新本文件；
2. 更新 `../../docs/api/openapi.yaml`；
3. MQTT 字段变化时更新 `../../docs/device-protocol.md`；
4. 更新 Handler、模型和测试；
5. 通知 Hardware、KMP、微信端负责人；
6. 在 Multica issue 记录兼容性、迁移和版本策略。
