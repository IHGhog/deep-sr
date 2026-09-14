package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"deepsr/internal/config"
	"deepsr/internal/engine/cmdutil"
	"deepsr/internal/engine/deepsr"
	"deepsr/internal/engine/ffmpeg"
	"deepsr/internal/models"
	"deepsr/internal/pipeline"
	"deepsr/internal/queue"
	"deepsr/internal/system"

	"github.com/google/uuid"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct
type App struct {
	ctx            context.Context
	queueManager   *queue.QueueManager
	modelManager   *models.ModelManager
	forceExit      bool
	detectMu       sync.Mutex
	lastDetectTime time.Time
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{
		modelManager: models.GetGlobalModelManager(),
	}
}

// startup is called when the app starts.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	if a.modelManager != nil {
		a.modelManager.SetContext(ctx)
	}

	// 初始化配置
	_, _ = config.InitConfig("")

	// 初始化任务队列
	a.queueManager = queue.NewQueueManager(a, a)

	// 注册文件/文件夹拖拽监听
	runtime.OnFileDrop(a.ctx, func(x, y int, paths []string) {
		if len(paths) > 0 {
			runtime.EventsEmit(a.ctx, "file_dropped", paths)
		}
	})

	// 启动系统 CPU / RAM / VRAM 资源监测后台轮询
	go func() {
		ticker := time.NewTicker(1500 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if a.ctx != nil {
					usage := system.GetSystemUsage()
					runtime.EventsEmit(a.ctx, "system_usage", usage)
				}
			case <-a.ctx.Done():
				return
			}
		}
	}()

	// 启动时初始化系统硬件环境与编码器缓存
	go func() {
		cfg := config.GetConfig()
		sysInfo := system.GetSystemInfo(cfg.TempDir)
		a.EmitLog("info", fmt.Sprintf("系统初始化就绪: %s (%d 核), 物理 GPU 设备数: %d", sysInfo.CPUName, sysInfo.CPUCores, len(sysInfo.GPUs)))

		// 快速尝试从本地 encoders.json 缓存加载已探测编码器
		encoders := ffmpeg.DetectEncoders(cfg.FFmpegPath, false)
		var supportedList []string
		for _, enc := range encoders {
			if enc.Supported {
				supportedList = append(supportedList, enc.ID)
			}
		}
		a.EmitLog("info", fmt.Sprintf("FFmpeg 环境就绪 (可用编码器: %s)", strings.Join(supportedList, ", ")))

		if a.ctx != nil {
			runtime.EventsEmit(a.ctx, "encoders_detected", encoders)
		}
	}()
}

// domReady is called after front-end resources have been loaded
func (a *App) domReady(ctx context.Context) {
	runtime.OnFileDrop(ctx, func(x, y int, paths []string) {
		if len(paths) > 0 {
			runtime.EventsEmit(ctx, "app_file_dropped", paths)
		}
	})
}

func (a *App) CheckPath(targetPath string) (map[string]interface{}, error) {
	fi, err := os.Stat(targetPath)
	if err != nil {
		return map[string]interface{}{"exists": false, "isDir": false}, nil
	}
	return map[string]interface{}{"exists": true, "isDir": fi.IsDir(), "size": fi.Size()}, nil
}

// beforeClose is called when the application is about to quit
func (a *App) beforeClose(ctx context.Context) (prevent bool) {
	if a.forceExit {
		if a.ctx != nil {
			runtime.WindowHide(a.ctx)
		}
		return false
	}
	// 若没有正在运行的任务，直接退出，不弹窗提示
	if a.queueManager == nil || !a.queueManager.HasRunningTasks() {
		if a.ctx != nil {
			runtime.WindowHide(a.ctx)
		}
		if a.queueManager != nil {
			a.queueManager.Stop()
		}
		if cmdutil.HasActivePids() {
			go cmdutil.KillAllEngineProcesses()
		}
		return false
	}
	// 仍有任务正在运行，拦截原生直接关闭，向前端派发退出确认弹窗事件
	runtime.EventsEmit(ctx, "prompt_exit_confirmation", nil)
	return true
}

