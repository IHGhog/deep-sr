package stream

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"deepsr-cli/pkg/align"
	"deepsr-cli/pkg/codeformer"
	"deepsr-cli/pkg/detector"
	"deepsr-cli/pkg/imgutil"
	"deepsr-cli/pkg/sr"
)

type MediaInfo struct {
	Width      int
	Height     int
	FrameRate  float64
	FrameCount int
	Duration   float64
	HasAudio   bool
}

type VideoStreamOptions struct {
	InputPath     string
	OutputPath    string
	Mode          string
	Scale         int
	Fidelity      float64
	TileSize      int
	Encoder       string
	CRF           int
	Preset        string
	JSONMode      bool
	LoadWorkers   int
	ProcWorkers   int
	SaveWorkers   int
	Det           *detector.RetinaFace
	CF            *codeformer.CodeFormer
	SingleSess    *sr.SingleGPUSession
	ExtraSessions []*sr.SingleGPUSession
	OnProgress    func(pct float64, current, total int, fps float64, msg string)
	OnLog         func(level, msg string)
}

func findBuiltinTool(name string) string {
	exePath, err := os.Executable()
	var exeDir string
	if err == nil {
		exeDir = filepath.Dir(exePath)
	}
	candidates := []string{
		filepath.Join(exeDir, name),
		name,
		filepath.Join(exeDir, "bin", name),
	}
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			abs, _ := filepath.Abs(c)
			return abs
		}
	}
	return name
}

type ffprobeStream struct {
	CodecType  string `json:"codec_type"`
	Width      int    `json:"width"`
	Height     int    `json:"height"`
	RFrameRate string `json:"r_frame_rate"`
	NbFrames   string `json:"nb_frames"`
}

type ffprobeFormat struct {
	Duration string `json:"duration"`
}

type ffprobeData struct {
	Streams []ffprobeStream `json:"streams"`
	Format  ffprobeFormat   `json:"format"`
}

func ProbeVideo(inPath string) (*MediaInfo, error) {
	bin := findBuiltinTool("ffprobe.exe")
	args := []string{
		"-v", "error",
		"-show_entries", "stream=width,height,r_frame_rate,nb_frames,codec_type",
		"-show_entries", "format=duration",
		"-of", "json",
		inPath,
	}

	cmd := exec.Command(bin, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("ffprobe 执行失败: %w", err)
	}

	info := &MediaInfo{
		Width:      1920,
		Height:     1080,
		FrameRate:  30.0,
		FrameCount: 0,
		Duration:   0.0,
		HasAudio:   false,
	}

	var data ffprobeData
	if err := json.Unmarshal(out, &data); err == nil {
		foundVideo := false
		for _, s := range data.Streams {
			if s.CodecType == "audio" {
				info.HasAudio = true
			}
			if s.CodecType == "video" && !foundVideo {
				foundVideo = true
				if s.Width > 0 {
					info.Width = s.Width
				}
				if s.Height > 0 {
					info.Height = s.Height
				}
				parts := strings.Split(strings.TrimSpace(s.RFrameRate), "/")
				if len(parts) == 2 {
					num, _ := strconv.ParseFloat(parts[0], 64)
					den, _ := strconv.ParseFloat(parts[1], 64)
					if den > 0 {
						info.FrameRate = num / den
					}
				} else if len(parts) == 1 {
					if fps, err := strconv.ParseFloat(parts[0], 64); err == nil && fps > 0 {
						info.FrameRate = fps
					}
				}
				if cnt, err := strconv.Atoi(strings.TrimSpace(s.NbFrames)); err == nil && cnt > 0 {
					info.FrameCount = cnt
				}
			}
		}
		if dur, err := strconv.ParseFloat(strings.TrimSpace(data.Format.Duration), 64); err == nil && dur > 0 {
			info.Duration = dur
		}
	}

	if info.FrameRate <= 0 || info.FrameRate > 240.0 {
		info.FrameRate = 25.0
	}
	if info.FrameCount <= 0 && info.Duration > 0 {
		info.FrameCount = int(math.Round(info.Duration * info.FrameRate))
	}
	if info.FrameCount <= 0 {
		info.FrameCount = 1
	}

	return info, nil
}

func checkRawVideoOutputSupport(ffmpegBin string) bool {
	cmd := exec.Command(ffmpegBin, "-muxers")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	return strings.Contains(string(out), " rawvideo ")
}

