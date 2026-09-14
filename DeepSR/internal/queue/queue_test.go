package queue

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"deepsr/internal/config"
)

func init() {
	tmpDir, err := os.MkdirTemp("", "queue_test_tasks_*")
	if err == nil {
		SetCustomTasksFilePathForTest(filepath.Join(tmpDir, "tasks.json"))
	}
}

type mockListener struct{}

func (m *mockListener) EmitTaskUpdated(task *Task)       {}
func (m *mockListener) EmitQueueUpdated(tasks []*Task)   {}
func (m *mockListener) EmitLog(level string, msg string) {}

type mockExecutor struct{}

func (m *mockExecutor) ExecuteTask(ctx context.Context, task *Task, cfg config.AppConfig, onProgress func(pct float64, stage TaskStage, stageText string, currentFrame, totalFrames int, speedFPS float64, msg string)) error {
	return nil
}

func TestQueueManagerStopReentrancy(t *testing.T) {
	qm := NewQueueManager(&mockListener{}, &mockExecutor{})

	// 1. 验证顺序多次调用 Stop 不会 panic
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("QueueManager.Stop() panicked on repeated calls: %v", r)
		}
	}()

	qm.Stop()
	qm.Stop()
	qm.Stop()

	if qm.running {
		t.Errorf("expected qm.running to be false after Stop()")
	}
}

func TestQueueManagerConcurrentStop(t *testing.T) {
	qm := NewQueueManager(&mockListener{}, &mockExecutor{})

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("QueueManager.Stop() panicked on concurrent calls: %v", r)
		}
	}()

	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			qm.Stop()
		}()
	}
	wg.Wait()

	if qm.running {
		t.Errorf("expected qm.running to be false after concurrent Stop()")
	}
}

func TestQueueManagerHasRunningTasks(t *testing.T) {
	qm := NewQueueManager(&mockListener{}, &mockExecutor{})
	defer qm.Stop()

	if qm.HasRunningTasks() {
		t.Errorf("expected no running tasks initially")
	}

	qm.mu.Lock()
	task := &Task{
		ID:     "test_task_1",
		Status: TaskStatusRunning,
	}
	qm.tasks = append(qm.tasks, task)
	qm.mu.Unlock()

	if !qm.HasRunningTasks() {
		t.Errorf("expected HasRunningTasks to return true when task is running")
	}

	qm.mu.Lock()
	task.Status = TaskStatusCompleted
	qm.mu.Unlock()

	if qm.HasRunningTasks() {
		t.Errorf("expected HasRunningTasks to return false when task is completed")
	}
}
