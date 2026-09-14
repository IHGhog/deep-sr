package deepsr

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"deepsr/internal/engine/cmdutil"
	"deepsr/internal/models"
	"deepsr/internal/system"
)

// Event 定义 deepsr-cli.exe -json 吐出的结构化事件
type Event struct {
	Type      string  `json:"type"`
	Timestamp float64 `json:"timestamp"`
	Phase     string  `json:"phase,omitempty"`
	Current   int     `json:"current,omitempty"`
	Total     int     `json:"total,omitempty"`
	Percent   float64 `json:"percent,omitempty"`
	Message   string  `json:"message,omitempty"`
	Input     string  `json:"input,omitempty"`
	Output    string  `json:"output,omitempty"`
	Elapsed   float64 `json:"elapsed,omitempty"`
	FaceCount *int    `json:"faceCount,omitempty"`
}

// UpscaleOptions 超分辨率与人脸修复执行参数
type UpscaleOptions struct {
	DeepSRPath        string
	ModelsDir         string
	InputPath         string
	OutputPath        string
	ModelName         string
	Scale             int
	EnableFaceBooster bool
	FaceFidelity      float64
	GPUDevice         int
	GPUDeviceStr      string
	TileSize          int
	Format            string
	EnableTTA         bool
	Threads           string // e.g. "3:2:3"
	EnableStreamPipe  bool
	Encoder           string
	CRF               int
	Preset            string
	FFmpegPath        string
	FFprobePath       string
	LogCommand        func(string)
	OnProgress        func(pct float64, current, total int, phase string, msg string)
	OnLog             func(level, msg string)
}

