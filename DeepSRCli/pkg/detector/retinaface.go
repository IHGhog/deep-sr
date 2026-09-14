package detector

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"deepsr-cli/pkg/ort"
)

type Point2D struct {
	X float32
	Y float32
}

type FaceInfo struct {
	Box       [4]float32 // x1, y1, x2, y2
	Score     float32
	Landmarks [5]Point2D // left_eye, right_eye, nose, left_mouth, right_mouth
}

type PriorBox struct {
	CX float32
	CY float32
	SX float32
	SY float32
}

type RetinaFace struct {
	engine     *ort.Engine
	modelPath  string
	session    ort.Session
	DeviceDesc string
}

func NewRetinaFace(engine *ort.Engine, modelPath string, device string) (*RetinaFace, error) {
	sess, desc, err := engine.CreateSessionWithDevice(modelPath, device)
	if err != nil {
		sess, desc, err = engine.CreateSessionWithDevice(modelPath, "cpu")
		if err != nil {
			return nil, err
		}
	}
	return &RetinaFace{
		engine:     engine,
		modelPath:  modelPath,
		session:    sess,
		DeviceDesc: desc,
	}, nil
}

func (r *RetinaFace) Close() {
	if r.session != 0 {
		r.engine.ReleaseSession(r.session)
		r.session = 0
	}
}

func generatePriors(imgW, imgH int) []PriorBox {
	strides := []int{8, 16, 32}
	minSizes := [][]float32{
		{16, 32},
		{64, 128},
		{256, 512},
	}

	var priors []PriorBox
	for k, stride := range strides {
		featW := int(math.Ceil(float64(imgW) / float64(stride)))
		featH := int(math.Ceil(float64(imgH) / float64(stride)))

		for i := 0; i < featH; i++ {
			for j := 0; j < featW; j++ {
				for _, minSize := range minSizes[k] {
					sKx := minSize / float32(imgW)
					sKy := minSize / float32(imgH)
					denseCX := (float32(j) + 0.5) * float32(stride) / float32(imgW)
					denseCY := (float32(i) + 0.5) * float32(stride) / float32(imgH)
					priors = append(priors, PriorBox{
						CX: denseCX,
						CY: denseCY,
						SX: sKx,
						SY: sKy,
					})
				}
			}
		}
	}
	return priors
}

var defaultPriors640 = generatePriors(640, 640)

func (r *RetinaFace) detectWithSession(session ort.Session, imgRGB []float32, imgW, imgH int, confThreshold float32, nmsThreshold float32) ([]FaceInfo, error) {
	detW, detH := 640, 640
	scaleX := float32(imgW) / float32(detW)
	scaleY := float32(imgH) / float32(detH)

	detInput := make([]float32, 1*3*detH*detW)
	for y := 0; y < detH; y++ {
		srcY := int(float32(y) * scaleY)
		if srcY >= imgH {
			srcY = imgH - 1
		}
		for x := 0; x < detW; x++ {
			srcX := int(float32(x) * scaleX)
			if srcX >= imgW {
				srcX = imgW - 1
			}
			srcIdx := (srcY*imgW + srcX) * 3
			rVal := imgRGB[srcIdx+0] * 255.0
			gVal := imgRGB[srcIdx+1] * 255.0
			bVal := imgRGB[srcIdx+2] * 255.0

			detInput[0*detH*detW+y*detW+x] = bVal - 104.0
			detInput[1*detH*detW+y*detW+x] = gVal - 117.0
			detInput[2*detH*detW+y*detW+x] = rVal - 123.0
		}
	}

	tInp, err := r.engine.CreateTensorFloat32([]int64{1, 3, int64(detH), int64(detW)}, detInput)
	if err != nil {
		return nil, err
	}
	defer r.engine.ReleaseValue(tInp)

	outs, err := r.engine.Run(session, []string{"input"}, []ort.Value{tInp}, []string{"loc", "conf", "landmarks"})
	if err != nil {
		return nil, err
	}
	defer r.engine.ReleaseValue(outs[0])
	defer r.engine.ReleaseValue(outs[1])
	defer r.engine.ReleaseValue(outs[2])

	priors := defaultPriors640
	numPriors := len(priors)

	locData, _ := r.engine.GetTensorDataFloat32(outs[0], numPriors*4)
	confData, _ := r.engine.GetTensorDataFloat32(outs[1], numPriors*2)
	lmData, _ := r.engine.GetTensorDataFloat32(outs[2], numPriors*10)

	var candidates []FaceInfo
	variances := []float32{0.1, 0.2}

	for i := 0; i < numPriors; i++ {
		score := confData[i*2+1]
		if score < confThreshold {
			continue
		}

		p := priors[i]
		locIdx := i * 4

		cx := p.CX + locData[locIdx+0]*variances[0]*p.SX
		cy := p.CY + locData[locIdx+1]*variances[0]*p.SY
		w := p.SX * float32(math.Exp(float64(locData[locIdx+2]*variances[1])))
		h := p.SY * float32(math.Exp(float64(locData[locIdx+3]*variances[1])))

		x1 := (cx - w/2) * float32(detW) * scaleX
		y1 := (cy - h/2) * float32(detH) * scaleY
		x2 := (cx + w/2) * float32(detW) * scaleX
		y2 := (cy + h/2) * float32(detH) * scaleY

		var lms [5]Point2D
		lmIdx := i * 10
		for j := 0; j < 5; j++ {
			lx := (p.CX + lmData[lmIdx+j*2+0]*variances[0]*p.SX) * float32(detW) * scaleX
			ly := (p.CY + lmData[lmIdx+j*2+1]*variances[0]*p.SY) * float32(detH) * scaleY
			lms[j] = Point2D{X: lx, Y: ly}
		}

		candidates = append(candidates, FaceInfo{
			Box:       [4]float32{x1, y1, x2, y2},
			Score:     score,
			Landmarks: lms,
		})
	}

	return nms(candidates, nmsThreshold), nil
}

