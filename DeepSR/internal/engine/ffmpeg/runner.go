package ffmpeg

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	"deepsr/internal/engine/cmdutil"
	"deepsr/internal/system"
)

// BuildHWAccelDecodeArgs 根据指定的视频编码设置与硬件配置生成针对 NVIDIA (cuda) / Intel (qsv) / AMD (d3d11va) 的抽帧参数
func BuildHWAccelDecodeArgs(preferredEncoder string, gpuDevice int, gpuDeviceStr string) (hwArgs []string, filterArgs []string, vendorDesc string) {
	// 1. 若用户显式指定了 CPU 软件编码，抽帧直接走纯 CPU 软解
	if preferredEncoder == "libx264" || preferredEncoder == "libx265" {
		return nil, nil, "CPU (软件解码)"
	}

	// 2. 若用户显式指定了某类硬件编码器，抽帧直接对应强制该显卡加速
	if strings.Contains(preferredEncoder, "nvenc") {
		return []string{"-hwaccel", "cuda"}, nil, "NVIDIA CUDA"
	}
	if strings.Contains(preferredEncoder, "qsv") {
		return []string{"-hwaccel", "qsv", "-hwaccel_output_format", "qsv"}, nil, "Intel QSV"
	}
	if strings.Contains(preferredEncoder, "amf") {
		return []string{"-hwaccel", "d3d11va"}, nil, "AMD D3D11VA"
	}

	// 3. auto 自动模式：根据任务 GPU 设备或自动探测首选独显/核显
	gpus := system.EnumerateGPUs()
	var targetGPU *system.GPUInfo

	if gpuDevice >= 0 && gpuDevice < len(gpus) {
		targetGPU = &gpus[gpuDevice]
	} else {
		// 优先选择独立显卡或首个检测到的显卡
		for i := range gpus {
			if gpus[i].IsDiscrete {
				targetGPU = &gpus[i]
				break
			}
		}
		if targetGPU == nil && len(gpus) > 0 {
			targetGPU = &gpus[0]
		}
	}

	if targetGPU != nil {
		if targetGPU.VendorID == 0x10DE { // NVIDIA
			return []string{"-hwaccel", "cuda"}, nil, fmt.Sprintf("NVIDIA CUDA [%s]", targetGPU.Name)
		} else if targetGPU.VendorID == 0x8086 { // Intel QSV
			return []string{"-hwaccel", "qsv", "-hwaccel_output_format", "qsv"}, nil, fmt.Sprintf("Intel QSV [%s]", targetGPU.Name)
		} else if targetGPU.VendorID == 0x1002 { // AMD
			return []string{"-hwaccel", "d3d11va"}, nil, fmt.Sprintf("AMD D3D11VA [%s]", targetGPU.Name)
		}
	}

	// 默认 Windows 通用硬件加速
	return []string{"-hwaccel", "d3d11va"}, nil, "Windows Direct3D11 (d3d11va)"
}

func cleanPartialExtractedFrames(outputPattern string) {
	dir := filepath.Dir(outputPattern)
	ext := filepath.Ext(outputPattern)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), "frame") && strings.HasSuffix(strings.ToLower(e.Name()), strings.ToLower(ext)) {
			_ = os.Remove(filepath.Join(dir, e.Name()))
		}
	}
}

