package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"
)

var (
	kernel32                = syscall.NewLazyDLL("kernel32.dll")
	user32                  = syscall.NewLazyDLL("user32.dll")
	procCreateMutexW        = kernel32.NewProc("CreateMutexW")
	procCloseHandle         = kernel32.NewProc("CloseHandle")
	procFindWindowW         = user32.NewProc("FindWindowW")
	procShowWindow          = user32.NewProc("ShowWindow")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
)

// AcquireSingleInstance 确保程序在系统级别单实例运行，并在 AppData/DeepSR 写入唯一性锁信息
func AcquireSingleInstance() (func(), bool) {
	userConfigDir, err := os.UserConfigDir()
	if err != nil {
		userConfigDir = "."
	}
	appDir := filepath.Join(userConfigDir, "DeepSR")
	_ = os.MkdirAll(appDir, 0755)
	lockFile := filepath.Join(appDir, "app.lock")

	mutexName, _ := syscall.UTF16PtrFromString("Global\\DeepSR_SingleInstance_AppMutex")
	handle, _, errCall := procCreateMutexW.Call(0, 1, uintptr(unsafe.Pointer(mutexName)))

	const ERROR_ALREADY_EXISTS = 183
	if errCall == syscall.Errno(ERROR_ALREADY_EXISTS) || handle == 0 {
		// 尝试唤醒激活已有运行的主窗口
		titlePtr, _ := syscall.UTF16PtrFromString("DeepSR - AI 画质超分辨率增强")
		hwnd, _, _ := procFindWindowW.Call(0, uintptr(unsafe.Pointer(titlePtr)))
		if hwnd != 0 {
			const SW_RESTORE = 9
			procShowWindow.Call(hwnd, SW_RESTORE)
			procSetForegroundWindow.Call(hwnd)
		}
		return nil, false
	}

	// 写入唯一性信息到 AppData/DeepSR/app.lock
	lockInfo := fmt.Sprintf("PID=%d\nStartTime=%s\n", os.Getpid(), time.Now().Format(time.RFC3339))
	_ = os.WriteFile(lockFile, []byte(lockInfo), 0644)

	cleanup := func() {
		_ = os.Remove(lockFile)
		if handle != 0 {
			procCloseHandle.Call(handle)
		}
	}

	return cleanup, true
}

// CleanupSingleInstance 清理应用锁文件
func CleanupSingleInstance() {
	userConfigDir, err := os.UserConfigDir()
	if err != nil {
		userConfigDir = "."
	}
	lockFile := filepath.Join(userConfigDir, "DeepSR", "app.lock")
	_ = os.Remove(lockFile)
}
