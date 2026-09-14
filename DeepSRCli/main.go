package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"deepsr-cli/pkg/align"
	"deepsr-cli/pkg/codeformer"
	"deepsr-cli/pkg/detector"
	"deepsr-cli/pkg/hardware"
	"deepsr-cli/pkg/imgutil"
	"deepsr-cli/pkg/ort"
	"deepsr-cli/pkg/sr"
	"deepsr-cli/pkg/stream"
)

const Version = "3.1.0"

type Event struct {
	Type      string  `json:"type"`
	Timestamp float64 `json:"timestamp"`
	Phase     string  `json:"phase,omitempty"`
	Current   int     `json:"current,omitempty"`
	Total     int     `json:"total,omitempty"`
	Percent   float64 `json:"percent,omitempty"`
	Message   string  `json:"message,omitempty"`
	Input     string  `json:"input,omitempty"`
	Output    string  `json:"output,omitempty"`
	Elapsed   float64 `json:"elapsed,omitempty"`
	FaceCount *int    `json:"faceCount,omitempty"`
}

func round3(val float64) float64 {
	return math.Round(val*1000.0) / 1000.0
}

func emit(jsonMode bool, ev Event) {
	ev.Timestamp = round3(float64(time.Now().UnixNano()) / 1e9)
	ev.Percent = round3(ev.Percent)
	if ev.Elapsed > 0 {
		ev.Elapsed = round3(ev.Elapsed)
	}
	if jsonMode {
		b, _ := json.Marshal(ev)
		fmt.Println(string(b))
	} else {
		switch ev.Type {
		case "progress":
			// 单行平滑高刷进度条（利用 \r 与 ANSI 擦除控制符）
			fmt.Printf("\r\033[K[%s] %5.1f%% - %s", strings.ToUpper(ev.Phase), ev.Percent, ev.Message)
		case "info":
			fmt.Printf("\n[INFO] %s\n", ev.Message)
		case "warn":
			fmt.Printf("\n[WARN] %s\n", ev.Message)
		case "error":
			fmt.Fprintf(os.Stderr, "\n[ERROR] %s\n", ev.Message)
		case "complete":
			fmt.Printf("\n[SUCCESS] Done in %.2fs -> %s\n", ev.Elapsed, ev.Output)
		}
	}
}

func resolveModelsDir(mFlag string) string {
	exePath, _ := os.Executable()
	exeDir := filepath.Dir(exePath)

	if mFlag != "" && mFlag != "models" {
		if filepath.IsAbs(mFlag) {
			return mFlag
		}
		if fi, err := os.Stat(mFlag); err == nil && fi.IsDir() {
			abs, _ := filepath.Abs(mFlag)
			return abs
		}
		return filepath.Join(exeDir, mFlag)
	}

	// 严格默认指向可执行文件同级的 models 目录
	return filepath.Join(exeDir, "models")
}

// findAsset 模型文件寻址：严格按传入名称寻址（补齐 .onnx），不进行隐式精度后缀兜底
func findAsset(name string, modelsDir string) (string, error) {
	// 1. 若直接传入的是已存在的绝对/相对路径
	if fi, err := os.Stat(name); err == nil && !fi.IsDir() {
		return name, nil
	}

	// 2. 严格补 .onnx 后缀在当前目录及 modelsDir 中寻址
	targetName := name
	if !strings.HasSuffix(strings.ToLower(targetName), ".onnx") {
		targetName = targetName + ".onnx"
	}

	if fi, err := os.Stat(targetName); err == nil && !fi.IsDir() {
		return targetName, nil
	}

	if modelsDir != "" {
		p := filepath.Join(modelsDir, targetName)
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p, nil
		}
	}

	return "", fmt.Errorf("模型文件未找到: %s (搜索目录: %s)", targetName, modelsDir)
}

func resolveSRModelNativeScale(name string) int {
	lower := strings.ToLower(name)
	if strings.Contains(lower, "x2plus") || strings.Contains(lower, "x2") {
		return 2
	}
	return 4
}

func parseThreads(threadsStr string) (int, int, int) {
	parts := strings.Split(threadsStr, ":")
	loadW := 3
	procW := 2
	saveW := 3
	if len(parts) >= 1 {
		if v, err := strconv.Atoi(parts[0]); err == nil && v > 0 {
			loadW = v
		}
	}
	if len(parts) >= 2 {
		if v, err := strconv.Atoi(parts[1]); err == nil && v > 0 {
			procW = v
		}
	}
	if len(parts) >= 3 {
		if v, err := strconv.Atoi(parts[2]); err == nil && v > 0 {
			saveW = v
		}
	}
	return loadW, procW, saveW
}

