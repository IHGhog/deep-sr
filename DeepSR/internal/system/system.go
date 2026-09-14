package system

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

type GPUInfo struct {
	Index        int    `json:"index"`
	Name         string `json:"name"`
	VendorID     uint32 `json:"vendorId"`
	VRAMMB       int    `json:"vramMb"`
	IsDiscrete   bool   `json:"isDiscrete"`
	IsIntegrated bool   `json:"isIntegrated"`
}

type SystemUsage struct {
	CPUPercent  float64 `json:"cpuPercent"`
	RAMPercent  float64 `json:"ramPercent"`
	VRAMPercent float64 `json:"vramPercent"`
	GPUPercent  float64 `json:"gpuPercent"`
}

var (
	lastIdleTime   uint64
	lastKernelTime uint64
	lastUserTime   uint64
	usageMutex     sync.Mutex
)

func fileTimeToUint64(ft syscall.Filetime) uint64 {
	return (uint64(ft.HighDateTime) << 32) | uint64(ft.LowDateTime)
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

// GetSystemUsage 获取当前系统的实时 CPU、RAM 和 VRAM 占用百分比
func GetSystemUsage() SystemUsage {
	usageMutex.Lock()
	defer usageMutex.Unlock()

	var usage SystemUsage

	// 1. RAM Usage
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	procGlobalMemoryStatusEx := kernel32.NewProc("GlobalMemoryStatusEx")
	type MEMORYSTATUSEX struct {
		dwLength                uint32
		dwMemoryLoad            uint32
		ullTotalPhys            uint64
		ullAvailPhys            uint64
		ullTotalPageFile        uint64
		ullAvailPageFile        uint64
		ullTotalVirtual         uint64
		ullAvailVirtual         uint64
		ullAvailExtendedVirtual uint64
	}
	var mem MEMORYSTATUSEX
	mem.dwLength = uint32(unsafe.Sizeof(mem))
	if r, _, _ := procGlobalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&mem))); r != 0 {
		usage.RAMPercent = float64(mem.dwMemoryLoad)
		if mem.ullTotalPhys > 0 {
			used := mem.ullTotalPhys - mem.ullAvailPhys
			usage.RAMPercent = (float64(used) / float64(mem.ullTotalPhys)) * 100.0
		}
	}

	// 2. CPU Usage
	procGetSystemTimes := kernel32.NewProc("GetSystemTimes")
	var idleTime, kernelTime, userTime syscall.Filetime
	if r, _, _ := procGetSystemTimes.Call(
		uintptr(unsafe.Pointer(&idleTime)),
		uintptr(unsafe.Pointer(&kernelTime)),
		uintptr(unsafe.Pointer(&userTime)),
	); r != 0 {
		curIdle := fileTimeToUint64(idleTime)
		curKernel := fileTimeToUint64(kernelTime)
		curUser := fileTimeToUint64(userTime)

		if lastKernelTime != 0 && lastUserTime != 0 {
			deltaIdle := curIdle - lastIdleTime
			deltaKernel := curKernel - lastKernelTime
			deltaUser := curUser - lastUserTime
			totalSys := deltaKernel + deltaUser
			if totalSys > 0 && totalSys >= deltaIdle {
				usage.CPUPercent = float64(totalSys-deltaIdle) / float64(totalSys) * 100.0
			}
		}
		lastIdleTime = curIdle
		lastKernelTime = curKernel
		lastUserTime = curUser
	}

	// 3. VRAM Usage
	usage.VRAMPercent = queryVRAMUsage()

	// 4. GPU Compute Utilization
	usage.GPUPercent = queryGPUUtilization()

	return usage
}

var (
	nvmlOnce     sync.Once
	nvmlLoaded   bool
	nvmlDLL      *syscall.LazyDLL
	nvmlInitFn   *syscall.LazyProc
	nvmlCountFn  *syscall.LazyProc
	nvmlHandleFn *syscall.LazyProc
	nvmlMemFn    *syscall.LazyProc
	nvmlUtilFn   *syscall.LazyProc
)

