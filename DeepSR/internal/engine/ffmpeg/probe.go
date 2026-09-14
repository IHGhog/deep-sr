package ffmpeg

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"deepsr/internal/engine/cmdutil"
)

// MediaInfo 媒体文件详细元数据
type MediaInfo struct {
	FilePath   string  `json:"filePath"`
	FileName   string  `json:"fileName"`
	FileSize   int64   `json:"fileSize"`
	IsVideo    bool    `json:"isVideo"`
	Width      int     `json:"width"`
	Height     int     `json:"height"`
	Duration   float64 `json:"duration"` // 秒
	FrameCount int     `json:"frameCount"`
	FrameRate  float64 `json:"frameRate"`
	VideoCodec string  `json:"videoCodec"`
	AudioCodec string  `json:"audioCodec"`
	HasAudio   bool    `json:"hasAudio"`
	Bitrate    int64   `json:"bitrate"`
	FormatName string  `json:"formatName"`
}

type ffprobeOutput struct {
	Streams []struct {
		CodecType    string `json:"codec_type"`
		CodecName    string `json:"codec_name"`
		Width        int    `json:"width"`
		Height       int    `json:"height"`
		RFrameRate   string `json:"r_frame_rate"`
		AvgFrameRate string `json:"avg_frame_rate"`
		Duration     string `json:"duration"`
		NbFrames     string `json:"nb_frames"`
		BitRate      string `json:"bit_rate"`
	} `json:"streams"`
	Format struct {
		Filename   string `json:"filename"`
		Duration   string `json:"duration"`
		Size       string `json:"size"`
		BitRate    string `json:"bit_rate"`
		FormatName string `json:"format_name"`
	} `json:"format"`
}

func findExecutable(customPath string, name string) string {
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

	nameExe := name
	if !strings.HasSuffix(strings.ToLower(nameExe), ".exe") {
		nameExe += ".exe"
	}

	p := filepath.Join(exeDir, "bin", nameExe)
	if abs, err := filepath.Abs(p); err == nil {
		return abs
	}
	return p
}

// ProbeMedia 使用 ffprobe 探测媒体文件元数据
func ProbeMedia(ffprobePath, filePath string) (*MediaInfo, error) {
	st, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("文件不存在: %w", err)
	}

	info := &MediaInfo{
		FilePath: filePath,
		FileName: st.Name(),
		FileSize: st.Size(),
	}

	// 检查是否为纯图片
	ext := strings.ToLower(st.Name())
	if strings.HasSuffix(ext, ".jpg") || strings.HasSuffix(ext, ".jpeg") ||
		strings.HasSuffix(ext, ".png") || strings.HasSuffix(ext, ".webp") ||
		strings.HasSuffix(ext, ".bmp") {
		if f, err := os.Open(filePath); err == nil {
			if imgCfg, _, err := image.DecodeConfig(f); err == nil {
				info.IsVideo = false
				info.Width = imgCfg.Width
				info.Height = imgCfg.Height
				info.FrameCount = 1
				_ = f.Close()
				return info, nil
			}
			_ = f.Close()
		}
	}

	resolved := findExecutable(ffprobePath, "ffprobe")
	if resolved != "" {
		ffprobePath = resolved
	} else if ffprobePath == "" {
		ffprobePath = "ffprobe"
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := cmdutil.CommandContext(ctx, ffprobePath,
		"-hide_banner",
		"-v", "quiet",
		"-print_format", "json",
		"-show_format",
		"-show_streams",
		filePath,
	)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return info, fmt.Errorf("ffprobe 解析失败: %v, stderr: %s", err, stderr.String())
	}

	var parsed ffprobeOutput
	if err := json.Unmarshal(stdout.Bytes(), &parsed); err != nil {
		return info, fmt.Errorf("解析 ffprobe json 失败: %w", err)
	}

	if d, err := strconv.ParseFloat(parsed.Format.Duration, 64); err == nil {
		info.Duration = d
	}
	if b, err := strconv.ParseInt(parsed.Format.BitRate, 10, 64); err == nil {
		info.Bitrate = b
	}
	info.FormatName = parsed.Format.FormatName

	for _, s := range parsed.Streams {
		if s.CodecType == "video" && info.Width == 0 {
			info.IsVideo = true
			info.Width = s.Width
			info.Height = s.Height
			info.VideoCodec = s.CodecName
			info.FrameRate = parseFraction(s.RFrameRate)
			if info.FrameRate == 0 {
				info.FrameRate = parseFraction(s.AvgFrameRate)
			}
			if fc, err := strconv.Atoi(s.NbFrames); err == nil && fc > 0 {
				info.FrameCount = fc
			}
		} else if s.CodecType == "audio" && !info.HasAudio {
			info.HasAudio = true
			info.AudioCodec = s.CodecName
		}
	}

	if info.FrameCount == 0 && info.Duration > 0 && info.FrameRate > 0 {
		info.FrameCount = int(info.Duration * info.FrameRate)
	}

	return info, nil
}

func parseFraction(s string) float64 {
	parts := strings.Split(s, "/")
	if len(parts) == 2 {
		num, err1 := strconv.ParseFloat(parts[0], 64)
		den, err2 := strconv.ParseFloat(parts[1], 64)
		if err1 == nil && err2 == nil && den > 0 {
			return num / den
		}
	}
	if v, err := strconv.ParseFloat(s, 64); err == nil {
		return v
	}
	return 0
}
