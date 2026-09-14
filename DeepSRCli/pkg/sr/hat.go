package sr

import (
	"fmt"
	"math"
	"sync"

	"deepsr-cli/pkg/ort"
)

type ProgressCallback func(current, total int, percent float64, message string)
type WarnCallback func(msg string)

func build2DWeightMask(w, h, pad int) []float32 {
	mask := make([]float32, w*h)
	if pad <= 0 {
		for i := range mask {
			mask[i] = 1.0
		}
		return mask
	}

	for y := 0; y < h; y++ {
		wy := float32(1.0)
		if y < pad {
			wy = float32(y+1) / float32(pad+1)
		} else if y >= h-pad {
			wy = float32(h-y) / float32(pad+1)
		}
		for x := 0; x < w; x++ {
			wx := float32(1.0)
			if x < pad {
				wx = float32(x+1) / float32(pad+1)
			} else if x >= w-pad {
				wx = float32(w-x) / float32(pad+1)
			}
			mask[y*w+x] = wx * wy
		}
	}
	return mask
}

type SingleGPUSession struct {
	mu          sync.Mutex
	engine      *ort.Engine
	session     ort.Session
	devID       int
	tileSize    int
	NativeScale int
	scale       int

	// 直推张量缓存
	lastW    int
	lastH    int
	chwInBuf []float32
	tInp     ort.Value

	// 切块张量缓存
	tileBuf  []float32
	tileTInp ort.Value
	fixedIn  int
}

func NewSingleGPUSession(engine *ort.Engine, modelPath string, devID int, tileSize int, scale int) (*SingleGPUSession, error) {
	if tileSize < 0 {
		tileSize = 0
	}
	if scale <= 0 {
		scale = 4
	}

	var sess ort.Session
	var err error
	if devID >= 0 {
		sess, err = engine.CreateSession(modelPath, true, devID)
	} else {
		sess, err = engine.CreateSession(modelPath, false, 0)
	}
	if err != nil {
		sess, err = engine.CreateSession(modelPath, false, 0)
		if err != nil {
			return nil, err
		}
	}

	s := &SingleGPUSession{
		engine:      engine,
		session:     sess,
		devID:       devID,
		tileSize:    tileSize,
		NativeScale: scale,
		scale:       scale,
	}

	if tileSize >= 32 {
		fixedIn := tileSize
		if fixedIn%16 != 0 {
			fixedIn = ((fixedIn / 16) + 1) * 16
		}
		s.fixedIn = fixedIn
		s.tileBuf = make([]float32, 1*3*fixedIn*fixedIn)
		tInp, err := engine.CreateTensorFloat32([]int64{1, 3, int64(fixedIn), int64(fixedIn)}, s.tileBuf)
		if err == nil {
			s.tileTInp = tInp
		}
	}

	return s, nil
}

func (s *SingleGPUSession) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.tInp != 0 {
		s.engine.ReleaseValue(s.tInp)
		s.tInp = 0
	}
	if s.tileTInp != 0 {
		s.engine.ReleaseValue(s.tileTInp)
		s.tileTInp = 0
	}
	if s.session != 0 {
		s.engine.ReleaseSession(s.session)
		s.session = 0
	}
}

// SuperResolveDirect 根据用户规则执行直推或切块推图
func (s *SingleGPUSession) SuperResolveDirect(imgRGB []float32, imgW, imgH int, tilePad int) ([]float32, int, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 用户显式指定了切块 (-t > 0)，100% 严格执行切块
	if s.tileSize > 0 {
		return s.superResolveTiled(imgRGB, imgW, imgH, s.tileSize, tilePad)
	}

	// 智能自适应模式 (tileSize <= 0):
	// <= 1080p (小于等于 1080p) 原生整图直推
	if imgW <= 1920 && imgH <= 1080 && imgW*imgH <= 1920*1080 {
		res, w, h, err := s.directInference(imgRGB, imgW, imgH)
		if err == nil {
			return res, w, h, nil
		}
		// 若直推失败则降级到梯级切块
	}

	// > 1080p 或直推遇显存不足：启动 512 -> 256 -> 128 梯级降级切块
	fallbackTiers := []int{512, 256, 128}
	var lastErr error
	for _, tier := range fallbackTiers {
		res, w, h, err := s.superResolveTiled(imgRGB, imgW, imgH, tier, tilePad)
		if err == nil {
			return res, w, h, nil
		}
		lastErr = err
	}

	// 128 依然失败，直接抛出底层内部原始错误信息
	return nil, 0, 0, lastErr
}

