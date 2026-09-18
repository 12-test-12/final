# STM32F103C8 环境监测、阈值报警与 Wi-Fi 通信

本项目是一个基于 STM32F103C8T6 的裸机环境监测程序：通过 DHT11 采集温湿度，通过 ADC 采集 MQ135 的模拟输出，并在 128×64 OLED 上实时显示。温度、湿度或气体浓度达到阈值时，报警灯和蜂鸣器开启。ESP8266/ESP8285 通过 Wi-Fi 建立 TCP 连接，上传传感器数据并在 OLED 上显示服务器下发的消息。

- 目标 MCU：STM32F103C8T6（Cortex-M3）
- 存储器配置：64 KiB Flash、20 KiB RAM
- 库：STM32F10x Standard Peripheral Library
- 构建系统：CMake 3.20+ + Arm GNU Toolchain
- 下载/调试接口：ST-LINK（SWD）

## 功能说明

程序每 1 s 采集和刷新一次：

- DHT11 温度与湿度。
- MQ135 气体浓度估算值，ADC 通道为 `PA1 / ADC1 Channel 1`。
- OLED 显示温度、湿度、气体浓度和当前报警来源。
- 任意数据超限时，`PA4` 报警灯和 `PB13` 蜂鸣器开启。

三组阈值集中在 `STM32_Project1/User/app_config.h`，默认为 30 ℃、80 %RH 和 20 ppm。气体浓度超过 20 ppm 时会触发蜂鸣器。

## 硬件与接线

### 所需硬件

- STM32F103C8T6 开发板（例如 Blue Pill）
- 0.96 英寸 128×64 I²C OLED（常见 SSD1306，7 位地址 `0x3C`）
- DHT11 温湿度传感器
- MQ135 气体传感器模块（模拟输出）
- LED 和有源蜂鸣器（如开发板未集成）
- ST-LINK/V2 或兼容的 SWD 调试器
- USB 数据线和若干杜邦线

### 模块接线

| 模块 | 模块引脚 | STM32 引脚 | 说明 |
| --- | --- | --- | --- |
| OLED | VCC | 3.3V | 请以实际模块允许电压为准 |
| OLED | GND | GND | 共地 |
| OLED | SCL | PB8 | 软件模拟 I²C 时钟 |
| OLED | SDA | PB9 | 软件模拟 I²C 数据 |
| DHT11 | VCC | 3.3V | 供电 |
| DHT11 | GND | GND | 共地 |
| DHT11 | DATA | PA5 | 数字温湿度数据 |
| MQ135 | VCC | 按模块要求 | 常见加热器模块需 5V，以实物为准 |
| MQ135 | GND | GND | 必须与 STM32 共地 |
| MQ135 | AO | PA1 | ADC 输入必须限制在 0～3.3V |
| LED | 控制端 | PA4 | 高电平报警 |
| 有源蜂鸣器 | 控制端 | PB13 | 高电平报警 |
| ESP TX | PA10 / USART1 RX | ESP 发送、STM32 接收 |
| ESP RX | PA9 / USART1 TX | STM32 发送、ESP 接收 |
| ESP GND | GND | 必须共地 |
| ESP VCC/EN | 稳定 3.3V | 不要接 5V，电源需要留有充足的瞬时电流余量 |

## Wi-Fi 与 TCP 配置

Wi-Fi 名称、密码、TCP 服务器地址、端口和设备编号使用本机配置：

```sh
cp Esp8266/esp8266_config.example.h Esp8266/esp8266_config.local.h
```

填写 `esp8266_config.local.h` 后重新编译。该文件已被 Git 忽略，禁止提交真实 Wi-Fi 或服务器凭据；未创建本机配置时，`esp8266.h` 的安全占位值只保证工程可编译，不能连接真实网络。

网络调试助手应以 TCP Server 方式监听配置的端口。STM32 连接后先发送注册帧，然后周期发送数据：

