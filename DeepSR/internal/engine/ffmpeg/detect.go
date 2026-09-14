package ffmpeg

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"deepsr/internal/engine/cmdutil"
)

//go:embed testdata/probe.mp4
var probeSample []byte

// EncoderInfo 视频编码器信息实体
type EncoderInfo struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Vendor    string `json:"vendor"` // "nvidia" | "intel" | "amd" | "cpu"
	Supported bool   `json:"supported"`
}

// CachedEncodersData 编码能力独立持久化数据结构（保存在 %APPDATA%/DeepSR/encoders.json）
type CachedEncodersData struct {
	Version    string        `json:"version"`
	FFmpegPath string        `json:"ffmpegPath"`
	Encoders   []EncoderInfo `json:"encoders"`
	UpdatedAt  string        `json:"updatedAt"`
}

var (
	encoderCacheMu     sync.RWMutex
	cachedEncoders     []EncoderInfo
	cachedFFmpegPath   string
	customCachePath    string // 用于单元测试隔离
	defaultEncoderList = []EncoderInfo{
		{ID: "h264_nvenc", Name: "NVIDIA NVENC (h264_nvenc)", Vendor: "nvidia"},
		{ID: "hevc_nvenc", Name: "NVIDIA NVENC (hevc_nvenc)", Vendor: "nvidia"},
		{ID: "av1_nvenc", Name: "NVIDIA NVENC (av1_nvenc)", Vendor: "nvidia"},
		{ID: "h264_qsv", Name: "Intel QSV (h264_qsv)", Vendor: "intel"},
		{ID: "hevc_qsv", Name: "Intel QSV (hevc_qsv)", Vendor: "intel"},
		{ID: "av1_qsv", Name: "Intel QSV (av1_qsv)", Vendor: "intel"},
		{ID: "h264_amf", Name: "AMD AMF (h264_amf)", Vendor: "amd"},
		{ID: "hevc_amf", Name: "AMD AMF (hevc_amf)", Vendor: "amd"},
		{ID: "av1_amf", Name: "AMD AMF (av1_amf)", Vendor: "amd"},
		{ID: "libx264", Name: "CPU 软件编码 (libx264)", Vendor: "cpu"},
		{ID: "libx265", Name: "CPU 软件编码 (libx265)", Vendor: "cpu"},
	}

	sampleOnce sync.Once
	samplePath string
	sampleErr  error
)

// SetCustomEncodersCachePathForTest 设置测试专属的 encoders.json 路径
func SetCustomEncodersCachePathForTest(p string) func() {
	encoderCacheMu.Lock()
	customCachePath = p
	cachedEncoders = nil
	cachedFFmpegPath = ""
	encoderCacheMu.Unlock()

	return func() {
		encoderCacheMu.Lock()
		customCachePath = ""
		cachedEncoders = nil
		cachedFFmpegPath = ""
		encoderCacheMu.Unlock()
	}
}

// GetEncodersConfigPath 获取 encoders.json 独立配置文件路径 (%APPDATA%\DeepSR\encoders.json)
func GetEncodersConfigPath() string {
	if customCachePath != "" {
		_ = os.MkdirAll(filepath.Dir(customCachePath), 0755)
		return customCachePath
	}
	userConfigDir, err := os.UserConfigDir()
	if err != nil {
		userConfigDir = "."
	}
	dir := filepath.Join(userConfigDir, "DeepSR")
	_ = os.MkdirAll(dir, 0755)
	return filepath.Join(dir, "encoders.json")
}

// ensureSampleFile 将内嵌的单帧标准测试样本 probe.mp4 写入系统临时目录供实测使用
func ensureSampleFile() (string, error) {
	sampleOnce.Do(func() {
		tmpDir := os.TempDir()
		target := filepath.Join(tmpDir, fmt.Sprintf("deepsr_probe_sample_%d.mp4", os.Getpid()))
		if fi, err := os.Stat(target); err == nil && fi.Size() == int64(len(probeSample)) {
			samplePath = target
			return
		}
		if err := os.WriteFile(target, probeSample, 0644); err != nil {
			sampleErr = fmt.Errorf("写入探测样本失败: %w", err)
			return
		}
		samplePath = target
	})
	return samplePath, sampleErr
}

