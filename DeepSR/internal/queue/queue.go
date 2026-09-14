package queue

import (
	"context"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"deepsr/internal/config"
)

type TaskType string

const (
	TaskTypeImage       TaskType = "image"
	TaskTypeBatchImage  TaskType = "batch_image"
	TaskTypeFolderImage TaskType = "folder_image"
	TaskTypeVideo       TaskType = "video"
	TaskTypeBatchVideo  TaskType = "batch_video"
)

type TaskStatus string

const (
	TaskStatusPending   TaskStatus = "pending"
	TaskStatusRunning   TaskStatus = "running"
	TaskStatusPaused    TaskStatus = "paused"
	TaskStatusCompleted TaskStatus = "completed"
	TaskStatusFailed    TaskStatus = "failed"
	TaskStatusCanceled  TaskStatus = "canceled"
)

type TaskStage string

const (
	TaskStageIdle       TaskStage = "idle"
	TaskStageExtracting TaskStage = "extracting"
	TaskStageUpscaling  TaskStage = "upscaling"
	TaskStageMerging    TaskStage = "merging"
	TaskStageCompleted  TaskStage = "completed"
)

type Task struct {
	ID                string     `json:"id"`
	Name              string     `json:"name"`
	Type              TaskType   `json:"type"`
	Status            TaskStatus `json:"status"`
	Stage             TaskStage  `json:"stage"`
	StageText         string     `json:"stageText"`
	Message           string     `json:"message,omitempty"`
	Progress          float64    `json:"progress"`
	CurrentFrame      int        `json:"currentFrame"`
	TotalFrames       int        `json:"totalFrames"`
	SpeedFPS          float64    `json:"speedFps"`
	InputPath         string     `json:"inputPath"`
	OutputPath        string     `json:"outputPath"`
	InputPaths        []string   `json:"inputPaths,omitempty"`
	ModelName         string     `json:"modelName"`
	Scale             int        `json:"scale"`
	EnableFaceBooster bool       `json:"enableFaceBooster"`
	FaceFidelity      float64    `json:"faceFidelity"`
	GPUDevice         int        `json:"gpuDevice"`
	GPUDeviceStr      string     `json:"gpuDeviceStr"`
	TileSize          int        `json:"tileSize"`
	Format            string     `json:"format"`
	Encoder           string     `json:"encoder"`
	CRF               int        `json:"crf"`
	Preset            string     `json:"preset"`
	EnableStreamPipe  bool       `json:"enableStreamPipe"`
	CreatedAt         time.Time  `json:"createdAt"`
	CompletedAt       time.Time  `json:"completedAt,omitempty"`
	Duration          float64    `json:"duration,omitempty"`
	Error             string     `json:"error,omitempty"`

	cancelFunc context.CancelFunc `json:"-"`
}

type EventListener interface {
	EmitTaskUpdated(task *Task)
	EmitQueueUpdated(tasks []*Task)
	EmitLog(level string, message string)
}

type PipelineExecutor interface {
	ExecuteTask(ctx context.Context, task *Task, cfg config.AppConfig, onProgress func(pct float64, stage TaskStage, stageText string, currentFrame, totalFrames int, speedFPS float64, msg string)) error
}

type QueueManager struct {
	tasks    []*Task
	mu       sync.RWMutex
	listener EventListener
	executor PipelineExecutor
	stopChan chan struct{}
	wakeChan chan struct{}
	running  bool
}

var customTasksFilePath string

// SetCustomTasksFilePathForTest 设置测试专属的任务持久化文件路径，防止测试污染生产配置
func SetCustomTasksFilePathForTest(path string) func() {
	customTasksFilePath = path
	return func() {
		customTasksFilePath = ""
	}
}

func getTasksFilePath() string {
	if customTasksFilePath != "" {
		_ = os.MkdirAll(filepath.Dir(customTasksFilePath), 0755)
		return customTasksFilePath
	}
	userConfigDir, err := os.UserConfigDir()
	if err != nil {
		userConfigDir = "."
	}
	appDataDir := filepath.Join(userConfigDir, "DeepSR")
	_ = os.MkdirAll(appDataDir, 0755)
	return filepath.Join(appDataDir, "tasks.json")
}

func (qm *QueueManager) saveTasksToDiskLocked() {
	filePath := getTasksFilePath()
	data, err := json.MarshalIndent(qm.tasks, "", "  ")
	if err == nil {
		_ = os.WriteFile(filePath, data, 0644)
	}
}