func initNVML() {
	candidates := []string{
		"nvml.dll",
		"C:\\Windows\\System32\\nvml.dll",
		"C:\\Program Files\\NVIDIA Corporation\\NVSMI\\nvml.dll",
	}
	for _, p := range candidates {
		dll := syscall.NewLazyDLL(p)
		if dll.Load() == nil {
			nvmlDLL = dll
			nvmlInitFn = dll.NewProc("nvmlInit_v2")
			if nvmlInitFn.Find() != nil {
				nvmlInitFn = dll.NewProc("nvmlInit")
			}
			nvmlCountFn = dll.NewProc("nvmlDeviceGetCount_v2")
			if nvmlCountFn.Find() != nil {
				nvmlCountFn = dll.NewProc("nvmlDeviceGetCount")
			}
			nvmlHandleFn = dll.NewProc("nvmlDeviceGetHandleByIndex_v2")
			if nvmlHandleFn.Find() != nil {
				nvmlHandleFn = dll.NewProc("nvmlDeviceGetHandleByIndex")
			}
			nvmlMemFn = dll.NewProc("nvmlDeviceGetMemoryInfo")
			nvmlUtilFn = dll.NewProc("nvmlDeviceGetUtilizationRates")

			if nvmlInitFn.Find() == nil {
				r, _, _ := nvmlInitFn.Call()
				if r == 0 {
					nvmlLoaded = true
					return
				}
			}
		}
	}
}

func queryNVMLVRAMUsage() float64 {
	nvmlOnce.Do(initNVML)
	if !nvmlLoaded || nvmlCountFn == nil || nvmlHandleFn == nil || nvmlMemFn == nil {
		return 0
	}

	var count uint32
	rCount, _, _ := nvmlCountFn.Call(uintptr(unsafe.Pointer(&count)))
	if rCount != 0 || count == 0 {
		return 0
	}

	var maxPct float64 = 0
	type nvmlMemory_t struct {
		Total uint64
		Free  uint64
		Used  uint64
	}

	for i := uint32(0); i < count; i++ {
		var handle uintptr
		rH, _, _ := nvmlHandleFn.Call(uintptr(i), uintptr(unsafe.Pointer(&handle)))
		if rH == 0 && handle != 0 {
			var mem nvmlMemory_t
			rMem, _, _ := nvmlMemFn.Call(handle, uintptr(unsafe.Pointer(&mem)))
			if rMem == 0 && mem.Total > 0 {
				pct := float64(mem.Used) / float64(mem.Total) * 100.0
				if pct > maxPct {
					maxPct = pct
				}
			}
		}
	}
	return maxPct
}

func queryVRAMUsage() float64 {
	// 1. 优先使用 NVIDIA 原生硬件驱动 API (NVML)，毫秒级准确直读独立显卡显存占用
	if pct := queryNVMLVRAMUsage(); pct > 0 {
		return pct
	}
	// 2. 回退到 DXGI VRAM 探测
	return queryDXGIVRAMUsage()
}

type nvmlUtilization_t struct {
	GPU    uint32
	Memory uint32
}

func queryNVMLGPUUtilization() float64 {
	nvmlOnce.Do(initNVML)
	if !nvmlLoaded || nvmlCountFn == nil || nvmlHandleFn == nil || nvmlUtilFn == nil || nvmlUtilFn.Find() != nil {
		return 0
	}

	var count uint32
	rCount, _, _ := nvmlCountFn.Call(uintptr(unsafe.Pointer(&count)))
	if rCount != 0 || count == 0 {
		return 0
	}

	var maxPct float64 = 0
	for i := uint32(0); i < count; i++ {
		var handle uintptr
		rH, _, _ := nvmlHandleFn.Call(uintptr(i), uintptr(unsafe.Pointer(&handle)))
		if rH == 0 && handle != 0 {
			var util nvmlUtilization_t
			rU, _, _ := nvmlUtilFn.Call(handle, uintptr(unsafe.Pointer(&util)))
			if rU == 0 {
				pct := float64(util.GPU)
				if pct > maxPct {
					maxPct = pct
				}
			}
		}
	}
	return maxPct
}

var (
	pdhOnce    sync.Once
	pdhLoaded  bool
	pdhQuery   uintptr
	pdhCounter uintptr
	pdhMutex   sync.Mutex
)

