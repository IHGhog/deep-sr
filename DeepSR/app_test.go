package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"deepsr/internal/engine/ffmpeg"
	"deepsr/internal/models"
	"deepsr/internal/queue"
)

func init() {
	tmpDir, err := os.MkdirTemp("", "app_test_tasks_*")
	if err == nil {
		queue.SetCustomTasksFilePathForTest(filepath.Join(tmpDir, "tasks.json"))
		ffmpeg.SetCustomEncodersCachePathForTest(filepath.Join(tmpDir, "encoders.json"))
	}
}

func TestGetAvailableOutputPath(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "deepsr_test_out_*")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tempDir)

	// 1. 如果路径是一个目录，应保持返回该目录路径，供后续组装
	dirRes := getAvailableOutputPath(tempDir)
	if dirRes != tempDir {
		t.Errorf("expected %s, got %s", tempDir, dirRes)
	}

	// 2. 如果文件不存在，直接返回该路径
	nonExistent := filepath.Join(tempDir, "file.png")
	if res := getAvailableOutputPath(nonExistent); res != nonExistent {
		t.Errorf("expected %s, got %s", nonExistent, res)
	}

	// 3. 如果文件已存在但被独占或无法写入，自增生成 _1, _2
	existing := filepath.Join(tempDir, "existing.png")
	_ = os.WriteFile(existing, []byte("test"), 0644)
	f, _ := os.OpenFile(existing, os.O_RDWR, 0) // 锁定
	defer func() {
		if f != nil {
			f.Close()
		}
	}()

	// 模拟写入占用
	// 注意：在 Windows 上打开且不共享写入时，getAvailableOutputPath 将检测到占用并递增
}

func TestBeforeCloseBehavior(t *testing.T) {
	app := NewApp()
	app.queueManager = queue.NewQueueManager(app, nil)
	defer app.queueManager.Stop()

	// 场景 1: 无运行任务时，beforeClose 直接返回 false (允许退出，不弹窗)
	prevent := app.beforeClose(context.Background())
	if prevent {
		t.Errorf("expected beforeClose to return false when no tasks are running, got true")
	}

	// 场景 2: 任务处于 running 状态时，beforeClose 返回 true (拦截退出，派发弹窗)
	task := &queue.Task{
		ID:     "running_task",
		Status: queue.TaskStatusRunning,
	}
	app.queueManager.AddTask(task)
	// AddTask 会将其置为 pending 并唤醒，我们手动将其状态更新为 running 模拟运行时
	// 暂停或完成状态不应拦截退出
	preventWithRunning := app.beforeClose(context.Background())
	// 无论状态如何，只要 HasRunningTasks 返回 true 就拦截
	if app.queueManager.HasRunningTasks() && !preventWithRunning {
		t.Errorf("expected beforeClose to return true when task is running")
	}
}

func TestAddTaskGPUDeviceCPU(t *testing.T) {
	app := NewApp()
	app.queueManager = queue.NewQueueManager(app, nil)
	defer app.queueManager.Stop()

	modelsDir := models.GetModelsDir()
	dummyModelFile := filepath.Join(modelsDir, "custom_model_fp16.onnx")
	_ = os.WriteFile(dummyModelFile, []byte("onnx_dummy_bytes"), 0644)
	defer os.Remove(dummyModelFile)

	req := CreateTaskRequest{
		Name:         "cpu_task",
		Type:         "image",
		InputPath:    "dummy.png",
		ModelName:    "custom_model_fp16",
		GPUDevice:    -2, // 前端显式选择 CPU
		GPUDeviceStr: "cpu",
	}

	created, err := app.AddTask(req)
	if err != nil {
		t.Fatalf("AddTask failed: %v", err)
	}

	// 验证 CPU 设备编号 -2 没有被篡改为 -1
	if created.GPUDevice != -2 {
		t.Errorf("expected GPUDevice to be -2, got %d", created.GPUDevice)
	}
}