func (qm *QueueManager) loadTasksFromDisk() {
	filePath := getTasksFilePath()
	data, err := os.ReadFile(filePath)
	if err != nil {
		return
	}
	var loaded []*Task
	if err := json.Unmarshal(data, &loaded); err != nil {
		return
	}

	cfg := config.GetConfig()
	for _, t := range loaded {
		// 若软件上次关闭前任务正在运行，重置为已暂停，防止启动自动乱跑
		if t.Status == TaskStatusRunning {
			t.Status = TaskStatusPaused
			t.StageText = "已暂停"
		}

		// 对于视频任务，自动探测其实际已完成帧数并恢复进度条与帧数指示
		if t.Type == TaskTypeVideo || t.Type == TaskTypeBatchVideo {
			tempDir := GetVideoTaskTempDir(cfg.TempDir, t.InputPath, t.ModelName, t.Scale, t.EnableFaceBooster)
			enhancedDir := filepath.Join(tempDir, "enhanced")
			if t.TotalFrames > 0 {
				format := t.Format
				if format == "" {
					format = "jpg"
				}
				doneCount := CountMatchingFiles(enhancedDir, "frame", "."+format)
				if doneCount > 0 {
					t.CurrentFrame = doneCount
					t.Progress = 15.0 + (float64(doneCount)/float64(t.TotalFrames))*70.0
					if doneCount >= t.TotalFrames {
						t.Progress = 85.0
					}
					if t.Status == TaskStatusPaused {
						t.Message = fmt.Sprintf("断点续传: 已超分 %d/%d 帧", doneCount, t.TotalFrames)
					}
				}
			}
		}
	}
	qm.tasks = loaded
}

func NewQueueManager(listener EventListener, executor PipelineExecutor) *QueueManager {
	qm := &QueueManager{
		tasks:    make([]*Task, 0),
		listener: listener,
		executor: executor,
		stopChan: make(chan struct{}),
		wakeChan: make(chan struct{}, 10),
		running:  true,
	}
	qm.loadTasksFromDisk()
	go qm.workerLoop()
	return qm
}

func (qm *QueueManager) AddTask(task *Task) {
	qm.mu.Lock()
	task.Status = TaskStatusPending
	task.Stage = TaskStageIdle
	task.StageText = "等待处理中"
	task.CreatedAt = time.Now()
	qm.tasks = append(qm.tasks, task)
	qm.saveTasksToDiskLocked()
	tasksCopy := qm.cloneTasks()
	qm.mu.Unlock()

	if qm.listener != nil {
		qm.listener.EmitTaskUpdated(task)
		qm.listener.EmitQueueUpdated(tasksCopy)
	}

	select {
	case qm.wakeChan <- struct{}{}:
	default:
	}
}

func (qm *QueueManager) GetTasks() []*Task {
	qm.mu.RLock()
	defer qm.mu.RUnlock()
	return qm.cloneTasks()
}

// HasRunningTasks 检查当前队列中是否有正在执行的任务
func (qm *QueueManager) HasRunningTasks() bool {
	qm.mu.RLock()
	defer qm.mu.RUnlock()
	for _, t := range qm.tasks {
		if t.Status == TaskStatusRunning {
			return true
		}
	}
	return false
}

func (qm *QueueManager) PauseTask(id string) error {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	for _, t := range qm.tasks {
		if t.ID == id {
			if t.Status == TaskStatusRunning {
				t.Status = TaskStatusPaused
				t.StageText = "已暂停"
				if t.cancelFunc != nil {
					t.cancelFunc()
				}
				qm.saveTasksToDiskLocked()
				if qm.listener != nil {
					qm.listener.EmitTaskUpdated(t)
				}
				return nil
			}
		}
	}
	return fmt.Errorf("任务不可暂停或不存在")
}

func (qm *QueueManager) ResumeTask(id string) error {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	for _, t := range qm.tasks {
		if t.ID == id {
			// 取消的任务不可恢复重新启动，仅支持暂停或失败的任务恢复
			if t.Status == TaskStatusPaused || t.Status == TaskStatusFailed {
				t.Status = TaskStatusPending
				t.StageText = "已重置，等待调度"
				t.Error = ""
				qm.saveTasksToDiskLocked()
				if qm.listener != nil {
					qm.listener.EmitTaskUpdated(t)
				}
				select {
				case qm.wakeChan <- struct{}{}:
				default:
				}
				return nil
			}
		}
	}
	return fmt.Errorf("任务不可恢复或不存在")
}