func parseTileSizes(tStr string, numGPUs int) []int {
	parts := strings.Split(tStr, ",")
	var result []int
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if v, err := strconv.Atoi(p); err == nil {
			if v <= 0 {
				v = 0
			} else if v < 32 {
				v = 32
			}
			result = append(result, v)
		}
	}
	if len(result) == 0 {
		result = []int{0}
	}
	for len(result) < numGPUs {
		result = append(result, result[len(result)-1])
	}
	return result
}

func processSingleImage(
	inPath string,
	outPath string,
	mode string,
	scale int,
	fidelity float64,
	tileSizes []int,
	tta bool,
	format string,
	jsonMode bool,
	det *detector.RetinaFace,
	cf *codeformer.CodeFormer,
	srModel *sr.SRModel,
) error {
	t0 := time.Now()
	emit(jsonMode, Event{Type: "start", Phase: "load", Input: inPath, Output: outPath})

	emit(jsonMode, Event{Type: "info", Phase: "loading", Message: fmt.Sprintf("Loading image: %s", filepath.Base(inPath))})
	imgRGB, w, h, err := imgutil.LoadImage(inPath)
	if err != nil {
		emit(jsonMode, Event{Type: "error", Message: fmt.Sprintf("Failed to load image: %v", err)})
		return err
	}
	emit(jsonMode, Event{Type: "info", Phase: "loaded", Message: fmt.Sprintf("Loaded image: %dx%d", w, h)})

	curRGB := imgRGB
	curW := w
	curH := h

	// Phase 1: Super-Resolution
	if mode == "sr" || mode == "all" {
		tStr := fmt.Sprintf("%v", tileSizes)
		if len(tileSizes) == 1 {
			tStr = fmt.Sprintf("%d", tileSizes[0])
		}
		emit(jsonMode, Event{Type: "info", Message: fmt.Sprintf("Starting Super-Resolution (TileSize=%s, NativeScale=%dx, TTA=%v) on %s...", tStr, srModel.NativeScale, tta, srModel.DeviceDesc)})
		var srRGB []float32
		var srW, srH int

		progressCb := func(cur, tot int, pct float64, msg string) {
			effectivePct := pct
			if mode == "all" {
				effectivePct = pct * 0.50
			}
			emit(jsonMode, Event{
				Type:    "progress",
				Phase:   "sr",
				Current: cur,
				Total:   tot,
				Percent: effectivePct,
				Message: fmt.Sprintf("%s (%d/%d)", msg, cur, tot),
			})
		}

		if tta {
			srRGB, srW, srH, err = srModel.SuperResolveTTA(curRGB, curW, curH, tileSizes, 16, progressCb)
		} else {
			srRGB, srW, srH, err = srModel.SuperResolve(curRGB, curW, curH, tileSizes, 16, progressCb)
		}

		if err != nil {
			emit(jsonMode, Event{Type: "error", Message: fmt.Sprintf("Super-Resolution failed: %v", err)})
			return err
		}

		if scale > 0 && scale != srModel.NativeScale {
			targetW := w * scale
			targetH := h * scale
			srRGB = imgutil.Resize(srRGB, srW, srH, targetW, targetH)
			srW = targetW
			srH = targetH
		}

		curRGB = srRGB
		curW = srW
		curH = srH
		emit(jsonMode, Event{Type: "info", Message: fmt.Sprintf("Super-Resolution completed: %dx%d", curW, curH)})
	}

	// Phase 2: Face Restoration (CodeFormer)
	if mode == "face" || mode == "all" {
		emit(jsonMode, Event{Type: "info", Message: "Starting Face Detection (RetinaFace)..."})
		faces, err := det.Detect(curRGB, curW, curH, 0.5, 0.4)
		if err != nil {
			emit(jsonMode, Event{Type: "error", Message: fmt.Sprintf("Face detection failed: %v", err)})
			return err
		}

		numFaces := len(faces)
		emit(jsonMode, Event{Type: "info", Message: fmt.Sprintf("Detected %d face(s)", numFaces), FaceCount: &numFaces})

		if numFaces > 0 {
			for i, face := range faces {
				forM, invM := align.EstimateSimilarityTransform(face.Landmarks, align.FaceTemplate512)
				croppedFace := align.WarpAffine(curRGB, curW, curH, invM, 512, 512)

				restoredFace, err := cf.RestoreFace(croppedFace, float32(fidelity))
				if err != nil {
					emit(jsonMode, Event{Type: "warn", Message: fmt.Sprintf("Face %d restoration failed: %v", i+1, err)})
					continue
				}

				align.PasteFaceBack(curRGB, curW, curH, restoredFace, 512, 512, forM, invM)

				pct := (float64(i+1) / float64(len(faces))) * 100.0
				if mode == "all" {
					pct = 50.0 + pct*0.50
				}
				emit(jsonMode, Event{
					Type:    "progress",
					Phase:   "face",
					Current: i + 1,
					Total:   len(faces),
					Percent: pct,
					Message: fmt.Sprintf("Restored face %d/%d", i+1, len(faces)),
				})
			}
		}
	}

	if err := imgutil.SaveImageFormat(outPath, curRGB, curW, curH, format, 95); err != nil {
		emit(jsonMode, Event{Type: "error", Message: fmt.Sprintf("Failed to save output: %v", err)})
		return err
	}

	emit(jsonMode, Event{Type: "progress", Phase: "done", Percent: 100.0, Message: "Finished"})
	emit(jsonMode, Event{Type: "complete", Output: outPath, Elapsed: time.Since(t0).Seconds()})
	return nil
}