func (s *SingleGPUSession) directInference(imgRGB []float32, imgW, imgH int) ([]float32, int, int, error) {
	scale := s.scale
	outW := imgW * scale
	outH := imgH * scale

	padW := imgW
	padH := imgH
	if padW%16 != 0 {
		padW = ((padW / 16) + 1) * 16
	}
	if padH%16 != 0 {
		padH = ((padH / 16) + 1) * 16
	}

	padOutW := padW * scale
	padOutH := padH * scale

	inLen := 1 * 3 * padH * padW
	outLen := 1 * 3 * padOutH * padOutW

	if s.lastW != padW || s.lastH != padH || s.tInp == 0 {
		if s.tInp != 0 {
			s.engine.ReleaseValue(s.tInp)
			s.tInp = 0
		}
		s.chwInBuf = make([]float32, inLen)
		tInp, err := s.engine.CreateTensorFloat32([]int64{1, 3, int64(padH), int64(padW)}, s.chwInBuf)
		if err != nil {
			return nil, 0, 0, err
		}
		s.tInp = tInp
		s.lastW = padW
		s.lastH = padH
	}

	padHW := padH * padW
	c0 := s.chwInBuf[0*padHW : 1*padHW]
	c1 := s.chwInBuf[1*padHW : 2*padHW]
	c2 := s.chwInBuf[2*padHW : 3*padHW]

	if padW == imgW && padH == imgH {
		hw := imgH * imgW
		for i := 0; i < hw; i++ {
			srcIdx := i * 3
			c0[i] = imgRGB[srcIdx+0]
			c1[i] = imgRGB[srcIdx+1]
			c2[i] = imgRGB[srcIdx+2]
		}
	} else {
		for y := 0; y < padH; y++ {
			srcY := y
			if srcY >= imgH {
				srcY = imgH - 1
			}
			for x := 0; x < padW; x++ {
				srcX := x
				if srcX >= imgW {
					srcX = imgW - 1
				}
				dstIdx := y*padW + x
				srcIdx := (srcY*imgW + srcX) * 3
				c0[dstIdx] = imgRGB[srcIdx+0]
				c1[dstIdx] = imgRGB[srcIdx+1]
				c2[dstIdx] = imgRGB[srcIdx+2]
			}
		}
	}

	outs, err := s.engine.Run(s.session, []string{"input"}, []ort.Value{s.tInp}, []string{"output"})
	if err != nil {
		if s.tInp != 0 {
			s.engine.ReleaseValue(s.tInp)
			s.tInp = 0
		}
		return nil, 0, 0, err
	}

	rawOut, err := s.engine.GetTensorDataFloat32(outs[0], outLen)
	s.engine.ReleaseValue(outs[0])
	if err != nil {
		return nil, 0, 0, err
	}

	outRGB := make([]float32, outW*outH*3)
	padOutHW := padOutH * padOutW
	rc0 := rawOut[0*padOutHW : 1*padOutHW]
	rc1 := rawOut[1*padOutHW : 2*padOutHW]
	rc2 := rawOut[2*padOutHW : 3*padOutHW]

	if padOutW == outW && padOutH == outH {
		hw := outH * outW
		for i := 0; i < hw; i++ {
			r := rc0[i]
			g := rc1[i]
			b := rc2[i]
			if r < 0 {
				r = 0
			} else if r > 1 {
				r = 1
			}
			if g < 0 {
				g = 0
			} else if g > 1 {
				g = 1
			}
			if b < 0 {
				b = 0
			} else if b > 1 {
				b = 1
			}
			dstIdx := i * 3
			outRGB[dstIdx+0] = r
			outRGB[dstIdx+1] = g
			outRGB[dstIdx+2] = b
		}
	} else {
		for y := 0; y < outH; y++ {
			for x := 0; x < outW; x++ {
				srcIdx := y*padOutW + x
				dstIdx := (y*outW + x) * 3

				r := rc0[srcIdx]
				g := rc1[srcIdx]
				b := rc2[srcIdx]
				if r < 0 {
					r = 0
				} else if r > 1 {
					r = 1
				}
				if g < 0 {
					g = 0
				} else if g > 1 {
					g = 1
				}
				if b < 0 {
					b = 0
				} else if b > 1 {
					b = 1
				}
				outRGB[dstIdx+0] = r
				outRGB[dstIdx+1] = g
				outRGB[dstIdx+2] = b
			}
		}
	}

	return outRGB, outW, outH, nil
}