func (qm *QueueManager) CancelTask(id string) error {
	qm.mu.Lock()
	var target *Task
	for _, t := range qm.tasks {
		if t.ID == id {
			target = t
			t.Status = TaskStatusCanceled
			t.StageText = "已取消"
			if t.cancelFunc != nil {
				t.cancelFunc()
			}
			break
		}
	}
	qm.saveTasksToDiskLocked()
	var tc Task
	if target != nil {
		tc = *target
	}
	qm.mu.Unlock()

	if target == nil {
		return fmt.Errorf("任务不存在")
	}

	// 任务取消时，彻底清理其关联的磁盘临时文件与未完成分块缓存（如果有）
	if (target.Type == TaskTypeVideo || target.Type == TaskTypeBatchVideo) && !target.EnableStreamPipe {
		cfg := config.GetConfig()
		tempDir := GetVideoTaskTempDir(cfg.TempDir, target.InputPath, target.ModelName, target.Scale, target.EnableFaceBooster)
		_ = os.RemoveAll(tempDir)
	}

	if qm.listener != nil {
		qm.listener.EmitTaskUpdated(&tc)
		qm.listener.EmitLog("info", fmt.Sprintf("任务 [%s] %s 已取消并清理缓存", target.ID, target.Name))
	}
	return nil
}

func (qm *QueueManager) DeleteTask(id string) error {
	qm.mu.Lock()
	var newTasks []*Task
	var target *Task
	for _, t := range qm.tasks {
		if t.ID == id {
			target = t
			// 若删除的是正在运行中的任务，先终止其子进程/管道
			if t.Status == TaskStatusRunning && t.cancelFunc != nil {
				t.cancelFunc()
			}
		} else {
			newTasks = append(newTasks, t)
		}
	}
	qm.tasks = newTasks
	qm.saveTasksToDiskLocked()
	tasksCopy := qm.cloneTasks()
	qm.mu.Unlock()

	if target != nil {
		hasCache := false
		if (target.Type == TaskTypeVideo || target.Type == TaskTypeBatchVideo) && !target.EnableStreamPipe {
			cfg := config.GetConfig()
			tempDir := GetVideoTaskTempDir(cfg.TempDir, target.InputPath, target.ModelName, target.Scale, target.EnableFaceBooster)
			_ = os.RemoveAll(tempDir)
			// 若删除的是运行中任务，子进程退出需数毫秒，延迟重试清理确保 Windows 下无句柄锁遗留
			go func(dir string) {
				for i := 0; i < 5; i++ {
					time.Sleep(300 * time.Millisecond)
					_ = os.RemoveAll(dir)
					if _, err := os.Stat(dir); os.IsNotExist(err) {
						break
					}
				}
			}(tempDir)
			hasCache = true
		}

		if qm.listener != nil {
			qm.listener.EmitQueueUpdated(tasksCopy)
			if hasCache {
				qm.listener.EmitLog("info", fmt.Sprintf("已删除任务 [%s] %s 并清理临时缓存", target.ID, target.Name))
			} else {
				qm.listener.EmitLog("info", fmt.Sprintf("已删除任务 [%s] %s", target.ID, target.Name))
			}
		}
		return nil
	}
	return fmt.Errorf("任务未找到")
}

// ClearCompleted 仅清理已完成的任务
func (qm *QueueManager) ClearCompleted() {
	qm.mu.Lock()
	var newTasks []*Task
	var cleared []*Task
	for _, t := range qm.tasks {
		if t.Status != TaskStatusCompleted {
			newTasks = append(newTasks, t)
		} else {
			cleared = append(cleared, t)
		}
	}
	qm.tasks = newTasks
	qm.saveTasksToDiskLocked()
	tasksCopy := qm.cloneTasks()
	qm.mu.Unlock()

	cfg := config.GetConfig()
	for _, t := range cleared {
		if (t.Type == TaskTypeVideo || t.Type == TaskTypeBatchVideo) && !t.EnableStreamPipe {
			tempDir := GetVideoTaskTempDir(cfg.TempDir, t.InputPath, t.ModelName, t.Scale, t.EnableFaceBooster)
			_ = os.RemoveAll(tempDir)
		}
	}

	if qm.listener != nil {
		qm.listener.EmitQueueUpdated(tasksCopy)
	}
}

// ClearCanceled 仅清理已取消的任务 (失败的任务保留)
func (qm *QueueManager) ClearCanceled() {
	qm.mu.Lock()
	var newTasks []*Task
	var cleared []*Task
	for _, t := range qm.tasks {
		if t.Status != TaskStatusCanceled {
			newTasks = append(newTasks, t)
		} else {
			cleared = append(cleared, t)
		}
	}
	qm.tasks = newTasks
	qm.saveTasksToDiskLocked()
	tasksCopy := qm.cloneTasks()
	qm.mu.Unlock()

	cfg := config.GetConfig()
	for _, t := range cleared {
		if (t.Type == TaskTypeVideo || t.Type == TaskTypeBatchVideo) && !t.EnableStreamPipe {
			tempDir := GetVideoTaskTempDir(cfg.TempDir, t.InputPath, t.ModelName, t.Scale, t.EnableFaceBooster)
			_ = os.RemoveAll(tempDir)
		}
	}

	if qm.listener != nil {
		qm.listener.EmitQueueUpdated(tasksCopy)
	}
}

