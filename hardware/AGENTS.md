# Hardware Agent Rules

- Preserve existing working code. Reuse first.
- 未经硬件验证，不修改现有设备通信协议。
- 不随意修改 GPIO、传感器、时钟、MCU、板级配置或报警逻辑。
- 所有修改必须考虑真实硬件、电气限制、交叉编译、烧录和现场验证条件。
- 与 Backend 相关的协议、帧格式、字段语义或连接方式变化必须显式记录并同步协调。
- 避免无意义格式化、目录迁移和大规模重构；现有代码默认是高复用资产。
- 构建成功不等同于烧录和实机验证成功，报告中必须区分两者。
- 当前硬件事实源是 STM32F103C8T6 + STM32F10x Standard Peripheral Library，而不是 HAL；迁移 HAL 需要单独决策，不能作为 MQTT 改造的附带重构。
- 当前已接入 DHT11、PA1 上的 MQ135 模拟采集、PB8/PB9 SSD1306 OLED、PA4 LED、PB13 蜂鸣器和 USART1 ESP8266。
- 目标通信为 EMQX MQTT，主题及 JSON 草案见 `../docs/device-protocol.md`；当前实现仍是 TCP 文本帧，迁移前必须保留本地报警闭环。
- 断网自治是硬性要求：网络、Broker 或 Backend 故障不得阻止本地采样、阈值判断和声光报警。
- 远程静音只能抑制蜂鸣器，不能关闭传感器采样、OLED 状态、LED 告警或告警上报；阈值写入 Flash 前必须校验范围并考虑掉电完整性。
- 负责 Hardware 的成员/Agent 必须依照根规则使用 `multica` CLI，同步接线、固件版本、构建/烧录/实机结果、协议变更和硬件阻塞。
- Hardware 修改遵守根 Git/PR 流程，分支使用 `feat/hardware-...`、`fix/hardware-...` 等完整名称；涉及引脚、协议或阈值的 PR 必须附编译结果和实机验证状态。
