# DeepSR - AI 画质超分辨率与人脸重塑引擎

> 基于 **DirectML + ONNX Runtime** 的高性能 AI 画质增强系统，覆盖全品牌显卡（NVIDIA / AMD / Intel）与 CPU 回退算力。包含 **GUI 桌面客户端** 与 **CLI 命令行引擎** 两种形态。

---

## 目录

- [一、 项目概览与架构拓扑](#一-项目概览与架构拓扑)
- [二、 GUI 桌面客户端 (DeepSR)](#二-gui-桌面客户端-deepsr)
  - [1. 技术栈](#1-技术栈)
  - [2. 核心功能与页面设计](#2-核心功能与页面设计)
  - [3. 关键架构设计与工程亮点](#3-关键架构设计与工程亮点)
  - [4. GUI 开发与编译构建](#4-gui-开发与编译构建)
- [三、 CLI 命令行引擎 (DeepSRCli)](#三-cli-命令行引擎-deepsrcli)
  - [1. 技术定位](#1-技术定位)
  - [2. 命令行参数详解](#2-命令行参数详解)
  - [3. 常用操作示例](#3-常用操作示例)
  - [4. CLI 编译构建](#4-cli-编译构建)
- [四、 预置模型矩阵](#四-预置模型矩阵)
- [五、 系统环境要求](#五-系统环境要求)

---

## 一、 项目概览与架构拓扑

DeepSR 项目由两个相互配合的核心子系统组成：

1. **`DeepSR/`（桌面客户端）**：基于 **Wails v2 + React 19 / TypeScript** 的原生桌面应用，提供可视化交互、视频/图片任务队列、模型下载切换与硬件监控。
2. **`DeepSRCli/`（底层推理引擎）**：基于 **Go + ONNX Runtime + DirectML** 的跨显卡原生 CLI，具备单帧/批量超分、人脸检测与保真修复、以及全内存流式管道能力。

```text
┌─────────────────────────────────────────────────────────────┐
│                      DeepSR (GUI 桌面应用)                   │
│      React 19 + TypeScript + Tailwind CSS v4 + Wails v2     │
└──────────────┬───────────────────────────────┬──────────────┘
               │                               │
               ▼                               ▼
┌──────────────────────────────┐ ┌────────────────────────────┐
│   DeepSRCli (deepsr-cli.exe) │ │    FFmpeg / FFprobe 工具链  │
│  ONNX Runtime + DirectML     │ │   NVENC / QSV / AMF 硬件编解码│
│  NVIDIA / AMD / Intel / CPU  │ │   抽帧、音画合流、多媒体探针 │
└──────────────────────────────┘ └────────────────────────────┘
```

---

## 二、 GUI 桌面客户端 (DeepSR)

### 1. 技术栈

| 层次 | 技术选型 | 版本 / 规范 |
| :--- | :--- | :--- |
| **应用宿主** | Wails v2 (Go 1.25+) | Webview2 原生嵌入，IPC 事件总线 |
| **前端框架** | React 19 + TypeScript | React Hooks + Context API |
| **构建工具** | Vite 7 | 毫秒级热重载与生产构建 |
| **UI 与样式** | Tailwind CSS v4 + Lucide React | 现代化轻量设计，无缝响应式适配 |
| **系统 API** | Windows API / Registry / Job Object | 单实例互斥、NVML/DXGI 资源探针、0 孤儿进程内核级管控 |

---

### 2. 核心功能与页面设计

客户端内置 5 大功能导航页面，按工作流科学布局：

1. **图片增强 (`ImageEnhancePage`)**：
   - 支持单张图片、批量多图、以及整个图片文件夹拖拽导入。
   - **动漫模型分类**：超高质量（AnimeVideo v3，支持 2x/3x/4x）与 顶级质量（RealESRGAN Anime，固定 4x）。
   - **写实人像分类**：超高质量（RealESRGAN x2+/x4+）与 顶级质量（Real-HAT-GAN Transformer，固定 4x）。
   - **CodeFormer 人脸修复联动**：识别人脸五官并超清重建，支持 0.0 ~ 1.0 连续保真度调节。
2. **视频增强 (`VideoEnhancePage`)**：
   - 自动探测视频时长、分辨率、编码器、帧率及音频轨。
   - 模式自选：全内存流式管道模式（0 磁盘碎片）与 经典断点续传模式。
   - 自定义输出格式（MP4 / MKV / MOV）、硬件编码器指定与质量（CRF / Preset）调优。
3. **任务调度队列 (`TaskQueuePage`)**：
   - 保证 GPU 资源独占的单并发调度机制。
   - 细粒度阶段追踪：`等待处理` -> `提取视频帧` -> `视频帧超分` -> `视频压制与合流` -> `处理完成`。
   - 单任务控制：实时 FPS 与剩余帧监控、**暂停**、**断点续跑**、**取消**、清理及一键打开生成目录。
4. **模型中心 (`ModelManagerPage`)**：
   - 7 大官方预置模型状态展示与体积清单。
   - 多镜像源加速切换（HuggingFace 镜像、ModelScope、GitHub 直链等）。
   - **毫秒级精度切换**：支持单个模型或全局一键在 **FP32** 与 **FP16** 之间平滑热切换。
5. **引擎设置 (`SettingsPage`)**：
   - 临时工作目录设置与“完成后自动清理临时文件”开关。
   - 视频编码器独立探测与手动指定（自动选择、NVENC、QSV、AMF、CPU 软编）。
   - **“重新检测”能力**：随时点击重新实测当前硬件对各硬件编码器的真实支持性。

---

### 3. 关键架构设计与工程亮点

- **硬件编码器真实冒烟探测**：
  摒弃静态列表推断，内嵌 2.2 KB 的真实单帧 H.264 MP4 视频样本（[testdata/probe.mp4](file:///D:/AgentSpace/deep-sr/DeepSR/internal/engine/ffmpeg/testdata/probe.mp4)），通过真实单帧压制并以产物体积大于 0 为唯一真值判据，彻底根治了 Intel QSV（`h264_qsv` / `hevc_qsv`）等硬件编码器的误判问题。
- **配置与探测物理隔离持久化**：
  硬件能力独立固化在 `%APPDATA%\DeepSR\encoders.json`；用户设置独立固化在 `%APPDATA%\DeepSR\config.json`。采用 `.tmp` + `os.Rename` 原子安全落盘，保存常规设置绝不冲刷抹除已探测到的硬件加速数据。
- **0 孤儿进程守护 (Windows Job Object)**：
  主程序启动时绑定 `JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE` 作业对象，无论程序正常退出、确认关闭还是崩溃，操作系统内核均保证级联终止所有衍生的 `deepsr-cli.exe` 与 `ffmpeg.exe` 子进程。
- **单实例互斥前置唤醒**：
  利用 Win32 Mutex 防止多开抢占 GPU 显存，重复启动时自动恢复并置顶前台已有运行窗口。
- **全内存流式管道 (Stream Pipe)**：
  针对视频超分提供流式管道，DirectML 解码 -> ONNX 推理 -> 管道直灌 FFmpeg 编码器，避免在固态硬盘上落地数十万张中间帧。

---

### 4. GUI 开发与编译构建

```bash
# 1. 进入 GUI 目录
cd D:\AgentSpace\deep-sr\DeepSR

# 2. 安装前端依赖
cd frontend && npm install && cd ..

# 3. 开发模式（带热重载与 Wails 实时绑定）
wails dev

# 4. 生产构建（自动执行前端构建与二进制剥离）
wails build -o deepsr.exe -trimpath
# 编译产物位于: D:\AgentSpace\deep-sr\DeepSR\build\bin\deepsr.exe

# 5. 运行 Go 自动化单元测试
go test -v -count=1 ./...
```

---

## 三、 CLI 命令行引擎 (DeepSRCli)

### 1. 技术定位

`DeepSRCli`（`deepsr-cli.exe`）是基于 **DirectML + ONNX Runtime** 编写的高性能命令行工具。不依赖 CUDA，能够在任意支持 DirectX 12 的设备（NVIDIA GeForce / RTX、AMD Radeon、Intel Iris Xe / Arc、以及 CPU 纯算力）上运行。

同时具备两套输出机制：面向人类用户的单行高刷终端进度条，以及面向 GUI 调用的 `-json` 结构化事件流。

---

### 2. 命令行参数详解

```text
用法: deepsr-cli.exe [选项] -i <输入路径> -o <输出路径>
```

| 参数 | 缩写 | 默认值 | 说明 |
| :--- | :--- | :--- | :--- |
| `--input <path>` | `-i` | *(必选)* | 输入图片文件、图片目录或视频文件路径 |
| `--output <path>` | `-o` | *(必选)* | 输出文件路径或目标文件夹 |
| `--model-name <name>` | `-n` | `realesrgan_x4plus_fp16` | 使用的模型名称（支持传入带精度后缀如 `realesrgan_x4plus_fp32`） |
| `--scale <n>` | `-s` | `4` | 放大倍率（`2`、`3`、`4`） |
| `--model-dir <dir>` | `-m` | `models` | 模型权重文件目录（默认为程序同级 `models/` 目录） |
| `--gpu <dev>` | `-g` | `auto` | 指定计算设备：`auto` 自动优先独显、`0` / `1` 指定显卡索引、或 `cpu` |
| `--tile-size <n>` | `-t` | `0` | 分块渲染尺寸（`0` 为整图不分块；显存较小时可设为 `128`、`256` 防止 OOM） |
| `--format <ext>` | `-f` | *(同输入)* | 输出图像格式（`png`、`jpg`、`webp`） |
| `--tta` | `-x` | `false` | 启用 TTA (Test-Time Augmentation) 8次旋转镜像增强（画质微升，耗时×8） |
| `--threads <str>` | `-j` | `3:2:3` | 多线程并发比例（`load:proc:save` 线程配比） |
| `--mode <mode>` | `-mode` | `sr` | 处理模式：`sr` (纯超分)、`face` (纯人脸修复)、`all` (超分 + 人脸修复) |
| `--fidelity <w>` | `-w` | `0.7` | CodeFormer 人脸保真度权重（`0.0 ~ 1.0`，越小越平滑修饰，越大越接近原图细节） |
| `--stream-pipe` | — | `false` | 启用视频全内存流式管道（0 磁盘中间小文件） |
| `--encoder <enc>` | — | `auto` | 流式管道压制所用的视频编码器（`h264_nvenc`、`hevc_qsv`、`libx264` 等） |
| `--crf <n>` | — | `20` | 流式管道视频压缩质量因子（`0 ~ 51`） |
| `--preset <str>` | — | `medium` | 编码速度预设档位（`veryfast`、`medium`、`slow` 等） |
| `-json` | — | `false` | 启用纯 JSON 换行事件流输出（供宿主程序集成消费） |
| `--show-devices` | — | — | 打印系统 CPU 与物理显卡设备列表（含索引与显存）并退出 |
| `--version` | `-v` | — | 查看 CLI 版本信息 |

---

### 3. 常用操作示例

#### (1) 查看系统计算硬件
```powershell
.\bin\deepsr-cli.exe --show-devices
```

#### (2) 单张动漫图片 4 倍放大
```powershell
.\bin\deepsr-cli.exe -i input.jpg -o output.png -n realesr_animevideov3_fp16 -s 4 -f png
```

#### (3) 老照片 4 倍超分 + CodeFormer 高精度人脸修复
```powershell
.\bin\deepsr-cli.exe -i old_photo.jpg -o restored.png -n realesrgan_x4plus_fp16 -mode all -w 0.75
```

#### (4) 显存受限场景（显存 < 4GB 或核显）
```powershell
# 开启 128 分块尺寸，改用 FP16 精度，避免显存溢出
.\bin\deepsr-cli.exe -i 4k_input.jpg -o 8k_output.png -n realesrgan_x4plus_fp16 -t 128
```

#### (5) 整目录多图批量处理
```powershell
.\bin\deepsr-cli.exe -i D:\photos\ -o D:\photos_enhanced\ -n realesrgan_x2plus_fp16 -s 2 -j 4:2:4
```

#### (6) 视频全内存流式直通超分
```powershell
.\bin\deepsr-cli.exe -i input.mp4 -o output_x4.mp4 -n realesr_animevideov3_fp16 -s 4 --stream-pipe --encoder hevc_nvenc --crf 19
```

---

### 4. CLI 编译构建

```bash
cd D:\AgentSpace\deep-sr\DeepSRCli
go build -o deepsr-cli.exe -trimpath main.go
```

---

## 四、 预置模型矩阵

模型权重统一部署在 `bin/models/` 目录中，区分 FP32 与 FP16 双精度格式：

| 模型标识 | 模型全称 | 分类 | 放大倍率 | 推荐适用场景 |
| :--- | :--- | :---: | :---: | :--- |
| `realesrgan-x4plus` | RealESRGAN x4+ | 通用写实 | 4x | 风景、纪实相片、老旧照片、综合画质修复 |
| `real_hat_gan_srx4` | Real-HAT-GAN x4 | 通用写实 | 4x | 基于 Transformer 架构，细节纹理极致锐利 |
| `realesr_animevideov3` | AnimeVideo v3 | 动漫动画 | 2x / 3x / 4x | 动漫番剧、手绘二次元视频，线条极速锐化 |
| `realesrgan-x4plus-anime` | RealESRGAN x4+ Anime | 动漫插画 | 4x | 高质量静态二次元插图、重绘去噪 |
| `realesrgan-x2plus` | RealESRGAN x2+ | 通用写实 | 2x | 轻度放大、中高画质素材微调与去噪 |
| `codeformer` | CodeFormer | 人脸修复 | 1x | 五官细节重建，支持 0.0~1.0 保真度微调 |
| `retinaface` | RetinaFace | 人脸检测 | 1x | 毫秒级多人脸关键点定位，赋能人脸修复 |

---

## 五、 系统环境要求

- **操作系统**：Windows 10 / Windows 11 (64-bit)
- **DirectX 支持**：DirectX 12 (DirectML 运行时依赖)
- **显卡兼容**：
  - **NVIDIA**：GeForce GTX 900 系列及以上（建议 RTX 20/30/40 系列以获得极致体验）
  - **Intel**：HD Graphics 500 系列及以上、Iris Xe、Arc 独显系列
  - **AMD**：Radeon HD 7000 系列及以上、RX 400/500/5000/6000/7000 系列
- **处理器与内存**：推荐 4 核 CPU 及 8 GB 以上可用系统内存
