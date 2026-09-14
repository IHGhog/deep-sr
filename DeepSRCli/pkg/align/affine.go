package align

import (
	"math"

	"deepsr-cli/pkg/detector"
)

// Standard 512x512 face template landmarks (CodeFormer / ArcFace standard)
var FaceTemplate512 = [5]detector.Point2D{
	{X: 192.98138, Y: 239.94708},
	{X: 318.90277, Y: 240.1736},
	{X: 257.1026, Y: 314.01935},
	{X: 201.34947, Y: 371.41043},
	{X: 313.4009, Y: 371.15118},
}

// AffineMatrix represents a 2x3 affine transformation matrix:
// [ a  b  tx ]
// [ c  d  ty ]
type AffineMatrix [6]float32

// EstimateSimilarityTransform computes the 2x3 affine matrix aligning src points to dst template.
// forward maps src (canvas) -> dst (512x512 template)
// inverse maps dst (512x512 template) -> src (canvas)
func EstimateSimilarityTransform(src [5]detector.Point2D, dst [5]detector.Point2D) (AffineMatrix, AffineMatrix) {
	var srcMeanX, srcMeanY float32
	var dstMeanX, dstMeanY float32
	for i := 0; i < 5; i++ {
		srcMeanX += src[i].X
		srcMeanY += src[i].Y
		dstMeanX += dst[i].X
		dstMeanY += dst[i].Y
	}
	srcMeanX /= 5.0
	srcMeanY /= 5.0
	dstMeanX /= 5.0
	dstMeanY /= 5.0

	var srcVar float32
	var sumU, sumV float32
	for i := 0; i < 5; i++ {
		sx := src[i].X - srcMeanX
		sy := src[i].Y - srcMeanY
		dx := dst[i].X - dstMeanX
		dy := dst[i].Y - dstMeanY

		srcVar += sx*sx + sy*sy
		sumU += sx*dx + sy*dy
		sumV += sx*dy - sy*dx
	}

	if srcVar < 1e-6 {
		srcVar = 1e-6
	}

	a := sumU / srcVar
	b := -sumV / srcVar
	c := sumV / srcVar
	d := sumU / srcVar

	tx := dstMeanX - (a*srcMeanX + b*srcMeanY)
	ty := dstMeanY - (c*srcMeanX + d*srcMeanY)

	forward := AffineMatrix{a, b, tx, c, d, ty}

	// Calculate inverse affine matrix
	det := a*d - b*c
	if math.Abs(float64(det)) < 1e-7 {
		det = 1e-7
	}
	invA := d / det
	invB := -b / det
	invC := -c / det
	invD := a / det
	invTx := (b*ty - d*tx) / det
	invTy := (c*tx - a*ty) / det

	inverse := AffineMatrix{invA, invB, invTx, invC, invD, invTy}
	return forward, inverse
}

// WarpAffine crops and aligns an image to 512x512 using the inverse affine matrix (maps template coords -> canvas coords)
func WarpAffine(srcRGB []float32, srcW, srcH int, invM AffineMatrix, dstW, dstH int) []float32 {
	dst := make([]float32, dstW*dstH*3)

	for dy := 0; dy < dstH; dy++ {
		for dx := 0; dx < dstW; dx++ {
			// Map destination (dx, dy) back to source (sx, sy) using invM
			sx := invM[0]*float32(dx) + invM[1]*float32(dy) + invM[2]
			sy := invM[3]*float32(dx) + invM[4]*float32(dy) + invM[5]

			dstIdx := (dy*dstW + dx) * 3

			if sx >= 0 && sx < float32(srcW-1) && sy >= 0 && sy < float32(srcH-1) {
				x0 := int(sx)
				y0 := int(sy)
				x1 := x0 + 1
				y1 := y0 + 1
				fx := sx - float32(x0)
				fy := sy - float32(y0)

				w00 := (1 - fx) * (1 - fy)
				w01 := fx * (1 - fy)
				w10 := (1 - fx) * fy
				w11 := fx * fy

				idx00 := (y0*srcW + x0) * 3
				idx01 := (y0*srcW + x1) * 3
				idx10 := (y1*srcW + x0) * 3
				idx11 := (y1*srcW + x1) * 3

				for c := 0; c < 3; c++ {
					val := w00*srcRGB[idx00+c] + w01*srcRGB[idx01+c] + w10*srcRGB[idx10+c] + w11*srcRGB[idx11+c]
					dst[dstIdx+c] = val
				}
			} else {
				dst[dstIdx+0] = 0.5
				dst[dstIdx+1] = 0.5
				dst[dstIdx+2] = 0.5
			}
		}
	}
	return dst
}

