# Device MQTT Protocol Draft

状态：`draft-v1`，尚未冻结。当前固件仍使用 TCP 文本帧，本文件描述目标 MQTT 契约。

## Transport

| Direction | Topic | QoS | Retain |
| --- | --- | --- | --- |
| Device → Cloud | `device/telemetry` | 1 | false |
| Cloud → Device | `device/control` | 1 | false |

首期遵照指定固定主题，通过 `deviceId` 区分设备。Broker ACL 必须限制设备发布/订阅方向。控制消息不得 retain，避免设备重连后执行过期命令。

JSON 使用 UTF-8；字段名和枚举区分大小写。所有消息包含 `schemaVersion`。时间戳采用 UTC Unix 毫秒；设备时间尚未同步时，`timestamp` 可为 `null`，但必须提供 `uptimeMs`，Backend 同时记录 `receivedAt`。

## Telemetry Payload

```json
{
  "schemaVersion": 1,
  "deviceId": "MCU001",
  "sequence": 42,
  "timestamp": 1790246400000,
  "uptimeMs": 125000,
  "temperatureC": 28.0,
  "humidityRh": 61.0,
  "gasAdcRaw": 1350,
  "gasAdcFiltered": 1328,
  "gasPpm": 25.0,
  "localAlarm": true,
  "alarmCauses": ["gas_high"],
  "buzzerMuted": false,
  "network": "online",
  "thresholdVersion": 3
}
```

### Telemetry Rules

- `sequence` 是设备启动期间单调递增的无符号计数，用于识别重复与丢包。
- `gasAdcRaw`、`gasAdcFiltered` 范围为 0–4095。
- `gasPpm` 是标定后的估算值；标定前客户端应优先展示 ADC 安全评级而非声称精确浓度。
- `alarmCauses` 可选值：`temperature_high`、`gas_high`、`rapid_temperature_rise`、`rapid_gas_rise`、`sensor_fault`。
- `network` 可选值：`online`、`reconnecting`。Backend 根据最后有效遥测独立计算设备是否离线。
- `localAlarm=true` 时，即使 `buzzerMuted=true`，LED、OLED 标识和上报仍保持告警。

## Control Payload

远程静音：

```json
{
  "schemaVersion": 1,
  "deviceId": "MCU001",
  "requestId": "01K5H0M8YH1F4H4X6B62R9JB5A",
  "issuedAt": 1790246400000,
  "expiresAt": 1790246460000,
  "type": "set_mute",
  "payload": {
    "muted": true
  }
}
```

修改阈值：

```json
{
  "schemaVersion": 1,
  "deviceId": "MCU001",
  "requestId": "01K5H0PN0M1N9NB8B7RBTVWT8P",
  "issuedAt": 1790246400000,
  "expiresAt": 1790246460000,
  "type": "set_thresholds",
  "payload": {
    "thresholdVersion": 4,
    "temperatureHighC": 30.0,
    "gasHighPpm": 80.0
  }
}
```

设备必须验证 `schemaVersion`、`deviceId`、过期时间、类型和数值范围，并按 `requestId` 去重。`set_mute` 不得清除告警状态；`set_thresholds` 只有在 Flash 校验写入成功后才更新 `thresholdVersion`。

## Command Acknowledgement

为闭合控制链路，设备使用同一上报主题发送确认事件；在实现评审时也可拆分为专用 ack 主题，但必须同步修改契约。

```json
{
  "schemaVersion": 1,
  "deviceId": "MCU001",
  "sequence": 43,
  "timestamp": 1790246401000,
  "uptimeMs": 126000,
  "messageType": "command_ack",
  "requestId": "01K5H0PN0M1N9NB8B7RBTVWT8P",
  "status": "applied",
  "thresholdVersion": 4,
  "errorCode": null
}
```

`status` 可选值：`applied`、`rejected`、`expired`、`duplicate`、`failed`。Backend REST 控制接口只表示命令已接受发布，客户端必须等待该确认或超时结果。

## Migration From Current Firmware

当前设备发送：

```text
REG|MCU001
APP001|<temperature>|<humidity>|<gas_ppm>
```

迁移时先在联调分支增加 MQTT 编解码和主题订阅，实机验证后再停止旧 TCP 发送。禁止在同一次未经验证的修改中同时迁移 HAL、重写传感器驱动并切换 MQTT。