// ConfirmQuit 前端确认退出
func (a *App) ConfirmQuit() {
	a.forceExit = true
	if a.ctx != nil {
		runtime.WindowHide(a.ctx)
	}
	go func() {
		if a.queueManager != nil {
			a.queueManager.Stop()
		}
		cmdutil.KillAllEngineProcesses()
		CleanupSingleInstance()
		os.Exit(0)
	}()
}

// shutdown is called at application termination
func (a *App) shutdown(ctx context.Context) {
	if a.queueManager != nil {
		a.queueManager.Stop()
	}
	if cmdutil.HasActivePids() {
		cmdutil.KillAllEngineProcesses()
	}
	CleanupSingleInstance()
}

// ======================= EventListener 接口实现 =======================

func (a *App) EmitTaskUpdated(task *queue.Task) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "task_updated", task)
	}
}

func (a *App) EmitQueueUpdated(tasks []*queue.Task) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "queue_updated", tasks)
	}
}

func (a *App) EmitLog(level string, message string) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "app_log", map[string]interface{}{
			"level": level,
			"msg":   message,
			"time":  time.Now().Format("15:04:05"),
		})
	}
}

// ======================= PipelineExecutor 接口实现 =======================

func (a *App) ExecuteTask(ctx context.Context, task *queue.Task, cfg config.AppConfig, onProgress func(pct float64, stage queue.TaskStage, stageText string, currentFrame, totalFrames int, speedFPS float64, msg string)) error {
	a.EmitLog("info", fmt.Sprintf("开始执行任务 [%s]: %s", task.ID, task.Name))

	logCmd := func(cmdStr string) {
		a.EmitLog("cmd", cmdStr)
	}

	switch task.Type {
	case queue.TaskTypeImage, queue.TaskTypeBatchImage, queue.TaskTypeFolderImage:
		return pipeline.ProcessImage(ctx, task, cfg, logCmd, func(pct float64, stageText string, currentCount, totalCount int, msg string) {
			if onProgress != nil {
				onProgress(pct, queue.TaskStageUpscaling, stageText, currentCount, totalCount, 0, msg)
			}
		})
	case queue.TaskTypeVideo, queue.TaskTypeBatchVideo:
		logInfo := func(infoStr string) {
			a.EmitLog("info", infoStr)
		}
		return pipeline.ProcessVideo(ctx, task, cfg, logInfo, logCmd, onProgress)
	default:
		return fmt.Errorf("未知任务类型: %s", task.Type)
	}
}

// ======================= 前端绑定 Go API =======================

func (a *App) GetConfig() config.AppConfig {
	return config.GetConfig()
}

func (a *App) SaveConfig(cfg config.AppConfig) error {
	return config.SaveConfig(cfg)
}

func (a *App) GetSystemInfo() system.SystemInfo {
	cfg := config.GetConfig()
	return system.GetSystemInfo(cfg.TempDir)
}

func (a *App) GetSystemUsage() system.SystemUsage {
	return system.GetSystemUsage()
}

func (a *App) ProbeMedia(filePath string) (*ffmpeg.MediaInfo, error) {
	cfg := config.GetConfig()
	return ffmpeg.ProbeMedia(cfg.FFprobePath, filePath)
}

func (a *App) GetAvailableEncoders() []ffmpeg.EncoderInfo {
	cfg := config.GetConfig()
	encoders := ffmpeg.DetectEncoders(cfg.FFmpegPath, false)
	return encoders
}