func (s *SingleGPUSession) superResolveTiled(imgRGB []float32, imgW, imgH int, tileSize int, tilePad int) ([]float32, int, int, error) {
	scale := s.scale
	outW := imgW * scale
	outH := imgH * scale

	if tileSize < 32 {
		tileSize = 256
	}
	fixedIn := tileSize
	if fixedIn%16 != 0 {
		fixedIn = ((fixedIn / 16) + 1) * 16
	}

	if s.fixedIn != fixedIn || s.tileTInp == 0 {
		if s.tileTInp != 0 {
			s.engine.ReleaseValue(s.tileTInp)
			s.tileTInp = 0
		}
		s.fixedIn = fixedIn
		s.tileBuf = make([]float32, 1*3*fixedIn*fixedIn)
		tInp, err := s.engine.CreateTensorFloat32([]int64{1, 3, int64(fixedIn), int64(fixedIn)}, s.tileBuf)
		if err != nil {
			return nil, 0, 0, err
		}
		s.tileTInp = tInp
	}

	fixedOut := fixedIn * scale

	step := fixedIn - 2*tilePad
	if step <= 0 {
		step = fixedIn / 2
		tilePad = fixedIn / 4
	}

	tilesX := int(math.Ceil(float64(imgW) / float64(step)))
	tilesY := int(math.Ceil(float64(imgH) / float64(step)))

	outRGB := make([]float32, outW*outH*3)
	weightMask := make([]float32, outW*outH)
	tileWeight := build2DWeightMask(fixedOut, fixedOut, tilePad*scale)

	for ty := 0; ty < tilesY; ty++ {
		for tx := 0; tx < tilesX; tx++ {
			vx0 := tx * step
			vy0 := ty * step

			inX0 := vx0 - tilePad
			inY0 := vy0 - tilePad

			for y := 0; y < fixedIn; y++ {
				srcY := inY0 + y
				if srcY < 0 {
					srcY = -srcY
				}
				if srcY >= imgH {
					srcY = 2*imgH - 1 - srcY
					if srcY < 0 {
						srcY = 0
					}
					if srcY >= imgH {
						srcY = imgH - 1
					}
				}
				for x := 0; x < fixedIn; x++ {
					srcX := inX0 + x
					if srcX < 0 {
						srcX = -srcX
					}
					if srcX >= imgW {
						srcX = 2*imgW - 1 - srcX
						if srcX < 0 {
							srcX = 0
						}
						if srcX >= imgW {
							srcX = imgW - 1
						}
					}
					srcIdx := (srcY*imgW + srcX) * 3
					s.tileBuf[0*fixedIn*fixedIn+y*fixedIn+x] = imgRGB[srcIdx+0]
					s.tileBuf[1*fixedIn*fixedIn+y*fixedIn+x] = imgRGB[srcIdx+1]
					s.tileBuf[2*fixedIn*fixedIn+y*fixedIn+x] = imgRGB[srcIdx+2]
				}
			}

			outs, err := s.engine.Run(s.session, []string{"input"}, []ort.Value{s.tileTInp}, []string{"output"})
			if err != nil {
				return nil, 0, 0, err
			}

			rawOut, err := s.engine.GetTensorDataFloat32(outs[0], 1*3*fixedOut*fixedOut)
			s.engine.ReleaseValue(outs[0])
			if err != nil {
				return nil, 0, 0, err
			}

			dstBaseX := inX0 * scale
			dstBaseY := inY0 * scale

			for cy := 0; cy < fixedOut; cy++ {
				dstY := dstBaseY + cy
				if dstY < 0 || dstY >= outH {
					continue
				}
				for cx := 0; cx < fixedOut; cx++ {
					dstX := dstBaseX + cx
					if dstX < 0 || dstX >= outW {
						continue
					}

					wVal := tileWeight[cy*fixedOut+cx]
					dstIdx := (dstY*outW + dstX) * 3
					srcIdx := cy*fixedOut + cx

					r := rawOut[0*fixedOut*fixedOut+srcIdx]
					g := rawOut[1*fixedOut*fixedOut+srcIdx]
					b := rawOut[2*fixedOut*fixedOut+srcIdx]

					if r < 0 {
						r = 0
					} else if r > 1 {
						r = 1
					}
					if g < 0 {
						g = 0
					} else if g > 1 {
						g = 1
					}
					if b < 0 {
						b = 0
					} else if b > 1 {
						b = 1
					}

					outRGB[dstIdx+0] += r * wVal
					outRGB[dstIdx+1] += g * wVal
					outRGB[dstIdx+2] += b * wVal
					weightMask[dstY*outW+dstX] += wVal
				}
			}
		}
	}

	for i := 0; i < outW*outH; i++ {
		w := weightMask[i]
		if w > 0 {
			outRGB[i*3+0] /= w
			outRGB[i*3+1] /= w
			outRGB[i*3+2] /= w
		}
	}
	return outRGB, outW, outH, nil
}