// ClearAll 清理所有非执行中的任务（已完成、已取消、失败）
func (qm *QueueManager) ClearAll() {
	qm.mu.Lock()
	var newTasks []*Task
	var cleared []*Task
	for _, t := range qm.tasks {
		if t.Status == TaskStatusRunning {
			newTasks = append(newTasks, t)
		} else {
			cleared = append(cleared, t)
		}
	}
	qm.tasks = newTasks
	qm.saveTasksToDiskLocked()
	tasksCopy := qm.cloneTasks()
	qm.mu.Unlock()

	cfg := config.GetConfig()
	for _, t := range cleared {
		if (t.Type == TaskTypeVideo || t.Type == TaskTypeBatchVideo) && !t.EnableStreamPipe {
			tempDir := GetVideoTaskTempDir(cfg.TempDir, t.InputPath, t.ModelName, t.Scale, t.EnableFaceBooster)
			_ = os.RemoveAll(tempDir)
		}
	}

	if qm.listener != nil {
		qm.listener.EmitQueueUpdated(tasksCopy)
	}
}

func (qm *QueueManager) Stop() {
	qm.mu.Lock()
	defer qm.mu.Unlock()

	if !qm.running {
		return
	}
	qm.running = false
	for _, t := range qm.tasks {
		if t.Status == TaskStatusRunning && t.cancelFunc != nil {
			t.cancelFunc()
		}
	}
	close(qm.stopChan)
}

func (qm *QueueManager) cloneTasks() []*Task {
	res := make([]*Task, len(qm.tasks))
	for i, t := range qm.tasks {
		cp := *t
		res[i] = &cp
	}
	return res
}

func (qm *QueueManager) workerLoop() {
	for {
		select {
		case <-qm.stopChan:
			return
		case <-qm.wakeChan:
		case <-time.After(1 * time.Second):
		}

		qm.mu.Lock()
		if !qm.running {
			qm.mu.Unlock()
			return
		}

		var nextTask *Task
		for _, t := range qm.tasks {
			if t.Status == TaskStatusPending {
				nextTask = t
				break
			}
		}
		qm.mu.Unlock()

		if nextTask == nil {
			continue
		}

		qm.processTask(nextTask)
	}
}

