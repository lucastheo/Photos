package media

import (
	"bytes"
	"image"
	"image/draw"
	"image/jpeg"
	"math"

	"github.com/lucastheo/photos/internal/domain"
)

// ApplyOrientation transforms img according to EXIF orientation tags 1..8 for
// standard and RAW formats. HEIC and HEIF images are returned unmodified because
// container-level rotation (irot/imir) is already applied during decoding.
func ApplyOrientation(img image.Image, format domain.MediaFormat, orientation int) image.Image {
	if img == nil {
		return nil
	}
	if format == domain.FormatHEIC || format == domain.FormatHEIF {
		return img
	}
	if orientation <= 1 || orientation > 8 {
		return img
	}

	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return img
	}

	src := toNRGBA(img)
	dw, dh := orientedDimensions(w, h, orientation)
	dst := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	for dy := range dh {
		dstRow := dy * dst.Stride
		for dx := range dw {
			sx, sy := mapOrientedPixel(dx, dy, w, h, orientation)
			sOff := sy*src.Stride + sx*4
			dOff := dstRow + dx*4
			copy(dst.Pix[dOff:dOff+4], src.Pix[sOff:sOff+4])
		}
	}
	return dst
}

func toNRGBA(img image.Image) *image.NRGBA {
	b := img.Bounds()
	if n, ok := img.(*image.NRGBA); ok && b.Min.X == 0 && b.Min.Y == 0 {
		return n
	}
	dst := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Src)
	return dst
}

func orientedDimensions(w, h, orientation int) (int, int) {
	if orientation >= 5 && orientation <= 8 {
		return h, w
	}
	return w, h
}

func mapOrientedPixel(dx, dy, w, h, orientation int) (int, int) {
	switch orientation {
	case 2:
		return w - 1 - dx, dy
	case 3:
		return w - 1 - dx, h - 1 - dy
	case 4:
		return dx, h - 1 - dy
	case 5:
		return dy, dx
	case 6:
		return dy, h - 1 - dx
	case 7:
		return w - 1 - dy, h - 1 - dx
	case 8:
		return w - 1 - dy, dx
	default:
		return dx, dy
	}
}

func resizeMaxDim(img image.Image, maxDim int) image.Image {
	if img == nil || maxDim <= 0 {
		return img
	}
	b := img.Bounds()
	sw, sh := b.Dx(), b.Dy()
	if sw <= 0 || sh <= 0 || (sw <= maxDim && sh <= maxDim) {
		return img
	}

	src := toNRGBA(img)
	dw, dh := scaledDimensions(sw, sh, maxDim)
	dst := image.NewNRGBA(image.Rect(0, 0, dw, dh))
	resizeNRGBABilinear(src, dst, sw, sh, dw, dh)
	return dst
}

func resizeNRGBABilinear(src, dst *image.NRGBA, sw, sh, dw, dh int) {
	x0s, x1s, wx := precomputeAxisWeights(dw, sw)
	for dy := range dh {
		sy := mappedSampleCoord(dy, dh, sh)
		y0 := int(sy)
		y1 := min(sh-1, y0+1)
		wy := int((sy - float64(y0)) * 256)
		wyInv := 256 - wy
		row0 := y0 * src.Stride
		row1 := y1 * src.Stride
		dstRow := dy * dst.Stride

		for dx := range dw {
			s00 := row0 + x0s[dx]
			s10 := row0 + x1s[dx]
			s01 := row1 + x0s[dx]
			s11 := row1 + x1s[dx]
			w00 := (256 - wx[dx]) * wyInv
			w10 := wx[dx] * wyInv
			w01 := (256 - wx[dx]) * wy
			w11 := wx[dx] * wy
			dOff := dstRow + dx*4
			for c := range 4 {
				val := int(src.Pix[s00+c])*w00 + int(src.Pix[s10+c])*w10 +
					int(src.Pix[s01+c])*w01 + int(src.Pix[s11+c])*w11
				dst.Pix[dOff+c] = uint8((val + 32768) >> 16)
			}
		}
	}
}

func precomputeAxisWeights(dstLen, srcLen int) ([]int, []int, []int) {
	x0s := make([]int, dstLen)
	x1s := make([]int, dstLen)
	wx := make([]int, dstLen)
	for d := range dstLen {
		sx := mappedSampleCoord(d, dstLen, srcLen)
		x0 := int(sx)
		x0s[d] = x0 * 4
		x1s[d] = min(srcLen-1, x0+1) * 4
		wx[d] = int((sx - float64(x0)) * 256)
	}
	return x0s, x1s, wx
}

func scaledDimensions(sw, sh, maxDim int) (int, int) {
	if sw >= sh {
		dw := maxDim
		dh := max(1, int(math.Round(float64(sh)*float64(maxDim)/float64(sw))))
		return dw, dh
	}
	dh := maxDim
	dw := max(1, int(math.Round(float64(sw)*float64(maxDim)/float64(sh))))
	return dw, dh
}

func mappedSampleCoord(d, dstLen, srcLen int) float64 {
	if dstLen <= 1 || srcLen <= 1 {
		return 0
	}
	pos := (float64(d)+0.5)*float64(srcLen)/float64(dstLen) - 0.5
	return min(float64(srcLen-1), max(0, pos))
}

func encodeJPEGBytes(img image.Image, quality int) ([]byte, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