type SRModel struct {
	engine      *ort.Engine
	modelPath   string
	gpuIDs      []int
	DeviceDesc  string
	NativeScale int
	WarnCb      WarnCallback

	// 常驻会话池 (Session Pooling: 消除单图/批量超分重复初始化 DirectML 的昂贵开销)
	sessions map[int]ort.Session
	sessMu   sync.Mutex
}

func NewSRModel(engine *ort.Engine, modelPath string, gpuIDs []int, deviceDesc string, nativeScale int, warnCb WarnCallback) (*SRModel, error) {
	if nativeScale <= 0 {
		nativeScale = 4
	}
	return &SRModel{
		engine:      engine,
		modelPath:   modelPath,
		gpuIDs:      gpuIDs,
		DeviceDesc:  deviceDesc,
		NativeScale: nativeScale,
		WarnCb:      warnCb,
		sessions:    make(map[int]ort.Session),
	}, nil
}

func (s *SRModel) getOrCreateSession(devID int) (ort.Session, error) {
	s.sessMu.Lock()
	defer s.sessMu.Unlock()

	if sess, exists := s.sessions[devID]; exists && sess != 0 {
		return sess, nil
	}

	var sess ort.Session
	var err error
	if devID >= 0 {
		sess, err = s.engine.CreateSession(s.modelPath, true, devID)
		if err != nil {
			if s.WarnCb != nil {
				s.WarnCb(fmt.Sprintf("GPU %d 显存不足或 DirectML 初始化失败 (%v)，已降级至 CPU 模式", devID, err))
			}
			sess, err = s.engine.CreateSession(s.modelPath, false, 0)
			if err != nil {
				return 0, err
			}
		}
	} else {
		sess, err = s.engine.CreateSession(s.modelPath, false, 0)
		if err != nil {
			return 0, err
		}
	}

	s.sessions[devID] = sess
	return sess, nil
}