```text
REG|MCU001
APP001|<temperature>|<humidity>|<gas_ppm>
```

OLED 仅作为网络消息屏使用。Wi-Fi 初始化期间顶部显示 `Linking:BobcGn`，初始化完成后根据 ESP 返回的热点连接状态切换为 `Linked:BobcGn` 或 `Link failed`；下方上电后显示 `msg:`，服务器向该 TCP 客户端发送 ASCII 消息后，ESP 的 `+IPD` 数据会被解析并显示：

```text
msg:<收到的消息>
```

消息会一直保留，直到服务器发送下一条消息；OLED 不再显示温度、湿度、Gas 和 Alarm 页面。较长消息会自动换行，最多显示 44 个 ASCII 字符。

### ST-LINK SWD 接线

| ST-LINK | STM32 |
| --- | --- |
| SWDIO | PA13 |
| SWCLK | PA14 |
| GND | GND |
| 3.3V / VTref | 3.3V |
| NRST（建议） | NRST |

> 接线和通电前请先核对开发板与仿真器标识。不要将 ST-LINK 的 5V 直接到 3.3V 引脚。

## 通用环境要求

下列命令需要能在终端中直接执行，即其 `bin` 目录已加入 `PATH`：

```text
cmake                 3.20 或更高版本
arm-none-eabi-gcc     Arm GNU bare-metal 交叉编译器
arm-none-eabi-objcopy 随 Arm GNU Toolchain 提供
arm-none-eabi-size    随 Arm GNU Toolchain 提供
```

macOS 上的现有 CMake 预设还需要 GNU Make；Windows PowerShell 流程则使用 Ninja。

安装完成后可检查：

```sh
cmake --version
arm-none-eabi-gcc --version
arm-none-eabi-objcopy --version
```

如果其中任意命令提示“找不到”，请先修正安装或 `PATH`，再配置项目。

## macOS 环境与运行

### 1. 安装构建工具

先安装 Xcode Command Line Tools：

```sh
xcode-select --install
```