// ExtractFrames 使用 ffmpeg 高速提取视频全部原始帧 (支持 NVIDIA cuda / Intel qsv / AMD d3d11va 硬件加速与自动回退)
func ExtractFrames(ctx context.Context, ffmpegPath string, inputVideo string, outputPattern string, frameFormat string, totalFrames int, preferredEncoder string, gpuDevice int, gpuDeviceStr string, logInfo func(string), logCmd func(string), onProgress func(pct float64, curFrame, totFrames int, speedFPS float64, msg string)) error {
	resolved := findExecutable(ffmpegPath, "ffmpeg")
	if resolved != "" {
		ffmpegPath = resolved
	} else if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}

	absInput, _ := filepath.Abs(inputVideo)
	absOutput, _ := filepath.Abs(outputPattern)

	hwArgs, filterArgs, vendorDesc := BuildHWAccelDecodeArgs(preferredEncoder, gpuDevice, gpuDeviceStr)

	runExtraction := func(useHW bool) error {
		var args []string
		args = append(args, "-hide_banner", "-y")
		if useHW && len(hwArgs) > 0 {
			args = append(args, hwArgs...)
		}
		args = append(args, "-i", filepath.ToSlash(absInput))
		if useHW && len(filterArgs) > 0 {
			args = append(args, filterArgs...)
		}
		args = append(args, "-q:v", "2", "-progress", "pipe:1", filepath.ToSlash(absOutput))

		if useHW && len(hwArgs) > 0 && logInfo != nil {
			logInfo(fmt.Sprintf("[HWACCEL] 使用 %s 硬件加速抽帧", vendorDesc))
		}
		if logCmd != nil {
			logCmd(fmt.Sprintf("%s %s", ffmpegPath, strings.Join(args, " ")))
		}

		cmd := cmdutil.CommandContext(ctx, ffmpegPath, args...)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return fmt.Errorf("创建 ffmpeg 管道失败: %w", err)
		}
		stderr, err := cmd.StderrPipe()
		if err != nil {
			return fmt.Errorf("创建 ffmpeg stderr 管道失败: %w", err)
		}

		if err := cmd.Start(); err != nil {
			return fmt.Errorf("启动 ffmpeg 抽帧失败: %w", err)
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

		scanner := bufio.NewScanner(stdout)
		currentFrame := 0
		speedFPS := 0.0

		for scanner.Scan() {
			line := scanner.Text()
			parts := strings.SplitN(line, "=", 2)
			if len(parts) == 2 {
				k := strings.TrimSpace(parts[0])
				v := strings.TrimSpace(parts[1])
				if k == "frame" {
					if f, err := strconv.Atoi(v); err == nil {
						currentFrame = f
						var pct float64
						if totalFrames > 0 {
							pct = float64(currentFrame) / float64(totalFrames) * 100.0
							if pct > 100.0 {
								pct = 100.0
							}
						}
						if onProgress != nil {
							onProgress(pct, currentFrame, totalFrames, speedFPS, fmt.Sprintf("提取视频帧: %d/%d", currentFrame, totalFrames))
						}
					}
				} else if k == "fps" {
					if fps, err := strconv.ParseFloat(v, 64); err == nil {
						speedFPS = fps
					}
				}
			}
		}

		if err := cmd.Wait(); err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			stderrMu.Lock()
			errText := stderrBuf.String()
			stderrMu.Unlock()
			return fmt.Errorf("ffmpeg 抽帧异常退出: %w, 详细日志: %s", err, errText)
		}
		return nil
	}

	// 1. 先尝试硬件加速抽帧
	err := runExtraction(true)
	if err != nil && ctx.Err() == nil {
		if logInfo != nil {
			logInfo("[HWACCEL] 显卡硬件解码未就绪或当前格式不受显卡硬解支持，平滑回退至 CPU 软件解码")
		}
		cleanPartialExtractedFrames(outputPattern)
		err = runExtraction(false)
	}
	if err != nil {
		return err
	}

	if onProgress != nil {
		onProgress(100.0, totalFrames, totalFrames, 0, "视频帧提取完成")
	}
	return nil
}

// BuildEncoderArgs 构建针对 NVIDIA(NVENC) / Intel(QSV) / AMD(AMF) / CPU(x264/x265) 的标准化编码器参数
func BuildEncoderArgs(encoder string, crf int, preset string) []string {
	if encoder == "" || encoder == "auto" {
		encoder = "libx264"
	}
	if crf <= 0 {
		crf = 20
	}

	args := []string{"-c:v", encoder}
	if strings.Contains(encoder, "nvenc") {
		// NVENC 恒定质量模式与现代 P1-P7 预设映射
		nvPreset := "p5"
		switch preset {
		case "veryfast":
			nvPreset = "p2"
		case "faster":
			nvPreset = "p3"
		case "fast":
			nvPreset = "p4"
		case "medium":
			nvPreset = "p5"
		case "slow":
			nvPreset = "p6"
		case "slower", "veryslow":
			nvPreset = "p7"
		default:
			if preset != "" {
				nvPreset = preset
			}
		}
		args = append(args, "-rc:v", "vbr", "-cq:v", strconv.Itoa(crf), "-preset", nvPreset)
	} else if strings.Contains(encoder, "qsv") {
		// Intel QSV 恒定质量模式（原生支持 veryfast..veryslow）
		if preset == "" {
			preset = "medium"
		}
		args = append(args, "-global_quality", strconv.Itoa(crf), "-preset", preset)
	} else if strings.Contains(encoder, "amf") {
		// AMD AMF 恒定量化参数 (CQP) 与使用场景映射
		amfUsage := "transcoding"
		switch preset {
		case "veryfast", "faster", "fast":
			amfUsage = "speed"
		case "medium":
			amfUsage = "balanced"
		case "slow", "slower", "veryslow":
			amfUsage = "quality"
		}
		args = append(args, "-rc", "cqp", "-qp_i", strconv.Itoa(crf), "-qp_p", strconv.Itoa(crf), "-usage", amfUsage)
	} else {
		// CPU 软件编码器 (libx264 / libx265)
		if preset == "" {
			preset = "medium"
		}
		args = append(args, "-crf", strconv.Itoa(crf), "-preset", preset)
	}
	return args
}

