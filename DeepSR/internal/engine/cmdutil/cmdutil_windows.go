//go:build windows

package cmdutil

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

var (
	activePidsMu sync.Mutex
	activePids   = make(map[int]struct{})
	globalJob    windows.Handle
	initJobOnce  sync.Once
)

type JOBOBJECT_BASIC_LIMIT_INFORMATION struct {
	PerProcessUserTimeLimit int64
	PerJobUserTimeLimit     int64
	LimitFlags              uint32
	MinimumWorkingSetSize   uintptr
	MaximumWorkingSetSize   uintptr
	ActiveProcessLimit      uint32
	Affinity                uintptr
	PriorityClass           uint32
	SchedulingClass         uint32
}

type IO_COUNTERS struct {
	ReadOperationCount  uint64
	WriteOperationCount uint64
	OtherOperationCount uint64
	ReadTransferCount   uint64
	WriteTransferCount  uint64
	OtherTransferCount  uint64
}

type JOBOBJECT_EXTENDED_LIMIT_INFORMATION struct {
	BasicLimitInformation JOBOBJECT_BASIC_LIMIT_INFORMATION
	IoInfo                IO_COUNTERS
	ProcessMemoryLimit    uintptr
	JobMemoryLimit        uintptr
	PeakProcessMemoryLimit uintptr
	PeakJobMemoryLimit    uintptr
}

const (
	JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE = 0x00002000
	JobObjectExtendedLimitInformation  = 9
)

// InitProcessJob 在应用启动时初始化 Windows Job Object
// 确保父进程由于任何原因退出或终止时，操作系统内核自动清理全部子进程树
func InitProcessJob() {
	initJobOnce.Do(func() {
		job, err := windows.CreateJobObject(nil, nil)
		if err != nil {
			return
		}
		var info JOBOBJECT_EXTENDED_LIMIT_INFORMATION
		info.BasicLimitInformation.LimitFlags = JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
		_, err = windows.SetInformationJobObject(
			job,
			JobObjectExtendedLimitInformation,
			uintptr(unsafe.Pointer(&info)),
			uint32(unsafe.Sizeof(info)),
		)
		if err != nil {
			windows.CloseHandle(job)
			return
		}
		err = windows.AssignProcessToJobObject(job, windows.CurrentProcess())
		if err != nil {
			windows.CloseHandle(job)
			return
		}
		globalJob = job
	})
}

// RegisterPid 登记活动的子进程 PID
func RegisterPid(pid int) {
	if pid <= 0 {
		return
	}
	activePidsMu.Lock()
	activePids[pid] = struct{}{}
	activePidsMu.Unlock()
}

// UnregisterPid 注销已完成的子进程 PID
func UnregisterPid(pid int) {
	activePidsMu.Lock()
	delete(activePids, pid)
	activePidsMu.Unlock()
}

// HasActivePids 检查是否存在已登记的活跃子进程
func HasActivePids() bool {
	activePidsMu.Lock()
	defer activePidsMu.Unlock()
	return len(activePids) > 0
}

// Command 创建带有 CREATE_NO_WINDOW 标记的静默子进程命令
func Command(name string, arg ...string) *exec.Cmd {
	cmd := exec.Command(name, arg...)
	HideWindow(cmd)
	return cmd
}

// CommandContext 创建带有 Context 与 CREATE_NO_WINDOW 标记的静默子进程命令，支持取消时递归强杀子进程树
func CommandContext(ctx context.Context, name string, arg ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, arg...)
	HideWindow(cmd)
	cmd.Cancel = func() error {
		if cmd.Process != nil && cmd.Process.Pid > 0 {
			UnregisterPid(cmd.Process.Pid)
			return KillProcessTree(cmd.Process.Pid)
		}
		return nil
	}
	return cmd
}

// KillProcessTree 在 Windows 下通过 taskkill /F /T /PID 递归强杀进程及其所有子进程
func KillProcessTree(pid int) error {
	killCmd := exec.Command("taskkill", "/F", "/T", "/PID", strconv.Itoa(pid))
	HideWindow(killCmd)
	return killCmd.Run()
}

// KillAllEngineProcesses 强杀所有正在运行的 deepsr-cli、ffmpeg 及 ffprobe 进程，确保 0 孤儿进程
func KillAllEngineProcesses() {
	// 1. 递归强杀所有已登记的活动子进程 PID（单条命令批量强杀）
	activePidsMu.Lock()
	pids := make([]int, 0, len(activePids))
	for pid := range activePids {
		pids = append(pids, pid)
	}
	activePids = make(map[int]struct{})
	activePidsMu.Unlock()

	if len(pids) > 0 {
		args := []string{"/F", "/T"}
		for _, pid := range pids {
			args = append(args, "/PID", strconv.Itoa(pid))
		}
		killCmd := exec.Command("taskkill", args...)
		HideWindow(killCmd)
		_ = killCmd.Run()
	}

	// 2. 按进程名二次全量兜底（单条命令合并批量强杀，耗时由数百毫秒降至几十毫秒）
	killCmd := exec.Command("taskkill", "/F", "/T", "/IM", "deepsr-cli.exe", "/IM", "ffmpeg.exe", "/IM", "ffprobe.exe")
	HideWindow(killCmd)
	_ = killCmd.Run()
}

// HideWindow 在 Windows 下彻底隐藏 CMD 命令行弹窗，杜绝闪烁
func HideWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags = 0x08000000 // CREATE_NO_WINDOW
}

// RefreshEnvironmentPath 从 Windows 注册表读取系统与用户 PATH 并更新当前进程
func RefreshEnvironmentPath() []string {
	var paths []string

	if k, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE); err == nil {
		if val, _, err := k.GetStringValue("Path"); err == nil && val != "" {
			for _, p := range strings.Split(val, ";") {
				p = strings.TrimSpace(p)
				if p != "" {
					paths = append(paths, p)
				}
			}
		}
		k.Close()
	}

	if k, err := registry.OpenKey(registry.LOCAL_MACHINE, `SYSTEM\CurrentControlSet\Control\Session Manager\Environment`, registry.QUERY_VALUE); err == nil {
		if val, _, err := k.GetStringValue("Path"); err == nil && val != "" {
			for _, p := range strings.Split(val, ";") {
				p = strings.TrimSpace(p)
				if p != "" {
					paths = append(paths, p)
				}
			}
		}
		k.Close()
	}

	curEnv := os.Getenv("PATH")
	for _, p := range strings.Split(curEnv, string(os.PathListSeparator)) {
		p = strings.TrimSpace(p)
		if p != "" {
			paths = append(paths, p)
		}
	}

	seen := make(map[string]bool)
	var unique []string
	for _, p := range paths {
		norm := filepath.Clean(p)
		if !seen[norm] {
			seen[norm] = true
			unique = append(unique, norm)
		}
	}

	if len(unique) > 0 {
		newPathStr := strings.Join(unique, string(os.PathListSeparator))
		_ = os.Setenv("PATH", newPathStr)
	}

	return unique
}
