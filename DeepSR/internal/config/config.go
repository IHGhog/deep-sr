package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"deepsr/internal/engine/ffmpeg"
)

type AppConfig struct {
	// 内部运行时绝对路径：系统基于执行目录动态解析，绝不持久化至 config.json 配置文件
	DeepSRPath  string `json:"-"`
	FFmpegPath  string `json:"-"`
	FFprobePath string `json:"-"`

	// 用户自定义全局配置（持久化至 config.json）
	TempDir          string `json:"tempDir"`
	AutoCleanupTemp  bool   `json:"autoCleanupTemp"`
	PreferredEncoder string `json:"preferredEncoder"`
	VideoCRF         int    `json:"videoCrf"`
	VideoPreset      string `json:"videoPreset"`
	TileSize         int    `json:"tileSize"`
	Threads          string `json:"threads"`
	EnableStreamPipe bool   `json:"enableStreamPipe"`

	// 硬件加速编码器检测缓存
	CachedEncoders   []ffmpeg.EncoderInfo `json:"cachedEncoders,omitempty"`
	EncodersCachedAt string               `json:"encodersCachedAt,omitempty"`
}

var (
	currentConfig AppConfig
	configMutex   sync.RWMutex
	configPath    string
)

// GetDeepSRCLIPath 获取固定内置 deepsr-cli 路径: .\bin\deepsr-cli.exe (内核专享，不支持自定义)
func GetDeepSRCLIPath() string {
	exePath, _ := os.Executable()
	exeDir := filepath.Dir(exePath)
	p := filepath.Join(exeDir, "bin", "deepsr-cli.exe")
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

// GetDefaultFFmpegPath 获取默认内置 ffmpeg 路径: .\bin\ffmpeg.exe
func GetDefaultFFmpegPath() string {
	exePath, _ := os.Executable()
	exeDir := filepath.Dir(exePath)
	p := filepath.Join(exeDir, "bin", "ffmpeg.exe")
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

// GetDefaultFFprobePath 获取默认内置 ffprobe 路径: .\bin\ffprobe.exe
func GetDefaultFFprobePath() string {
	exePath, _ := os.Executable()
	exeDir := filepath.Dir(exePath)
	p := filepath.Join(exeDir, "bin", "ffprobe.exe")
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

// GetDefaultConfig 返回开箱即用的默认配置
func GetDefaultConfig() AppConfig {
	userConfigDir, _ := os.UserConfigDir()
	appDataDir := filepath.Join(userConfigDir, "DeepSR")
	tempDir := filepath.Join(appDataDir, "temp")

	return AppConfig{
		DeepSRPath:       GetDeepSRCLIPath(),
		FFmpegPath:       GetDefaultFFmpegPath(),
		FFprobePath:      GetDefaultFFprobePath(),
		TempDir:          tempDir,
		AutoCleanupTemp:  true,
		PreferredEncoder: "auto",
		VideoCRF:         20,
		VideoPreset:      "medium",
		TileSize:         0,
		Threads:          "3:2:3",
		EnableStreamPipe: false,
	}
}

// InitConfig 初始化并加载配置
func InitConfig(customPath string) (AppConfig, error) {
	configMutex.Lock()
	defer configMutex.Unlock()

	if customPath != "" {
		configPath = customPath
	} else {
		userConfigDir, err := os.UserConfigDir()
		if err != nil {
			userConfigDir = "."
		}
		configPath = filepath.Join(userConfigDir, "DeepSR", "config.json")
	}

	// 确保配置目录与临时目录存在
	_ = os.MkdirAll(filepath.Dir(configPath), 0755)

	defCfg := GetDefaultConfig()
	_ = os.MkdirAll(defCfg.TempDir, 0755)

	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		currentConfig = defCfg
		data, _ := json.MarshalIndent(currentConfig, "", "  ")
		_ = os.WriteFile(configPath, data, 0644)
		return currentConfig, nil
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		currentConfig = defCfg
		return currentConfig, err
	}

	currentConfig = defCfg
	if err := json.Unmarshal(data, &currentConfig); err != nil {
		currentConfig = defCfg
		return currentConfig, err
	}

	// CLI 核心程序与 FFmpeg 工具链绝对严格锁定为当前运行目录下的 .\bin 目录
	currentConfig.DeepSRPath = GetDeepSRCLIPath()
	currentConfig.FFmpegPath = GetDefaultFFmpegPath()
	currentConfig.FFprobePath = GetDefaultFFprobePath()

	// 自动清洗配置文件：去除已废弃字段与绝对路径，以规范格式重写 config.json
	if cleanData, err := json.MarshalIndent(currentConfig, "", "  "); err == nil {
		_ = os.WriteFile(configPath, cleanData, 0644)
	}

	return currentConfig, nil
}

// GetConfig 获取当前配置的副本
func GetConfig() AppConfig {
	configMutex.RLock()
	defer configMutex.RUnlock()
	cfg := currentConfig
	if len(cfg.CachedEncoders) == 0 {
		cfg.CachedEncoders = ffmpeg.GetCachedEncoders()
	}
	return cfg
}

// SaveConfig 保存用户配置，保护编码器缓存不被冲刷并采用原子写入
func SaveConfig(cfg AppConfig) error {
	configMutex.Lock()
	defer configMutex.Unlock()

	// 确保 CLI 与 FFmpeg 工具链永远锁定为内置 bin 目录，不被外部篡改
	cfg.DeepSRPath = GetDeepSRCLIPath()
	cfg.FFmpegPath = GetDefaultFFmpegPath()
	cfg.FFprobePath = GetDefaultFFprobePath()

	// 保护已缓存的编码器信息，若入参为空则保留当前已有缓存
	if len(cfg.CachedEncoders) == 0 {
		if len(currentConfig.CachedEncoders) > 0 {
			cfg.CachedEncoders = currentConfig.CachedEncoders
			cfg.EncodersCachedAt = currentConfig.EncodersCachedAt
		} else {
			cfg.CachedEncoders = ffmpeg.GetCachedEncoders()
		}
	}

	currentConfig = cfg
	data, err := json.MarshalIndent(currentConfig, "", "  ")
	if err != nil {
		return err
	}

	dir := filepath.Dir(configPath)
	_ = os.MkdirAll(dir, 0755)

	tmpFile := configPath + ".tmp"
	if err := os.WriteFile(tmpFile, data, 0644); err != nil {
		return err
	}

	_ = os.Remove(configPath)
	if err := os.Rename(tmpFile, configPath); err != nil {
		_ = os.Remove(tmpFile)
		return err
	}

	return nil
}
