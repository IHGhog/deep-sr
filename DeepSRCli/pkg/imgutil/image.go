package imgutil

import (
	"fmt"
	"image"
	"image/color"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"

	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

var rgbaPool = sync.Pool{
	New: func() any {
		return nil
	},
}

func getRGBA(w, h int) *image.RGBA {
	needed := w * h * 4
	v := rgbaPool.Get()
	if v != nil {
		img := v.(*image.RGBA)
		if cap(img.Pix) >= needed {
			img.Pix = img.Pix[:needed]
			img.Stride = w * 4
			img.Rect = image.Rect(0, 0, w, h)
			return img
		}
	}
	return image.NewRGBA(image.Rect(0, 0, w, h))
}

func putRGBA(img *image.RGBA) {
	if img != nil && cap(img.Pix) <= 4096*2160*4 {
		rgbaPool.Put(img)
	}
}

func imageToFloatRGB(img image.Image) ([]float32, int, int) {
	bounds := img.Bounds()
	w := bounds.Dx()
	h := bounds.Dy()
	rgb := make([]float32, w*h*3)

	switch m := img.(type) {
	case *image.NRGBA:
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				i := m.PixOffset(bounds.Min.X+x, bounds.Min.Y+y)
				idx := (y*w + x) * 3
				rgb[idx+0] = float32(m.Pix[i+0]) / 255.0
				rgb[idx+1] = float32(m.Pix[i+1]) / 255.0
				rgb[idx+2] = float32(m.Pix[i+2]) / 255.0
			}
		}
	case *image.RGBA:
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				i := m.PixOffset(bounds.Min.X+x, bounds.Min.Y+y)
				idx := (y*w + x) * 3
				rgb[idx+0] = float32(m.Pix[i+0]) / 255.0
				rgb[idx+1] = float32(m.Pix[i+1]) / 255.0
				rgb[idx+2] = float32(m.Pix[i+2]) / 255.0
			}
		}
	case *image.YCbCr:
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				yi := m.YOffset(bounds.Min.X+x, bounds.Min.Y+y)
				ci := m.COffset(bounds.Min.X+x, bounds.Min.Y+y)
				r, g, b := color.YCbCrToRGB(m.Y[yi], m.Cb[ci], m.Cr[ci])
				idx := (y*w + x) * 3
				rgb[idx+0] = float32(r) / 255.0
				rgb[idx+1] = float32(g) / 255.0
				rgb[idx+2] = float32(b) / 255.0
			}
		}
	default:
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				r, g, b, _ := img.At(bounds.Min.X+x, bounds.Min.Y+y).RGBA()
				idx := (y*w + x) * 3
				rgb[idx+0] = float32(r>>8) / 255.0
				rgb[idx+1] = float32(g>>8) / 255.0
				rgb[idx+2] = float32(b>>8) / 255.0
			}
		}
	}
	return rgb, w, h
}

// decodeWithFFmpeg 当 Go 原生解码器无法解码时（例如部分非常规格式或复杂 WebP/AVIF）回退调用内置 FFmpeg 解码
func decodeWithFFmpeg(path string) ([]float32, int, int, error) {
	ffmpegBin := findBuiltinFFmpeg()
	args := []string{
		"-v", "error",
		"-i", path,
		"-c:v", "png",
		"-f", "image2pipe",
		"pipe:1",
	}
	cmd := exec.Command(ffmpegBin, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, 0, 0, err
	}
	if err := cmd.Start(); err != nil {
		return nil, 0, 0, err
	}
	img, _, err := image.Decode(stdout)
	_ = cmd.Wait()
	if err != nil {
		return nil, 0, 0, fmt.Errorf("ffmpeg decode error: %w", err)
	}
	rgb, w, h := imageToFloatRGB(img)
	return rgb, w, h, nil
}

// LoadImage 加载图像并返回 [0, 1] 范围的 float32 RGB
func LoadImage(path string) ([]float32, int, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, 0, 0, err
	}
	defer f.Close()

	img, _, decodeErr := image.Decode(f)
	if decodeErr == nil {
		rgb, w, h := imageToFloatRGB(img)
		return rgb, w, h, nil
	}

	// 原生解码失败时，无缝回退至内置 FFmpeg 管道解码
	if rgb, w, h, ffErr := decodeWithFFmpeg(path); ffErr == nil {
		return rgb, w, h, nil
	}

	return nil, 0, 0, decodeErr
}