// SaveEncodersToFile 将探测结果原子持久化至指定的 JSON 文件 (.tmp + os.Rename)
func SaveEncodersToFile(filePath, ffmpegPath, version string, encoders []EncoderInfo) error {
	dir := filepath.Dir(filePath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("创建配置目录失败: %w", err)
	}

	data := CachedEncodersData{
		Version:    version,
		FFmpegPath: ffmpegPath,
		Encoders:   encoders,
		UpdatedAt:  time.Now().Format("2006-01-02 15:04:05"),
	}

	raw, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("序列化编码器数据失败: %w", err)
	}

	tmpFile := filePath + ".tmp"
	if err := os.WriteFile(tmpFile, raw, 0644); err != nil {
		return fmt.Errorf("写入临时配置文件失败: %w", err)
	}

	_ = os.Remove(filePath)
	if err := os.Rename(tmpFile, filePath); err != nil {
		_ = os.Remove(tmpFile)
		return fmt.Errorf("重命名配置文件失败: %w", err)
	}

	return nil
}

// LoadEncodersFromFile 从指定的 JSON 文件读取缓存的编码器探测信息
func LoadEncodersFromFile(filePath, currentFFmpeg string) ([]EncoderInfo, error) {
	raw, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}

	var data CachedEncodersData
	if err := json.Unmarshal(raw, &data); err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	if len(data.Encoders) == 0 {
		return nil, fmt.Errorf("配置文件中编码器列表为空")
	}

	// 校验 FFmpeg 路径：若路径不匹配，说明工具链位置已变更，需重新实测
	if currentFFmpeg != "" && data.FFmpegPath != "" {
		absCur, _ := filepath.Abs(currentFFmpeg)
		absCached, _ := filepath.Abs(data.FFmpegPath)
		if !strings.EqualFold(absCur, absCached) {
			return nil, fmt.Errorf("FFmpeg 路径已变更 (%s != %s)", absCur, absCached)
		}
	}

	return data.Encoders, nil
}

// runSingleSmokeTest 对指定硬件编码器执行真实单帧压制实测
// 以能否在磁盘真正产出体积 > 0 的有效视频为唯一真值判据
func runSingleSmokeTest(ffmpegPath, sampleVideo, encoderID string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
	defer cancel()

	tmpOut := filepath.Join(os.TempDir(), fmt.Sprintf("deepsr_smoke_%d_%s.mkv", os.Getpid(), encoderID))
	_ = os.Remove(tmpOut)
	defer os.Remove(tmpOut)

	args := []string{
		"-hide_banner",
		"-loglevel", "error",
		"-i", filepath.ToSlash(sampleVideo),
		"-frames:v", "1",
		"-an",
		"-c:v", encoderID,
		"-f", "matroska",
		"-y", filepath.ToSlash(tmpOut),
	}

	cmd := cmdutil.CommandContext(ctx, ffmpegPath, args...)
	_ = cmd.Run()

	fi, err := os.Stat(tmpOut)
	return err == nil && fi.Size() > 0
}

