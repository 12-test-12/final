# STM32F103C8 环境监测、阈值报警与 Wi-Fi 通信

本项目是一个基于 STM32F103C8T6 的裸机环境监测程序：通过 DHT11 采集温湿度，通过 ADC 采集 MQ135 的模拟输出，在 128×64 OLED 上轮播显示读数、气体详情、报警状态与网络状态。温度、湿度、气体浓度超限或温升/气体突增时，报警灯与蜂鸣器开启，且该判断不依赖网络。ESP8266/ESP8285 通过 Wi-Fi 建立 TCP 连接上传传感器数据，并把服务器下发的消息显示在 OLED 的网络页上。

本地判断逻辑（滤波、阈值、突增、静音优先级、页面内容）位于不依赖硬件的 `core/` 模块中，可在主机上完整测试；见「本地逻辑与主机测试」。

- 目标 MCU：STM32F103C8T6（Cortex-M3）
- 存储器配置：64 KiB Flash、20 KiB RAM
- 库：STM32F10x Standard Peripheral Library
- 构建系统：CMake 3.20+ + Arm GNU Toolchain
- 下载/调试接口：ST-LINK（SWD）

## 功能说明

主循环以 **100 ms** 为一个节拍，各任务按节拍分频执行：

| 任务 | 周期 | 说明 |
| --- | --- | --- |
| MQ135 ADC 采样 | 100 ms | 原始值进入 10 点滑动窗口 |
| DHT11 温湿度 | 1 s | 器件要求的最小采样间隔 |
| 本地安全判断 | 100 ms | 不等待网络；断网时照常执行 |
| OLED 刷新 | 500 ms | 每 2 s 轮播下一页 |
| 遥测上报（TCP） | 1 s | 沿用现有 `APP001` 文本帧 |

**本地报警**在每次判断时综合以下条件，任一条成立即点亮 `PA4` 并驱动 `PB13`：

1. 温度达到 `TEMP_HIGH_THRESHOLD_C`；
2. 湿度达到 `HUMIDITY_HIGH_THRESHOLD_RH`；
3. 滤波后气体估算值达到 `GAS_HIGH_THRESHOLD_PPM`；
4. 温度突增：60 秒窗口内累计上升达到 `TEMP_RISE_THRESHOLD_C`；
5. 气体突增：60 秒窗口内 `gasAdcFiltered` 增量达到 `GAS_RISE_THRESHOLD_ADC`（ADC 码）；
6. DHT11 读取失败：上报 `sensor_fault`，并保持最后一次有效读数。

阈值全部集中在 `STM32_Project1/User/app_config.h`，默认为 30 ℃、80 %RH、20 ppm、3 ℃ 突增、150 ADC 突增。
`hardware/core/env_monitor.h` 直接从该头文件取默认值，因此**只需要改一个地方**，设备行为与后台上报的阈值版本 1 会同步变化。

**静音语义**：远程静音只抑制蜂鸣器。LED、OLED 的告警标识、本地告警状态与上报全部保持有效；出现**新的**报警原因时会自动解除静音，避免一次静音把下一次报警一并压掉。

**滤波**：`gasAdcRaw` 与 `gasAdcFiltered` 同时保留。滤波使用固定 10 点环形窗口的算术平均，窗口未填满时按已采样本数取平均，因此上电后第一秒即可用，而不是等窗口填满才输出。气体浓度估算值由**滤波后**的 ADC 换算，保证同一帧里的 `gasAdcFiltered` 与 `gasPpm` 描述同一个采样。

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

OLED 轮播四页，每 2 秒切换，每 500 ms 刷新：

```text
Temp: 25C          ADC raw:1350      Alarm: ---         Linked:Lab
Humi: 050%         ADC flt:1328      Buzzer: off        msg:<第一条消息...>
Gas: 012ppm        Est: 012ppm*      Sensor: ok         <第二条消息...>
Thr: v1            Level: OK*        State: clear       <第三条消息...>
```