// ResolveBestEncoder 自动解析系统可用的最佳硬件加速编码器 (优先读内存缓存，0 毫秒瞬时响应)
func ResolveBestEncoder(ffmpegPath string, preferred string) string {
	if preferred != "" && preferred != "auto" {
		return preferred
	}

	encoders := DetectEncoders(ffmpegPath, false)

	// 优先级 1: NVIDIA NVENC (hevc > h264 > av1)
	for _, id := range []string{"hevc_nvenc", "h264_nvenc", "av1_nvenc"} {
		for _, enc := range encoders {
			if enc.ID == id && enc.Supported {
				return id
			}
		}
	}
	// 优先级 2: Intel QSV (hevc > h264 > av1)
	for _, id := range []string{"hevc_qsv", "h264_qsv", "av1_qsv"} {
		for _, enc := range encoders {
			if enc.ID == id && enc.Supported {
				return id
			}
		}
	}
	// 优先级 3: AMD AMF (hevc > h264 > av1)
	for _, id := range []string{"hevc_amf", "h264_amf", "av1_amf"} {
		for _, enc := range encoders {
			if enc.ID == id && enc.Supported {
				return id
			}
		}
	}
	// 优先级 4: CPU 软件编码
	for _, enc := range encoders {
		if enc.ID == "libx264" && enc.Supported {
			return "libx264"
		}
	}
	return "libx264"
}

// EncodeVideoOptions 图片序列全量编码与音画合流参数
type EncodeVideoOptions struct {
	FFmpegPath    string
	FramesPattern string // e.g. "enhanced/frame%08d.jpg"
	StartNumber   int    // 1-indexed start frame number (通常为 1)
	FrameCount    int    // 总帧数 (可选，<=0 时不限制 -vframes)
	FrameRate     float64
	OriginalVideo string // 原视频路径（用于音频 copy 混流，可为空）
	OutputPath    string // 目标输出文件路径（.mp4 / .mkv / .mov 等）
	Encoder       string // 编码器（hevc_nvenc, hevc_qsv, libx264 等）
	CRF           int
	Preset        string
	LogCommand    func(string)
	OnProgress    func(curFrame, totalFrames int, speedFPS float64)
}