type RawFileTask struct {
	InFile  string
	OutFile string
}

type DecodedTask struct {
	InFile  string
	OutFile string
	ImgRGB  []float32
	W       int
	H       int
}

type ProcessedTask struct {
	InFile     string
	OutFile    string
	SrRGB      []float32
	SrW        int
	SrH        int
	WorkerDesc string
	WorkerIdx  int
}

func runFolderWorkStealingPipeline(
	files []string,
	inDir string,
	outDir string,
	targetGPUs []int,
	tileSizes []int,
	mode string,
	scale int,
	fidelity float64,
	tta bool,
	format string,
	jsonMode bool,
	engine *ort.Engine,
	modelPathFlag string,
	modelNameFlag string,
	gpus []hardware.GPUInfo,
	loadWorkers int,
	procWorkers int,
	saveWorkers int,
) {
	totalFiles := len(files)
	tFolderStart := time.Now()

	rawChan := make(chan RawFileTask, totalFiles)
	for _, name := range files {
		inFile := filepath.Join(inDir, name)
		outName := name
		if format != "" {
			outName = strings.TrimSuffix(name, filepath.Ext(name)) + "." + strings.TrimPrefix(format, ".")
		}
		outFile := filepath.Join(outDir, outName)
		rawChan <- RawFileTask{InFile: inFile, OutFile: outFile}
	}
	close(rawChan)

	// Stage 1: Async Loader Pool
	procQueueCap := loadWorkers * 4
	if procQueueCap < 16 {
		procQueueCap = 16
	}
	decodedChan := make(chan DecodedTask, procQueueCap)

	var loadWg sync.WaitGroup
	for i := 0; i < loadWorkers; i++ {
		loadWg.Add(1)
		go func() {
			defer loadWg.Done()
			for task := range rawChan {
				rgb, w, h, err := imgutil.LoadImage(task.InFile)
				if err == nil {
					decodedChan <- DecodedTask{
						InFile:  task.InFile,
						OutFile: task.OutFile,
						ImgRGB:  rgb,
						W:       w,
						H:       h,
					}
				}
			}
		}()
	}

	go func() {
		loadWg.Wait()
		close(decodedChan)
	}()

	// Stage 2: Hardware Workers Pool
	actualProcWorkers := procWorkers
	if actualProcWorkers < len(targetGPUs) {
		actualProcWorkers = len(targetGPUs)
	}

	saveQueueCap := saveWorkers * 4
	if saveQueueCap < 16 {
		saveQueueCap = 16
	}
	processedChan := make(chan ProcessedTask, saveQueueCap)

	var completedCount int64
	perDeviceCount := make([]int64, actualProcWorkers)
	var procWg sync.WaitGroup

	for workerIdx := 0; workerIdx < actualProcWorkers; workerIdx++ {
		procWg.Add(1)
		devID := targetGPUs[workerIdx%len(targetGPUs)]
		wTileSize := tileSizes[0]
		if (workerIdx % len(targetGPUs)) < len(tileSizes) {
			wTileSize = tileSizes[workerIdx%len(targetGPUs)]
		}

		devDesc := "CPU"
		if devID >= 0 && devID < len(gpus) {
			devDesc = fmt.Sprintf("GPU %d: %s", devID, gpus[devID].Name)
			if actualProcWorkers > len(targetGPUs) {
				devDesc = fmt.Sprintf("GPU %d: %s (Worker %d)", devID, gpus[devID].Name, workerIdx+1)
			}
		}

		go func(wIdx int, dID int, tileSize int, desc string) {
			defer procWg.Done()

			var det *detector.RetinaFace
			var cf *codeformer.CodeFormer
			var singleSess *sr.SingleGPUSession

			devStr := "cpu"
			if dID >= 0 {
				devStr = fmt.Sprintf("gpu:%d", dID)
			}

			if mode == "face" || mode == "all" {
				retinaModel := "retinaface_fp32"
				cfModel := "codeformer_fp32"
				if strings.Contains(strings.ToLower(modelNameFlag), "fp16") {
					retinaModel = "retinaface_fp16"
					cfModel = "codeformer_fp16"
				}

				retinaPath, err := findAsset(retinaModel, modelPathFlag)
				if err == nil {
					det, _ = detector.NewRetinaFace(engine, retinaPath, devStr)
					if det != nil {
						defer det.Close()
					}
				}

				cfPath, err := findAsset(cfModel, modelPathFlag)
				if err == nil {
					cf, _ = codeformer.NewCodeFormer(engine, cfPath, devStr)
					if cf != nil {
						defer cf.Close()
					}
				}
			}

			if mode == "sr" || mode == "all" {
				nativeScale := resolveSRModelNativeScale(modelNameFlag)
				srPath, err := findAsset(modelNameFlag, modelPathFlag)
				if err != nil {
					emit(jsonMode, Event{Type: "error", Message: err.Error()})
					fmt.Fprintf(os.Stderr, "\n[FATAL] %v\n", err)
					os.Exit(1)
				}
				singleSess, _ = sr.NewSingleGPUSession(engine, srPath, dID, tileSize, nativeScale)
				if singleSess != nil {
					defer singleSess.Close()
				}
			}

			for task := range decodedChan {
				curRGB := task.ImgRGB
				curW := task.W
				curH := task.H

				if mode == "sr" || mode == "all" {
					if singleSess != nil {
						srRGB, srW, srH, err := singleSess.SuperResolveDirect(curRGB, curW, curH, 16)
						if err != nil {
							emit(jsonMode, Event{Type: "error", Message: err.Error()})
							fmt.Fprintf(os.Stderr, "\n[FATAL] %v\n", err)
							os.Exit(1)
						} else {
							if scale > 0 && scale != singleSess.NativeScale {
								targetW := task.W * scale
								targetH := task.H * scale
								srRGB = imgutil.Resize(srRGB, srW, srH, targetW, targetH)
								srW = targetW
								srH = targetH
							}
							curRGB = srRGB
							curW = srW
							curH = srH
						}
					}
				}

				if mode == "face" || mode == "all" {
					if det != nil && cf != nil {
						faces, err := det.Detect(curRGB, curW, curH, 0.5, 0.4)
						if err == nil && len(faces) > 0 {
							for _, face := range faces {
								forM, invM := align.EstimateSimilarityTransform(face.Landmarks, align.FaceTemplate512)
								croppedFace := align.WarpAffine(curRGB, curW, curH, invM, 512, 512)
								restoredFace, err := cf.RestoreFace(croppedFace, float32(fidelity))
								if err == nil {
									align.PasteFaceBack(curRGB, curW, curH, restoredFace, 512, 512, forM, invM)
								}
							}
						}
					}
				}

				processedChan <- ProcessedTask{
					InFile:     task.InFile,
					OutFile:    task.OutFile,
					SrRGB:      curRGB,
					SrW:        curW,
					SrH:        curH,
					WorkerDesc: desc,
					WorkerIdx:  wIdx,
				}
			}
		}(workerIdx, devID, wTileSize, devDesc)
	}

	go func() {
		procWg.Wait()
		close(processedChan)
	}()

	// Stage 3: Async Saver Pool
	var saveWg sync.WaitGroup
	for i := 0; i < saveWorkers; i++ {
		saveWg.Add(1)
		go func() {
			defer saveWg.Done()
			for task := range processedChan {
				_ = imgutil.SaveImageFormat(task.OutFile, task.SrRGB, task.SrW, task.SrH, format, 95)
				task.SrRGB = nil

				cur := atomic.AddInt64(&completedCount, 1)
				atomic.AddInt64(&perDeviceCount[task.WorkerIdx], 1)
				pct := (float64(cur) / float64(totalFiles)) * 100.0

				emit(jsonMode, Event{
					Type:    "progress",
					Phase:   "batch",
					Current: int(cur),
					Total:   totalFiles,
					Percent: pct,
					Message: fmt.Sprintf("[%s] Processed %s (%d/%d)", task.WorkerDesc, filepath.Base(task.InFile), cur, totalFiles),
				})
			}
		}()
	}

	saveWg.Wait()
	totalElapsed := time.Since(tFolderStart).Seconds()

	if !jsonMode {
		fmt.Println("\n==================================================")
		fmt.Printf("[BATCH COMPLETED] Processed %d images in %.2fs (Average: %.3fs/image, %.1f FPS)\n", totalFiles, totalElapsed, totalElapsed/float64(totalFiles), float64(totalFiles)/totalElapsed)
		fmt.Printf("[PIPELINE CONFIG] Threads Load:Proc:Save = %d:%d:%d\n", loadWorkers, actualProcWorkers, saveWorkers)
		fmt.Println("[WORK-STEALING LOAD DISTRIBUTION]:")
		for wIdx := 0; wIdx < actualProcWorkers; wIdx++ {
			devID := targetGPUs[wIdx%len(targetGPUs)]
			desc := "CPU"
			if devID >= 0 && devID < len(gpus) {
				desc = fmt.Sprintf("GPU %d: %s", devID, gpus[devID].Name)
				if actualProcWorkers > len(targetGPUs) {
					desc = fmt.Sprintf("GPU %d: %s (Worker %d)", devID, gpus[devID].Name, wIdx+1)
				}
			}
			cnt := atomic.LoadInt64(&perDeviceCount[wIdx])
			pct := (float64(cnt) / float64(totalFiles)) * 100.0
			fmt.Printf("  • %-38s : %3d images (%5.1f%% of workload)\n", desc, cnt, pct)
		}
		fmt.Println("==================================================")
	}
}