// DecodeImageFromReader 从 io.Reader 流直接解码图像
func DecodeImageFromReader(r io.Reader) ([]float32, int, int, error) {
	img, _, err := image.Decode(r)
	if err != nil {
		return nil, 0, 0, err
	}
	rgb, w, h := imageToFloatRGB(img)
	return rgb, w, h, nil
}

// RawBytesToFloatRGB 将 rawvideo rgb24 原始字节流转换为 [0, 1] 的 float32 像素
func RawBytesToFloatRGB(raw []byte, w, h int, dst []float32) {
	hw := w * h
	for i := 0; i < hw; i++ {
		srcIdx := i * 3
		dst[srcIdx+0] = float32(raw[srcIdx+0]) / 255.0
		dst[srcIdx+1] = float32(raw[srcIdx+1]) / 255.0
		dst[srcIdx+2] = float32(raw[srcIdx+2]) / 255.0
	}
}

// FloatRGBToRawBytes 将 [0, 1] float32 像素量化为 uint8 原始字节
func FloatRGBToRawBytes(rgb []float32, w, h int, dst []byte) {
	hw := w * h
	for i := 0; i < hw; i++ {
		srcIdx := i * 3
		r := int(rgb[srcIdx+0]*255.0 + 0.5)
		if r < 0 {
			r = 0
		} else if r > 255 {
			r = 255
		}
		g := int(rgb[srcIdx+1]*255.0 + 0.5)
		if g < 0 {
			g = 0
		} else if g > 255 {
			g = 255
		}
		b := int(rgb[srcIdx+2]*255.0 + 0.5)
		if b < 0 {
			b = 0
		} else if b > 255 {
			b = 255
		}
		dst[srcIdx+0] = uint8(r)
		dst[srcIdx+1] = uint8(g)
		dst[srcIdx+2] = uint8(b)
	}
}

// FloatRGBToRawBytesParallel 多核并行将 float32 像素转换为 rawvideo rgb24 字节
func FloatRGBToRawBytesParallel(rgb []float32, w, h int, dst []byte, workers int) {
	if workers <= 1 || h < workers*4 {
		FloatRGBToRawBytes(rgb, w, h, dst)
		return
	}

	var wg sync.WaitGroup
	rowsPerWorker := (h + workers - 1) / workers

	for i := 0; i < workers; i++ {
		startY := i * rowsPerWorker
		endY := startY + rowsPerWorker
		if endY > h {
			endY = h
		}
		if startY >= endY {
			break
		}

		wg.Add(1)
		go func(sy, ey int) {
			defer wg.Done()
			startIdx := sy * w
			endIdx := ey * w
			for j := startIdx; j < endIdx; j++ {
				srcIdx := j * 3
				r := int(rgb[srcIdx+0]*255.0 + 0.5)
				if r < 0 {
					r = 0
				} else if r > 255 {
					r = 255
				}
				g := int(rgb[srcIdx+1]*255.0 + 0.5)
				if g < 0 {
					g = 0
				} else if g > 255 {
					g = 255
				}
				b := int(rgb[srcIdx+2]*255.0 + 0.5)
				if b < 0 {
					b = 0
				} else if b > 255 {
					b = 255
				}
				dst[srcIdx+0] = uint8(r)
				dst[srcIdx+1] = uint8(g)
				dst[srcIdx+2] = uint8(b)
			}
		}(startY, endY)
	}
	wg.Wait()
}