// RefreshAvailableEncoders 强制重新检测本机可用硬件编码器（带并发互斥与 2 秒节流保护），并将实测结果写入 encoders.json
func (a *App) RefreshAvailableEncoders() ([]ffmpeg.EncoderInfo, error) {
	a.detectMu.Lock()
	defer a.detectMu.Unlock()

	cfg := config.GetConfig()
	// 若距离上次探测小于 2 秒且已有缓存，直接返回（防狂点节流）
	if time.Since(a.lastDetectTime) < 2*time.Second {
		if cached := ffmpeg.GetCachedEncoders(); len(cached) > 0 {
			return cached, nil
		}
	}

	a.EmitLog("info", "正在重新检测本机支持的 FFmpeg 硬件编码器...")
	encoders := ffmpeg.DetectEncoders(cfg.FFmpegPath, true)
	a.lastDetectTime = time.Now()

	var supportedList []string
	for _, enc := range encoders {
		if enc.Supported {
			supportedList = append(supportedList, enc.ID)
		}
	}
	a.EmitLog("success", fmt.Sprintf("编码器检测完成，当前可用: %s", strings.Join(supportedList, ", ")))

	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "encoders_detected", encoders)
	}

	return encoders, nil
}

type DialogFilter struct {
	DisplayName string `json:"displayName"`
	Pattern     string `json:"pattern"`
}

func (a *App) SelectFile(title string, filters []DialogFilter) (string, error) {
	var fileFilters []runtime.FileFilter
	for _, f := range filters {
		fileFilters = append(fileFilters, runtime.FileFilter{
			DisplayName: f.DisplayName,
			Pattern:     f.Pattern,
		})
	}
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   title,
		Filters: fileFilters,
	})
}

func (a *App) SelectMultipleFiles(title string, filters []DialogFilter) ([]string, error) {
	var fileFilters []runtime.FileFilter
	for _, f := range filters {
		fileFilters = append(fileFilters, runtime.FileFilter{
			DisplayName: f.DisplayName,
			Pattern:     f.Pattern,
		})
	}
	return runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   title,
		Filters: fileFilters,
	})
}

func (a *App) SelectDirectory(title string) (string, error) {
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: title,
	})
}

func (a *App) SelectSaveFile(title string, defaultName string, filter DialogFilter) (string, error) {
	return runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title:           title,
		DefaultFilename: defaultName,
		Filters: []runtime.FileFilter{
			{
				DisplayName: filter.DisplayName,
				Pattern:     filter.Pattern,
			},
		},
	})
}

func (a *App) OpenInExplorer(targetPath string) error {
	return system.OpenInExplorer(targetPath)
}

// CreateTaskRequest 创建任务入参
type CreateTaskRequest struct {
	Name              string   `json:"name"`
	Type              string   `json:"type"` // "image", "batch_image", "folder_image", "video", "batch_video"
	InputPath         string   `json:"inputPath"`
	OutputPath        string   `json:"outputPath"`
	InputPaths        []string `json:"inputPaths,omitempty"`
	ModelName         string   `json:"modelName"`
	Scale             int      `json:"scale"`
	EnableFaceBooster bool     `json:"enableFaceBooster"`
	FaceFidelity      float64  `json:"faceFidelity"`
	GPUDevice         int      `json:"gpuDevice"`
	GPUDeviceStr      string   `json:"gpuDeviceStr,omitempty"`
	TileSize          int      `json:"tileSize"`
	Format            string   `json:"format"`
	Encoder           string   `json:"encoder"`
	CRF               int      `json:"crf"`
	Preset            string   `json:"preset"`
}

func getAvailableOutputPath(targetPath string) string {
	if targetPath == "" {
		return ""
	}
	fi, err := os.Stat(targetPath)
	if os.IsNotExist(err) {
		return targetPath
	}
	if err == nil && fi.IsDir() {
		return targetPath
	}
	f, err := os.OpenFile(targetPath, os.O_WRONLY, 0666)
	if err == nil {
		f.Close()
		return targetPath
	}
	ext := filepath.Ext(targetPath)
	base := strings.TrimSuffix(targetPath, ext)
	for i := 1; i <= 100; i++ {
		candidate := fmt.Sprintf("%s_%d%s", base, i, ext)
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			return candidate
		}
	}
	return targetPath
}