func (s *SRModel) Close() {
	s.sessMu.Lock()
	defer s.sessMu.Unlock()

	for devID, sess := range s.sessions {
		if sess != 0 {
			s.engine.ReleaseSession(sess)
		}
		delete(s.sessions, devID)
	}
}

type TileTask struct {
	Index   int
	TX      int
	TY      int
	ValidX0 int
	ValidY0 int
	ValidX1 int
	ValidY1 int
}

func (s *SRModel) runMultiGPU(targetGPUs []int, imgRGB []float32, imgW, imgH int, tileSize int, tilePad int, onProgress ProgressCallback) ([]float32, int, int, error) {
	scale := s.NativeScale
	outW := imgW * scale
	outH := imgH * scale

	baseTileSize := tileSize
	if baseTileSize < 32 {
		baseTileSize = 256
	}

	step := baseTileSize - 2*tilePad
	if step <= 0 {
		step = baseTileSize / 2
		tilePad = baseTileSize / 4
	}

	tilesX := int(math.Ceil(float64(imgW) / float64(step)))
	tilesY := int(math.Ceil(float64(imgH) / float64(step)))
	totalTiles := tilesX * tilesY

	outRGB := make([]float32, outW*outH*3)
	weightMask := make([]float32, outW*outH)
	var canvasMu sync.Mutex

	taskChan := make(chan TileTask, totalTiles)
	idx := 0
	for ty := 0; ty < tilesY; ty++ {
		for tx := 0; tx < tilesX; tx++ {
			idx++
			vx0 := tx * step
			vy0 := ty * step
			vx1 := int(math.Min(float64(vx0+step), float64(imgW)))
			vy1 := int(math.Min(float64(vy0+step), float64(imgH)))
			taskChan <- TileTask{
				Index:   idx,
				TX:      tx,
				TY:      ty,
				ValidX0: vx0,
				ValidY0: vy0,
				ValidX1: vx1,
				ValidY1: vy1,
			}
		}
	}
	close(taskChan)

	var wg sync.WaitGroup
	var completedCount int
	var countMu sync.Mutex
	var workerErr error
	var errOnce sync.Once

	tileWeight := build2DWeightMask(baseTileSize*scale, baseTileSize*scale, tilePad*scale)

	for _, devID := range targetGPUs {
		wg.Add(1)

		go func(dID int, fixedIn int) {
			defer wg.Done()

			if fixedIn%16 != 0 {
				fixedIn = ((fixedIn / 16) + 1) * 16
			}

			sess, err := s.getOrCreateSession(dID)
			if err != nil {
				errOnce.Do(func() { workerErr = err })
				return
			}

			tileBuf := make([]float32, 1*3*fixedIn*fixedIn)
			tInp, err := s.engine.CreateTensorFloat32([]int64{1, 3, int64(fixedIn), int64(fixedIn)}, tileBuf)
			if err != nil {
				errOnce.Do(func() { workerErr = err })
				return
			}
			defer s.engine.ReleaseValue(tInp)

			fixedOut := fixedIn * scale

			for task := range taskChan {
				inX0 := task.ValidX0 - tilePad
				inY0 := task.ValidY0 - tilePad

				for y := 0; y < fixedIn; y++ {
					srcY := inY0 + y
					if srcY < 0 {
						srcY = -srcY
					}
					if srcY >= imgH {
						srcY = 2*imgH - 1 - srcY
						if srcY < 0 {
							srcY = 0
						}
						if srcY >= imgH {
							srcY = imgH - 1
						}
					}

					for x := 0; x < fixedIn; x++ {
						srcX := inX0 + x
						if srcX < 0 {
							srcX = -srcX
						}
						if srcX >= imgW {
							srcX = 2*imgW - 1 - srcX
							if srcX < 0 {
								srcX = 0
							}
							if srcX >= imgW {
								srcX = imgW - 1
							}
						}

						srcIdx := (srcY*imgW + srcX) * 3
						tileBuf[0*fixedIn*fixedIn+y*fixedIn+x] = imgRGB[srcIdx+0]
						tileBuf[1*fixedIn*fixedIn+y*fixedIn+x] = imgRGB[srcIdx+1]
						tileBuf[2*fixedIn*fixedIn+y*fixedIn+x] = imgRGB[srcIdx+2]
					}
				}

				outs, err := s.engine.Run(sess, []string{"input"}, []ort.Value{tInp}, []string{"output"})
				if err != nil {
					errOnce.Do(func() { workerErr = err })
					return
				}

				rawOut, err := s.engine.GetTensorDataFloat32(outs[0], 1*3*fixedOut*fixedOut)
				s.engine.ReleaseValue(outs[0])
				if err != nil {
					errOnce.Do(func() { workerErr = err })
					return
				}

				dstBaseX := inX0 * scale
				dstBaseY := inY0 * scale

				canvasMu.Lock()
				for cy := 0; cy < fixedOut; cy++ {
					dstY := dstBaseY + cy
					if dstY < 0 || dstY >= outH {
						continue
					}
					for cx := 0; cx < fixedOut; cx++ {
						dstX := dstBaseX + cx
						if dstX < 0 || dstX >= outW {
							continue
						}

						wVal := tileWeight[cy*fixedOut+cx]
						dstIdx := (dstY*outW + dstX) * 3
						srcIdx := cy*fixedOut + cx

						r := rawOut[0*fixedOut*fixedOut+srcIdx]
						g := rawOut[1*fixedOut*fixedOut+srcIdx]
						b := rawOut[2*fixedOut*fixedOut+srcIdx]

						if r < 0 {
							r = 0
						} else if r > 1 {
							r = 1
						}
						if g < 0 {
							g = 0
						} else if g > 1 {
							g = 1
						}
						if b < 0 {
							b = 0
						} else if b > 1 {
							b = 1
						}

						outRGB[dstIdx+0] += r * wVal
						outRGB[dstIdx+1] += g * wVal
						outRGB[dstIdx+2] += b * wVal
						weightMask[dstY*outW+dstX] += wVal
					}
				}
				canvasMu.Unlock()

				countMu.Lock()
				completedCount++
				curCount := completedCount
				countMu.Unlock()

				if onProgress != nil {
					pct := (float64(curCount) / float64(totalTiles)) * 100.0
					devName := fmt.Sprintf("GPU %d", dID)
					if dID < 0 {
						devName = "CPU"
					}
					onProgress(curCount, totalTiles, pct, fmt.Sprintf("Processing tile (%s)", devName))
				}
			}
		}(devID, baseTileSize)
	}

	wg.Wait()

	if workerErr != nil {
		return nil, 0, 0, workerErr
	}

	for i := 0; i < outW*outH; i++ {
		w := weightMask[i]
		if w > 0 {
			outRGB[i*3+0] /= w
			outRGB[i*3+1] /= w
			outRGB[i*3+2] /= w
		}
	}

	return outRGB, outW, outH, nil
}