- 第 1 页：温度、湿度、气体估算值、当前生效的阈值版本。
- 第 2 页：原始与滤波后的 ADC 值、估算浓度、气体安全等级。`*` 表示估算来自**未标定**曲线，此时应以等级而不是精确 ppm 作为判断依据。
- 第 3 页：报警原因字母（`T` 温度、`H` 湿度、`G` 气体、`t` 温度突增、`g` 气体突增、`F` 传感器故障）、蜂鸣器实际状态、传感器状态、总状态。静音时显示 `State: MUTED`，**同时仍然显示报警原因**，不会把告警显示成正常。
- 第 4 页：Wi-Fi 状态与服务器下发的消息（沿用原来的 `msg:` 行为，最多 44 个 ASCII 字符，超出部分省略）。

Wi-Fi 初始化期间第 4 页顶部显示 `Linking:<SSID>`，完成后切换为 `Linked:<SSID>` 或 `Link fail`。
页面渲染逻辑位于 `hardware/core/display_model.c`，可在主机上测试，见下文「本地逻辑与主机测试」。

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

固件启动后，OLED 第 1 页显示 `Temp: <温度>C`、`Humi: <%RH>%`、`Gas: <ppm>ppm`、`Thr: v1`，随后每 2 秒轮播到下一页并回到第 1 页。

