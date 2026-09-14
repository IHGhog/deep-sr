package config

import (
	"os"
	"strings"
	"testing"

	"deepsr/internal/engine/ffmpeg"
)

func TestCleanConfig(t *testing.T) {
	cfg, err := InitConfig("")
	if err != nil {
		t.Fatalf("InitConfig failed: %v", err)
	}

	if cfg.DeepSRPath == "" {
		t.Errorf("DeepSRPath should not be empty")
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("failed to read config file: %v", err)
	}

	content := string(data)
	deprecatedKeys := []string{
		"deepsrPath",
		"ffmpegPath",
		"ffprobePath",
		"defaultModel",
		"defaultScale",
		"enableFaceBooster",
		"faceFidelity",
		"gpuDevice",
		"outputFormat",
	}

	for _, key := range deprecatedKeys {
		if strings.Contains(content, `"`+key+`"`) {
			t.Errorf("config.json should not contain deprecated/internal key: %s", key)
		}
	}
}

func TestSaveConfigProtectsEncoders(t *testing.T) {
	tmpDir := t.TempDir()
	cfgFile := tmpDir + "/config.json"
	_, err := InitConfig(cfgFile)
	if err != nil {
		t.Fatalf("InitConfig failed: %v", err)
	}

	// 模拟已存在编码器缓存
	mockEncoders := []ffmpeg.EncoderInfo{
		{ID: "h264_qsv", Name: "Intel QSV", Vendor: "intel", Supported: true},
	}
	currentConfig.CachedEncoders = mockEncoders

	// 模拟前端提交不含 cachedEncoders 的常规设置表单
	form := AppConfig{
		TempDir:          tmpDir + "/temp",
		PreferredEncoder: "h264_qsv",
		VideoCRF:         18,
		VideoPreset:      "fast",
	}

	if err := SaveConfig(form); err != nil {
		t.Fatalf("SaveConfig failed: %v", err)
	}

	saved := GetConfig()
	if len(saved.CachedEncoders) == 0 {
		t.Errorf("SaveConfig 导致编码器缓存被冲刷为空")
	}
	if saved.VideoCRF != 18 {
		t.Errorf("VideoCRF 未正确更新: got %d, want 18", saved.VideoCRF)
	}
}