// DetectEncoders 真实探测系统 FFmpeg 和硬件实际支持的编码器列表。
// force: 若为 true，忽略已有缓存强制重新实测并覆盖写入 %APPDATA%/DeepSR/encoders.json；
// 若为 false，优先秒级读取本地持久化缓存。
func DetectEncoders(ffmpegPath string, force bool) []EncoderInfo {
	resolved := findExecutable(ffmpegPath, "ffmpeg")
	if resolved != "" {
		ffmpegPath = resolved
	} else if ffmpegPath == "" {
		ffmpegPath = "ffmpeg"
	}

	configPath := GetEncodersConfigPath()

	// 1. 若非强制探测，先查内存缓存
	encoderCacheMu.RLock()
	if !force && len(cachedEncoders) > 0 && cachedFFmpegPath == ffmpegPath {
		res := make([]EncoderInfo, len(cachedEncoders))
		copy(res, cachedEncoders)
		encoderCacheMu.RUnlock()
		return res
	}
	encoderCacheMu.RUnlock()

	// 2. 若非强制探测，尝试从本地持久化配置文件 %APPDATA%/DeepSR/encoders.json 读取
	if !force {
		if loaded, err := LoadEncodersFromFile(configPath, ffmpegPath); err == nil && len(loaded) > 0 {
			encoderCacheMu.Lock()
			cachedEncoders = make([]EncoderInfo, len(loaded))
			copy(cachedEncoders, loaded)
			cachedFFmpegPath = ffmpegPath
			encoderCacheMu.Unlock()

			res := make([]EncoderInfo, len(loaded))
			copy(res, loaded)
			return res
		}
	}

	encoderCacheMu.Lock()
	defer encoderCacheMu.Unlock()

	// 双重检查防并发争抢
	if !force && len(cachedEncoders) > 0 && cachedFFmpegPath == ffmpegPath {
		res := make([]EncoderInfo, len(cachedEncoders))
		copy(res, cachedEncoders)
		return res
	}

	// 3. 准备内嵌真实单帧视频样本
	sample, err := ensureSampleFile()
	if err != nil {
		// 样本若无法加载，回退至纯 CPU
		fallback := make([]EncoderInfo, len(defaultEncoderList))
		copy(fallback, defaultEncoderList)
		for i := range fallback {
			fallback[i].Supported = fallback[i].Vendor == "cpu"
		}
		cachedEncoders = fallback
		cachedFFmpegPath = ffmpegPath
		return fallback
	}

	// 4. 快速获取 FFmpeg 编译支持的编码器列表及版本号
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	cmdEncoders := cmdutil.CommandContext(ctx, ffmpegPath, "-encoders")
	outEncoders, _ := cmdEncoders.Output()
	cancel()
	compiledEncodersStr := string(outEncoders)

	ctxVer, cancelVer := context.WithTimeout(context.Background(), 2*time.Second)
	cmdVer := cmdutil.CommandContext(ctxVer, ffmpegPath, "-version")
	outVer, _ := cmdVer.Output()
	cancelVer()
	ffmpegVerStr := ""
	if lines := strings.Split(string(outVer), "\n"); len(lines) > 0 {
		ffmpegVerStr = strings.TrimSpace(lines[0])
	}

	results := make([]EncoderInfo, len(defaultEncoderList))
	copy(results, defaultEncoderList)

	// 5. 并发冒烟实测所有已编译的硬件编码器 (并发数 4)
	type testTask struct {
		index     int
		encoderID string
	}
	var tasks []testTask

	for idx, enc := range results {
		// 未编译该编码器，直接判定为不支持
		if !strings.Contains(compiledEncodersStr, enc.ID) {
			results[idx].Supported = false
			continue
		}

		// CPU 软件编码器编译即支持，无需进行耗时冒烟实测
		if enc.Vendor == "cpu" {
			results[idx].Supported = true
			continue
		}

		// 硬件编码器加入待实测任务列表
		tasks = append(tasks, testTask{index: idx, encoderID: enc.ID})
	}

	if len(tasks) > 0 {
		var wg sync.WaitGroup
		sem := make(chan struct{}, 4)
		var testMu sync.Mutex

		for _, t := range tasks {
			wg.Add(1)
			go func(task testTask) {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()

				ok := runSingleSmokeTest(ffmpegPath, sample, task.encoderID)

				testMu.Lock()
				results[task.index].Supported = ok
				testMu.Unlock()
			}(t)
		}
		wg.Wait()
	}

	// 6. 将实测结果原子固化写入 %APPDATA%/DeepSR/encoders.json
	_ = SaveEncodersToFile(configPath, ffmpegPath, ffmpegVerStr, results)

	cachedEncoders = make([]EncoderInfo, len(results))
	copy(cachedEncoders, results)
	cachedFFmpegPath = ffmpegPath

	ret := make([]EncoderInfo, len(cachedEncoders))
	copy(ret, cachedEncoders)
	return ret
}

// SetCachedEncoders 手动设置已缓存的编码器列表，使自动编码器解析实现 0 延迟秒级响应
func SetCachedEncoders(encoders []EncoderInfo, ffmpegPath string) {
	encoderCacheMu.Lock()
	defer encoderCacheMu.Unlock()
	cachedEncoders = make([]EncoderInfo, len(encoders))
	copy(cachedEncoders, encoders)
	cachedFFmpegPath = ffmpegPath
}

// GetCachedEncoders 获取当前内存缓存的编码器列表副本（若未缓存则返回 nil）
func GetCachedEncoders() []EncoderInfo {
	encoderCacheMu.RLock()
	defer encoderCacheMu.RUnlock()
	if len(cachedEncoders) == 0 {
		return nil
	}
	res := make([]EncoderInfo, len(cachedEncoders))
	copy(res, cachedEncoders)
	return res
}
