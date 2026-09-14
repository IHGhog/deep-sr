package hardware

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

type LUID struct {
	LowPart  uint32
	HighPart int32
}

type DXGI_ADAPTER_DESC struct {
	Description           [128]uint16
	VendorId              uint32
	DeviceId              uint32
	SubSysId              uint32
	Revision              uint32
	DedicatedVideoMemory   uintptr
	DedicatedSystemMemory  uintptr
	SharedSystemMemory     uintptr
	AdapterLuid           LUID
}

var IID_IDXGIFactory = syscall.GUID{
	Data1: 0x7b7166ec,
	Data2: 0x21c7,
	Data3: 0x44ae,
	Data4: [8]byte{0xb2, 0x1a, 0xc9, 0xae, 0x32, 0x1a, 0xe3, 0x69},
}

type GPUInfo struct {
	Index        int    `json:"index"`
	Name         string `json:"name"`
	VendorID     uint32 `json:"vendor_id"`
	VRAMBytes    uint64 `json:"vram_bytes"`
	VRAMMB       int    `json:"vram_mb"`
	IsDiscrete   bool   `json:"is_discrete"`
	IsIntegrated bool   `json:"is_integrated"`
}

func GetCPUInfo() string {
	advapi32 := syscall.NewLazyDLL("advapi32.dll")
	procRegOpenKeyExW := advapi32.NewProc("RegOpenKeyExW")
	procRegQueryValueExW := advapi32.NewProc("RegQueryValueExW")
	procRegCloseKey := advapi32.NewProc("RegCloseKey")

	const HKEY_LOCAL_MACHINE = 0x80000002
	const KEY_READ = 0x20019

	subKey, _ := syscall.UTF16PtrFromString(`HARDWARE\DESCRIPTION\System\CentralProcessor\0`)
	var hKey uintptr
	r1, _, _ := procRegOpenKeyExW.Call(
		HKEY_LOCAL_MACHINE,
		uintptr(unsafe.Pointer(subKey)),
		0,
		KEY_READ,
		uintptr(unsafe.Pointer(&hKey)),
	)
	if r1 != 0 || hKey == 0 {
		return fmt.Sprintf("CPU (%d Logical Cores)", runtime.NumCPU())
	}
	defer procRegCloseKey.Call(hKey)

	valName, _ := syscall.UTF16PtrFromString("ProcessorNameString")
	var valType uint32
	var bufSize uint32 = 512
	buf := make([]uint16, 256)

	r1, _, _ = procRegQueryValueExW.Call(
		hKey,
		uintptr(unsafe.Pointer(valName)),
		0,
		uintptr(unsafe.Pointer(&valType)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&bufSize)),
	)
	if r1 == 0 {
		name := strings.TrimSpace(syscall.UTF16ToString(buf))
		return fmt.Sprintf("%s (%d Cores)", name, runtime.NumCPU())
	}

	return fmt.Sprintf("CPU (%d Cores)", runtime.NumCPU())
}

func getVtableMethod(obj unsafe.Pointer, index int) uintptr {
	if obj == nil {
		return 0
	}
	vtbl := *(**[32]uintptr)(obj)
	if vtbl == nil || index < 0 || index >= len(vtbl) {
		return 0
	}
	return vtbl[index]
}

func releaseComObject(obj unsafe.Pointer) {
	if obj == nil {
		return
	}
	releaseFn := getVtableMethod(obj, 2)
	syscall.SyscallN(releaseFn, uintptr(obj))
}

func EnumerateGPUs() ([]GPUInfo, error) {
	dxgi := syscall.NewLazyDLL("dxgi.dll")
	procCreateDXGIFactory := dxgi.NewProc("CreateDXGIFactory")
	if err := procCreateDXGIFactory.Find(); err != nil {
		return nil, err
	}

	var factory unsafe.Pointer
	hr, _, _ := procCreateDXGIFactory.Call(
		uintptr(unsafe.Pointer(&IID_IDXGIFactory)),
		uintptr(unsafe.Pointer(&factory)),
	)
	if hr != 0 || factory == nil {
		return nil, fmt.Errorf("CreateDXGIFactory failed")
	}

	enumAdaptersFn := getVtableMethod(factory, 7)
	defer releaseComObject(factory)

	var gpus []GPUInfo
	for i := 0; ; i++ {
		var adapter unsafe.Pointer
		hr, _, _ = syscall.SyscallN(enumAdaptersFn, uintptr(factory), uintptr(i), uintptr(unsafe.Pointer(&adapter)))
		if hr != 0 || adapter == nil {
			break
		}

		getDescFn := getVtableMethod(adapter, 8)
		var desc DXGI_ADAPTER_DESC
		hr, _, _ = syscall.SyscallN(getDescFn, uintptr(adapter), uintptr(unsafe.Pointer(&desc)))
		releaseComObject(adapter)

		if hr != 0 {
			continue
		}

		name := strings.TrimSpace(syscall.UTF16ToString(desc.Description[:]))
		// Filter out Microsoft Basic Render Driver / software WARP emulators (Vendor 0x1414)
		if desc.VendorId == 0x1414 || strings.Contains(name, "Basic Render Driver") || strings.Contains(name, "WARP") {
			continue
		}

		vramMB := int(uint64(desc.DedicatedVideoMemory) / (1024 * 1024))
		isDiscrete := (desc.VendorId == 0x10DE || desc.VendorId == 0x1002 || vramMB >= 512)
		isIntegrated := !isDiscrete

		gpus = append(gpus, GPUInfo{
			Index:        len(gpus),
			Name:         name,
			VendorID:     desc.VendorId,
			VRAMBytes:    uint64(desc.DedicatedVideoMemory),
			VRAMMB:       vramMB,
			IsDiscrete:   isDiscrete,
			IsIntegrated: isIntegrated,
		})
	}
	return gpus, nil
}

// ParseGPUIDs parses "-g 0,1,cpu" or "-g 1" or "-g auto" or "-g cpu"
func ParseGPUIDs(gpuFlag string, gpus []GPUInfo) []int {
	trimmed := strings.ToLower(strings.TrimSpace(gpuFlag))
	if trimmed == "cpu" || trimmed == "-1" {
		return []int{-1} // CPU mode
	}

	if trimmed == "" || trimmed == "auto" {
		// Prefer discrete GPU first
		for _, g := range gpus {
			if g.IsDiscrete {
				return []int{g.Index}
			}
		}
		// Fallback to integrated GPU
		for _, g := range gpus {
			if g.IsIntegrated {
				return []int{g.Index}
			}
		}
		return []int{-1} // CPU fallback
	}

	parts := strings.Split(trimmed, ",")
	var result []int
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "cpu" || p == "-1" {
			result = append(result, -1)
			continue
		}
		if idx, err := strconv.Atoi(p); err == nil {
			if idx >= 0 && idx < len(gpus) {
				result = append(result, idx)
			}
		}
	}
	if len(result) == 0 {
		return []int{-1}
	}
	return result
}
