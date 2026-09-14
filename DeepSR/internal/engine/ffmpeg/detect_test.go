package ffmpeg

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDetectEncodersRealAndCache(t *testing.T) {
	// 定位内置 ffmpeg
	cur, _ := os.Getwd()
	var ffmpegPath string
	for i := 0; i < 6; i++ {
		checkFFmpeg := filepath.Join(cur, "bin", "ffmpeg.exe")
		if fi, err := os.Stat(checkFFmpeg); err == nil && !fi.IsDir() {
			ffmpegPath = checkFFmpeg
			break
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}

	if ffmpegPath == "" {
		t.Skip("未找到 bin/ffmpeg.exe，跳过集成测试")
	}

	// 1. 设置独立的测试缓存路径，避免污染生产环境 %APPDATA%/DeepSR/encoders.json
	tmpDir := t.TempDir()
	testCachePath := filepath.Join(tmpDir, "test_encoders.json")
	cleanup := SetCustomEncodersCachePathForTest(testCachePath)
	defer cleanup()

	// 2. 首次强制实测
	encoders := DetectEncoders(ffmpegPath, true)
	if len(encoders) == 0 {
		t.Fatalf("DetectEncoders 返回列表为空")
	}

	// 检查 encoders.json 是否已原子写入
	fi, err := os.Stat(testCachePath)
	if err != nil {
		t.Fatalf("encoders.json 未生成: %v", err)
	}
	if fi.Size() == 0 {
		t.Fatalf("encoders.json 大小为 0")
	}

	// 校验当前开发机的 QSV 支持性（已知本机 Intel 支持 h264_qsv 和 hevc_qsv）
	var h264QSV, hevcQSV, x264 *EncoderInfo
	for i := range encoders {
		switch encoders[i].ID {
		case "h264_qsv":
			h264QSV = &encoders[i]
		case "hevc_qsv":
			hevcQSV = &encoders[i]
		case "libx264":
			x264 = &encoders[i]
		}
	}

	if x264 == nil || !x264.Supported {
		t.Errorf("libx264 应必然受支持")
	}
	if h264QSV != nil && !h264QSV.Supported {
		t.Errorf("h264_qsv 探测失败，预期在本机应受支持")
	}
	if hevcQSV != nil && !hevcQSV.Supported {
		t.Errorf("hevc_qsv 探测失败，预期在本机应受支持")
	}

	// 3. 测试秒级读缓存（force = false）
	cached := DetectEncoders(ffmpegPath, false)
	if len(cached) != len(encoders) {
		t.Errorf("缓存读取的编码器数量不匹配: got %d, want %d", len(cached), len(encoders))
	}

	// 4. 测试 LoadEncodersFromFile
	loaded, err := LoadEncodersFromFile(testCachePath, ffmpegPath)
	if err != nil {
		t.Fatalf("LoadEncodersFromFile 失败: %v", err)
	}
	if len(loaded) != len(encoders) {
		t.Errorf("从文件加载的编码器数量不匹配: got %d, want %d", len(loaded), len(encoders))
	}
}