如果已安装 [Homebrew](https://brew.sh/)，可用它安装 CMake 和 Arm GNU Toolchain：

```sh
brew install cmake arm-none-eabi-gcc
```

也可分别使用 [CMake 官方安装包](https://cmake.org/download/) 和 [Arm GNU Toolchain 官方发行版](https://developer.arm.com/Tools%20and%20Software/GNU%20Toolchain)。Apple Silicon Mac 需要选择与当前主机架构兼容的工具链；如使用 x86_64 版，则还需要 Rosetta 2。

### 2. 获取项目并进入根目录

```sh
git clone <项目仓库地址>
cd <项目目录>
```

如果项目由压缩包解压得到，直接在终端中 `cd` 到本 README 所在的根目录即可。

### 3. 编译固件

Debug 构建：

```sh
cmake --preset debug
cmake --build --preset debug
```

Release 构建：

```sh
cmake --preset release
cmake --build --preset release
```

### 4. 烧录并运行

安装跨平台的 [STM32CubeProgrammer](https://www.st.com/en/development-tools/stm32cubeprog.html)，然后：

1. 按上表连接 ST-LINK 与开发板，再将 ST-LINK 接入 Mac。
2. 打开 STM32CubeProgrammer，选择 `ST-LINK` 和 `SWD`，点击 **Connect**。
3. 选择 `build/debug/STM32_Project1.hex`（或 Release 目录中的文件）。
4. 启用烧录后校验，执行下载，然后复位开发板。

如果 `STM32_Programmer_CLI` 已加入 `PATH`，也可使用：

```sh
STM32_Programmer_CLI -c port=SWD -w build/debug/STM32_Project1.hex -v -rst
```

## Windows 环境与运行

### 1. 安装构建工具

安装以下软件：

1. [CMake 3.20+](https://cmake.org/download/)：安装时选择将 CMake 加入 `PATH`。
2. [Arm GNU Toolchain](https://developer.arm.com/Tools%20and%20Software/GNU%20Toolchain)：选择 Windows 主机、`arm-none-eabi` 裸机目标的版本，并将工具链的 `bin` 目录加入 `PATH`。
3. [Ninja](https://ninja-build.org/)：安装后将 `ninja.exe` 所在目录加入 `PATH`。
4. [STM32CubeProgrammer](https://www.st.com/en/development-tools/stm32cubeprog.html)：用于通过 ST-LINK 烧录；安装包中也提供 Windows ST-LINK USB 驱动。

> 安装或修改 `PATH` 后，请关闭并重新打开 PowerShell，再执行前文的版本检查命令。

另外检查 Ninja：

```powershell
ninja --version
```

### 2. 在 PowerShell 中编译

```powershell
git clone <项目仓库地址>
Set-Location <项目目录>

cmake -S . -B build/windows-debug -G Ninja `
  -DCMAKE_TOOLCHAIN_FILE=cmake/arm-none-eabi-gcc.cmake `
  -DCMAKE_BUILD_TYPE=Debug
cmake --build build/windows-debug
```

如需 Release 固件：

```powershell
cmake -S . -B build/windows-release -G Ninja `
  -DCMAKE_TOOLCHAIN_FILE=cmake/arm-none-eabi-gcc.cmake `
  -DCMAKE_BUILD_TYPE=Release
cmake --build build/windows-release
```

> 仓库中的 `debug` / `release` 预设固定使用 `Unix Makefiles`，主要面向 macOS 等 Unix 环境。为避免 Windows 原生 PowerShell 下的生成器差异，上述命令显式使用 Ninja。

### 3. 烧录并运行

1. 用 SWD 连接 ST-LINK 和开发板，将 ST-LINK 接入电脑。
2. 打开 STM32CubeProgrammer，选择 `ST-LINK` / `SWD` 并连接。
3. 选择 `build\windows-debug\STM32_Project1.hex`。
4. 执行擦除、烧录和校验，然后复位开发板。

已将 CLI 加入 `PATH` 时，可在 PowerShell 中执行：

```powershell
STM32_Programmer_CLI -c port=SWD -w build\windows-debug\STM32_Project1.hex -v -rst
```

## 构建产物

以 Debug 为例，macOS 预设的文件位于 `build/debug/`，Windows Ninja 流程的文件位于 `build/windows-debug/`：

| 文件 | 用途 |
| --- | --- |
| `STM32_Project1.elf` | 带调试信息的可执行固件，适合 GDB/调试器 |
| `STM32_Project1.hex` | Intel HEX 固件，推荐用于烧录 |
| `STM32_Project1.bin` | 原始二进制固件，烧录起始地址为 `0x08000000` |
| `STM32_Project1.map` | 链接映射，用于分析符号和存储器占用 |

清理某个配置的构建结果：

```sh
cmake --build --preset debug --target clean
```

如果工具链路径变更后 CMake 仍使用旧路径，删除对应的 `build/debug` 或 `build/release` 目录后重新配置。

## 预期现象

固件启动后，OLED 应显示：

```text
Temp: 25C
Humi: 060%
Gas: 025ppm
Alarm:---
```

`Alarm:` 后的 `T`、`H`、`G` 分别表示温度、湿度、气体浓度超限，`-` 表示对应项正常。DHT11 通信失败时温湿度位置显示 `ERR`。

## 常见问题

### CMake 找不到 Arm 编译器

典型提示为 `Could not find CMAKE_C_COMPILER using arm-none-eabi-gcc`。先确认：

```sh
arm-none-eabi-gcc --version
```

如命令不可用，将 Arm GNU Toolchain 的 `bin` 目录加入 `PATH`，重开终端，并删除已生成的构建目录后再试。

### CMake 找不到构建程序

macOS 预设生成器是 `Unix Makefiles`，因此需要 `make`；Windows 命令使用 `Ninja`，因此需要 `ninja.exe`。两者不可互相替代。请按所在平台执行 `make --version` 或 `ninja --version` 进行确认。

### ST-LINK 无法连接

- 检查 SWDIO、SWCLK、GND 和 3.3V/VTref，最好同时连接 NRST。
- 确认目标板已供电，且 ST-LINK 驱动/固件已正确安装。
- 尝试降低 SWD 频率，或选择 **Connect under reset** 再连接。

### OLED 无显示

- 检查 PB8/PB9 是否接反，以及电源和共地。
- 本驱动默认使用 128×64 OLED 和地址 `0x3C`。
- 默认显示方向与 SSD1306 配置对应；其他控制器或尺寸的屏幕可能需要修改 `OLED/OLED.c`。

### 气体浓度不准确

- 当前 `ADC/adc.c` 使用 `RL=1 kΩ`、`Ro=10 kΩ` 和简化拟合曲线，显示值为估算值。
- 实际使用前需要充分预热 MQ135，并根据传感器、负载电阻和标准气体校准 `Ro` 与拟合参数。
- 保证 `PA1` 电压不超过 3.3V；必要时在 MQ135 模块 AO 与 STM32 之间加入分压。

## 项目结构

```text
.
├── CMakeLists.txt                 # 构建目标与源文件
├── CMakePresets.json             # Debug/Release 构建预设
├── cmake/arm-none-eabi-gcc.cmake # Arm GNU 交叉编译工具链
├── STM32_Project1/               # 主程序、启动文件、外设库和链接脚本
├── ADC/                           # PA1 ADC 采样与 MQ135 浓度估算
├── dht11/                         # DHT11 温湿度驱动
├── Esp8266/                       # USART1 AT指令、Wi-Fi、TCP和下行消息解析
├── OLED/                          # OLED 驱动
├── OLED_DATA/                     # ASCII/中文字模
├── LED/                           # LED/蜂鸣器辅助驱动
└── 字模提取PCtoLCD2002/          # Windows 字模提取工具及配置示例
```

## Keil 工程说明

`STM32_Project1/Project.uvprojx` 是仓库中保留的旧 Keil 工程文件，但当前其源文件组为空，未完整登记现有代码。因此，Windows 下也建议使用上述 CMake + Arm GNU Toolchain 流程。若需使用 Keil µVision，应先重建工程分组、源文件、头文件路径、预处理宏和启动文件，不应直接依赖该文件构建当前固件。

## Reuse Assessment

本节基于当前目录中的源码、配置、文档、资源和已有构建产物进行初始化审计。审计未修改固件行为、GPIO、传感器配置或通信协议。

### Inventory

- 自研/项目源码：`STM32_Project1/main.c`，ADC/MQ135、DHT11、ESP8266、OLED、LED/蜂鸣器、按键/输入和 delay 模块。
- 平台依赖：STM32F10x Standard Peripheral Library、CMSIS/启动文件和 STM32F103C8 链接脚本。
- 构建配置：CMake 工程、Debug/Release presets、Arm GNU Toolchain 文件，以及一个未完整登记源码的旧 Keil 工程。
- 文档与资源：本 README、`BUILDING.md`、OLED 字模数据，以及随项目保存的 Windows 字模提取工具和配置。
- 生成物：`build/`、`STM32_Project1/Objects/` 和 `STM32_Project1/Listings/` 中存在历史编译缓存、ELF/HEX/BIN、目标文件和映射文件；它们是生成物，不应被视为可移植源码事实源。

### Directly Reusable

- STM32F103C8 启动文件、链接脚本、Standard Peripheral Library 与 CMake 编译目标。
- DHT11 读取、超时与校验和处理。
- OLED SSD1306 软件 I²C 驱动、显示缓存和字模资源。
- LED 与蜂鸣器驱动、1 秒采样主循环和阈值报警流程。
- USART1/ESP8266 AT 通信、TCP 连接、断线状态处理及 `+IPD` 下行文本解析，可作为后续联调基础。
- 已有 HEX/BIN 可用于追溯历史结果，但重新烧录前应从当前源码重新构建。

按当前可辨识的 8 个固件功能单元（主流程、平台/构建、DHT11、ADC/MQ135、OLED、LED/蜂鸣器、ESP8266/TCP、辅助输入）统计，6 个可直接沿用，2 个需要环境配置或实机校准后沿用，即约 75% 可直接复用、25% 可经少量修改复用；没有发现必须整体重写的单元。该比例是功能单元口径，不是代码行数口径，也不代表已经完成实机验收。

### Reusable With Minor Changes

- Wi-Fi、服务器地址、端口和设备 ID 通过被 Git 忽略的 `Esp8266/esp8266_config.local.h` 按部署环境调整；仓库仅保留无敏感信息的示例。
- MQ135 估算使用固定 `RL=1 kΩ`、`Ro=10 kΩ` 和简化曲线；代码可复用，但最终测量需要预热、分压确认和实物标定。
- 阈值集中在 `STM32_Project1/User/app_config.h`，结构可沿用，数值需以最终需求为准。
- 若团队坚持使用 Keil，需要补齐工程分组和源文件配置；当前推荐继续使用已可工作的 CMake 工程。

### Needs Follow-up

- 在目标开发板上完成一次完整的接线、烧录、传感器读取、报警、Wi-Fi/TCP 上传与下行消息验证；当前构建成功不能替代实机验证。
- 与 Go Backend 联调现有两类换行结尾文本帧：注册帧 `REG|<device-id>`，数据帧 `APP001|<temperature>|<humidity>|<gas_ppm>`。
- 现有协议适合作为最小联调 baseline，但不建议未经验证直接冻结为最终协议：它缺少显式协议版本、字段名/单位声明、错误帧、鉴权和更严格的边界约束。后续若变化，需同时记录并协调 Hardware 与 Backend。
- 补充可重复的烧录/实机验收记录；当前仓库只有构建和操作说明，没有自动化硬件测试。

### Known Risks / Environment Dependencies

- 网络部署值依赖本机 `Esp8266/esp8266_config.local.h`；缺少该文件时固件只能使用不可联网的安全占位值。
- GPIO、外设和器件型号均与当前接线强绑定：DHT11 PA5、MQ135 PA1、LED PA4、蜂鸣器 PB13、OLED PB8/PB9、ESP8266 USART1 PA9/PA10。
- 需要 CMake 3.20+、Arm GNU Toolchain、Make（presets）以及烧录时的 ST-LINK/STM32CubeProgrammer。
- 当前 `build/debug` 和 `build/release` 内含从旧源码路径生成的 CMake 缓存；移动项目后直接复用会发生路径冲突。应重新配置构建目录，而不是把历史缓存当作可移植构建环境。
- `字模提取PCtoLCD2002/` 包含 Windows 可执行程序和运行库，仅适合受控的 Windows 环境；其来源、许可与安全性需由团队确认。
- DHT11 时序、ESP AT 固件差异、MQ135 供电/分压与预热均依赖真实硬件环境。

## Collaboration Workflow

```sh
git clone <repository-url>
cd final
git switch main
git pull --ff-only
git switch -c feat/hardware-mqtt-telemetry

# 修改并完成交叉编译；有条件时执行烧录与实机验证
git add hardware docs/device-protocol.md
git commit -m "feat(hardware): publish mqtt telemetry"
git push -u origin feat/hardware-mqtt-telemetry
```

分支必须采用 `<type>/hardware-<complete-description>`，例如 `fix/hardware-dht11-timeout`。提交 PR 时关联 Multica issue，记录板卡/接线、工具链、构建结果、烧录与实机验证状态；协议、引脚或阈值变化必须请求 Backend 及相关负责人审核。检查通过并获批准后才能合并。
