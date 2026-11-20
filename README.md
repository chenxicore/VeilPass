# VeilPass

> 轻量跨平台独立 Chrome 环境启动器：按代理出口自动同步时区、当地时间、语言与指纹，开箱即用。  
> A lightweight, zero-dependency launcher for isolated Chrome environments—automatically syncing timezone, local time, locale, and browser fingerprint to your proxy exit.

## 功能特性

- **零外部依赖**：macOS 原生 Swift 通用包（`arm64` + `x86_64`）与 Windows 原生 Win32 单文件 `.exe`（纯 Go 标准库），无需 Python 或额外运行时。
- **独立档案隔离**：每次启动拥有独立的 User Data 目录，互不串号、互不污染 Cookie 与本地缓存。
- **出口环境自动对齐**：自动探测代理出口国家/城市与时区，同步注入匹配的时区、当地时间、语言（`Accept-Language` / `navigator.languages`）、视口分辨率与硬件指纹；新打开的标签页也会自动继承。
- **三种网络出口**：
  - **系统代理**：自动读取 macOS / Windows 系统当前开启的代理设置。
  - **手动代理**：支持填写无需账号密码的 `http` / `https` / `socks4` / `socks5` 地址。
  - **直连网络**：强制绕过系统代理，直接基于本地网络构造独立环境。
- **多实例管理**：自动识别运行中的隔离 Chrome 窗口（展示地区、代理、当地时间与调试端口），支持一键单独关闭。

## 目录结构

```text
VeilPass/
├── macos/
│   ├── StableBrowserApp.swift   # macOS SwiftUI 原生桌面端与浏览器隔离引擎
│   └── build_macos.sh           # 生成应用图标并编译 Universal Binary / .dmg / .zip
└── windows/
    ├── engine.go                # 纯 Go 标准库隔离浏览器内核（CDP / SOCKS5 / tzdata）
    ├── engine_test.go           # 核心单元测试
    ├── system_windows.go        # Windows 注册表系统代理读取与 Chrome 路径发现
    ├── system_other.go          # 非 Windows 平台测试桩
    ├── ui_windows.go            # 高分屏（PerMonitorV2）Win32 原生界面
    ├── gen_syso.go              # 内嵌图标与 DPI Manifest 的 COFF 资源生成器
    └── build_windows.sh         # macOS 上一键交叉编译 Windows x64 单文件 .exe
```

## 构建方式

### macOS（需 Xcode / Swift 6+，支持 `arm64` + `x86_64` 双架构）

```bash
chmod +x macos/build_macos.sh

# 仅编译 VeilPass.app
./macos/build_macos.sh

# 编译并生成 VeilPass.dmg 安装镜像
./macos/build_macos.sh --dmg

# 编译并安装到 /Applications/VeilPass.app
./macos/build_macos.sh --install
```

### Windows（在 macOS 上交叉编译单文件 `VeilPass.exe`，需 Go 1.22+）

```bash
chmod +x windows/build_windows.sh
./windows/build_windows.sh
```

会在项目根目录生成 `VeilPass.exe`（`PE32+ GUI x86-64`）。

## 运行前置条件

- 目标电脑需已安装 **Google Chrome**。
- macOS 分发的未公证自签名应用首次打开时，如遇 Gatekeeper 提示，右键点「打开」或在终端执行一次 `xattr -cr /Applications/VeilPass.app` 即可。