// Resize 使用双线性插值缩放 float32 RGB
func Resize(src []float32, srcW, srcH, dstW, dstH int) []float32 {
	if srcW == dstW && srcH == dstH {
		res := make([]float32, len(src))
		copy(res, src)
		return res
	}

	dst := make([]float32, dstW*dstH*3)
	scaleX := float32(srcW) / float32(dstW)
	scaleY := float32(srcH) / float32(dstH)

	for dy := 0; dy < dstH; dy++ {
		sy := float32(dy) * scaleY
		y0 := int(sy)
		y1 := y0 + 1
		if y1 >= srcH {
			y1 = srcH - 1
		}
		fy := sy - float32(y0)

		for dx := 0; dx < dstW; dx++ {
			sx := float32(dx) * scaleX
			x0 := int(sx)
			x1 := x0 + 1
			if x1 >= srcW {
				x1 = srcW - 1
			}
			fx := sx - float32(x0)

			w00 := (1 - fx) * (1 - fy)
			w01 := fx * (1 - fy)
			w10 := (1 - fx) * fy
			w11 := fx * fy

			idx00 := (y0*srcW + x0) * 3
			idx01 := (y0*srcW + x1) * 3
			idx10 := (y1*srcW + x0) * 3
			idx11 := (y1*srcW + x1) * 3

			dstIdx := (dy*dstW + dx) * 3
			for c := 0; c < 3; c++ {
				dst[dstIdx+c] = w00*src[idx00+c] + w01*src[idx01+c] + w10*src[idx10+c] + w11*src[idx11+c]
			}
		}
	}
	return dst
}

func findBuiltinFFmpeg() string {
	exePath, err := os.Executable()
	var exeDir string
	if err == nil {
		exeDir = filepath.Dir(exePath)
	}
	candidates := []string{
		filepath.Join(exeDir, "ffmpeg.exe"),
		"ffmpeg.exe",
		filepath.Join(exeDir, "bin", "ffmpeg.exe"),
	}
	for _, c := range candidates {
		if fi, err := os.Stat(c); err == nil && !fi.IsDir() {
			abs, _ := filepath.Abs(c)
			return abs
		}
	}
	return "ffmpeg.exe"
}

// saveRealWebP 使用内置专属 FFmpeg 的 libwebp 编码器保存真实的 WebP 图片
func saveRealWebP(targetPath string, rawBytes []byte, w, h int, quality int) error {
	ffmpegBin := findBuiltinFFmpeg()
	if quality <= 0 || quality > 100 {
		quality = 95
	}

	args := []string{
		"-y",
		"-f", "rawvideo",
		"-pix_fmt", "rgb24",
		"-s", fmt.Sprintf("%dx%d", w, h),
		"-i", "pipe:0",
		"-c:v", "libwebp",
		"-quality", strconv.Itoa(quality),
		"-f", "image2",
		targetPath,
	}

	cmd := exec.Command(ffmpegBin, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}

	if err := cmd.Start(); err != nil {
		return err
	}

	_, writeErr := stdin.Write(rawBytes)
	_ = stdin.Close()
	if writeErr != nil {
		_ = cmd.Process.Kill()
		return writeErr
	}

	return cmd.Wait()
}

