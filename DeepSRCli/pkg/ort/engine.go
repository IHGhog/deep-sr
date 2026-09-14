package ort

import (
	"fmt"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

type Env uintptr
type Session uintptr
type SessionOptions uintptr
type MemoryInfo uintptr
type Value uintptr
type Status uintptr

type Engine struct {
	dll     *syscall.LazyDLL
	api     []uintptr
	env     Env
	memInfo MemoryInfo
}

func (e *Engine) checkStatus(status Status) error {
	if status == 0 {
		return nil
	}
	// Idx_GetErrorMessage
	msgPtr, _, _ := syscall.SyscallN(e.api[Idx_GetErrorMessage], uintptr(status))
	var msg string
	if msgPtr != 0 {
		p := *(**[1024]byte)(unsafe.Pointer(&msgPtr))
		if p != nil {
			var b []byte
			for _, c := range p {
				if c == 0 {
					break
				}
				b = append(b, c)
			}
			msg = string(b)
		}
	}
	// Idx_ReleaseStatus
	syscall.SyscallN(e.api[Idx_ReleaseStatus], uintptr(status))
	return fmt.Errorf("ONNX error: %s", msg)
}

func NewEngine(dllPath string) (*Engine, error) {
	absDll, err := filepath.Abs(dllPath)
	if err != nil {
		return nil, err
	}
	dll := syscall.NewLazyDLL(absDll)
	procGetApiBase := dll.NewProc("OrtGetApiBase")
	r1, _, err := procGetApiBase.Call()
	if r1 == 0 {
		return nil, fmt.Errorf("failed to call OrtGetApiBase from %s: %w", absDll, err)
	}

	type rawApiBase struct {
		GetApi           uintptr
		GetVersionString uintptr
	}
	base := *(**rawApiBase)(unsafe.Pointer(&r1))
	// Request API version 17 or higher
	apiPtr, _, _ := syscall.SyscallN(base.GetApi, 17)
	if apiPtr == 0 {
		return nil, fmt.Errorf("failed to get OrtApi v17 from base")
	}

	apiBasePtr := *(**[OrtApiFunctionCount]uintptr)(unsafe.Pointer(&apiPtr))
	funcs := make([]uintptr, OrtApiFunctionCount)
	copy(funcs, apiBasePtr[:])

	eng := &Engine{
		dll: dll,
		api: funcs,
	}

	// Idx_CreateEnv: (log_level=3 (warning), log_id="DeepSR_Go", out=&env)
	logId, _ := syscall.BytePtrFromString("DeepSR_Go")
	var env Env
	status, _, _ := syscall.SyscallN(funcs[Idx_CreateEnv], 3, uintptr(unsafe.Pointer(logId)), uintptr(unsafe.Pointer(&env)))
	if err := eng.checkStatus(Status(status)); err != nil {
		return nil, fmt.Errorf("CreateEnv failed: %w", err)
	}
	eng.env = env

	// Idx_CreateCpuMemoryInfo: (type=0 (OrtArenaAllocator), mem_type=0 (OrtMemTypeDefault), out=&memInfo)
	var memInfo MemoryInfo
	status, _, _ = syscall.SyscallN(funcs[Idx_CreateCpuMemoryInfo], 0, 0, uintptr(unsafe.Pointer(&memInfo)))
	if err := eng.checkStatus(Status(status)); err != nil {
		return nil, fmt.Errorf("CreateCpuMemoryInfo failed: %w", err)
	}
	eng.memInfo = memInfo

	return eng, nil
}

// CreateSession creates an ONNX session with the specified device configuration
func (e *Engine) CreateSession(modelPath string, useDirectML bool, deviceId int) (Session, error) {
	// Idx_CreateSessionOptions
	var opts SessionOptions
	status, _, _ := syscall.SyscallN(e.api[Idx_CreateSessionOptions], uintptr(unsafe.Pointer(&opts)))
	if err := e.checkStatus(Status(status)); err != nil {
		return 0, err
	}
	defer syscall.SyscallN(e.api[Idx_ReleaseSessionOptions], uintptr(opts))

	// Idx_SetSessionGraphOptimizationLevel: (opts, 99 (ORT_ENABLE_ALL))
	syscall.SyscallN(e.api[Idx_SetSessionGraphOptimizationLevel], uintptr(opts), 99)

	// Idx_SetIntraOpNumThreads: (opts, 4)
	syscall.SyscallN(e.api[Idx_SetIntraOpNumThreads], uintptr(opts), 4)

	// Idx_SetSessionExecutionMode: (opts, 0 (ORT_SEQUENTIAL))
	syscall.SyscallN(e.api[Idx_SetSessionExecutionMode], uintptr(opts), 0)

	// Append DirectML EP if requested
	if useDirectML {
		// Disable MemPattern to prevent DML heap collision
		syscall.SyscallN(e.api[Idx_DisableMemPattern], uintptr(opts))

		procDML := e.dll.NewProc("OrtSessionOptionsAppendExecutionProvider_DML")
		if procDML.Find() == nil {
			r1, _, _ := procDML.Call(uintptr(opts), uintptr(deviceId))
			if r1 != 0 {
				return 0, e.checkStatus(Status(r1))
			}
		}
	}

	absModel, err := filepath.Abs(modelPath)
	if err != nil {
		return 0, err
	}
	wPath, err := syscall.UTF16PtrFromString(absModel)
	if err != nil {
		return 0, err
	}

	var session Session
	// Idx_CreateSession: (env, wPath, opts, &session)
	status, _, _ = syscall.SyscallN(e.api[Idx_CreateSession], uintptr(e.env), uintptr(unsafe.Pointer(wPath)), uintptr(opts), uintptr(unsafe.Pointer(&session)))
	if err := e.checkStatus(Status(status)); err != nil {
		return 0, err
	}
	return session, nil
}

// CreateSessionWithDevice automatically falls back to GPU or CPU based on device preference
func (e *Engine) CreateSessionWithDevice(modelPath string, device string) (Session, string, error) {
	dev := strings.ToLower(strings.TrimSpace(device))
	if dev == "" || dev == "auto" || dev == "gpu" || dev == "cuda" || dev == "directml" {
		// Try discrete GPU 1 first (usually NVIDIA/AMD)
		sess, err := e.CreateSession(modelPath, true, 1)
		if err == nil {
			return sess, "GPU (DirectML Device 1: Discrete GPU)", nil
		}
		// Try integrated GPU 0 (Intel)
		sess, err = e.CreateSession(modelPath, true, 0)
		if err == nil {
			return sess, "GPU (DirectML Device 0: Integrated GPU)", nil
		}
		// If auto, fallback to CPU
		if dev == "auto" {
			sess, err = e.CreateSession(modelPath, false, 0)
			if err == nil {
				return sess, "CPU (Fallback)", nil
			}
		}
		return 0, "", fmt.Errorf("failed to create GPU session: %w", err)
	}

	if dev == "gpu:0" || dev == "0" {
		sess, err := e.CreateSession(modelPath, true, 0)
		if err != nil {
			return 0, "", err
		}
		return sess, "GPU (DirectML Device 0)", nil
	}

	if dev == "gpu:1" || dev == "1" {
		sess, err := e.CreateSession(modelPath, true, 1)
		if err != nil {
			return 0, "", err
		}
		return sess, "GPU (DirectML Device 1)", nil
	}

	// CPU
	sess, err := e.CreateSession(modelPath, false, 0)
	if err != nil {
		return 0, "", err
	}
	return sess, "CPU", nil
}

func (e *Engine) CreateTensorFloat32(shape []int64, data []float32) (Value, error) {
	var shapePtr uintptr
	if len(shape) > 0 {
		shapePtr = uintptr(unsafe.Pointer(&shape[0]))
	}
	var dataPtr uintptr
	if len(data) > 0 {
		dataPtr = uintptr(unsafe.Pointer(&data[0]))
	}
	var val Value
	// Idx_CreateTensorWithDataAsOrtValue:
	// (memInfo, dataPtr, dataBytes, shapePtr, shapeLen, type=1 (FLOAT), &val)
	status, _, _ := syscall.SyscallN(
		e.api[Idx_CreateTensorWithDataAsOrtValue],
		uintptr(e.memInfo),
		dataPtr,
		uintptr(len(data)*4), // bytes count
		shapePtr,
		uintptr(len(shape)),
		1, // ONNX_TENSOR_ELEMENT_DATA_TYPE_FLOAT
		uintptr(unsafe.Pointer(&val)),
	)
	if err := e.checkStatus(Status(status)); err != nil {
		return 0, err
	}
	return val, nil
}

func (e *Engine) ReleaseValue(val Value) {
	if val != 0 {
		syscall.SyscallN(e.api[Idx_ReleaseValue], uintptr(val))
	}
}

func (e *Engine) ReleaseSession(session Session) {
	if session != 0 {
		syscall.SyscallN(e.api[Idx_ReleaseSession], uintptr(session))
	}
}

func (e *Engine) Run(session Session, inNames []string, inVals []Value, outNames []string) ([]Value, error) {
	inNamePtrs := make([]uintptr, len(inNames))
	for i, name := range inNames {
		b, _ := syscall.BytePtrFromString(name)
		inNamePtrs[i] = uintptr(unsafe.Pointer(b))
	}

	outNamePtrs := make([]uintptr, len(outNames))
	for i, name := range outNames {
		b, _ := syscall.BytePtrFromString(name)
		outNamePtrs[i] = uintptr(unsafe.Pointer(b))
	}

	outVals := make([]Value, len(outNames))

	// Idx_Run: (session, runOpts=0, inNames, inVals, inCount, outNames, outCount, outVals)
	status, _, _ := syscall.SyscallN(
		e.api[Idx_Run],
		uintptr(session),
		0,
		uintptr(unsafe.Pointer(&inNamePtrs[0])),
		uintptr(unsafe.Pointer(&inVals[0])),
		uintptr(len(inVals)),
		uintptr(unsafe.Pointer(&outNamePtrs[0])),
		uintptr(len(outNames)),
		uintptr(unsafe.Pointer(&outVals[0])),
	)
	if err := e.checkStatus(Status(status)); err != nil {
		return nil, err
	}
	return outVals, nil
}

func (e *Engine) GetTensorDataFloat32(val Value, length int) ([]float32, error) {
	// Idx_GetTensorMutableData: (val, &dataPtr)
	var dataPtr unsafe.Pointer
	status, _, _ := syscall.SyscallN(e.api[Idx_GetTensorMutableData], uintptr(val), uintptr(unsafe.Pointer(&dataPtr)))
	if err := e.checkStatus(Status(status)); err != nil {
		return nil, err
	}

	res := make([]float32, length)
	if dataPtr != nil && length > 0 {
		src := unsafe.Slice((*float32)(dataPtr), length)
		copy(res, src)
	}
	return res, nil
}

func (e *Engine) Close() {
	if e.memInfo != 0 {
		syscall.SyscallN(e.api[Idx_ReleaseMemoryInfo], uintptr(e.memInfo))
		e.memInfo = 0
	}
	if e.env != 0 {
		syscall.SyscallN(e.api[Idx_ReleaseEnv], uintptr(e.env))
		e.env = 0
	}
}