// SuperResolve 单图超分入口
func (s *SRModel) SuperResolve(imgRGB []float32, imgW, imgH int, tileSizes []int, tilePad int, onProgress ProgressCallback) ([]float32, int, int, error) {
	activeTile := 0
	if len(tileSizes) > 0 && tileSizes[0] > 0 {
		activeTile = tileSizes[0]
	}

	// 用户显式指定了 -t > 0，严格按用户尺寸执行切块推图
	if activeTile > 0 {
		return s.runMultiGPU(s.gpuIDs, imgRGB, imgW, imgH, activeTile, tilePad, onProgress)
	}

	// 智能自适应模式 (tileSize <= 0):
	// <= 1080p 原生整图直推
	if imgW <= 1920 && imgH <= 1080 && imgW*imgH <= 1920*1080 && len(s.gpuIDs) <= 1 {
		devID := -1
		if len(s.gpuIDs) > 0 {
			devID = s.gpuIDs[0]
		}
		singleSess, err := NewSingleGPUSession(s.engine, s.modelPath, devID, 0, s.NativeScale)
		if err == nil {
			defer singleSess.Close()
			if onProgress != nil {
				onProgress(0, 1, 0, "AI 原生全图推演中...")
			}
			outRGB, outW, outH, sErr := singleSess.SuperResolveDirect(imgRGB, imgW, imgH, tilePad)
			if sErr == nil {
				if onProgress != nil {
					onProgress(1, 1, 100.0, "AI 全图推演完成")
				}
				return outRGB, outW, outH, nil
			}
		}
	}

	// > 1080p 或直推失败：启动 512 -> 256 -> 128 梯级降级切块
	fallbackTiers := []int{512, 256, 128}
	var lastErr error
	for _, tier := range fallbackTiers {
		res, w, h, err := s.runMultiGPU(s.gpuIDs, imgRGB, imgW, imgH, tier, tilePad, onProgress)
		if err == nil {
			return res, w, h, nil
		}
		lastErr = err
	}

	// 128 依然失败，直接抛出底层内部原始错误信息
	return nil, 0, 0, lastErr
}