func nextJPEGFrameFast(br *bufio.Reader, buf []byte) ([]byte, error) {
	var prev byte
	for {
		b, err := br.ReadByte()
		if err != nil {
			return nil, err
		}
		if prev == 0xFF && b == 0xD8 {
			break
		}
		prev = b
	}

	buf = buf[:0]
	buf = append(buf, 0xFF, 0xD8)
	prev = 0xD8

	for {
		b, err := br.ReadByte()
		if err != nil {
			return nil, err
		}
		buf = append(buf, b)
		if prev == 0xFF && b == 0xD9 {
			return buf, nil
		}
		prev = b
	}
}

type rawFramePacket struct {
	index int
	raw   []byte
	w     int
	h     int
}

type decodedFrameTask struct {
	index int
	rgbF  []float32
	w     int
	h     int
}

type processedFrameTask struct {
	index      int
	srRGB      []float32
	w          int
	h          int
	finishedAt time.Time
}

type encodedFramePacket struct {
	index      int
	rawBytes   []byte
	finishedAt time.Time
}

func extractFFmpegCoreError(fullErr string) string {
	lines := strings.Split(fullErr, "\n")
	var coreLines []string
	seen := make(map[string]bool)
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" {
			continue
		}
		lower := strings.ToLower(trimmed)
		if strings.Contains(lower, "error") || strings.Contains(lower, "unknown") ||
			strings.Contains(lower, "invalid") || strings.Contains(lower, "failed") ||
			strings.Contains(lower, "not found") || strings.Contains(lower, "unsupported") ||
			strings.Contains(lower, "cannot open") || strings.Contains(lower, "could not find") ||
			strings.Contains(lower, "no such") {
			if !seen[trimmed] {
				seen[trimmed] = true
				coreLines = append(coreLines, trimmed)
			}
		}
	}
	if len(coreLines) > 0 {
		return strings.Join(coreLines, " | ")
	}
	var nonEmpties []string
	for _, l := range lines {
		t := strings.TrimSpace(l)
		if t != "" && !seen[t] {
			seen[t] = true
			nonEmpties = append(nonEmpties, t)
		}
	}
	if len(nonEmpties) > 2 {
		return strings.Join(nonEmpties[len(nonEmpties)-2:], " | ")
	}
	return strings.Join(nonEmpties, " ")
}

// tailBuffer 安全循环读取 pipe 保持最后 N 字节并排空管道，杜绝 Windows 管道缓冲区打满死锁
type tailBuffer struct {
	mu  sync.Mutex
	buf []byte
	max int
}

func newTailBuffer(max int) *tailBuffer {
	if max <= 0 {
		max = 16384
	}
	return &tailBuffer{max: max, buf: make([]byte, 0, max)}
}

func (tb *tailBuffer) drainFrom(r io.Reader) {
	tmp := make([]byte, 4096)
	for {
		n, err := r.Read(tmp)
		if n > 0 {
			tb.mu.Lock()
			tb.buf = append(tb.buf, tmp[:n]...)
			if len(tb.buf) > tb.max {
				tb.buf = tb.buf[len(tb.buf)-tb.max:]
			}
			tb.mu.Unlock()
		}
		if err != nil {
			return
		}
	}
}

func (tb *tailBuffer) String() string {
	tb.mu.Lock()
	defer tb.mu.Unlock()
	return string(tb.buf)
}