func printHelp() {
	helpText := `Usage: deepsr-cli [options] -i <input> -o <output>

Basic Options:
  -h, --help                 Show this help manual
  -V, --version              Show version information
  -i, --input <path>         Input image or video path, or directory of images
  -o, --output <path>        Output image or video path, or directory
  -s, --scale <ratio>        Upscale ratio (2, 3, 4. default=4)
  -t, --tile-size <size>     Tile size (>=32, 0=auto, default=0).
                             Auto mode: <=1080p direct inference, >1080p cascaded tiled (512->256->128)
  -m, --model-path <dir>     Directory containing pre-trained models (default=models)
  -n, --model-name <name>    Exact model name to load (default=realesrgan_x4plus_fp32)
                             e.g. realesrgan_x4plus_fp32, realesrgan_x4plus_fp16, real-hat-gan
  -g, --gpu <device>         GPU compute device (default=auto, can be auto | 0 | 1 | 0,1 | cpu)
  -j, --threads <L:P:S>      Pipeline thread control (default=3:2:3)
                             L (Load queue depth) : P (Proc worker count) : S (Save queue depth)
  -x, --tta                  Enable 8x Test-Time Augmentation mode
  -f, --format <ext>         Output image format (jpg | png | webp, default=source extension)
  -v, --verbose              Enable verbose runtime logging
  -d, --devices              List all detected CPU and GPU compute devices

Pipeline & Restoration Modes:
  -mode, --mode <mode>       Execution mode: 'sr' (default), 'face' (CodeFormer only), 'all' (SR + Face)
  -w, --fidelity <weight>    CodeFormer face restoration fidelity weight (0.0 to 1.0, default=0.7)
  -json                      Output structured real-time JSON stream for GUI IPC

In-Memory Video Streaming Pipeline:
  --stream-pipe              Enable zero-disk rawvideo in-memory video stream pipeline (0 temp files)
  --encoder <name>           Video encoder (default=libx264, or h264_nvenc, hevc_nvenc, h264_qsv, etc.)
  --crf <crf>                CRF video quality value (0-51, default=20)
  --preset <preset>          Encoder compression preset (default=medium)

Examples:
  1. Upscale single image to 4x using GPU 1:
     deepsr-cli -i input.jpg -o output.png -s 4 -n realesrgan_x4plus_fp32 -g 1

  2. Save genuine WebP image:
     deepsr-cli -i input.jpg -o output.webp -f webp -n realesrgan_x4plus_fp32

  3. Process video in-memory stream pipeline without temp files:
     deepsr-cli -i input.mp4 -o output.mp4 --stream-pipe -s 2 -n realesrgan_x2plus_fp32 -j 3:2:3
`
	fmt.Print(helpText)
}