// SaveImageFormat 将 float32 RGB 写入目标路径，支持 jpg/png/webp 真实格式
func SaveImageFormat(path string, rgb []float32, w, h int, format string, quality int) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}

	ext := strings.ToLower(format)
	if ext == "" {
		ext = strings.ToLower(filepath.Ext(path))
	}
	ext = strings.TrimPrefix(ext, ".")

	targetPath := path
	if format != "" && !strings.HasSuffix(strings.ToLower(path), "."+ext) {
		targetPath = strings.TrimSuffix(path, filepath.Ext(path)) + "." + ext
	}

	// 真实 WebP 格式支持 (调用内置专属 libwebp)
	if ext == "webp" {
		rawBytes := make([]byte, w*h*3)
		FloatRGBToRawBytesParallel(rgb, w, h, rawBytes, runtime.NumCPU())
		return saveRealWebP(targetPath, rawBytes, w, h, quality)
	}

	outImg := getRGBA(w, h)
	defer putRGBA(outImg)
	pix := outImg.Pix
	hw := w * h

	numWorkers := runtime.NumCPU()
	if numWorkers > 8 {
		numWorkers = 8
	}
	if numWorkers <= 1 || h < numWorkers*4 {
		for i := 0; i < hw; i++ {
			srcIdx := i * 3
			pixIdx := i * 4

			rVal := int(rgb[srcIdx+0]*255.0 + 0.5)
			if rVal < 0 {
				rVal = 0
			} else if rVal > 255 {
				rVal = 255
			}

			gVal := int(rgb[srcIdx+1]*255.0 + 0.5)
			if gVal < 0 {
				gVal = 0
			} else if gVal > 255 {
				gVal = 255
			}

			bVal := int(rgb[srcIdx+2]*255.0 + 0.5)
			if bVal < 0 {
				bVal = 0
			} else if bVal > 255 {
				bVal = 255
			}

			pix[pixIdx+0] = uint8(rVal)
			pix[pixIdx+1] = uint8(gVal)
			pix[pixIdx+2] = uint8(bVal)
			pix[pixIdx+3] = 255
		}
	} else {
		var wg sync.WaitGroup
		rowsPerWorker := (h + numWorkers - 1) / numWorkers
		for wid := 0; wid < numWorkers; wid++ {
			startY := wid * rowsPerWorker
			endY := startY + rowsPerWorker
			if startY >= h {
				break
			}
			if endY > h {
				endY = h
			}
			wg.Add(1)
			go func(sy, ey int) {
				defer wg.Done()
				startIdx := sy * w
				endIdx := ey * w
				for j := startIdx; j < endIdx; j++ {
					srcIdx := j * 3
					pixIdx := j * 4

					rVal := int(rgb[srcIdx+0]*255.0 + 0.5)
					if rVal < 0 {
						rVal = 0
					} else if rVal > 255 {
						rVal = 255
					}

					gVal := int(rgb[srcIdx+1]*255.0 + 0.5)
					if gVal < 0 {
						gVal = 0
					} else if gVal > 255 {
						gVal = 255
					}

					bVal := int(rgb[srcIdx+2]*255.0 + 0.5)
					if bVal < 0 {
						bVal = 0
					} else if bVal > 255 {
						bVal = 255
					}

					pix[pixIdx+0] = uint8(rVal)
					pix[pixIdx+1] = uint8(gVal)
					pix[pixIdx+2] = uint8(bVal)
					pix[pixIdx+3] = 255
				}
			}(startY, endY)
		}
		wg.Wait()
	}

	f, err := os.Create(targetPath)
	if err != nil {
		return err
	}
	defer f.Close()

	if ext == "png" {
		return png.Encode(f, outImg)
	}

	if quality <= 0 || quality > 100 {
		quality = 95
	}
	return jpeg.Encode(f, outImg, &jpeg.Options{Quality: quality})
}

// SaveImage 默认以 jpg 保存
func SaveImage(path string, rgb []float32, w, h int, quality int) error {
	return SaveImageFormat(path, rgb, w, h, "", quality)
}

// WriteJPEGToWriter 将 float32 RGB 写入 JPEG 字节流
func WriteJPEGToWriter(w io.Writer, rgb []float32, width, height int, quality int) error {
	outImg := getRGBA(width, height)
	defer putRGBA(outImg)
	pix := outImg.Pix
	hw := width * height

	for i := 0; i < hw; i++ {
		srcIdx := i * 3
		pixIdx := i * 4

		rVal := int(rgb[srcIdx+0]*255.0 + 0.5)
		if rVal < 0 {
			rVal = 0
		} else if rVal > 255 {
			rVal = 255
		}

		gVal := int(rgb[srcIdx+1]*255.0 + 0.5)
		if gVal < 0 {
			gVal = 0
		} else if gVal > 255 {
			gVal = 255
		}

		bVal := int(rgb[srcIdx+2]*255.0 + 0.5)
		if bVal < 0 {
			bVal = 0
		} else if bVal > 255 {
			bVal = 255
		}

		pix[pixIdx+0] = uint8(rVal)
		pix[pixIdx+1] = uint8(gVal)
		pix[pixIdx+2] = uint8(bVal)
		pix[pixIdx+3] = 255
	}

	if quality <= 0 || quality > 100 {
		quality = 95
	}
	return jpeg.Encode(w, outImg, &jpeg.Options{Quality: quality})
}