// RunVideoStreamPipeline 全内存无损流式超分主循环 (配合 load:proc:save 水位流控与 Re-order Buffer 保序窗口)
func RunVideoStreamPipeline(ctx context.Context, opt VideoStreamOptions) error {
	t0 := time.Now()
	ffmpegBin := findBuiltinTool("ffmpeg.exe")

	media, err := ProbeVideo(opt.InputPath)
	if err != nil {
		return fmt.Errorf("无法解析视频元数据: %w", err)
	}

	inW := media.Width
	inH := media.Height
	if inW <= 0 || inH <= 0 {
		return fmt.Errorf("无效的视频尺寸: %dx%d", inW, inH)
	}

	scale := opt.Scale
	if scale <= 0 {
		scale = 4
	}
	outW := inW * scale
	outH := inH * scale
	totalFrames := media.FrameCount

	loadWorkers := opt.LoadWorkers
	if loadWorkers <= 0 {
		loadWorkers = 2
	}
	procWorkers := opt.ProcWorkers
	if procWorkers <= 0 {
		procWorkers = 1
	}
	saveWorkers := opt.SaveWorkers
	if saveWorkers <= 0 {
		saveWorkers = 4
	}

	allSessions := []*sr.SingleGPUSession{}
	if opt.SingleSess != nil {
		allSessions = append(allSessions, opt.SingleSess)
	}
	for _, s := range opt.ExtraSessions {
		if s != nil {
			allSessions = append(allSessions, s)
		}
	}
	if len(allSessions) == 0 {
		return fmt.Errorf("未提供有效的超分会话")
	}

	actualProcWorkers := procWorkers
	if actualProcWorkers <= 0 {
		actualProcWorkers = 1
	}

	tileSize := opt.TileSize
	tileDesc := strconv.Itoa(tileSize)
	if tileSize <= 0 {
		tileDesc = "Auto"
	}

	supportRawDecodeOut := checkRawVideoOutputSupport(ffmpegBin)

	if opt.OnLog != nil {
		opt.OnLog("info", fmt.Sprintf("启动全内存流式管道: %dx%d -> %dx%d (共 %d 帧, %.2f FPS, 音轨=%v, 分块=%s, 线程池=%d:%d:%d)",
			inW, inH, outW, outH, totalFrames, media.FrameRate, media.HasAudio, tileDesc, loadWorkers, actualProcWorkers, saveWorkers))
	}

	// 1. 启动 FFmpeg 解码子进程
	var decodeArgs []string
	if supportRawDecodeOut {
		decodeArgs = []string{
			"-nostats",
			"-i", opt.InputPath,
			"-an",
			"-f", "rawvideo",
			"-pix_fmt", "rgb24",
			"pipe:1",
		}
	} else {
		decodeArgs = []string{
			"-nostats",
			"-i", opt.InputPath,
			"-an",
			"-f", "image2pipe",
			"-vcodec", "mjpeg",
			"-q:v", "1",
			"pipe:1",
		}
	}

	decodeCmd := exec.CommandContext(ctx, ffmpegBin, decodeArgs...)
	decodeCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	decodeOut, err := decodeCmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("创建 FFmpeg 解码管道失败: %w", err)
	}
	decodeErrPipe, _ := decodeCmd.StderrPipe()

	if err := decodeCmd.Start(); err != nil {
		return fmt.Errorf("启动 FFmpeg 解码器失败: %w", err)
	}
	defer func() {
		if decodeCmd.Process != nil {
			_ = decodeCmd.Process.Kill()
		}
	}()

	// 2. 启动 FFmpeg 编码子进程 (严格使用用户输入的编码器，不做任何兜底猜测)
	encoder := opt.Encoder
	if encoder == "" || encoder == "auto" {
		encoder = "libx264"
	}
	crf := opt.CRF
	if crf <= 0 {
		crf = 20
	}
	preset := opt.Preset
	if preset == "" {
		preset = "medium"
	}

	encodeArgs := []string{
		"-nostats",
		"-y",
		"-f", "rawvideo",
		"-pix_fmt", "rgb24",
		"-s", fmt.Sprintf("%dx%d", outW, outH),
		"-r", fmt.Sprintf("%.3f", media.FrameRate),
		"-i", "pipe:0",
	}

	if media.HasAudio {
		encodeArgs = append(encodeArgs,
			"-i", opt.InputPath,
			"-map", "0:v:0",
			"-map", "1:a?",
			"-c:a", "copy",
		)
	}

	encodeArgs = append(encodeArgs,
		"-c:v", encoder,
		"-crf", strconv.Itoa(crf),
		"-preset", preset,
		"-pix_fmt", "yuv420p",
		opt.OutputPath,
	)

	encodeCmd := exec.CommandContext(ctx, ffmpegBin, encodeArgs...)
	encodeCmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	encodeIn, err := encodeCmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("创建 FFmpeg 编码管道失败: %w", err)
	}
	encodeErrPipe, _ := encodeCmd.StderrPipe()

	if err := encodeCmd.Start(); err != nil {
		return fmt.Errorf("启动 FFmpeg 编码器失败: %w", err)
	}
	defer func() {
		if encodeCmd.Process != nil {
			_ = encodeCmd.Process.Kill()
		}
	}()

	decodeErrBuf := newTailBuffer(16384)
	encodeErrBuf := newTailBuffer(16384)
	if decodeErrPipe != nil {
		go decodeErrBuf.drainFrom(decodeErrPipe)
	}
	if encodeErrPipe != nil {
		go encodeErrBuf.drainFrom(encodeErrPipe)
	}

	loadQueueCap := loadWorkers * 4
	if loadQueueCap < 16 {
		loadQueueCap = 16
	}
	procQueueCap := actualProcWorkers * 4
	if procQueueCap < 16 {
		procQueueCap = 16
	}
	saveQueueCap := saveWorkers * 4
	if saveQueueCap < 16 {
		saveQueueCap = 16
	}
	orderQueueCap := saveWorkers * 4
	if orderQueueCap < 16 {
		orderQueueCap = 16
	}

	rawFrameChan := make(chan rawFramePacket, loadQueueCap)
	decodedChan := make(chan decodedFrameTask, procQueueCap)
	processedChan := make(chan processedFrameTask, saveQueueCap)
	orderedReadyChan := make(chan encodedFramePacket, orderQueueCap)
	errChan := make(chan error, 4)

	pipeCtx, pipeCancel := context.WithCancel(ctx)
	defer pipeCancel()

	// 内存池复用：避免每帧高频分配切片导致 Go GC 频繁卡顿
	rawInPool := sync.Pool{
		New: func() any {
			return make([]byte, inW*inH*3)
		},
	}
	floatInPool := sync.Pool{
		New: func() any {
			return make([]float32, inW*inH*3)
		},
	}
	rawOutPool := sync.Pool{
		New: func() any {
			return make([]byte, outW*outH*3)
		},
	}

	// Stage 1a: 独占单协程按帧顺序从 FFmpeg stdout (decodeOut) 管道读取裸流数据
	go func() {
		defer close(rawFrameChan)

		if supportRawDecodeOut {
			inFrameBytes := inW * inH * 3
			idx := 0
			for {
				if pipeCtx.Err() != nil {
					return
				}
				buf := rawInPool.Get().([]byte)
				if len(buf) != inFrameBytes {
					buf = make([]byte, inFrameBytes)
				}
				_, err := io.ReadFull(decodeOut, buf)
				if err != nil {
					rawInPool.Put(buf)
					break // EOF
				}
				idx++
				select {
				case rawFrameChan <- rawFramePacket{index: idx, raw: buf, w: inW, h: inH}:
				case <-pipeCtx.Done():
					rawInPool.Put(buf)
					return
				}
			}
		} else {
			br := bufio.NewReaderSize(decodeOut, 1024*1024)
			jBuf := make([]byte, 0, 512*1024)
			idx := 0
			for {
				if pipeCtx.Err() != nil {
					return
				}
				frameBytes, err := nextJPEGFrameFast(br, jBuf)
				if err != nil {
					break
				}
				dataCopy := make([]byte, len(frameBytes))
				copy(dataCopy, frameBytes)
				idx++
				select {
				case rawFrameChan <- rawFramePacket{index: idx, raw: dataCopy, w: inW, h: inH}:
				case <-pipeCtx.Done():
					return
				}
			}
		}
	}()

	// Stage 1b: loadWorkers 个并发协程在 CPU 上异步将 rawBytes 转换为 float32 张量（0 停等投喂 GPU）
	var loadWg sync.WaitGroup
	for lIdx := 0; lIdx < loadWorkers; lIdx++ {
		loadWg.Add(1)
		go func() {
			defer loadWg.Done()
			for packet := range rawFrameChan {
				if pipeCtx.Err() != nil {
					return
				}
				var floatBuf []float32
				if supportRawDecodeOut {
					floatBuf = floatInPool.Get().([]float32)
					if len(floatBuf) != packet.w*packet.h*3 {
						floatBuf = make([]float32, packet.w*packet.h*3)
					}
					imgutil.RawBytesToFloatRGB(packet.raw, packet.w, packet.h, floatBuf)
					rawInPool.Put(packet.raw)
				} else {
					rgbIn, w, h, err := imgutil.DecodeImageFromReader(bytes.NewReader(packet.raw))
					if err != nil {
						continue
					}
					floatBuf = rgbIn
					packet.w = w
					packet.h = h
				}

				select {
				case decodedChan <- decodedFrameTask{index: packet.index, rgbF: floatBuf, w: packet.w, h: packet.h}:
				case <-pipeCtx.Done():
					return
				}
			}
		}()
	}

	go func() {
		loadWg.Wait()
		close(decodedChan)
	}()

	// Stage 2: actualProcWorkers 个并发 GPU 推理池 (支持单卡多 Worker / 多卡多 Worker)
	var procWg sync.WaitGroup
	var cfMu sync.Mutex
	var detMu sync.Mutex

	for wIdx := 0; wIdx < actualProcWorkers; wIdx++ {
		procWg.Add(1)
		currSess := allSessions[wIdx%len(allSessions)]

		go func(workerID int, sess *sr.SingleGPUSession) {
			defer procWg.Done()

			for task := range decodedChan {
				if pipeCtx.Err() != nil {
					return
				}

				curRGB := task.rgbF
				curW := task.w
				curH := task.h

				if opt.Mode == "sr" || opt.Mode == "all" {
					srRGB, srW, srH, err := sess.SuperResolveDirect(curRGB, curW, curH, 16)
					// 回收输入 float32 buffer
					if supportRawDecodeOut && len(curRGB) == inW*inH*3 {
						floatInPool.Put(curRGB)
					}
					if err != nil {
						select {
						case errChan <- fmt.Errorf("流式超分第 %d 帧失败: %w", task.index, err):
						default:
						}
						pipeCancel()
						return
					}
					if scale > 0 && scale != sess.NativeScale {
						targetW := inW * scale
						targetH := inH * scale
						srRGB = imgutil.Resize(srRGB, srW, srH, targetW, targetH)
						srW = targetW
						srH = targetH
					}
					curRGB = srRGB
					curW = srW
					curH = srH
				}

				if (opt.Mode == "face" || opt.Mode == "all") && opt.Det != nil && opt.CF != nil {
					detMu.Lock()
					faces, err := opt.Det.Detect(curRGB, curW, curH, 0.5, 0.4)
					detMu.Unlock()
					if err == nil && len(faces) > 0 {
						cfMu.Lock()
						for _, face := range faces {
							forM, invM := align.EstimateSimilarityTransform(face.Landmarks, align.FaceTemplate512)
							croppedFace := align.WarpAffine(curRGB, curW, curH, invM, 512, 512)
							restoredFace, err := opt.CF.RestoreFace(croppedFace, float32(opt.Fidelity))
							if err == nil {
								align.PasteFaceBack(curRGB, curW, curH, restoredFace, 512, 512, forM, invM)
							}
						}
						cfMu.Unlock()
					}
				}

				select {
				case processedChan <- processedFrameTask{
					index:      task.index,
					srRGB:      curRGB,
					w:          curW,
					h:          curH,
					finishedAt: time.Now(),
				}:
				case <-pipeCtx.Done():
					return
				}
			}
		}(wIdx, currSess)
	}

	go func() {
		procWg.Wait()
		close(processedChan)
	}()

	// Stage 3: saveWorkers 个并发协程在 CPU 上执行 FloatRGBToRawBytes
	var saveWg sync.WaitGroup
	for sIdx := 0; sIdx < saveWorkers; sIdx++ {
		saveWg.Add(1)
		go func() {
			defer saveWg.Done()

			for task := range processedChan {
				if pipeCtx.Err() != nil {
					return
				}

				rawOut := rawOutPool.Get().([]byte)
				if len(rawOut) != outW*outH*3 {
					rawOut = make([]byte, outW*outH*3)
				}
				imgutil.FloatRGBToRawBytes(task.srRGB, task.w, task.h, rawOut)

				select {
				case orderedReadyChan <- encodedFramePacket{
					index:      task.index,
					rawBytes:   rawOut,
					finishedAt: task.finishedAt,
				}:
				case <-pipeCtx.Done():
					rawOutPool.Put(rawOut)
					return
				}
			}
		}()
	}

	go func() {
		saveWg.Wait()
		close(orderedReadyChan)
	}()

	// Stage 4: 独占单协程 Re-order Buffer 保序排序与 FFmpeg pipe:0 连续写入
	nextSeq := 1
	pendingMap := make(map[int]encodedFramePacket)
	curFrame := 0
	pipeStartTime := time.Now()
	const rollingWindowSize = 30
	var recentTimes []time.Time
	var writeErr error

	for item := range orderedReadyChan {
		pendingMap[item.index] = item

		for {
			ready, exists := pendingMap[nextSeq]
			if !exists {
				break
			}
			delete(pendingMap, nextSeq)

			if writeErr == nil {
				if _, err := encodeIn.Write(ready.rawBytes); err != nil {
					time.Sleep(50 * time.Millisecond)
					errMsg := strings.TrimSpace(encodeErrBuf.String())
					if errMsg != "" {
						writeErr = fmt.Errorf("FFmpeg encoder error: %s", extractFFmpegCoreError(errMsg))
					} else {
						writeErr = fmt.Errorf("FFmpeg pipe write failed on frame %d: %w", ready.index, err)
					}
					pipeCancel()
				}
			}

			// 回收输出内存切片到池中
			rawOutPool.Put(ready.rawBytes)

			curFrame++
			nextSeq++

			if opt.OnProgress != nil && writeErr == nil {
				totalElapsed := time.Since(pipeStartTime).Seconds()
				speedFPS := 0.0

				// 维护最近 N 帧由 GPU 实际完成推理的时间戳窗口
				if !ready.finishedAt.IsZero() {
					recentTimes = append(recentTimes, ready.finishedAt)
					if len(recentTimes) > rollingWindowSize {
						recentTimes = recentTimes[1:]
					}
				}

				// 若窗口样本充足（>=10 帧）且跨度大于 0.5s，依据 GPU 实际出帧时差计算真实平滑帧率
				if len(recentTimes) >= 10 {
					windowDuration := recentTimes[len(recentTimes)-1].Sub(recentTimes[0]).Seconds()
					if windowDuration >= 0.5 {
						speedFPS = float64(len(recentTimes)-1) / windowDuration
					}
				}

				// 样本不足或发生短时间密集冲刷时，回退至全局真实耗时测速兜底，杜绝虚高尖峰
				if speedFPS <= 0.0 && totalElapsed > 0.1 && curFrame > 0 {
					speedFPS = float64(curFrame) / totalElapsed
				}

				displayTotal := totalFrames
				if curFrame > displayTotal {
					displayTotal = curFrame
				}
				pct := (float64(curFrame) / float64(displayTotal)) * 100.0
				if pct > 99.0 && curFrame < displayTotal {
					pct = 99.0
				}
				if pct > 100.0 {
					pct = 100.0
				}
				msg := fmt.Sprintf("流式超分: 帧 %d / %d (%.2f FPS)", curFrame, displayTotal, speedFPS)
				opt.OnProgress(pct, curFrame, displayTotal, speedFPS, msg)
			}
		}
	}

	if writeErr != nil {
		return writeErr
	}

	select {
	case err := <-errChan:
		if err != nil {
			return err
		}
	default:
	}

	_ = encodeIn.Close()
	if err := encodeCmd.Wait(); err != nil {
		errMsg := strings.TrimSpace(encodeErrBuf.String())
		if errMsg != "" {
			return fmt.Errorf("FFmpeg encoder error: %s", extractFFmpegCoreError(errMsg))
		}
		return fmt.Errorf("FFmpeg encoder error: %w", err)
	}

	_ = decodeCmd.Wait()
	if curFrame == 0 && strings.TrimSpace(decodeErrBuf.String()) != "" {
		return fmt.Errorf("FFmpeg decoder error: %s", extractFFmpegCoreError(decodeErrBuf.String()))
	}

	totalElapsed := time.Since(t0).Seconds()
	if curFrame > 0 && opt.OnProgress != nil {
		speedFPS := 0.0
		if totalElapsed > 0 {
			speedFPS = float64(curFrame) / totalElapsed
		}
		msg := fmt.Sprintf("流式超分完成: 共 %d 帧 (%.2f FPS)", curFrame, speedFPS)
		opt.OnProgress(100.0, curFrame, curFrame, speedFPS, msg)
	}
	if opt.OnLog != nil {
		opt.OnLog("success", fmt.Sprintf("全内存流式管道处理完成: 共 %d 帧, 总耗时 %.2f 秒 (平均 %.2f FPS)",
			curFrame, totalElapsed, float64(curFrame)/totalElapsed))
	}

	return nil
}
