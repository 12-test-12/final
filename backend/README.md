# Backend

该目录是项目的 Go 服务端边界，负责衔接硬件设备与两套客户端。当前使用标准库提供健康检查与已评审路由的可测试骨架；业务路由明确返回 `501 not_implemented`，尚未接入数据库、MQTT、WebSocket 或 Web Framework。

## Current Status

运行：

```sh
go run .
```

服务默认监听 `:8080`，可通过 `BACKEND_ADDR` 修改。当前可用健康检查：

```sh
curl http://localhost:8080/healthz
```

基础检查：

```sh
go fmt ./...
go vet ./...
go test ./...
```

`go.mod` 暂时使用 `final/backend` 作为 module path。当前目录不属于可识别的 Git 仓库，无法可靠推导远程仓库路径；确定代码托管地址后应更新该值。

## Boundary

- Backend 主体语言为 Go，初始化阶段优先使用标准库。
- Backend 是客户端与设备之间的系统边界。
- 后续底层 C 能力必须经清晰边界接入，业务代码不得直接依赖具体硬件实现。
- API 草案见 `../docs/api/openapi.yaml`，设备协议草案见 `../docs/device-protocol.md`，二者尚未冻结。

## Route Skeleton

| Method | Path | Purpose |
| --- | --- | --- |
| GET | `/healthz` | 进程健康检查，已实现 |
| GET | `/api/v1/devices/{deviceId}/status` | 设备在线状态 |
| GET | `/api/v1/devices/{deviceId}/telemetry/latest` | 最新遥测 |
| GET | `/api/v1/devices/{deviceId}/telemetry` | 历史遥测 |
| GET | `/api/v1/devices/{deviceId}/alerts` | 历史告警 |
| GET/PUT | `/api/v1/devices/{deviceId}/thresholds` | 查询或修改阈值 |
| POST | `/api/v1/devices/{deviceId}/commands/mute` | 远程静音/恢复 |
| GET | `/ws/v1/devices/{deviceId}/telemetry` | WebSocket 实时流 |

## Collaboration Workflow

```sh
git clone <repository-url>
cd final
git switch main
git pull --ff-only
git switch -c feat/backend-telemetry-query

cd backend
go fmt ./...
go vet ./...
go test ./...
cd ..

git add backend docs/api/openapi.yaml docs/device-protocol.md
git commit -m "feat(backend): add telemetry query"
git push -u origin feat/backend-telemetry-query
```

分支必须采用 `<type>/backend-<complete-description>`，例如 `fix/backend-offline-timeout`。PR 必须关联 Multica issue并列出路由、Schema、迁移影响和测试结果；API/MQTT 契约变化需要 Hardware 与客户端负责人审核。所有检查通过并获批准后才能合并。