func initPDH() {
	pdh := syscall.NewLazyDLL("pdh.dll")
	pOpen := pdh.NewProc("PdhOpenQueryW")
	pAdd := pdh.NewProc("PdhAddEnglishCounterW")
	pCollect := pdh.NewProc("PdhCollectQueryData")
	if pOpen.Find() != nil || pAdd.Find() != nil || pCollect.Find() != nil {
		return
	}

	var hQuery uintptr
	r, _, _ := pOpen.Call(0, 0, uintptr(unsafe.Pointer(&hQuery)))
	if r != 0 || hQuery == 0 {
		return
	}

	pathPtr, err := syscall.UTF16PtrFromString(`\GPU Engine(*)\Utilization Percentage`)
	if err != nil {
		return
	}

	var hCounter uintptr
	r, _, _ = pAdd.Call(hQuery, uintptr(unsafe.Pointer(pathPtr)), 0, uintptr(unsafe.Pointer(&hCounter)))
	if r != 0 || hCounter == 0 {
		pClose := pdh.NewProc("PdhCloseQuery")
		if pClose.Find() == nil {
			pClose.Call(hQuery)
		}
		return
	}

	pCollect.Call(hQuery)
	pdhQuery = hQuery
	pdhCounter = hCounter
	pdhLoaded = true
}

func queryPDHGPUUtilization() float64 {
	pdhOnce.Do(initPDH)
	if !pdhLoaded {
		return 0
	}

	pdhMutex.Lock()
	defer pdhMutex.Unlock()

	pdh := syscall.NewLazyDLL("pdh.dll")
	pCollect := pdh.NewProc("PdhCollectQueryData")
	pGetArray := pdh.NewProc("PdhGetFormattedCounterArrayW")
	if pCollect.Find() != nil || pGetArray.Find() != nil {
		return 0
	}

	r, _, _ := pCollect.Call(pdhQuery)
	if r != 0 {
		return 0
	}

	var bufSize uint32
	var itemCount uint32
	r, _, _ = pGetArray.Call(pdhCounter, 0x00000200, uintptr(unsafe.Pointer(&bufSize)), uintptr(unsafe.Pointer(&itemCount)), 0)
	if bufSize == 0 || itemCount == 0 {
		return 0
	}

	buf := make([]byte, bufSize)
	r, _, _ = pGetArray.Call(pdhCounter, 0x00000200, uintptr(unsafe.Pointer(&bufSize)), uintptr(unsafe.Pointer(&itemCount)), uintptr(unsafe.Pointer(&buf[0])))
	if r != 0 {
		return 0
	}

	type PDH_FMT_COUNTERVALUE_ITEM struct {
		SzName *uint16
		Status uint32
		Value  float64
	}
	items := unsafe.Slice((*PDH_FMT_COUNTERVALUE_ITEM)(unsafe.Pointer(&buf[0])), itemCount)
	var total float64
	for _, item := range items {
		if item.Status == 0 && item.Value > 0 {
			total += item.Value
		}
	}
	if total > 100.0 {
		total = 100.0
	}
	return total
}

// queryGPUUtilization 综合 NVML 与 PDH 获取实时 GPU 核心算力利用率 (0-100%)
func queryGPUUtilization() float64 {
	nvmlPct := queryNVMLGPUUtilization()
	if nvmlPct > 0 {
		return nvmlPct
	}
	pdhPct := queryPDHGPUUtilization()
	if pdhPct > nvmlPct {
		return pdhPct
	}
	return nvmlPct
}