func main() {
	args := os.Args[1:]

	// 默认参数配置
	var (
		inputPath      string
		outputPath     string
		scaleVal       = 4
		tileSizeStr    = "0"
		modelPath      = "models"
		modelName      = "realesrgan_x4plus_fp32"
		gpuVal         = "auto"
		threadsVal     = "3:2:3"
		ttaVal         = false
		formatVal      = ""
		verboseVal     = false
		modeVal        = "sr"
		fidelityVal    = 0.7
		jsonVal        = false
		showDevices    = false
		showHelp       = false
		showVersion    = false
		streamPipeVal  = false
		encoderVal     = "libx264"
		crfVal         = 20
		presetVal      = "medium"
	)

	// 手工解析 POSIX/GNU 长短参数，确保 -i/--input, -t/--tile-size 等完全兼容
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-h" || arg == "--help":
			showHelp = true
		case arg == "-V" || arg == "--version" || arg == "-version":
			showVersion = true
		case arg == "-d" || arg == "--devices" || arg == "-devices" || arg == "--list-devices" || arg == "--list-gpu":
			showDevices = true
		case arg == "-json" || arg == "--json":
			jsonVal = true
		case arg == "-x" || arg == "--tta":
			ttaVal = true
		case arg == "-v" || arg == "--verbose":
			verboseVal = true
		case arg == "--stream-pipe":
			streamPipeVal = true

		case arg == "-i" || arg == "--input":
			if i+1 < len(args) {
				i++
				inputPath = args[i]
			}
		case strings.HasPrefix(arg, "--input="):
			inputPath = strings.TrimPrefix(arg, "--input=")

		case arg == "-o" || arg == "--output":
			if i+1 < len(args) {
				i++
				outputPath = args[i]
			}
		case strings.HasPrefix(arg, "--output="):
			outputPath = strings.TrimPrefix(arg, "--output=")

		case arg == "-s" || arg == "--scale":
			if i+1 < len(args) {
				i++
				if v, err := strconv.Atoi(args[i]); err == nil {
					scaleVal = v
				}
			}
		case strings.HasPrefix(arg, "--scale="):
			if v, err := strconv.Atoi(strings.TrimPrefix(arg, "--scale=")); err == nil {
				scaleVal = v
			}

		case arg == "-t" || arg == "--tile-size":
			if i+1 < len(args) {
				i++
				tileSizeStr = args[i]
			}
		case strings.HasPrefix(arg, "--tile-size="):
			tileSizeStr = strings.TrimPrefix(arg, "--tile-size=")

		case arg == "-m" || arg == "--model-path":
			if i+1 < len(args) {
				i++
				modelPath = args[i]
			}
		case strings.HasPrefix(arg, "--model-path="):
			modelPath = strings.TrimPrefix(arg, "--model-path=")

		case arg == "-n" || arg == "--model-name":
			if i+1 < len(args) {
				i++
				modelName = args[i]
			}
		case strings.HasPrefix(arg, "--model-name="):
			modelName = strings.TrimPrefix(arg, "--model-name=")

		case arg == "-g" || arg == "--gpu":
			if i+1 < len(args) {
				i++
				gpuVal = args[i]
			}
		case strings.HasPrefix(arg, "--gpu="):
			gpuVal = strings.TrimPrefix(arg, "--gpu=")

		case arg == "-j" || arg == "--threads":
			if i+1 < len(args) {
				i++
				threadsVal = args[i]
			}
		case strings.HasPrefix(arg, "--threads="):
			threadsVal = strings.TrimPrefix(arg, "--threads=")

		case arg == "-f" || arg == "--format":
			if i+1 < len(args) {
				i++
				formatVal = args[i]
			}
		case strings.HasPrefix(arg, "--format="):
			formatVal = strings.TrimPrefix(arg, "--format=")

		case arg == "-mode" || arg == "--mode":
			if i+1 < len(args) {
				i++
				modeVal = args[i]
			}
		case strings.HasPrefix(arg, "--mode="):
			modeVal = strings.TrimPrefix(arg, "--mode=")

		case arg == "-w" || arg == "--fidelity":
			if i+1 < len(args) {
				i++
				if v, err := strconv.ParseFloat(args[i], 64); err == nil {
					fidelityVal = v
				}
			}
		case strings.HasPrefix(arg, "--fidelity="):
			if v, err := strconv.ParseFloat(strings.TrimPrefix(arg, "--fidelity="), 64); err == nil {
				fidelityVal = v
			}


		case arg == "--encoder":
			if i+1 < len(args) {
				i++
				encoderVal = args[i]
			}
		case strings.HasPrefix(arg, "--encoder="):
			encoderVal = strings.TrimPrefix(arg, "--encoder=")

		case arg == "--crf":
			if i+1 < len(args) {
				i++
				if v, err := strconv.Atoi(args[i]); err == nil {
					crfVal = v
				}
			}
		case strings.HasPrefix(arg, "--crf="):
			if v, err := strconv.Atoi(strings.TrimPrefix(arg, "--crf=")); err == nil {
				crfVal = v
			}

		case arg == "--preset":
			if i+1 < len(args) {
				i++
				presetVal = args[i]
			}
		case strings.HasPrefix(arg, "--preset="):
			presetVal = strings.TrimPrefix(arg, "--preset=")
		}
	}

	cpuInfo := hardware.GetCPUInfo()
	gpus, _ := hardware.EnumerateGPUs()

	if showDevices {
		fmt.Printf("CPU: %s\n", cpuInfo)
		fmt.Println("GPU:")
		for _, g := range gpus {
			kind := "Integrated"
			if g.IsDiscrete {
				kind = "Dedicated (Discrete GPU)"
			}
			fmt.Printf("  [%d] %s (VRAM: %d MB, %s)\n", g.Index, g.Name, g.VRAMMB, kind)
		}
		return
	}

	if showVersion {
		fmt.Printf("deepsr-cli version %s (DirectML & Rawvideo Stream Engine)\n", Version)
		return
	}

	if showHelp || inputPath == "" {
		printHelp()
		if inputPath == "" && !showHelp {
			os.Exit(1)
		}
		return
	}

	targetGPUs := hardware.ParseGPUIDs(gpuVal, gpus)
	var devDescParts []string
	for _, id := range targetGPUs {
		if id < 0 {
			devDescParts = append(devDescParts, "CPU")
		} else if id < len(gpus) {
			devDescParts = append(devDescParts, fmt.Sprintf("GPU %d: %s (%dMB)", id, gpus[id].Name, gpus[id].VRAMMB))
		}
	}
	deviceDesc := strings.Join(devDescParts, " + ")

	primaryDevStr := "cpu"
	if len(targetGPUs) > 0 && targetGPUs[0] >= 0 {
		primaryDevStr = fmt.Sprintf("gpu:%d", targetGPUs[0])
	}

	modelsDir := resolveModelsDir(modelPath)

	exePath, _ := os.Executable()
	exeDir := filepath.Dir(exePath)
	dllPath := filepath.Join(exeDir, "onnxruntime.dll")
	if _, err := os.Stat(dllPath); err != nil {
		dllPath = "onnxruntime.dll"
	}

	engine, err := ort.NewEngine(dllPath)
	if err != nil {
		emit(jsonVal, Event{Type: "error", Message: fmt.Sprintf("Failed to initialize ONNX Runtime: %v", err)})
		os.Exit(1)
	}
	defer engine.Close()

	if !jsonVal {
		fmt.Printf("[HARDWARE] Host CPU: %s\n", cpuInfo)
		fmt.Printf("[HARDWARE] Target Compute Device: %s\n", deviceDesc)
	}

	emit(jsonVal, Event{Type: "info", Phase: "start", Message: fmt.Sprintf("Initialized Deep-SR Engine v%s (Device: %s)", Version, deviceDesc)})

	inPath, _ := filepath.Abs(inputPath)
	st, err := os.Stat(inPath)
	if err != nil {
		emit(jsonVal, Event{Type: "error", Message: fmt.Sprintf("Input path not found: %s", inPath)})
		os.Exit(1)
	}

	tileSizes := parseTileSizes(tileSizeStr, len(targetGPUs))
	loadW, procW, saveW := parseThreads(threadsVal)

	if verboseVal && !jsonVal {
		fmt.Printf("[CONFIG] Mode=%s, Model=%s, TileSizes=%v, Scale=%dx, Fidelity=%.2f, Threads=%d:%d:%d\n", modeVal, modelName, tileSizes, scaleVal, fidelityVal, loadW, procW, saveW)
	}

	if st.IsDir() {
		outDir := inPath + "_enhanced"
		if outputPath != "" {
			outDir, _ = filepath.Abs(outputPath)
		}
		_ = os.MkdirAll(outDir, 0755)

		entries, _ := os.ReadDir(inPath)
		var files []string
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			ext := strings.ToLower(filepath.Ext(entry.Name()))
			if ext == ".jpg" || ext == ".jpeg" || ext == ".png" || ext == ".webp" {
				files = append(files, entry.Name())
			}
		}

		runFolderWorkStealingPipeline(
			files,
			inPath,
			outDir,
			targetGPUs,
			tileSizes,
			modeVal,
			scaleVal,
			fidelityVal,
			ttaVal,
			formatVal,
			jsonVal,
			engine,
			modelsDir,
			modelName,
			gpus,
			loadW,
			procW,
			saveW,
		)
	} else {
		var det *detector.RetinaFace
		var cf *codeformer.CodeFormer
		var srModel *sr.SRModel

		if modeVal == "face" || modeVal == "all" {
			retinaModel := "retinaface_fp32"
			cfModel := "codeformer_fp32"
			if strings.Contains(strings.ToLower(modelName), "fp16") {
				retinaModel = "retinaface_fp16"
				cfModel = "codeformer_fp16"
			}

			retinaPath, err := findAsset(retinaModel, modelsDir)
			if err != nil {
				emit(jsonVal, Event{Type: "error", Message: err.Error()})
				os.Exit(1)
			}
			det, err = detector.NewRetinaFace(engine, retinaPath, primaryDevStr)
			if err != nil {
				emit(jsonVal, Event{Type: "error", Message: fmt.Sprintf("Failed to load %s: %v", retinaPath, err)})
				os.Exit(1)
			}
			defer det.Close()

			cfPath, err := findAsset(cfModel, modelsDir)
			if err != nil {
				emit(jsonVal, Event{Type: "error", Message: err.Error()})
				os.Exit(1)
			}
			cf, err = codeformer.NewCodeFormer(engine, cfPath, primaryDevStr)
			if err != nil {
				emit(jsonVal, Event{Type: "error", Message: fmt.Sprintf("Failed to load %s: %v", cfPath, err)})
				os.Exit(1)
			}
			defer cf.Close()
		}

		inExt := strings.ToLower(filepath.Ext(inPath))
		isVideoFile := streamPipeVal || inExt == ".mp4" || inExt == ".mkv" || inExt == ".mov" || inExt == ".avi" || inExt == ".webm" || inExt == ".flv" || inExt == ".ts" || inExt == ".m4v"

		if isVideoFile {
			var singleSess *sr.SingleGPUSession
			var extraSessions []*sr.SingleGPUSession

			if modeVal == "sr" || modeVal == "all" {
				nativeScale := resolveSRModelNativeScale(modelName)
				srPath, err := findAsset(modelName, modelsDir)
				if err != nil {
					emit(jsonVal, Event{Type: "error", Message: err.Error()})
					os.Exit(1)
				}

				actualProcWorkers := procW
				if actualProcWorkers < len(targetGPUs) {
					actualProcWorkers = len(targetGPUs)
				}
				if actualProcWorkers <= 0 {
					actualProcWorkers = 1
				}

				for i := 0; i < actualProcWorkers; i++ {
					devID := -1
					if len(targetGPUs) > 0 {
						devID = targetGPUs[i%len(targetGPUs)]
					}
					tSize := tileSizes[0]
					if len(targetGPUs) > 0 && (i%len(targetGPUs)) < len(tileSizes) {
						tSize = tileSizes[i%len(targetGPUs)]
					}
					sess, err := sr.NewSingleGPUSession(engine, srPath, devID, tSize, nativeScale)
					if err != nil {
						emit(jsonVal, Event{Type: "error", Message: fmt.Sprintf("Failed to load %s: %v", srPath, err)})
						os.Exit(1)
					}
					defer sess.Close()
					if i == 0 {
						singleSess = sess
					} else {
						extraSessions = append(extraSessions, sess)
					}
				}
			}

			outFile := outputPath
			if outFile == "" {
				outFile = strings.TrimSuffix(inPath, filepath.Ext(inPath)) + "_enhanced" + inExt
			}

			streamOpt := stream.VideoStreamOptions{
				InputPath:     inPath,
				OutputPath:    outFile,
				Mode:          modeVal,
				Scale:         scaleVal,
				Fidelity:      fidelityVal,
				TileSize:      tileSizes[0],
				Encoder:       encoderVal,
				CRF:           crfVal,
				Preset:        presetVal,
				JSONMode:      jsonVal,
				LoadWorkers:   loadW,
				ProcWorkers:   procW,
				SaveWorkers:   saveW,
				Det:           det,
				CF:            cf,
				SingleSess:    singleSess,
				ExtraSessions: extraSessions,
				OnProgress: func(pct float64, current, total int, fps float64, msg string) {
					emit(jsonVal, Event{
						Type:    "progress",
						Phase:   "upscaling",
						Current: current,
						Total:   total,
						Percent: pct,
						Message: msg,
					})
				},
				OnLog: func(level, msg string) {
					emit(jsonVal, Event{
						Type:    level,
						Message: msg,
					})
				},
			}

			tVideoStart := time.Now()
			if err := stream.RunVideoStreamPipeline(context.Background(), streamOpt); err != nil {
				emit(jsonVal, Event{Type: "error", Message: err.Error()})
				os.Exit(1)
			}
			emit(jsonVal, Event{Type: "complete", Output: outFile, Elapsed: time.Since(tVideoStart).Seconds()})
			return
		}

		if modeVal == "sr" || modeVal == "all" {
			nativeScale := resolveSRModelNativeScale(modelName)
			srPath, err := findAsset(modelName, modelsDir)
			if err != nil {
				emit(jsonVal, Event{Type: "error", Message: err.Error()})
				os.Exit(1)
			}
			warnCb := func(msg string) {
				emit(jsonVal, Event{Type: "warn", Message: msg})
			}
			srModel, err = sr.NewSRModel(engine, srPath, targetGPUs, deviceDesc, nativeScale, warnCb)
			if err != nil {
				emit(jsonVal, Event{Type: "error", Message: fmt.Sprintf("Failed to load %s: %v", srPath, err)})
				os.Exit(1)
			}
			defer srModel.Close()
		}

		outFile := outputPath
		if outFile == "" {
			ext := filepath.Ext(inPath)
			if formatVal != "" {
				ext = "." + strings.TrimPrefix(formatVal, ".")
			}
			outFile = strings.TrimSuffix(inPath, filepath.Ext(inPath)) + "_enhanced" + ext
		}
		if err := processSingleImage(inPath, outFile, modeVal, scaleVal, fidelityVal, tileSizes, ttaVal, formatVal, jsonVal, det, cf, srModel); err != nil {
			os.Exit(1)
		}
	}
}
