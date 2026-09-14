package ffmpeg

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEncodeFramesToVideo(t *testing.T) {
	tmpDir, err := os.MkdirTemp("", "deepsr_test_encode_*")
	if err != nil {
		t.Fatalf("创建临时目录失败: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	// 1. 生成 15 张彩色测试帧
	framesDir := filepath.Join(tmpDir, "frames")
	if err := os.MkdirAll(framesDir, 0755); err != nil {
		t.Fatalf("创建帧目录失败: %v", err)
	}

	for i := 1; i <= 15; i++ {
		img := image.NewRGBA(image.Rect(0, 0, 160, 120))
		for y := 0; y < 120; y++ {
			for x := 0; x < 160; x++ {
				img.Set(x, y, color.RGBA{
					R: uint8((x + i*10) % 255),
					G: uint8((y + i*5) % 255),
					B: 120,
					A: 255,
				})
			}
		}
		fPath := filepath.Join(framesDir, fmt.Sprintf("frame%08d.jpg", i))
		f, err := os.Create(fPath)
		if err != nil {
			t.Fatalf("创建测试图片失败: %v", err)
		}
		if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 90}); err != nil {
			f.Close()
			t.Fatalf("写入测试图片失败: %v", err)
		}
		f.Close()
	}

	// 2. 定位 ffmpeg 与测试音频视频
	cur, _ := os.Getwd()
	var ffmpegPath, sampleVideo string
	for i := 0; i < 6; i++ {
		checkFFmpeg := filepath.Join(cur, "bin", "ffmpeg.exe")
		if fi, err := os.Stat(checkFFmpeg); err == nil && !fi.IsDir() {
			ffmpegPath = checkFFmpeg
		}
		checkSample := filepath.Join(cur, "samples", "samples_input", "onepiece.mp4")
		if fi, err := os.Stat(checkSample); err == nil && !fi.IsDir() {
			sampleVideo = checkSample
		}
		checkSampleParent := filepath.Join(cur, "..", "samples", "samples_input", "onepiece.mp4")
		if fi, err := os.Stat(checkSampleParent); err == nil && !fi.IsDir() {
			sampleVideo, _ = filepath.Abs(checkSampleParent)
		}
		if ffmpegPath != "" && sampleVideo != "" {
			break
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			break
		}
		cur = parent
	}

	outVideo := filepath.Join(tmpDir, "output.mp4")
	progressCalls := 0

	opt := EncodeVideoOptions{
		FFmpegPath:    ffmpegPath,
		FramesPattern: filepath.Join(framesDir, "frame%08d.jpg"),
		StartNumber:   1,
		FrameCount:    15,
		FrameRate:     24.0,
		OriginalVideo: sampleVideo,
		OutputPath:    outVideo,
		Encoder:       "libx264",
		CRF:           23,
		Preset:        "ultrafast",
		OnProgress: func(curFrame, totalFrames int, speedFPS float64) {
			progressCalls++
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := EncodeFramesToVideo(ctx, opt); err != nil {
		t.Fatalf("EncodeFramesToVideo 失败: %v", err)
	}

	// 3. 验证产物
	fi, err := os.Stat(outVideo)
	if err != nil {
		t.Fatalf("目标输出视频未生成: %v", err)
	}
	if fi.Size() == 0 {
		t.Fatalf("目标输出视频大小为 0")
	}

	tmpFile := filepath.Join(tmpDir, "output.tmp.mp4")
	if _, err := os.Stat(tmpFile); err == nil {
		t.Errorf("临时文件 %s 未被清理", tmpFile)
	}

	if progressCalls == 0 {
		t.Errorf("未接收到任何进度回调")
	}
}
