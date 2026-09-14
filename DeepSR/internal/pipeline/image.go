package pipeline

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"deepsr/internal/config"
	"deepsr/internal/engine/deepsr"
	"deepsr/internal/queue"
)

func mapPhaseToStageText(phase string, enableFaceBooster bool) string {
	switch phase {
	case "start":
		return "准备中"
	case "load", "loading", "loaded":
		return "加载图片"
	case "sr":
		return "图片超分"
	case "recognize", "recognizing", "recognized", "detect":
		return "识别人脸"
	case "face":
		return "人脸修复"
	case "done":
		return "处理完成"
	case "batch":
		return "批量处理中"
	default:
		if enableFaceBooster {
			return "图片超分与人脸修复"
		}
		return "图片超分"
	}
}

// ProcessImage 处理单张图片、批量图片或图片文件夹超分任务
func ProcessImage(ctx context.Context, task *queue.Task, cfg config.AppConfig, logCmd func(string), onProgress func(pct float64, stageText string, currentCount, totalCount int, msg string)) error {
	// 0. 前置校验：模型文件是否存在（未就绪立即终止）
	if err := deepsr.CheckModelReady(task.ModelName, "", task.EnableFaceBooster); err != nil {
		return err
	}

	task.Stage = queue.TaskStageUpscaling
	task.StageText = "图片超分"

	deepsrPath := cfg.DeepSRPath

	// 1. 处理文件夹整体超分任务
	if task.Type == queue.TaskTypeFolderImage {
		outDir := task.OutputPath
		if outDir == "" {
			outDir = task.InputPath + "_deepsr"
		}
		_ = os.MkdirAll(outDir, 0755)

		tileSize := task.TileSize
		if tileSize <= 0 {
			tileSize = cfg.TileSize
		}

		entries, _ := os.ReadDir(task.InputPath)
		validCount := 0
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			ext := strings.ToLower(filepath.Ext(e.Name()))
			if ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".webp" || ext == ".bmp" {
				validCount++
			}
		}
		task.TotalFrames = validCount
		task.CurrentFrame = 0

		opt := deepsr.UpscaleOptions{
			DeepSRPath:        deepsrPath,
			InputPath:         task.InputPath,
			OutputPath:        outDir,
			ModelName:         task.ModelName,
			Scale:             task.Scale,
			EnableFaceBooster: task.EnableFaceBooster,
			FaceFidelity:      task.FaceFidelity,
			GPUDevice:         task.GPUDevice,
			GPUDeviceStr:      task.GPUDeviceStr,
			TileSize:          tileSize,
			Threads:           cfg.Threads,
			Format:            task.Format,
			LogCommand:        logCmd,
			OnProgress: func(pct float64, current, total int, phase string, msg string) {
				stText := mapPhaseToStageText(phase, task.EnableFaceBooster)
				curCount := current
				totCount := total
				if totCount <= 0 {
					totCount = validCount
				}
				detailMsg := msg
				if totCount > 0 && curCount > 0 {
					detailMsg = fmt.Sprintf("[%d/%d] %s", curCount, totCount, msg)
				}
				if onProgress != nil {
					onProgress(pct, stText, curCount, totCount, detailMsg)
				}
			},
		}

		return deepsr.RunUpscale(ctx, opt)
	}

	// 2. 处理多图批量任务 (task.InputPaths)
	if len(task.InputPaths) > 0 {
		total := len(task.InputPaths)
		task.TotalFrames = total
		task.CurrentFrame = 0
		for idx, inPath := range task.InputPaths {
			if ctx.Err() != nil {
				return ctx.Err()
			}

			ext := filepath.Ext(inPath)
			base := strings.TrimSuffix(inPath, ext)
			fileName := filepath.Base(inPath)
			outExt := ext
			if task.Format != "" {
				outExt = "." + strings.TrimPrefix(task.Format, ".")
			}
			outPath := fmt.Sprintf("%s_deepsr_x%d%s", base, task.Scale, outExt)
			if task.OutputPath != "" {
				if fi, err := os.Stat(task.OutputPath); err == nil && fi.IsDir() {
					outPath = filepath.Join(task.OutputPath, filepath.Base(base)+fmt.Sprintf("_deepsr_x%d%s", task.Scale, outExt))
				}
			}

			basePct := (float64(idx) / float64(total)) * 100.0
			subSpan := 100.0 / float64(total)

			tileSize := task.TileSize
			if tileSize <= 0 {
				tileSize = cfg.TileSize
			}

			opt := deepsr.UpscaleOptions{
				DeepSRPath:        deepsrPath,
				InputPath:         inPath,
				OutputPath:        outPath,
				ModelName:         task.ModelName,
				Scale:             task.Scale,
				EnableFaceBooster: task.EnableFaceBooster,
				FaceFidelity:      task.FaceFidelity,
				GPUDevice:         task.GPUDevice,
				GPUDeviceStr:      task.GPUDeviceStr,
				TileSize:          tileSize,
				Threads:           cfg.Threads,
				Format:            task.Format,
				LogCommand:        logCmd,
				OnProgress: func(subPct float64, current, tot int, phase string, msg string) {
					overall := basePct + (subPct/100.0)*subSpan
					stText := mapPhaseToStageText(phase, task.EnableFaceBooster)
					detailMsg := fmt.Sprintf("[%d/%d] 正在处理 %s: %.0f%%", idx+1, total, fileName, subPct)
					if onProgress != nil {
						onProgress(overall, stText, idx+1, total, detailMsg)
					}
				},
			}

			if err := deepsr.RunUpscale(ctx, opt); err != nil {
				return fmt.Errorf("处理文件 [%s] 失败: %w", filepath.Base(inPath), err)
			}
		}

		if onProgress != nil {
			onProgress(100.0, "处理完成", total, total, fmt.Sprintf("批量处理完成，共 %d 张图片", total))
		}
		return nil
	}

	// 3. 处理单图超分
	task.TotalFrames = 1
	task.CurrentFrame = 1
	outPath := task.OutputPath
	ext := filepath.Ext(task.InputPath)
	base := strings.TrimSuffix(filepath.Base(task.InputPath), ext)
	outExt := ext
	if task.Format != "" {
		outExt = "." + strings.TrimPrefix(task.Format, ".")
	}
	defaultFileName := fmt.Sprintf("%s_deepsr_x%d%s", base, task.Scale, outExt)

	if outPath == "" {
		outPath = filepath.Join(filepath.Dir(task.InputPath), defaultFileName)
	} else if fi, err := os.Stat(outPath); err == nil && fi.IsDir() {
		outPath = filepath.Join(outPath, defaultFileName)
	}
	task.OutputPath = outPath

	tileSize := task.TileSize
	if tileSize <= 0 {
		tileSize = cfg.TileSize
	}

	opt := deepsr.UpscaleOptions{
		DeepSRPath:        deepsrPath,
		InputPath:         task.InputPath,
		OutputPath:        outPath,
		ModelName:         task.ModelName,
		Scale:             task.Scale,
		EnableFaceBooster: task.EnableFaceBooster,
		FaceFidelity:      task.FaceFidelity,
		GPUDevice:         task.GPUDevice,
		GPUDeviceStr:      task.GPUDeviceStr,
		TileSize:          tileSize,
		Threads:           cfg.Threads,
		Format:            task.Format,
		LogCommand:        logCmd,
		OnProgress: func(pct float64, current, total int, phase string, msg string) {
			stText := mapPhaseToStageText(phase, task.EnableFaceBooster)
			if onProgress != nil {
				onProgress(pct, stText, 1, 1, msg)
			}
		},
	}

	return deepsr.RunUpscale(ctx, opt)
}