// CheckModelReady 检查指定的超分及人脸模型是否已就绪
func (a *App) CheckModelReady(modelName string, enableFaceBooster bool) (bool, string) {
	if err := deepsr.CheckModelReady(modelName, "", enableFaceBooster); err != nil {
		return false, err.Error()
	}
	return true, ""
}

func (a *App) AddTask(req CreateTaskRequest) (*queue.Task, error) {
	cfg := config.GetConfig()

	modelName := req.ModelName
	if modelName == "" {
		modelName = "realesrgan_x4plus_fp32"
	}

	// 0. 前置模型就绪强校验（若未下载模型立即报错拦截，防止浪费资源）
	if err := deepsr.CheckModelReady(modelName, "", req.EnableFaceBooster); err != nil {
		return nil, err
	}

	taskID := uuid.New().String()[:8]
	taskName := req.Name
	if taskName == "" {
		if req.InputPath != "" {
			taskName = filepath.Base(req.InputPath)
		} else if len(req.InputPaths) > 0 {
			taskName = fmt.Sprintf("批量图片 (%d 张)", len(req.InputPaths))
		} else {
			taskName = "任务_" + taskID
		}
	}

	scale := req.Scale
	if scale <= 0 {
		scale = 4
	}

	gpu := req.GPUDevice
	if gpu < -2 {
		gpu = -1
	}
	format := req.Format
	if format == "" {
		format = "png"
	}
	encoder := req.Encoder
	if encoder == "" || encoder == "auto" {
		encoder = cfg.PreferredEncoder
	}
	encoder = ffmpeg.ResolveBestEncoder(cfg.FFmpegPath, encoder)
	crf := req.CRF
	if crf <= 0 {
		crf = cfg.VideoCRF
	}
	preset := req.Preset
	if preset == "" {
		preset = cfg.VideoPreset
	}

	outPath := req.OutputPath
	if outPath == "" {
		if req.InputPath != "" {
			ext := filepath.Ext(req.InputPath)
			base := strings.TrimSuffix(req.InputPath, ext)
			if req.Type == "video" || req.Type == "batch_video" {
				outExt := format
				if outExt == "" {
					outExt = "mp4"
				}
				if !strings.HasPrefix(outExt, ".") {
					outExt = "." + outExt
				}
				outPath = fmt.Sprintf("%s_deepsr_x%d%s", base, scale, outExt)
			} else if req.Type == "folder_image" {
				outPath = req.InputPath + "_deepsr"
			} else {
				outPath = fmt.Sprintf("%s_deepsr_x%d.%s", base, scale, format)
			}
		}
	} else if req.Type != "folder_image" {
		// 若指定的 OutputPath 是一个现存的目录，则在其下拼装单文件输出路径
		if fi, err := os.Stat(outPath); err == nil && fi.IsDir() {
			ext := filepath.Ext(req.InputPath)
			base := strings.TrimSuffix(filepath.Base(req.InputPath), ext)
			if req.Type == "video" || req.Type == "batch_video" {
				outExt := format
				if outExt == "" {
					outExt = "mp4"
				}
				if !strings.HasPrefix(outExt, ".") {
					outExt = "." + outExt
				}
				outPath = filepath.Join(outPath, fmt.Sprintf("%s_deepsr_x%d%s", base, scale, outExt))
			} else {
				outPath = filepath.Join(outPath, fmt.Sprintf("%s_deepsr_x%d.%s", base, scale, format))
			}
		}
	}
	if req.Type != "folder_image" {
		outPath = getAvailableOutputPath(outPath)
	}

	task := &queue.Task{
		ID:                taskID,
		Name:              taskName,
		Type:              queue.TaskType(req.Type),
		InputPath:         req.InputPath,
		OutputPath:        outPath,
		InputPaths:        req.InputPaths,
		ModelName:         modelName,
		Scale:             scale,
		EnableFaceBooster: req.EnableFaceBooster,
		FaceFidelity:      req.FaceFidelity,
		GPUDevice:         gpu,
		GPUDeviceStr:      req.GPUDeviceStr,
		TileSize:          req.TileSize,
		Format:            format,
		Encoder:           encoder,
		CRF:               crf,
		Preset:            preset,
		EnableStreamPipe:  cfg.EnableStreamPipe,
	}

	a.queueManager.AddTask(task)
	a.EmitLog("success", fmt.Sprintf("已添加任务到队列: [%s] %s", task.Type, task.Name))
	return task, nil
}