func findDeepSRExecutable(customPath string) string {
	if customPath != "" {
		if fi, err := os.Stat(customPath); err == nil && !fi.IsDir() {
			if abs, err := filepath.Abs(customPath); err == nil {
				return abs
			}
			return customPath
		}
	}
	exePath, err := os.Executable()
	var exeDir string
	if err == nil {
		exeDir = filepath.Dir(exePath)
	}
	p := filepath.Join(exeDir, "bin", "deepsr-cli.exe")
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

func resolveConcreteGPUDevice(gpuStr string, gpuDev int) string {
	trimmed := strings.ToLower(strings.TrimSpace(gpuStr))
	if trimmed == "cpu" || trimmed == "-2" {
		return "cpu"
	}
	if trimmed != "" && trimmed != "auto" && trimmed != "-1" {
		return gpuStr
	}
	if gpuDev >= 0 {
		return strconv.Itoa(gpuDev)
	}

	// 自动选择最佳物理硬件设备编号 (优先独立显卡 -> 其次集成显卡 -> 最终回退至 cpu)
	gpus := system.EnumerateGPUs()
	for _, g := range gpus {
		if g.IsDiscrete {
			return strconv.Itoa(g.Index)
		}
	}
	for _, g := range gpus {
		if g.IsIntegrated {
			return strconv.Itoa(g.Index)
		}
	}
	if len(gpus) > 0 {
		return strconv.Itoa(gpus[0].Index)
	}
	return "cpu"
}

func resolveInstalledModelName(baseName string, modelsDir string) (string, error) {
	if modelsDir == "" {
		modelsDir = models.GetModelsDir()
	}

	cleanName := strings.TrimSuffix(strings.TrimSpace(baseName), ".onnx")
	lower := strings.ToLower(cleanName)

	// 情况 A：传入名称已显式包含精度（如 realesr_animevideov3_fp32 或 realesrgan_x4plus_fp16）
	if strings.Contains(lower, "fp16") || strings.Contains(lower, "fp32") {
		targetPath := filepath.Join(modelsDir, cleanName+".onnx")
		fi, err := os.Stat(targetPath)
		if err != nil || fi.Size() == 0 {
			return "", fmt.Errorf("未检测到模型 [%s] 的权重文件（缺少 %s.onnx），请先前往【模型管理】页面下载", cleanName, cleanName)
		}
		// 若为 FP32 且存在伴生 .onnx.data 文件，同时校验其存在性与有效性
		if strings.Contains(lower, "fp32") {
			dataPath := targetPath + ".data"
			for _, m := range models.PresetModels {
				for _, f := range m.FilesFP32 {
					if f == cleanName+".onnx.data" {
						if dfi, derr := os.Stat(dataPath); derr != nil || dfi.Size() == 0 {
							return "", fmt.Errorf("模型 [%s] 缺少权重数据文件 %s.data，请前往【模型管理】重新下载", cleanName, cleanName+".onnx")
						}
					}
				}
			}
		}
		return cleanName, nil
	}

	// 情况 B：传入基础模型名（如 realesrgan-x4plus, realesr_animevideov3 等），需结合用户选定的精度解析
	actualBase := cleanName
	targetMetaID := "realesrgan-x4plus"
	switch {
	case strings.Contains(lower, "x2plus") || strings.Contains(lower, "x2"):
		actualBase = "realesrgan_x2plus"
		targetMetaID = "realesrgan-x2plus"
	case strings.Contains(lower, "animevideo") || strings.Contains(lower, "videov3"):
		actualBase = "realesr_animevideov3"
		targetMetaID = "realesr_animevideov3"
	case strings.Contains(lower, "anime") || strings.Contains(lower, "6b"):
		actualBase = "realesrgan_x4plus_anime"
		targetMetaID = "realesrgan-x4plus-anime"
	case strings.Contains(lower, "hat"):
		actualBase = "real_hat_gan_srx4"
		targetMetaID = "real_hat_gan_srx4"
	case strings.Contains(lower, "codeformer"):
		actualBase = "codeformer"
		targetMetaID = "codeformer"
	case strings.Contains(lower, "retinaface"):
		actualBase = "retinaface"
		targetMetaID = "retinaface"
	case strings.Contains(lower, "x4plus") || strings.Contains(lower, "realesrgan") || strings.Contains(lower, "esrgan"):
		actualBase = "realesrgan_x4plus"
		targetMetaID = "realesrgan-x4plus"
	}

	// 严格按照用户在模型中心选定的精度（所选即所得，绝不模糊兜底）
	mgr := models.GetGlobalModelManager()
	selectedVar := mgr.GetSelectedVariant(targetMetaID)
	targetFilename := actualBase + "_" + selectedVar + ".onnx"
	targetPath := filepath.Join(modelsDir, targetFilename)

	if fi, err := os.Stat(targetPath); err == nil && fi.Size() > 0 {
		// 校验是否有对应的 .data 伴生文件
		if selectedVar == "fp32" {
			for _, m := range models.PresetModels {
				if m.ID == targetMetaID {
					if !models.CheckFilesExist(modelsDir, m.FilesFP32) {
						return "", fmt.Errorf("模型 [%s (FP32)] 缺少必要的权重数据文件，请前往【模型管理】重新下载", cleanName)
					}
				}
			}
		}
		return actualBase + "_" + selectedVar, nil
	}

	// 容错回退：若选定精度的文件未就绪，但另一种精度（FP16/FP32）的文件存在，则自动回退
	altVar := "fp16"
	if selectedVar == "fp16" {
		altVar = "fp32"
	}
	altFilename := actualBase + "_" + altVar + ".onnx"
	altPath := filepath.Join(modelsDir, altFilename)
	if fi, err := os.Stat(altPath); err == nil && fi.Size() > 0 {
		altReady := true
		if altVar == "fp32" {
			for _, m := range models.PresetModels {
				if m.ID == targetMetaID {
					if !models.CheckFilesExist(modelsDir, m.FilesFP32) {
						altReady = false
						break
					}
				}
			}
		}
		if altReady {
			return actualBase + "_" + altVar, nil
		}
	}

	return "", fmt.Errorf("未检测到模型 [%s (%s)] 的权重文件（缺少 %s），请先前往【模型管理】页面下载", cleanName, strings.ToUpper(selectedVar), targetFilename)
}

// CheckModelReady 检查指定的超分模型及人脸修复模型（若启用）是否已下载就绪
func CheckModelReady(modelName string, modelsDir string, enableFaceBooster bool) error {
	if modelName == "" {
		modelName = "realesrgan-x4plus"
	}
	if _, err := resolveInstalledModelName(modelName, modelsDir); err != nil {
		return err
	}
	if enableFaceBooster {
		if _, err := resolveInstalledModelName("codeformer", modelsDir); err != nil {
			return err
		}
		if _, err := resolveInstalledModelName("retinaface", modelsDir); err != nil {
			return err
		}
	}
	return nil
}

// RunUpscale 调用 deepsr-cli 执行图片或文件夹超分与人脸增强
func RunUpscale(ctx context.Context, opt UpscaleOptions) error {
	binPath := findDeepSRExecutable(opt.DeepSRPath)

	absInput, err := filepath.Abs(opt.InputPath)
	if err != nil {
		return fmt.Errorf("无效输入路径: %w", err)
	}
	absOutput, err := filepath.Abs(opt.OutputPath)
	if err != nil {
		return fmt.Errorf("无效输出路径: %w", err)
	}

	modelName := opt.ModelName
	if modelName == "" {
		modelName = "realesrgan-x4plus"
	}

	// 校验并解析已下载的具体模型精度文件名称
	concreteModelName, err := resolveInstalledModelName(modelName, opt.ModelsDir)
	if err != nil {
		return err
	}

	scale := opt.Scale
	if scale <= 0 {
		scale = 4
	}

	concreteGPU := resolveConcreteGPUDevice(opt.GPUDeviceStr, opt.GPUDevice)

	args := []string{
		"-i", filepath.ToSlash(absInput),
		"-o", filepath.ToSlash(absOutput),
		"-n", concreteModelName,
		"-s", strconv.Itoa(scale),
	}

	// 1. 仅当开启人脸加强时才传递 -mode all 与 -w 保真度参数
	if opt.EnableFaceBooster {
		if _, err := resolveInstalledModelName("codeformer", opt.ModelsDir); err != nil {
			return err
		}
		if _, err := resolveInstalledModelName("retinaface", opt.ModelsDir); err != nil {
			return err
		}
		fidelity := opt.FaceFidelity
		if fidelity <= 0 || fidelity > 1.0 {
			fidelity = 0.7
		}
		args = append(args, "-mode", "all", "-w", fmt.Sprintf("%.2f", fidelity))
	}

	args = append(args, "-g", concreteGPU, "-json")

	if opt.ModelsDir != "" && opt.ModelsDir != "models" {
		args = append(args, "-m", filepath.ToSlash(opt.ModelsDir))
	}

	if opt.TileSize > 0 {
		args = append(args, "-t", strconv.Itoa(opt.TileSize))
	}

	if opt.Format != "" {
		args = append(args, "-f", strings.TrimPrefix(opt.Format, "."))
	}

	if opt.EnableTTA {
		args = append(args, "-x")
	}

	if opt.Threads != "" {
		args = append(args, "-j", opt.Threads)
	}

	if opt.EnableStreamPipe {
		args = append(args, "--stream-pipe")
		if opt.Encoder != "" {
			args = append(args, "--encoder", opt.Encoder)
		}
		if opt.CRF > 0 {
			args = append(args, "--crf", strconv.Itoa(opt.CRF))
		}
		if opt.Preset != "" {
			args = append(args, "--preset", opt.Preset)
		}
	}

	if opt.LogCommand != nil {
		opt.LogCommand(fmt.Sprintf("%s %s", binPath, strings.Join(args, " ")))
	}

	cmd := cmdutil.CommandContext(ctx, binPath, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("创建 deepsr-cli stdout 管道失败: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("创建 deepsr-cli stderr 管道失败: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 deepsr-cli.exe 失败: %w", err)
	}
	if cmd.Process != nil && cmd.Process.Pid > 0 {
		cmdutil.RegisterPid(cmd.Process.Pid)
		defer cmdutil.UnregisterPid(cmd.Process.Pid)
	}

	var stderrBuf bytes.Buffer
	var stderrMu sync.Mutex
	go func() {
		s := bufio.NewScanner(stderr)
		for s.Scan() {
			stderrMu.Lock()
			if stderrBuf.Len() < 4096 {
				stderrBuf.WriteString(s.Text() + "\n")
			}
			stderrMu.Unlock()
		}
	}()

	var lastErrorMsg string
	var recentStdoutLines []string
	scanner := bufio.NewScanner(stdout)
	buf := make([]byte, 64*1024)
	scanner.Buffer(buf, 1024*1024)
	for scanner.Scan() {
		parseOutputLine(scanner.Text(), &opt, &lastErrorMsg, &recentStdoutLines)
	}

	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if lastErrorMsg != "" {
			return fmt.Errorf("%s", FormatFriendlyErrorMessage(lastErrorMsg))
		}
		stderrMu.Lock()
		errText := strings.TrimSpace(stderrBuf.String())
		stderrMu.Unlock()
		if errText != "" {
			lowerErr := strings.ToLower(errText)
			if strings.Contains(lowerErr, "[error]") {
				idx := strings.Index(lowerErr, "[error]")
				return fmt.Errorf("%s", FormatFriendlyErrorMessage(strings.TrimSpace(errText[idx+7:])))
			}
			return fmt.Errorf("%s", FormatFriendlyErrorMessage(errText))
		}
		if len(recentStdoutLines) > 0 {
			combined := strings.Join(recentStdoutLines, "\n")
			return fmt.Errorf("%s", FormatFriendlyErrorMessage(combined))
		}
		return fmt.Errorf("deepsr-cli 运行失败: %w", err)
	}

	// 兜底防御：进程退出码虽为 0，若中途捕获到错误或产物未生成，拦截假成功！
	if lastErrorMsg != "" {
		return fmt.Errorf("%s", FormatFriendlyErrorMessage(lastErrorMsg))
	}
	if opt.OutputPath != "" {
		if fi, err := os.Stat(opt.OutputPath); err != nil || (fi.Mode().IsRegular() && fi.Size() == 0) {
			return fmt.Errorf("超分处理未生成目标输出文件，任务未完成")
		}
	}

	return nil
}

// FormatFriendlyErrorMessage 针对常见的显存不足与 GPU 驱动挂起超时等底层错误进行优雅修饰，
// 避免向用户呈现冗长晦涩的 DirectML/C++ 源码路径与内存地址堆栈。
func FormatFriendlyErrorMessage(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	lower := strings.ToLower(raw)

	// 1. 显存不足 (OOM) 场景
	if strings.Contains(lower, "8007000e") ||
		strings.Contains(lower, "outofmemory") ||
		strings.Contains(lower, "out of memory") ||
		strings.Contains(lower, "not enough memory") {
		return "GPU 显存不足 (Out of Memory)。建议在【引擎设置】中指定分块尺寸 (如设为 128 或 64)、改用 FP16 精度模型或在计算设备中选择 CPU 模式"
	}

	// 2. GPU 计算超时挂起 / 驱动被系统重置 (TDR) 场景
	if strings.Contains(lower, "887a0006") ||
		strings.Contains(lower, "887a0005") ||
		strings.Contains(lower, "device_hung") ||
		strings.Contains(lower, "device hung") ||
		strings.Contains(lower, "device_removed") ||
		strings.Contains(lower, "设备挂起") {
		return "GPU 计算超时或驱动被系统重置 (DXGI_ERROR_DEVICE_HUNG)。当前模型计算负荷过高，建议减小分块尺寸 (如设为 128 或 64)、改用 FP16 精度或在计算设备中选择 CPU 模式"
	}

	// 3. 通用 ONNX 错误修饰：去除冗长的 C++ 源代码绝对路径（如 E:\_work\1\s\...）
	if strings.Contains(raw, "onnxruntime.dll!") {
		if idx := strings.LastIndex(raw, ":"); idx != -1 && idx < len(raw)-1 {
			trimmed := strings.TrimSpace(raw[idx+1:])
			if len(trimmed) > 5 && !strings.Contains(trimmed, "Exception") {
				return trimmed
			}
		}
	}

	return raw
}

// parseOutputLine 解析 deepsr-cli 输出的单行日志
// 支持解析 JSON 结构化日志，以及提取非结构化的 [ERROR]、panic 等异常信息兜底
func parseOutputLine(line string, opt *UpscaleOptions, lastErrorMsg *string, recentStdoutLines *[]string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}

	if recentStdoutLines != nil {
		if len(*recentStdoutLines) >= 10 {
			*recentStdoutLines = (*recentStdoutLines)[1:]
		}
		*recentStdoutLines = append(*recentStdoutLines, line)
	}

	if strings.HasPrefix(line, "{") && strings.HasSuffix(line, "}") {
		var ev Event
		if err := json.Unmarshal([]byte(line), &ev); err == nil {
			switch ev.Type {
			case "progress":
				if opt != nil && opt.OnProgress != nil {
					opt.OnProgress(ev.Percent, ev.Current, ev.Total, ev.Phase, ev.Message)
				}
			case "info":
				if ev.Phase != "" && opt != nil && opt.OnProgress != nil {
					pct := ev.Percent
					if pct <= 0 && (ev.Phase == "recognizing" || ev.Phase == "recognized") {
						pct = 50.0
					}
					opt.OnProgress(pct, ev.Current, ev.Total, ev.Phase, ev.Message)
				}
				if opt != nil && opt.OnLog != nil {
					opt.OnLog("info", ev.Message)
				}
			case "warn":
				if opt != nil && opt.OnLog != nil {
					opt.OnLog("warn", ev.Message)
				}
			case "error":
				friendly := FormatFriendlyErrorMessage(ev.Message)
				if lastErrorMsg != nil {
					*lastErrorMsg = friendly
				}
				if opt != nil && opt.OnLog != nil {
					opt.OnLog("error", friendly)
				}
			case "complete":
				if opt != nil && opt.OnProgress != nil {
					opt.OnProgress(100.0, ev.Total, ev.Total, "done", fmt.Sprintf("处理完成 (耗时 %.2fs)", ev.Elapsed))
				}
			}
			return
		}
	}

	// 非 JSON 行：探测是否包含关键错误前缀或关键字
	lower := strings.ToLower(line)
	var extractedErr string
	switch {
	case strings.HasPrefix(lower, "[error]"):
		extractedErr = strings.TrimSpace(line[7:])
	case strings.HasPrefix(lower, "error:"):
		extractedErr = strings.TrimSpace(line[6:])
	case strings.HasPrefix(lower, "fatal:"):
		extractedErr = strings.TrimSpace(line[6:])
	case strings.HasPrefix(lower, "fatal error:"):
		extractedErr = strings.TrimSpace(line[12:])
	case strings.HasPrefix(lower, "panic:"):
		extractedErr = line
	case strings.Contains(lower, "[error]"):
		idx := strings.Index(lower, "[error]")
		extractedErr = strings.TrimSpace(line[idx+7:])
	}

	if extractedErr != "" {
		friendly := FormatFriendlyErrorMessage(extractedErr)
		if lastErrorMsg != nil {
			*lastErrorMsg = friendly
		}
		if opt != nil && opt.OnLog != nil {
			opt.OnLog("error", friendly)
		}
		return
	}

	// 普通文本日志输出
	if opt != nil && opt.OnLog != nil {
		opt.OnLog("raw", line)
	}
}