// EncodeFramesToVideo 使用 ffmpeg 直接将图片序列编码为目标视频并混流原音轨 (单次直出 + 原子写入)
func EncodeFramesToVideo(ctx context.Context, opt EncodeVideoOptions) error {
	ffmpegPath := opt.FFmpegPath
	resolved := findExecutable(ffmpegPath, "ffmpeg")
	if resolved != "" {
		ffmpegPath = resolved
	} else if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}

	absPattern, _ := filepath.Abs(opt.FramesPattern)
	absOutput, _ := filepath.Abs(opt.OutputPath)
	ext := strings.ToLower(filepath.Ext(absOutput))
	base := strings.TrimSuffix(absOutput, filepath.Ext(absOutput))
	tmpOutput := fmt.Sprintf("%s.tmp%s", base, ext)
	_ = os.Remove(tmpOutput)

	fpsStr := fmt.Sprintf("%.3f", opt.FrameRate)
	if opt.FrameRate <= 0 {
		fpsStr = "30"
	}

	startNum := opt.StartNumber
	if startNum <= 0 {
		startNum = 1
	}

	args := []string{
		"-hide_banner",
		"-y",
		"-r", fpsStr,
		"-start_number", strconv.Itoa(startNum),
		"-i", filepath.ToSlash(absPattern),
	}

	hasAudio := false
	if opt.OriginalVideo != "" {
		absOrig, _ := filepath.Abs(opt.OriginalVideo)
		args = append(args, "-i", filepath.ToSlash(absOrig))
		hasAudio = true
	}

	if opt.FrameCount > 0 {
		args = append(args, "-vframes", strconv.Itoa(opt.FrameCount))
	}

	if hasAudio {
		args = append(args, "-map", "0:v:0", "-map", "1:a:0?")
	} else {
		args = append(args, "-map", "0:v:0")
	}

	args = append(args, BuildEncoderArgs(opt.Encoder, opt.CRF, opt.Preset)...)

	if hasAudio {
		args = append(args, "-c:a", "copy")
	}

	formatName := ""
	switch ext {
	case ".mp4", ".m4v":
		formatName = "mp4"
	case ".mkv":
		formatName = "matroska"
	case ".mov":
		formatName = "mov"
	case ".webm":
		formatName = "webm"
	}
	if formatName != "" {
		args = append(args, "-f", formatName)
	}

	if ext == ".mp4" || ext == ".mov" || ext == ".m4v" {
		args = append(args, "-movflags", "+faststart")
	}

	args = append(args, "-progress", "pipe:1", filepath.ToSlash(tmpOutput))

	if opt.LogCommand != nil {
		opt.LogCommand(fmt.Sprintf("%s %s", ffmpegPath, strings.Join(args, " ")))
	}

	cmd := cmdutil.CommandContext(ctx, ffmpegPath, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("创建 stdout 管道失败: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("创建 stderr 管道失败: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 ffmpeg 视频编码失败: %w", err)
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

	scanner := bufio.NewScanner(stdout)
	currentFrame := 0
	speedFPS := 0.0

	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			k := strings.TrimSpace(parts[0])
			v := strings.TrimSpace(parts[1])
			if k == "frame" {
				if f, err := strconv.Atoi(v); err == nil {
					currentFrame = f
					if opt.OnProgress != nil {
						opt.OnProgress(currentFrame, opt.FrameCount, speedFPS)
					}
				}
			} else if k == "fps" {
				if fps, err := strconv.ParseFloat(v, 64); err == nil {
					speedFPS = fps
				}
			}
		}
	}

	if err := cmd.Wait(); err != nil {
		_ = os.Remove(tmpOutput)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		stderrMu.Lock()
		errText := stderrBuf.String()
		stderrMu.Unlock()
		return fmt.Errorf("ffmpeg 视频编码异常退出: %w, 详细日志: %s", err, errText)
	}

	_ = os.Remove(absOutput)
	if err := os.Rename(tmpOutput, absOutput); err != nil {
		return fmt.Errorf("视频输出文件重命名失败: %w", err)
	}

	return nil
}

// ChunkEncodeOptions 1800 帧分段编码参数
type ChunkEncodeOptions struct {
	FFmpegPath    string
	FramesPattern string // e.g. "enhanced/frame%08d.jpg"
	StartNumber   int    // 1-indexed start frame number (e.g. 1, 1801, 3601)
	FrameCount    int    // Number of frames in this chunk (e.g. 1800)
	FrameRate     float64
	OutputPath    string // e.g. "chunks/chunk_0000.ts"
	Encoder       string
	CRF           int
	Preset        string
	LogCommand    func(string)
	OnProgress    func(curChunkFrame, totalChunkFrames int, speedFPS float64)
}

