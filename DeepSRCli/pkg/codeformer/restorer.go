package codeformer

import (
	"fmt"
	"math"
	"strings"

	"deepsr-cli/pkg/ort"
)

type CodeFormer struct {
	engine     *ort.Engine
	modelPath  string
	session    ort.Session
	DeviceDesc string
}

func NewCodeFormer(engine *ort.Engine, modelPath string, device string) (*CodeFormer, error) {
	sess, desc, err := engine.CreateSessionWithDevice(modelPath, device)
	if err != nil {
		sess, desc, err = engine.CreateSessionWithDevice(modelPath, "cpu")
		if err != nil {
			return nil, err
		}
	}
	return &CodeFormer{
		engine:     engine,
		modelPath:  modelPath,
		session:    sess,
		DeviceDesc: desc,
	}, nil
}

func (cf *CodeFormer) Close() {
	if cf.session != 0 {
		cf.engine.ReleaseSession(cf.session)
		cf.session = 0
	}
}

func (cf *CodeFormer) restoreWithSession(session ort.Session, faceRGB []float32, fidelityW float32) ([]float32, error) {
	size := 512
	tensorIn := make([]float32, 1*3*size*size)

	// Normalize [0, 1] to [-1, 1]
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			srcIdx := (y*size + x) * 3
			r := (faceRGB[srcIdx+0] - 0.5) / 0.5
			g := (faceRGB[srcIdx+1] - 0.5) / 0.5
			b := (faceRGB[srcIdx+2] - 0.5) / 0.5

			tensorIn[0*size*size+y*size+x] = r
			tensorIn[1*size*size+y*size+x] = g
			tensorIn[2*size*size+y*size+x] = b
		}
	}

	tImg, err := cf.engine.CreateTensorFloat32([]int64{1, 3, int64(size), int64(size)}, tensorIn)
	if err != nil {
		return nil, err
	}
	defer cf.engine.ReleaseValue(tImg)

	tW, err := cf.engine.CreateTensorFloat32([]int64{}, []float32{fidelityW})
	if err != nil {
		return nil, err
	}
	defer cf.engine.ReleaseValue(tW)

	outs, err := cf.engine.Run(session, []string{"image", "w"}, []ort.Value{tImg, tW}, []string{"output"})
	if err != nil {
		return nil, err
	}
	defer cf.engine.ReleaseValue(outs[0])

	rawOut, err := cf.engine.GetTensorDataFloat32(outs[0], 1*3*size*size)
	if err != nil {
		return nil, err
	}

	// Denormalize [-1, 1] back to [0, 1]
	outRGB := make([]float32, size*size*3)
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			dstIdx := (y*size + x) * 3
			r := rawOut[0*size*size+y*size+x]
			g := rawOut[1*size*size+y*size+x]
			b := rawOut[2*size*size+y*size+x]

			outRGB[dstIdx+0] = float32(math.Min(1.0, math.Max(0.0, float64(r*0.5+0.5))))
			outRGB[dstIdx+1] = float32(math.Min(1.0, math.Max(0.0, float64(g*0.5+0.5))))
			outRGB[dstIdx+2] = float32(math.Min(1.0, math.Max(0.0, float64(b*0.5+0.5))))
		}
	}
	return outRGB, nil
}

func (cf *CodeFormer) RestoreFace(faceRGB []float32, fidelityW float32) ([]float32, error) {
	outRGB, err := cf.restoreWithSession(cf.session, faceRGB, fidelityW)
	if err != nil && (strings.Contains(err.Error(), "887A0005") || strings.Contains(err.Error(), "887A0006") || strings.Contains(err.Error(), "Dml") || strings.Contains(err.Error(), "GPU")) {
		fmt.Printf("[WARN] CodeFormer GPU device was reset by OS. Automatically falling back to CPU restorer...\n")
		cpuSess, _, cpuErr := cf.engine.CreateSessionWithDevice(cf.modelPath, "cpu")
		if cpuErr == nil {
			defer cf.engine.ReleaseSession(cpuSess)
			return cf.restoreWithSession(cpuSess, faceRGB, fidelityW)
		}
	}
	return outRGB, err
}