func flipH(src []float32, w, h int) []float32 {
	dst := make([]float32, len(src))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			srcIdx := (y*w + x) * 3
			dstIdx := (y*w + (w - 1 - x)) * 3
			dst[dstIdx+0] = src[srcIdx+0]
			dst[dstIdx+1] = src[srcIdx+1]
			dst[dstIdx+2] = src[srcIdx+2]
		}
	}
	return dst
}

func flipV(src []float32, w, h int) []float32 {
	dst := make([]float32, len(src))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			srcIdx := (y*w + x) * 3
			dstIdx := ((h-1-y)*w + x) * 3
			dst[dstIdx+0] = src[srcIdx+0]
			dst[dstIdx+1] = src[srcIdx+1]
			dst[dstIdx+2] = src[srcIdx+2]
		}
	}
	return dst
}

func (s *SRModel) SuperResolveTTA(imgRGB []float32, imgW, imgH int, tileSizes []int, tilePad int, onProgress ProgressCallback) ([]float32, int, int, error) {
	scale := s.NativeScale
	outW := imgW * scale
	outH := imgH * scale
	accum := make([]float32, outW*outH*3)

	res, _, _, err := s.SuperResolve(imgRGB, imgW, imgH, tileSizes, tilePad, onProgress)
	if err != nil {
		return nil, 0, 0, err
	}
	for i := range accum {
		accum[i] += res[i]
	}

	resH, _, _, _ := s.SuperResolve(flipH(imgRGB, imgW, imgH), imgW, imgH, tileSizes, tilePad, nil)
	resH = flipH(resH, outW, outH)
	for i := range accum {
		accum[i] += resH[i]
	}

	resV, _, _, _ := s.SuperResolve(flipV(imgRGB, imgW, imgH), imgW, imgH, tileSizes, tilePad, nil)
	resV = flipV(resV, outW, outH)
	for i := range accum {
		accum[i] += resV[i]
	}

	resHV, _, _, _ := s.SuperResolve(flipV(flipH(imgRGB, imgW, imgH), imgW, imgH), imgW, imgH, tileSizes, tilePad, nil)
	resHV = flipH(flipV(resHV, outW, outH), outW, outH)
	for i := range accum {
		accum[i] += resHV[i]
	}

	for i := range accum {
		accum[i] /= 4.0
	}
	return accum, outW, outH, nil
}