func (qm *QueueManager) processTask(task *Task) {
	ctx, cancel := context.WithCancel(context.Background())

	qm.mu.Lock()
	task.Status = TaskStatusRunning
	task.Stage = TaskStageExtracting
	task.StageText = "准备执行..."
	task.Progress = 0
	task.CurrentFrame = 0
	task.SpeedFPS = 0
	task.cancelFunc = cancel
	taskCopy := *task
	qm.mu.Unlock()

	if qm.listener != nil {
		qm.listener.EmitTaskUpdated(&taskCopy)
	}

	cfg := config.GetConfig()
	startTime := time.Now()

	var lastSaveTime time.Time
	var err error
	if qm.executor != nil {
		err = qm.executor.ExecuteTask(ctx, task, cfg, func(pct float64, stage TaskStage, stageText string, currentFrame, totalFrames int, speedFPS float64, msg string) {
			qm.mu.Lock()
			found := false
			for _, t := range qm.tasks {
				if t.ID == task.ID {
					found = true
					break
				}
			}
			if !found {
				qm.mu.Unlock()
				return
			}

			task.Progress = pct
			task.Stage = stage
			task.StageText = stageText
			task.Message = msg
			task.CurrentFrame = currentFrame
			if totalFrames > 0 {
				task.TotalFrames = totalFrames
			}
			task.SpeedFPS = speedFPS

			if time.Since(lastSaveTime) > 3*time.Second || currentFrame == totalFrames {
				qm.saveTasksToDiskLocked()
				lastSaveTime = time.Now()
			}

			tc := *task
			qm.mu.Unlock()

			if qm.listener != nil {
				qm.listener.EmitTaskUpdated(&tc)
			}
		})
	}

	qm.mu.Lock()
	found := false
	for _, t := range qm.tasks {
		if t.ID == task.ID {
			found = true
			break
		}
	}
	if !found {
		// 任务在运行过程中已经被删除，确保临时目录被彻底清理，不再持久化或广播更新
		if (task.Type == TaskTypeVideo || task.Type == TaskTypeBatchVideo) && !task.EnableStreamPipe {
			cfg := config.GetConfig()
			tempDir := GetVideoTaskTempDir(cfg.TempDir, task.InputPath, task.ModelName, task.Scale, task.EnableFaceBooster)
			_ = os.RemoveAll(tempDir)
		}
		qm.mu.Unlock()
		return
	}

	task.CompletedAt = time.Now()
	task.Duration = time.Since(startTime).Seconds()
	task.cancelFunc = nil

	if err != nil {
		if ctx.Err() != nil || task.Status == TaskStatusCanceled || task.Status == TaskStatusPaused {
			// 被主动暂停或取消，保留当前状态
		} else {
			task.Status = TaskStatusFailed
			task.StageText = "处理失败"
			errMsg := err.Error()
			lowerErr := strings.ToLower(errMsg)
			if strings.Contains(errMsg, "OOM") || strings.Contains(errMsg, "显存") || strings.Contains(lowerErr, "out of memory") || strings.Contains(lowerErr, "8007000e") {
				task.Error = "GPU 显存不足 (Out of Memory)。建议在【引擎设置】中指定分块尺寸 (如设为 128 或 64)、改用 FP16 精度模型或在计算设备中选择 CPU 模式"
				task.Message = task.Error
			} else if strings.Contains(lowerErr, "887a0006") || strings.Contains(lowerErr, "887a0005") || strings.Contains(lowerErr, "device_hung") || strings.Contains(errMsg, "设备挂起") {
				task.Error = "GPU 计算超时或驱动被系统重置 (DXGI_ERROR_DEVICE_HUNG)。当前模型计算负荷过高，建议减小分块尺寸 (如设为 128 或 64)、改用 FP16 精度或在计算设备中选择 CPU 模式"
				task.Message = task.Error
			} else {
				task.Error = errMsg
				task.Message = errMsg
			}
			if qm.listener != nil {
				qm.listener.EmitLog("error", fmt.Sprintf("任务 [%s] 失败: %s", task.ID, task.Error))
			}
		}
	} else {
		task.Status = TaskStatusCompleted
		task.Stage = TaskStageCompleted
		if task.TotalFrames > 0 {
			task.CurrentFrame = task.TotalFrames
		}
		task.StageText = "处理完成"
		if task.Type == TaskTypeBatchImage || task.Type == TaskTypeFolderImage {
			task.Message = fmt.Sprintf("批量处理完成，共 %d 张图片", task.TotalFrames)
		} else if task.Type == TaskTypeVideo || task.Type == TaskTypeBatchVideo {
			task.Message = fmt.Sprintf("视频处理完成，共 %d 帧", task.TotalFrames)
		} else {
			task.Message = ""
		}
		task.Progress = 100.0
		durationStr := formatDurationStr(task.Duration)
		if qm.listener != nil {
			qm.listener.EmitLog("success", fmt.Sprintf("任务 [%s] %s 处理完成，总耗时 %s", task.ID, task.Name, durationStr))
		}
	}

	qm.saveTasksToDiskLocked()
	finalCopy := *task
	tasksCopy := qm.cloneTasks()
	qm.mu.Unlock()

	if qm.listener != nil {
		qm.listener.EmitTaskUpdated(&finalCopy)
		qm.listener.EmitQueueUpdated(tasksCopy)
	}
}

func formatDurationStr(seconds float64) string {
	if seconds < 60 {
		return fmt.Sprintf("%.1f 秒", seconds)
	}
	mins := int(seconds) / 60
	remSecs := int(seconds) % 60
	return fmt.Sprintf("%d 分 %d 秒", mins, remSecs)
}

// GetVideoTaskTempDir 基于任务特征生成专属临时工作目录
func GetVideoTaskTempDir(baseTempDir, inputPath, modelName string, scale int, faceBooster bool) string {
	raw := fmt.Sprintf("%s_%s_%d_%v", inputPath, modelName, scale, faceBooster)
	h := md5.Sum([]byte(raw))
	hashStr := fmt.Sprintf("%x", h)[:12]
	return filepath.Join(baseTempDir, fmt.Sprintf("deepsr_vid_%s", hashStr))
}

// CountMatchingFiles 统计指定前缀和后缀的文件数
func CountMatchingFiles(dir, prefix, ext string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	count := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasPrefix(e.Name(), prefix) && strings.HasSuffix(strings.ToLower(e.Name()), strings.ToLower(ext)) {
			count++
		}
	}
	return count
}