func queryDXGIVRAMUsage() float64 {
	dxgi := syscall.NewLazyDLL("dxgi.dll")
	procCreateDXGIFactory := dxgi.NewProc("CreateDXGIFactory")
	if procCreateDXGIFactory.Find() != nil {
		return 0
	}

	var factory unsafe.Pointer
	r, _, _ := procCreateDXGIFactory.Call(
		uintptr(unsafe.Pointer(&IID_IDXGIFactory)),
		uintptr(unsafe.Pointer(&factory)),
	)
	if r != 0 || factory == nil {
		return 0
	}
	defer releaseComObject(factory)

	enumAdaptersProc := getVtableMethod(factory, 7)

	var highestUsage float64 = 0
	for i := uintptr(0); ; i++ {
		var adapter unsafe.Pointer
		r1, _, _ := syscall.SyscallN(enumAdaptersProc, uintptr(factory), i, uintptr(unsafe.Pointer(&adapter)))
		if r1 != 0 || adapter == nil {
			break
		}

		type DXGI_QUERY_VIDEO_MEMORY_INFO struct {
			Budget                  uint64
			CurrentUsage            uint64
			AvailableForReservation uint64
			CurrentReservation      uint64
		}

		// 获取该显卡的物理专用独立显存
		getDescFn := getVtableMethod(adapter, 8)
		var desc DXGI_ADAPTER_DESC
		_, _, _ = syscall.SyscallN(getDescFn, uintptr(adapter), uintptr(unsafe.Pointer(&desc)))
		dedicatedBytes := uint64(desc.DedicatedVideoMemory)

		IID_IDXGIAdapter3 := syscall.GUID{
			Data1: 0x645967a4,
			Data2: 0x5869,
			Data3: 0x4386,
			Data4: [8]byte{0x8a, 0x9e, 0xb4, 0x39, 0xd4, 0xab, 0x30, 0xdb},
		}
		var adapter3 unsafe.Pointer
		queryInterfaceProc := getVtableMethod(adapter, 0)
		rQI, _, _ := syscall.SyscallN(queryInterfaceProc, uintptr(adapter), uintptr(unsafe.Pointer(&IID_IDXGIAdapter3)), uintptr(unsafe.Pointer(&adapter3)))
		if rQI == 0 && adapter3 != nil {
			queryVideoMemProc := getVtableMethod(adapter3, 14)
			var memInfo DXGI_QUERY_VIDEO_MEMORY_INFO
			rMem, _, _ := syscall.SyscallN(queryVideoMemProc, uintptr(adapter3), 0, 0, uintptr(unsafe.Pointer(&memInfo)))
			if rMem == 0 {
				var totalVRAM uint64 = dedicatedBytes
				if totalVRAM == 0 {
					totalVRAM = memInfo.Budget
				}
				if totalVRAM > 0 {
					pct := float64(memInfo.CurrentUsage) / float64(totalVRAM) * 100.0
					if pct > highestUsage {
						highestUsage = pct
					}
				}
			}
			releaseComObject(adapter3)
		}

		releaseComObject(adapter)
	}

	return highestUsage
}

type SystemInfo struct {
	CPUName       string    `json:"cpuName"`
	CPUCores      int       `json:"cpuCores"`
	OS            string    `json:"os"`
	Arch          string    `json:"arch"`
	GPUs          []GPUInfo `json:"gpus"`
	TempDiskFree  string    `json:"tempDiskFree"`
	TempDiskTotal string    `json:"tempDiskTotal"`
}

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
	DedicatedVideoMemory  uintptr
	DedicatedSystemMemory uintptr
	SharedSystemMemory    uintptr
	AdapterLuid           LUID
}

var IID_IDXGIFactory = syscall.GUID{
	Data1: 0x7b7166ec,
	Data2: 0x21c7,
	Data3: 0x44ae,
	Data4: [8]byte{0xb2, 0x1a, 0xc9, 0xae, 0x32, 0x1a, 0xe3, 0x69},
}

// GetCPUInfo 获取 Windows CPU 名称
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

// EnumerateGPUs 通过 DXGI 枚举系统显卡
func EnumerateGPUs() []GPUInfo {
	dxgi := syscall.NewLazyDLL("dxgi.dll")
	procCreateDXGIFactory := dxgi.NewProc("CreateDXGIFactory")
	if err := procCreateDXGIFactory.Find(); err != nil {
		return []GPUInfo{}
	}

	var factory unsafe.Pointer
	hr, _, _ := procCreateDXGIFactory.Call(
		uintptr(unsafe.Pointer(&IID_IDXGIFactory)),
		uintptr(unsafe.Pointer(&factory)),
	)
	if hr != 0 || factory == nil {
		return []GPUInfo{}
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
			VRAMMB:       vramMB,
			IsDiscrete:   isDiscrete,
			IsIntegrated: isIntegrated,
		})
	}
	return gpus
}