// PasteFaceBack blends a 512x512 restored face back into the main canvas using backward mapping (gather with bilinear interpolation)
// forM maps canvas (cx, cy) -> face template (fx, fy)
// invM maps face template (fx, fy) -> canvas (cx, cy)
func PasteFaceBack(canvasRGB []float32, canW, canH int, restoredFace []float32, faceW, faceH int, forM AffineMatrix, invM AffineMatrix) {
	// Find canvas bounding box containing the transformed 512x512 face
	corners := [4][2]float32{
		{0, 0},
		{float32(faceW), 0},
		{float32(faceW), float32(faceH)},
		{0, float32(faceH)},
	}

	minCX, minCY := float32(canW), float32(canH)
	maxCX, maxCY := float32(0), float32(0)

	for _, c := range corners {
		cx := invM[0]*c[0] + invM[1]*c[1] + invM[2]
		cy := invM[3]*c[0] + invM[4]*c[1] + invM[5]
		if cx < minCX {
			minCX = cx
		}
		if cx > maxCX {
			maxCX = cx
		}
		if cy < minCY {
			minCY = cy
		}
		if cy > maxCY {
			maxCY = cy
		}
	}

	startX := int(math.Max(0, math.Floor(float64(minCX))))
	endX := int(math.Min(float64(canW-1), math.Ceil(float64(maxCX))))
	startY := int(math.Max(0, math.Floor(float64(minCY))))
	endY := int(math.Min(float64(canH-1), math.Ceil(float64(maxCY))))

	feather := float32(40.0)

	// Backward mapping: iterate over canvas bounding box and sample face with bilinear interpolation
	for cy := startY; cy <= endY; cy++ {
		for cx := startX; cx <= endX; cx++ {
			// Map canvas (cx, cy) -> face (fx, fy) using forM
			fx := forM[0]*float32(cx) + forM[1]*float32(cy) + forM[2]
			fy := forM[3]*float32(cx) + forM[4]*float32(cy) + forM[5]

			if fx < 0 || fx >= float32(faceW-1) || fy < 0 || fy >= float32(faceH-1) {
				continue
			}

			// Smooth feather weighting on border
			distX := float32(math.Min(float64(fx), float64(float32(faceW)-1-fx)))
			distY := float32(math.Min(float64(fy), float64(float32(faceH)-1-fy)))

			weightX := float32(1.0)
			if distX < feather {
				weightX = float32(0.5 * (1.0 - math.Cos(float64(distX/feather)*math.Pi)))
			}
			weightY := float32(1.0)
			if distY < feather {
				weightY = float32(0.5 * (1.0 - math.Cos(float64(distY/feather)*math.Pi)))
			}

			alpha := weightX * weightY
			if alpha <= 0.001 {
				continue
			}

			// Bilinear interpolation on restored face
			x0 := int(fx)
			y0 := int(fy)
			x1 := x0 + 1
			y1 := y0 + 1
			rx := fx - float32(x0)
			ry := fy - float32(y0)

			w00 := (1 - rx) * (1 - ry)
			w01 := rx * (1 - ry)
			w10 := (1 - rx) * ry
			w11 := rx * ry

			idx00 := (y0*faceW + x0) * 3
			idx01 := (y0*faceW + x1) * 3
			idx10 := (y1*faceW + x0) * 3
			idx11 := (y1*faceW + x1) * 3

			canIdx := (cy*canW + cx) * 3

			for c := 0; c < 3; c++ {
				faceVal := w00*restoredFace[idx00+c] + w01*restoredFace[idx01+c] + w10*restoredFace[idx10+c] + w11*restoredFace[idx11+c]
				origVal := canvasRGB[canIdx+c]
				canvasRGB[canIdx+c] = origVal*(1.0-alpha) + faceVal*alpha
			}
		}
	}
}