func (a *App) GetTasks() []*queue.Task {
	return a.queueManager.GetTasks()
}

func (a *App) PauseTask(id string) error {
	return a.queueManager.PauseTask(id)
}

func (a *App) ResumeTask(id string) error {
	return a.queueManager.ResumeTask(id)
}

func (a *App) CancelTask(id string) error {
	return a.queueManager.CancelTask(id)
}

func (a *App) DeleteTask(id string) error {
	return a.queueManager.DeleteTask(id)
}

// ======================= 模型管理 API 接口 =======================

// GetModelList 获取所有模型状态列表
func (a *App) GetModelList() []models.ModelItem {
	if a.modelManager == nil {
		return nil
	}
	return a.modelManager.GetModelList()
}

// DownloadModel 触发异步下载模型
func (a *App) DownloadModel(modelID string, variant string, mirror string, customURL string) error {
	if a.modelManager == nil {
		return fmt.Errorf("模型管理器未就绪")
	}
	return a.modelManager.DownloadModel(modelID, variant, mirror, customURL)
}

// DownloadAllModels 按体积由小到大排序并依次加入单任务下载队列
func (a *App) DownloadAllModels(mirror string) error {
	if a.modelManager == nil {
		return fmt.Errorf("模型管理器未就绪")
	}
	return a.modelManager.DownloadAllModels(mirror)
}

// CancelModelDownload 取消模型下载
func (a *App) CancelModelDownload(modelID string) error {
	if a.modelManager == nil {
		return nil
	}
	return a.modelManager.CancelDownload(modelID)
}

// DeleteModel 删除本地模型文件
func (a *App) DeleteModel(modelID string, variant string) error {
	if a.modelManager == nil {
		return nil
	}
	return a.modelManager.DeleteModel(modelID, variant)
}

// SwitchModelVariant 本地毫秒级切换模型的运行精度 (无需重新下载)
func (a *App) SwitchModelVariant(modelID string, variant string) error {
	if a.modelManager == nil {
		return fmt.Errorf("模型管理器未就绪")
	}
	return a.modelManager.SwitchModelVariant(modelID, variant)
}

// SwitchGlobalVariant 切换全局精度并持久化
func (a *App) SwitchGlobalVariant(variant string) error {
	if a.modelManager == nil {
		return fmt.Errorf("模型管理器未就绪")
	}
	return a.modelManager.SwitchGlobalVariant(variant)
}

// GetModelConfig 获取独立的 models.json 模型配置
func (a *App) GetModelConfig() models.ModelConfig {
	return models.GetModelConfig()
}

// SetSelectedMirror 切换并持久化下载镜像源至 models.json
func (a *App) SetSelectedMirror(mirror string) error {
	if a.modelManager == nil {
		return fmt.Errorf("模型管理器未就绪")
	}
	return a.modelManager.SetSelectedMirror(mirror)
}

func (a *App) ClearCompletedTasks() {
	a.queueManager.ClearCompleted()
}

func (a *App) ClearCanceledTasks() {
	a.queueManager.ClearCanceled()
}

func (a *App) ClearAllTasks() {
	a.queueManager.ClearAll()
}
