package pipeline

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"deepsr/internal/config"
	"deepsr/internal/engine/deepsr"
	"deepsr/internal/engine/ffmpeg"
	"deepsr/internal/queue"
)

// copyOrLinkFile 优先尝试硬链接，若跨驱动器则回退至高速文件复制
func copyOrLinkFile(src, dst string) error {
	_ = os.Remove(dst)
	if err := os.Link(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	return err
}

func countValidFrames(dir, ext string, total int) int {
	count := 0
	for i := 1; i <= total; i++ {
		fName := fmt.Sprintf("frame%08d.%s", i, ext)
		fPath := filepath.Join(dir, fName)
		if fi, err := os.Stat(fPath); err == nil && fi.Size() > 0 {
			count++
		} else {
			break
		}
	}
	return count
}

// ProcessVideo 处理完整视频超分辨率增强管道 (全流程断点续抽、增量超分与 1800 帧分段断点合并)
func ProcessVideo(ctx context.Context, task *queue.Task, cfg config.AppConfig, logInfo func(string), logCmd func(string), onProgress func(pct float64, stage queue.TaskStage, stageText string, currentFrame int, totalFrames int, speedFPS float64, msg string)) error {
	// 0. 前置校验：视频超分/人脸修复模型文件是否存在（未就绪立即终止，避免无效抽帧）
	if err := deepsr.CheckModelReady(task.ModelName, "", task.EnableFaceBooster); err != nil {
		return err
	}

	// 1. 获取视频详细元数据
	mediaInfo, err := ffmpeg.ProbeMedia(cfg.FFprobePath, task.InputPath)
	if err != nil {
		return fmt.Errorf("视频元数据解析失败: %w", err)
	}

	task.TotalFrames = mediaInfo.FrameCount
	if task.TotalFrames <= 0 && mediaInfo.Duration > 0 && mediaInfo.FrameRate > 0 {
		task.TotalFrames = int(mediaInfo.Duration * mediaInfo.FrameRate)
	}
	if task.TotalFrames <= 0 {
		task.TotalFrames = 1
	}

	encoder := task.Encoder
	if encoder == "" || encoder == "auto" {
		encoder = cfg.PreferredEncoder
	}
	encoder = ffmpeg.ResolveBestEncoder(cfg.FFmpegPath, encoder)

	// 校验并对齐输出路径：若 OutputPath 为空或指向一个现存目录，自动在其下拼装视频文件名
	ext := filepath.Ext(task.InputPath)
	base := strings.TrimSuffix(filepath.Base(task.InputPath), ext)
	outExt := task.Format
	if outExt == "" {
		outExt = "mp4"
	}
	if !strings.HasPrefix(outExt, ".") {
		outExt = "." + outExt
	}
	defaultVideoName := fmt.Sprintf("%s_deepsr_x%d%s", base, task.Scale, outExt)

	if task.OutputPath == "" {
		task.OutputPath = filepath.Join(filepath.Dir(task.InputPath), defaultVideoName)
	} else if fi, err := os.Stat(task.OutputPath); err == nil && fi.IsDir() {
		task.OutputPath = filepath.Join(task.OutputPath, defaultVideoName)
	}

	// 【全内存流式管道模式】：0 磁盘小文件读写，流式直通 GPU 与硬件编码器（由任务创建时绑定的模式决定，不受全局设置后续变动干扰）
	if task.EnableStreamPipe {
		task.Stage = queue.TaskStageUpscaling
		task.StageText = "全内存流式超分中"
		if onProgress != nil {
			onProgress(0, task.Stage, "启动全内存流式管道...", 0, task.TotalFrames, 0, "0 磁盘文件落地，内存管道直通")
		}

		tileSize := task.TileSize
		if tileSize <= 0 {
			tileSize = cfg.TileSize
		}

		opt := deepsr.UpscaleOptions{
			DeepSRPath:        cfg.DeepSRPath,
			InputPath:         task.InputPath,
			OutputPath:        task.OutputPath,
			ModelName:         task.ModelName,
			Scale:             task.Scale,
			EnableFaceBooster: task.EnableFaceBooster,
			FaceFidelity:      task.FaceFidelity,
			GPUDevice:         task.GPUDevice,
			GPUDeviceStr:      task.GPUDeviceStr,
			TileSize:          tileSize,
			Threads:           cfg.Threads,
			EnableStreamPipe:  true,
			Encoder:           encoder,
			CRF:               cfg.VideoCRF,
			Preset:            cfg.VideoPreset,
			LogCommand:        logCmd,
			OnLog: func(level, msg string) {
				if logInfo != nil {
					logInfo(msg)
				}
			},
			OnProgress: func(pct float64, current, total int, phase, msg string) {
				if total > task.TotalFrames {
					task.TotalFrames = total
				}
				if current > task.TotalFrames {
					task.TotalFrames = current
				}
				if onProgress != nil {
					onProgress(pct, queue.TaskStageUpscaling, "全内存流式超分中", current, task.TotalFrames, 0, msg)
				}
			},
		}

		if err := deepsr.RunUpscale(ctx, opt); err != nil {
			return fmt.Errorf("全内存流式超分失败: %w", err)
		}
		if onProgress != nil {
			onProgress(100.0, queue.TaskStageCompleted, "流式超分完成", task.TotalFrames, task.TotalFrames, 0, "处理完成")
		}
		return nil
	}

	// 2. 定位并初始化基于指纹的专属工作目录 (经典落地模式)
	taskTempDir := queue.GetVideoTaskTempDir(cfg.TempDir, task.InputPath, task.ModelName, task.Scale, task.EnableFaceBooster)
	framesDir := filepath.Join(taskTempDir, "frames")
	enhancedDir := filepath.Join(taskTempDir, "enhanced")
	pendingDir := filepath.Join(taskTempDir, "pending")

	_ = os.MkdirAll(framesDir, 0755)
	_ = os.MkdirAll(enhancedDir, 0755)
	_ = os.MkdirAll(pendingDir, 0755)

	isCompleted := false
	defer func() {
		_ = os.RemoveAll(pendingDir)
		if isCompleted && cfg.AutoCleanupTemp {
			_ = os.RemoveAll(taskTempDir)
		}
	}()

	frameFormat := "jpg"
	if task.Format == "png" {
		frameFormat = "png"
	}
	framePattern := filepath.Join(framesDir, "frame%08d."+frameFormat)

	// 3. 阶段一：视频抽帧 (占比总体进度 15%)
	task.Stage = queue.TaskStageExtracting
	actualFrames := queue.CountMatchingFiles(framesDir, "frame", "."+frameFormat)

	if actualFrames > 0 && actualFrames >= task.TotalFrames {
		task.TotalFrames = actualFrames
		task.StageText = "原始帧已就绪"
		if onProgress != nil {
			onProgress(15.0, task.Stage, "原始帧已就绪 (0秒跳过)", task.TotalFrames, task.TotalFrames, 0, "检测到完整原始视频帧，直接进入 AI 超分")
		}
	} else {
		task.StageText = "提取视频帧"
		if onProgress != nil {
			onProgress(0, task.Stage, "开始提取视频帧...", 0, task.TotalFrames, 0, "正在提取视频原始帧")
		}

		extractErr := ffmpeg.ExtractFrames(ctx, cfg.FFmpegPath, task.InputPath, framePattern, frameFormat, task.TotalFrames, cfg.PreferredEncoder, task.GPUDevice, task.GPUDeviceStr, logInfo, logCmd, func(subPct float64, curFrame, totFrames int, fps float64, msg string) {
			overallPct := subPct * 0.15
			if onProgress != nil {
				onProgress(overallPct, queue.TaskStageExtracting, "正在提取视频帧", curFrame, totFrames, fps, msg)
			}
		})
		if extractErr != nil {
			return fmt.Errorf("提取视频帧失败: %w", extractErr)
		}

		if actualCount := queue.CountMatchingFiles(framesDir, "frame", "."+frameFormat); actualCount > 0 {
			task.TotalFrames = actualCount
		}
	}

	if ctx.Err() != nil {
		return ctx.Err()
	}

	// 4. 阶段二：AI 超分与人脸修复处理 (占比总体进度 15% -> 85%)
	task.Stage = queue.TaskStageUpscaling
	existingEnhanced := countValidFrames(enhancedDir, frameFormat, task.TotalFrames)

	if existingEnhanced >= task.TotalFrames {
		task.StageText = "超分帧已全部就绪"
		if onProgress != nil {
			onProgress(85.0, task.Stage, "超分帧已就绪 (0秒跳过)", task.TotalFrames, task.TotalFrames, 0, "检测到全部增强帧已完成，直接进入分段合成")
		}
	} else {
		task.StageText = "视频帧超分"
		_ = os.RemoveAll(pendingDir)
		_ = os.MkdirAll(pendingDir, 0755)

		missingCount := 0
		for i := 1; i <= task.TotalFrames; i++ {
			fName := fmt.Sprintf("frame%08d.%s", i, frameFormat)
			enhPath := filepath.Join(enhancedDir, fName)
			if fi, err := os.Stat(enhPath); err != nil || fi.Size() == 0 {
				srcPath := filepath.Join(framesDir, fName)
				dstPath := filepath.Join(pendingDir, fName)
				_ = copyOrLinkFile(srcPath, dstPath)
				missingCount++
			}
		}

		initialPct := 15.0 + (float64(existingEnhanced)/float64(task.TotalFrames))*70.0
		if onProgress != nil {
			onProgress(initialPct, task.Stage, "视频帧超分", existingEnhanced, task.TotalFrames, 0, fmt.Sprintf("正在从第 %d 帧断点继续超分", existingEnhanced+1))
		}

		tileSize := task.TileSize
		if tileSize <= 0 {
			tileSize = cfg.TileSize
		}

		upscaleOpt := deepsr.UpscaleOptions{
			DeepSRPath:        cfg.DeepSRPath,
			InputPath:         pendingDir,
			OutputPath:        enhancedDir,
			ModelName:         task.ModelName,
			Scale:             task.Scale,
			EnableFaceBooster: task.EnableFaceBooster,
			FaceFidelity:      task.FaceFidelity,
			GPUDevice:         task.GPUDevice,
			GPUDeviceStr:      task.GPUDeviceStr,
			TileSize:          tileSize,
			Threads:           cfg.Threads,
			Format:            frameFormat,
			LogCommand:        logCmd,
			OnProgress: func(subPct float64, current, total int, phase string, msg string) {
				newDone := int(float64(missingCount) * (subPct / 100.0))
				curFrame := existingEnhanced + newDone
				if curFrame > task.TotalFrames {
					curFrame = task.TotalFrames
				}
				overallPct := 15.0 + (float64(curFrame)/float64(task.TotalFrames))*70.0
				stageText := "视频帧超分"
				if task.EnableFaceBooster {
					stageText = "视频帧超分与人脸修复"
				}
				if onProgress != nil {
					onProgress(overallPct, queue.TaskStageUpscaling, stageText, curFrame, task.TotalFrames, 0, msg)
				}
			},
		}

		if err := deepsr.RunUpscale(ctx, upscaleOpt); err != nil {
			return fmt.Errorf("AI 超分过程出错: %w", err)
		}
	}

	if ctx.Err() != nil {
		return ctx.Err()
	}

	// 5. 阶段三：全量帧直出编码与极速音画合流 (占比总体进度 85% -> 100%)
	task.Stage = queue.TaskStageMerging
	task.StageText = "视频压制与音画合流"

	fps := mediaInfo.FrameRate
	if fps <= 0 {
		fps = 30.0
	}

	encoder = task.Encoder
	if encoder == "" || encoder == "auto" {
		encoder = cfg.PreferredEncoder
	}
	encoder = ffmpeg.ResolveBestEncoder(cfg.FFmpegPath, encoder)

	crf := task.CRF
	if crf <= 0 {
		crf = cfg.VideoCRF
	}
	if crf <= 0 {
		crf = 20
	}

	preset := task.Preset
	if preset == "" {
		preset = cfg.VideoPreset
	}
	if preset == "" {
		preset = "medium"
	}

	enhancedPattern := filepath.Join(enhancedDir, "frame%08d."+frameFormat)

	_ = os.MkdirAll(filepath.Dir(task.OutputPath), 0755)

	if onProgress != nil {
		onProgress(85.0, queue.TaskStageMerging, "正在启动视频压制...", 0, task.TotalFrames, 0, "正在将增强帧编码为目标视频并混流音轨")
	}

	encodeOpt := ffmpeg.EncodeVideoOptions{
		FFmpegPath:    cfg.FFmpegPath,
		FramesPattern: enhancedPattern,
		StartNumber:   1,
		FrameCount:    task.TotalFrames,
		FrameRate:     fps,
		OriginalVideo: task.InputPath,
		OutputPath:    task.OutputPath,
		Encoder:       encoder,
		CRF:           crf,
		Preset:        preset,
		LogCommand:    logCmd,
		OnProgress: func(curFrame, totalFrames int, speedFPS float64) {
			if curFrame > task.TotalFrames {
				curFrame = task.TotalFrames
			}
			overallPct := 85.0 + (float64(curFrame)/float64(task.TotalFrames))*15.0
			if overallPct > 99.0 {
				overallPct = 99.0
			}
			if onProgress != nil {
				onProgress(overallPct, queue.TaskStageMerging, "正在压制与合流音视频", curFrame, task.TotalFrames, speedFPS, fmt.Sprintf("正在编码视频帧: %d/%d (%.1f fps)", curFrame, task.TotalFrames, speedFPS))
			}
		},
	}

	if err := ffmpeg.EncodeFramesToVideo(ctx, encodeOpt); err != nil {
		return err
	}

	isCompleted = true
	if onProgress != nil {
		onProgress(100.0, queue.TaskStageCompleted, "增强处理完成", task.TotalFrames, task.TotalFrames, 0, fmt.Sprintf("视频增强已成功输出至: %s", filepath.Base(task.OutputPath)))
	}

	return nil
}