// GetDiskSpace 获取目录所在驱动器的可用磁盘空间
func GetDiskSpace(dirPath string) (freeStr, totalStr string) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	procGetDiskFreeSpaceExW := kernel32.NewProc("GetDiskFreeSpaceExW")
	if err := procGetDiskFreeSpaceExW.Find(); err != nil {
		return "未知", "未知"
	}

	absPath, err := filepath.Abs(dirPath)
	if err != nil {
		absPath = dirPath
	}
	root := filepath.VolumeName(absPath) + "\\"

	rootPtr, _ := syscall.UTF16PtrFromString(root)
	var freeBytesAvailable, totalNumberOfBytes, totalNumberOfFreeBytes uint64

	r1, _, _ := procGetDiskFreeSpaceExW.Call(
		uintptr(unsafe.Pointer(rootPtr)),
		uintptr(unsafe.Pointer(&freeBytesAvailable)),
		uintptr(unsafe.Pointer(&totalNumberOfBytes)),
		uintptr(unsafe.Pointer(&totalNumberOfFreeBytes)),
	)
	if r1 == 0 {
		return "未知", "未知"
	}

	freeGB := float64(freeBytesAvailable) / (1024 * 1024 * 1024)
	totalGB := float64(totalNumberOfBytes) / (1024 * 1024 * 1024)
	return fmt.Sprintf("%.1f GB", freeGB), fmt.Sprintf("%.1f GB", totalGB)
}

// GetSystemInfo 获取系统整体硬件诊断信息
func GetSystemInfo(tempDir string) SystemInfo {
	free, total := GetDiskSpace(tempDir)
	return SystemInfo{
		CPUName:       GetCPUInfo(),
		CPUCores:      runtime.NumCPU(),
		OS:            runtime.GOOS,
		Arch:          runtime.GOARCH,
		GPUs:          EnumerateGPUs(),
		TempDiskFree:  free,
		TempDiskTotal: total,
	}
}

// OpenInExplorer 打开文件管理器并高亮选中文件或打开目录
func OpenInExplorer(targetPath string) error {
	absPath, err := filepath.Abs(targetPath)
	if err != nil {
		absPath = targetPath
	}

	fi, err := os.Stat(absPath)
	if err != nil {
		// 如果文件不存在，尝试打开其父目录
		parent := filepath.Dir(absPath)
		cmd := exec.Command("explorer.exe", parent)
		return cmd.Start()
	}

	if fi.IsDir() {
		cmd := exec.Command("explorer.exe", absPath)
		return cmd.Start()
	}

	// 单文件高亮显示
	cmd := exec.Command("explorer.exe", "/select,", absPath)
	return cmd.Start()
}

// ParseGPUIDs 解析 GPU 参数
func ParseGPUIDs(gpuFlag string, gpus []GPUInfo) []int {
	trimmed := strings.ToLower(strings.TrimSpace(gpuFlag))
	if trimmed == "cpu" || trimmed == "-1" {
		return []int{-1}
	}
	if trimmed == "" || trimmed == "auto" {
		for _, g := range gpus {
			if g.IsDiscrete {
				return []int{g.Index}
			}
		}
		for _, g := range gpus {
			if g.IsIntegrated {
				return []int{g.Index}
			}
		}
		return []int{-1}
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

// ResolveDeviceDescription 解析设备字符串并返回清晰的人类可读描述 (区分独显/核显/CPU)
func ResolveDeviceDescription(gpuStr string) string {
	gpus := EnumerateGPUs()
	trimmed := strings.ToLower(strings.TrimSpace(gpuStr))
	if trimmed == "" || trimmed == "auto" || trimmed == "-1" {
		for _, g := range gpus {
			if g.IsDiscrete {
				return fmt.Sprintf("自动调度 -> [独显] GPU %d: %s (%dMB)", g.Index, g.Name, g.VRAMMB)
			}
		}
		for _, g := range gpus {
			if g.IsIntegrated {
				return fmt.Sprintf("自动调度 -> [核显] GPU %d: %s (%dMB)", g.Index, g.Name, g.VRAMMB)
			}
		}
		return fmt.Sprintf("自动调度 -> [CPU] %s", GetCPUInfo())
	}
	if trimmed == "cpu" || trimmed == "-2" {
		return fmt.Sprintf("指定设备 -> [CPU] %s", GetCPUInfo())
	}
	idx, err := strconv.Atoi(trimmed)
	if err == nil && idx >= 0 && idx < len(gpus) {
		g := gpus[idx]
		kind := "核显"
		if g.IsDiscrete {
			kind = "独显"
		}
		return fmt.Sprintf("指定设备 -> [%s] GPU %d: %s (%dMB)", kind, g.Index, g.Name, g.VRAMMB)
	}
	return fmt.Sprintf("计算设备 -> %s", gpuStr)
}