// EncodeChunk 使用 ffmpeg 压制 1800 帧独立自包含 MPEG-TS 分段 (带逐帧进度流与 .tmp 原子落盘)
func EncodeChunk(ctx context.Context, opt ChunkEncodeOptions) error {
	ffmpegPath := opt.FFmpegPath
	resolved := findExecutable(ffmpegPath, "ffmpeg")
	if resolved != "" {
		ffmpegPath = resolved
	} else if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}

	absPattern, _ := filepath.Abs(opt.FramesPattern)
	absOutput, _ := filepath.Abs(opt.OutputPath)
	tmpOutput := absOutput + ".tmp"
	_ = os.Remove(tmpOutput)

	fpsStr := fmt.Sprintf("%.3f", opt.FrameRate)
	if opt.FrameRate <= 0 {
		fpsStr = "30"
	}

	args := []string{
		"-hide_banner",
		"-y",
		"-r", fpsStr,
		"-start_number", strconv.Itoa(opt.StartNumber),
		"-i", filepath.ToSlash(absPattern),
		"-vframes", strconv.Itoa(opt.FrameCount),
	}

	args = append(args, BuildEncoderArgs(opt.Encoder, opt.CRF, opt.Preset)...)
	args = append(args, "-f", "mpegts", "-progress", "pipe:1", filepath.ToSlash(tmpOutput))

	if opt.LogCommand != nil {
		opt.LogCommand(fmt.Sprintf("%s %s", ffmpegPath, strings.Join(args, " ")))
	}

	cmd := cmdutil.CommandContext(ctx, ffmpegPath, args...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("创建 stdout 管道失败: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("创建 stderr 管道失败: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 ffmpeg 分段编码失败: %w", err)
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

	scanner := bufio.NewScanner(stdout)
	currentFrame := 0
	speedFPS := 0.0

	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, "=", 2)
		if len(parts) == 2 {
			k := strings.TrimSpace(parts[0])
			v := strings.TrimSpace(parts[1])
			if k == "frame" {
				if f, err := strconv.Atoi(v); err == nil {
					currentFrame = f
					if opt.OnProgress != nil {
						opt.OnProgress(currentFrame, opt.FrameCount, speedFPS)
					}
				}
			} else if k == "fps" {
				if fps, err := strconv.ParseFloat(v, 64); err == nil {
					speedFPS = fps
				}
			}
		}
	}

	if err := cmd.Wait(); err != nil {
		_ = os.Remove(tmpOutput)
		if ctx.Err() != nil {
			return ctx.Err()
		}
		stderrMu.Lock()
		errText := stderrBuf.String()
		stderrMu.Unlock()
		return fmt.Errorf("ffmpeg 分段编码异常退出 (分段 %s): %w, 详细日志: %s", filepath.Base(opt.OutputPath), err, errText)
	}

	_ = os.Remove(absOutput)
	if err := os.Rename(tmpOutput, absOutput); err != nil {
		return fmt.Errorf("分段文件重命名失败: %w", err)
	}

	return nil
}

// ConcatChunksOptions 极速拼接全部分段并混流原音频
type ConcatChunksOptions struct {
	FFmpegPath    string
	ConcatList    string // e.g. "chunks.txt"
	OriginalVideo string // for audio copy
	OutputPath    string // final .mp4 / .mkv / .mov
	LogCommand    func(string)
}

// ConcatChunks 使用 FFmpeg concat demuxer 配合 -c copy 极速无损合成最终视频
func ConcatChunks(ctx context.Context, opt ConcatChunksOptions) error {
	ffmpegPath := opt.FFmpegPath
	resolved := findExecutable(ffmpegPath, "ffmpeg")
	if resolved != "" {
		ffmpegPath = resolved
	} else if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}

	absList, _ := filepath.Abs(opt.ConcatList)
	absOutput, _ := filepath.Abs(opt.OutputPath)

	args := []string{
		"-hide_banner",
		"-y",
		"-f", "concat",
		"-safe", "0",
		"-i", filepath.ToSlash(absList),
	}

	if opt.OriginalVideo != "" {
		absOrig, _ := filepath.Abs(opt.OriginalVideo)
		args = append(args, "-i", filepath.ToSlash(absOrig), "-map", "0:v:0", "-map", "1:a:0?")
	} else {
		args = append(args, "-map", "0:v:0")
	}

	args = append(args, "-c", "copy")

	ext := strings.ToLower(filepath.Ext(absOutput))
	if ext == ".mp4" || ext == ".mov" || ext == ".m4v" {
		args = append(args, "-movflags", "+faststart")
	}

	args = append(args, filepath.ToSlash(absOutput))

	if opt.LogCommand != nil {
		opt.LogCommand(fmt.Sprintf("%s %s", ffmpegPath, strings.Join(args, " ")))
	}

	cmd := cmdutil.CommandContext(ctx, ffmpegPath, args...)
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动 ffmpeg 分段拼接失败: %w", err)
	}
	if cmd.Process != nil && cmd.Process.Pid > 0 {
		cmdutil.RegisterPid(cmd.Process.Pid)
		defer cmdutil.UnregisterPid(cmd.Process.Pid)
	}

	if err := cmd.Wait(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("ffmpeg 分段拼接失败: %w, 详细日志: %s", err, stderrBuf.String())
	}

	return nil
}