func (r *RetinaFace) Detect(imgRGB []float32, imgW, imgH int, confThreshold float32, nmsThreshold float32) ([]FaceInfo, error) {
	faces, err := r.detectWithSession(r.session, imgRGB, imgW, imgH, confThreshold, nmsThreshold)
	if err != nil && (strings.Contains(err.Error(), "887A0005") || strings.Contains(err.Error(), "887A0006") || strings.Contains(err.Error(), "Dml") || strings.Contains(err.Error(), "GPU")) {
		fmt.Printf("[WARN] RetinaFace GPU device was reset by OS. Automatically falling back to CPU detector...\n")
		cpuSess, _, cpuErr := r.engine.CreateSessionWithDevice(r.modelPath, "cpu")
		if cpuErr == nil {
			defer r.engine.ReleaseSession(cpuSess)
			return r.detectWithSession(cpuSess, imgRGB, imgW, imgH, confThreshold, nmsThreshold)
		}
	}
	return faces, err
}

func nms(boxes []FaceInfo, threshold float32) []FaceInfo {
	if len(boxes) == 0 {
		return nil
	}

	sort.Slice(boxes, func(i, j int) bool {
		return boxes[i].Score > boxes[j].Score
	})

	var result []FaceInfo
	suppressed := make([]bool, len(boxes))

	for i := 0; i < len(boxes); i++ {
		if suppressed[i] {
			continue
		}
		result = append(result, boxes[i])
		for j := i + 1; j < len(boxes); j++ {
			if suppressed[j] {
				continue
			}
			if iou(boxes[i].Box, boxes[j].Box) > threshold {
				suppressed[j] = true
			}
		}
	}
	return result
}

func iou(a, b [4]float32) float32 {
	x1 := float32(math.Max(float64(a[0]), float64(b[0])))
	y1 := float32(math.Max(float64(a[1]), float64(b[1])))
	x2 := float32(math.Min(float64(a[2]), float64(b[2])))
	y2 := float32(math.Min(float64(a[3]), float64(b[3])))

	w := float32(math.Max(0, float64(x2-x1)))
	h := float32(math.Max(0, float64(y2-y1)))
	inter := w * h

	areaA := (a[2] - a[0]) * (a[3] - a[1])
	areaB := (b[2] - b[0]) * (b[3] - b[1])
	union := areaA + areaB - inter

	if union <= 0 {
		return 0
	}
	return inter / union
}
