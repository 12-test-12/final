# Project Overview

这是一个由 5 人团队完成的软硬件综合实训项目。系统最终由硬件、服务端和客户端三层组成：

```text
Hardware
   ↓
Backend
   ↓
Client
```

客户端保留两套相互独立的实现方案：

- A. KMP Client：以 Kotlin Multiplatform 共享可复用的客户端业务逻辑。
- B. WeChat Native Client：使用微信原生小程序技术实现，作为对照基线。

后续工程对照应基于相同需求、相同 Backend API、相同硬件数据源、尽可能相同的 UI/UX 和相同验收场景。两套客户端的内部实现保持独立，避免为了对齐目录或代码而引入隐式耦合。

## Repository Structure

```text
hardware/          STM32、传感器、显示、报警及设备通信实现
backend/           Go 服务端（当前仅有最小可运行入口）
client-kmp/        Kotlin Multiplatform 客户端方案
client-wx-native/  微信原生小程序基线方案
```

## Current Phase

当前阶段：**System Design and API Contract Draft**。

项目主题为“智慧机房/实验室微环境动环监控与早期火情预警系统”。目标链路为：

```text
Sensors → STM32 local safety loop → ESP8266/MQTT → EMQX
        → Go Backend → REST/WebSocket → WeChat/KMP Clients
```

当前仓库事实与目标态存在明确差异：硬件现用 STM32F10x Standard Peripheral Library（非 HAL）、DHT11 和 ESP8266 TCP 文本帧。MQTT、JSON Payload、远程阈值持久化、数据入库、复合预警和客户端业务页面均按阶段实施，不能把目标描述成已有能力。

设计事实源：

- [完整实现方案](docs/implementation-plan.md)
- [设备 MQTT 协议草案](docs/device-protocol.md)
- [Backend OpenAPI 草案](docs/api/openapi.yaml)

本阶段明确不做：

- 正式业务实现
- API 设计冻结（当前仅为可评审草案）
- 数据库设计
- UI 实现
- KMP / 微信性能对照
- CGO 集成
- 大规模重构

以上内容留待需求和跨模块契约明确后的后续阶段处理。

## Collaboration Workflow

首次仓库基线建立后，所有变更采用分支和 Pull Request 协作：

```sh
git clone <repository-url>
cd final
git switch main
git pull --ff-only
git switch -c feat/backend-telemetry-query

# 修改后执行所属模块 README 中的检查
git add <files>
git commit -m "feat(backend): add telemetry query contract"
git push -u origin feat/backend-telemetry-query
```

随后创建 PR，填写关联 Multica issue、变更范围、接口/协议影响、验证结果、风险与回退方式。PR 经相关模块负责人审核、检查通过并批准后才能合并；合并后删除功能分支并在 Multica 中把最终 PR/commit 同步到 issue。

分支格式为 `<type>/<module>-<complete-description>`：

- 类型：`feat`、`fix`、`docs`、`test`、`refactor`、`chore`
- 模块：`hardware`、`backend`、`kmp`、`wx`、`repo`、`docs`
- 示例：`feat/hardware-mqtt-telemetry`、`fix/backend-offline-timeout`、`docs/repo-api-contract`

禁止直接在 `main` 开发或使用 `dev`、`test1`、姓名等含糊分支名。完整约束见 [AGENTS.md](AGENTS.md)。
