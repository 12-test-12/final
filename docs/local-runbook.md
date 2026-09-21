# 本地启动手册：硬件与 Go 服务端

本手册用于在同一局域网中启动真实 STM32/ESP8266、EMQX、`postgres-dev` 和 Go Backend。建议顺序是：**数据库 → Broker → Backend → 硬件**。

## 1. 前置条件

- Docker Desktop 或 OrbStack，并已存在 `postgres-dev` 容器。
- Go 1.25+、CMake 3.20+、GNU Make 和 Arm GNU Toolchain。
- ST-LINK 与 STM32F103C8T6 已通过 SWD 连接。
- ESP8266 与运行 EMQX 的电脑处于同一 Wi-Fi/手机热点。

在仓库根目录执行本文命令。不要将 Wi-Fi 密码或 PostgreSQL 密码写入受 Git 管理的文件。

## 2. 启动 PostgreSQL 和 EMQX

```sh
docker start postgres-dev
docker exec -i postgres-dev psql -U postgres -d postgres -v ON_ERROR_STOP=1 \
  < backend/database/bootstrap.sql
docker compose -f deploy/compose.yaml up -d emqx
docker compose -f deploy/compose.yaml ps
```

`bootstrap.sql` 可重复执行，只初始化 `lab` 数据库所需表、约束和索引。EMQX 对外使用 `1883`，Dashboard 使用 `18083`。

## 3. 启动 Go Backend

先确认 `postgres-dev` 的现有密码，然后在终端中临时设置环境变量：

```sh
cd backend
DATABASE_URL='postgres://postgres:<existing-password>@localhost:5432/lab?sslmode=disable' \
MQTT_BROKER_URL='localhost:1883' \
MQTT_USERNAME='backend' \
MQTT_PASSWORD='backend-secret' \
DEVICE_ALLOWLIST='MCU001' \
AUTH_MODE='none' \
BACKEND_ADDR=':8080' \
go run .
```

`AUTH_MODE=none` 只用于受信任的本地联调网络。成功时日志会出现 `connected to postgres and applied migrations`、`mqtt connected` 和 `http server listening`。

在另一个终端验证：

```sh
curl http://localhost:8080/healthz
curl http://localhost:8080/api/v1/devices/MCU001/status
curl http://localhost:8080/api/v1/devices/MCU001/telemetry/latest
```

省略 `DATABASE_URL` 时将使用内存存储，进程退出后数据丢失，不算完整验收。

## 4. 配置硬件网络

```sh
cp hardware/Esp8266/esp8266_config.example.h \
   hardware/Esp8266/esp8266_config.local.h
```

编辑被 Git 忽略的 `esp8266_config.local.h`：

```c
#define WIFI_SSID       "your-hotspot-name"
#define WIFI_PASSWORD   "your-hotspot-password"
#define SERVER_IP       "192.168.x.x"
#define SERVER_PORT     "1883"
#define DEVICE_ID       "MCU001"
```

`SERVER_IP` 必须是运行 EMQX 的 Mac/PC 在当前 Wi-Fi 或手机热点中的局域网 IP，**不能**填 `localhost` 或 `127.0.0.1`。macOS 可查找 Wi-Fi 接口后查询 IPv4：

```sh
networksetup -listallhardwareports
ipconfig getifaddr en0
```

如 Wi-Fi 对应的不是 `en0`，请替换为实际接口名。

## 5. 编译、烧录并运行固件

```sh
cd hardware
cmake --preset debug
cmake --build --preset debug
```

使用 STM32CubeProgrammer 选择 `hardware/build/debug/STM32_Project1.hex`，通过 ST-LINK/SWD 烧录、校验并复位。如已安装 OpenOCD，也可在 `hardware` 目录执行：

```sh
openocd -f interface/stlink.cfg -f target/stm32f1x.cfg \
  -c 'program build/debug/STM32_Project1.elf verify reset exit'
```

上电后预期：OLED 轮播数据；完成 Wi-Fi、MQTT CONNACK 和 SUBACK 后显示联网；设备每秒向 `device/telemetry` 发布 QoS 1 JSON；告警时 LED 亮且 PB13 输出 2 kHz PWM。

## 6. 联调检查与停止

```sh
docker exec lab-monitoring-emqx-1 emqx ctl clients list
docker exec postgres-dev psql -U postgres -d lab \
  -c "SELECT device_id, boot_id, sequence, received_at FROM telemetry ORDER BY received_at DESC LIMIT 5;"
```

正常时 EMQX 同时存在 `MCU001` 和 `lab-backend`，REST 返回 `connectivity=online`，PostgreSQL 的遥测序号持续增加。

停止 Backend 可按 `Ctrl-C`。停止本项目 EMQX：

```sh
docker compose -f deploy/compose.yaml down
```

该命令不会删除或停止独立的 `postgres-dev`。

## 7. 当前已知限制

- 真实遥测上行已通；远程静音、阈值写入和设备 ACK 还未接入固件主循环。
- 当前实测板的 DHT11 返回时序错误，需确认 PA5、上拉电阻、3.3 V/GND 和传感器型号。
- MQ135 ppm 尚未现场标定，验收时应同时观察原始/滤波 ADC。