报警时第 3 页的 `Alarm:` 后出现对应字母，`Buzzer` 显示 `ON`，`State` 显示 `ALARM`；被远程静音后 `Buzzer` 变为 `off`、`State` 变为 `MUTED`，但**字母与 LED 不变**。DHT11 读取失败时第 3 页显示 `Sensor: FAULT` 与 `Alarm: F`，第 1 页保留最后一次有效读数。

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
├── CMakeLists.txt                 # 固件构建目标与源文件
├── CMakePresets.json             # Debug/Release 构建预设
├── cmake/arm-none-eabi-gcc.cmake # Arm GNU 交叉编译工具链
├── core/                          # 纯逻辑：无 STM32 依赖，可在主机测试
│   ├── env_monitor.[ch]          # 滤波、阈值、突增判断、告警状态与静音
│   ├── display_model.[ch]        # OLED 轮播页面内容
│   └── text_format.[ch]          # 无 libc 的整数/定点转文本
├── tests/                         # 主机单元测试（独立 CMake 工程）
├── scripts/host_coverage.sh       # 核心逻辑行覆盖率
├── STM32_Project1/               # 主程序、启动文件、外设库和链接脚本
├── ADC/                           # PA1 ADC 采样与 MQ135 浓度换算
├── dht11/                         # DHT11 温湿度驱动
├── Esp8266/                       # USART1 AT指令、Wi-Fi、TCP和下行消息解析
├── OLED/                          # OLED 驱动
├── OLED_DATA/                     # ASCII/中文字模
├── LED/                           # LED/蜂鸣器辅助驱动
└── 字模提取PCtoLCD2002/          # Windows 字模提取工具及配置示例
```

## 本地逻辑与主机测试

`hardware/core/` 下的三个模块不依赖 STM32、GPIO 或任何驱动，只做数值到决策的转换。所有安全关键判断（阈值边界、突增判定、传感器故障、静音优先级、页面内容）都在这里，因此可以在主机上完整测试，不需要开发板在桌上。

主机测试是独立的 CMake 工程，使用主机编译器；固件仍由交叉编译器构建，两者共用同一份 `core/` 源码。

```sh
cmake -S hardware/tests -B hardware/build/host-tests
cmake --build hardware/build/host-tests
./hardware/build/host-tests/host_tests
```

行覆盖率（阈值 80%，低于阈值脚本以非零码退出）：

```sh
cmake -S hardware/tests -B hardware/build/host-tests-coverage -DENABLE_COVERAGE=ON
cmake --build hardware/build/host-tests-coverage
./hardware/scripts/host_coverage.sh hardware/build/host-tests-coverage
```

该脚本使用 clang 的 profile 格式与 `llvm-profdata`/`llvm-cov`：macOS 上 clang 写出的文件名是 `<name>.c.gcno`，而 `gcov` 查找 `<name>.gcno`，因此 `--coverage` + gcov 无法读取自己产生的数据。

最新一次本机结果（macOS / Apple clang）：`core/` 三个模块合计 **96% 行覆盖、92% 分支覆盖**，286 项断言全部通过。

**主机测试不能替代实机验证。** 它证明的是判断逻辑本身正确，不能证明 DHT11 时序、MQ135 预热与标定、OLED 刷新、ESP8266 连接或电气连接在现场可用。见下文「已知限制」。

## Keil 工程说明

`STM32_Project1/Project.uvprojx` 是仓库中保留的旧 Keil 工程文件，但当前其源文件组为空，未完整登记现有代码。因此，Windows 下也建议使用上述 CMake + Arm GNU Toolchain 流程。若需使用 Keil µVision，应先重建工程分组、源文件、头文件路径、预处理宏和启动文件，不应直接依赖该文件构建当前固件。

## 已知限制与待验证项

以下内容**尚未**在真实硬件上验证，不得按已验证对待：

1. **未烧录、未上电验证。** 本仓库当前只有交叉编译成功与主机单元测试的结果。DHT11 时序、MQ135 预热曲线、OLED 刷新、ESP8266 连接都需要在开发板上复现。
2. **气体估算未标定。** `MQ135_EstimatePpm` 使用 `RL=1 kΩ`、`Ro=10 kΩ` 与简化拟合曲线的示例常数，`116.30 / ratio²` 只是近似。在完成预热、负载电阻确认与标准气体标定之前，`gasPpm` 只能作为相对指标，上位机应以 `gasCalibrated=false` 与 ADC 分级呈现。阈值 20 ppm 也只是一个相对限值。
3. **ESP8266 重连仍是阻塞的。** `ESP8266_Init` 与 `ESP8266_WaitFor` 最长可等待数秒，期间主循环不会前进，本地采样会被推迟最多数秒。报警输出（LED/蜂鸣器）是 GPIO 保持状态，因此已有报警不会中断；但**新的**报警最多会被推迟这几秒。把网络状态机改为由节拍驱动的非阻塞实现属于 SHIXUN-8 的范围，在此之前不得声称「断网自治」已完全达成。
4. **Wi-Fi 与服务器配置**依赖本机 `Esp8266/esp8266_config.local.h`。缺少该文件时固件使用不可联网的占位值，只保证可编译。
5. **阈值和突增参数未现场整定。** 默认值来自实施方案文档，尚未在真实环境中统计误报与漏报。
6. **页面文字为英文。** 现有字模资源包含中文字模，但轮播页面使用 16 字符宽的 ASCII 行；改为中文需要按宽度重新排版并核验字模覆盖范围。

## Reuse Assessment

本节基于当前目录中的源码、配置、文档、资源和已有构建产物进行初始化审计。审计未修改固件行为、GPIO、传感器配置或通信协议。

### Inventory

- 自研/项目源码：`STM32_Project1/main.c`，`core/`（env_monitor、display_model、text_format）、`tests/`（主机单元测试），以及 ADC/MQ135、DHT11、ESP8266、OLED、LED/蜂鸣器、按键/输入和 delay 模块。
- 平台依赖：STM32F10x Standard Peripheral Library、CMSIS/启动文件和 STM32F103C8 链接脚本。
- 构建配置：CMake 工程、Debug/Release presets、Arm GNU Toolchain 文件，以及一个未完整登记源码的旧 Keil 工程。
- 文档与资源：本 README、`BUILDING.md`、OLED 字模数据，以及随项目保存的 Windows 字模提取工具和配置。
- 生成物：`build/`、`STM32_Project1/Objects/` 和 `STM32_Project1/Listings/` 中存在历史编译缓存、ELF/HEX/BIN、目标文件和映射文件；它们是生成物，不应被视为可移植源码事实源。

### Directly Reusable

- STM32F103C8 启动文件、链接脚本、Standard Peripheral Library 与 CMake 编译目标。
- DHT11 读取、超时与校验和处理。
- OLED SSD1306 软件 I²C 驱动、显示缓存和字模资源。
- LED 与蜂鸣器驱动，以及按节拍分频的主循环。原有「1 秒循环 + 三阈值比较」已由 `core/env_monitor` 取代：判定逻辑被抽出为纯函数，新增滤波、突增判断与传感器故障处理，并可在主机上测试。
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
