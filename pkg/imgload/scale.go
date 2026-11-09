package imgload

import "github.com/diamondburned/gotk4/pkg/gdk/v4"

// FitDimensions computes the target (width, height) that fits within
// (maxW, maxH) while preserving aspect ratio. It never upscales.
func FitDimensions(w, h, maxW, maxH int) (int, int) {
	if w <= 0 || h <= 0 {
		return 0, 0
	}
	if maxW <= 0 && maxH <= 0 {
		return w, h
	}
	if maxW <= 0 {
		maxW = w
	}
	if maxH <= 0 {
		maxH = h
	}
	if w <= maxW && h <= maxH {
		return w, h
	}

	// Compare w/maxW vs h/maxH using 64-bit integer cross-multiplication.
	if int64(w)*int64(maxH) >= int64(h)*int64(maxW) {
		outW := maxW
		outH := max(int((int64(h)*int64(maxW)+int64(w)/2)/int64(w)), 1)
		return outW, outH
	}
	outH := maxH
	outW := max(int((int64(w)*int64(maxH)+int64(h)/2)/int64(h)), 1)
	return outW, outH
}

// Downscale scales src down to fit within (maxW, maxH) using fixed-point
// bilinear interpolation (16.16 fixed-point coordinates) into dst.
// If src already fits within (maxW, maxH), Downscale returns src unchanged
// with zero allocations.
func Downscale(src PixelBuffer, maxW, maxH int, dst []byte) PixelBuffer {
	dstW, dstH := FitDimensions(src.Width, src.Height, maxW, maxH)
	if dstW == src.Width && dstH == src.Height {
		return src
	}

	bpp := bytesPerPixel(src.Format)
	if bpp == 0 {
		return src
	}

	outStride := dstW * bpp
	out := ensureSlice(dst, dstH*outStride)

	scaleX := (uint64(src.Width) << 16) / uint64(dstW)
	scaleY := (uint64(src.Height) << 16) / uint64(dstH)

	switch bpp {
	case 4:
		downscaleBilinear4(out, dstW, dstH, outStride, src.Pix, src.Width, src.Height, src.Stride, scaleX, scaleY)
	case 3:
		downscaleBilinear3(out, dstW, dstH, outStride, src.Pix, src.Width, src.Height, src.Stride, scaleX, scaleY)
	case 1:
		downscaleBilinear1(out, dstW, dstH, outStride, src.Pix, src.Width, src.Height, src.Stride, scaleX, scaleY)
	default:
		return src
	}

	return PixelBuffer{
		Pix:    out,
		Width:  dstW,
		Height: dstH,
		Stride: outStride,
		Format: src.Format,
	}
}

func bytesPerPixel(format gdk.MemoryFormat) int {
	switch format {
	case gdk.MemoryG8:
		return 1
	case gdk.MemoryG16:
		return 2
	case gdk.MemoryR8G8B8, gdk.MemoryB8G8R8:
		return 3
	case gdk.MemoryR8G8B8A8, gdk.MemoryR8G8B8A8Premultiplied,
		gdk.MemoryB8G8R8A8, gdk.MemoryB8G8R8A8Premultiplied,
		gdk.MemoryA8R8G8B8, gdk.MemoryA8R8G8B8Premultiplied:
		return 4
	default:
		return 0
	}
}

func downscaleBilinear4(dst []byte, dstW, dstH, dstStride int, src []byte, srcW, srcH, srcStride int, scaleX, scaleY uint64) {
	maxX := srcW - 1
	maxY := srcH - 1
	for dy := range dstH {
		syFixed := uint64(dy) * scaleY
		y0 := int(syFixed >> 16)
		wy := uint32((syFixed >> 8) & 0xFF)
		y1 := min(y0+1, maxY)

		row0 := src[y0*srcStride:]
		row1 := src[y1*srcStride:]
		dRow := dst[dy*dstStride : dy*dstStride+dstW*4]

		invWy := 256 - wy
		for dx := range dstW {
			sxFixed := uint64(dx) * scaleX
			x0 := int(sxFixed >> 16)
			wx := uint32((sxFixed >> 8) & 0xFF)
			x1 := min(x0+1, maxX)

			invWx := 256 - wx
			w00 := invWx * invWy
			w10 := wx * invWy
			w01 := invWx * wy
			w11 := wx * wy

			i00 := x0 * 4
			i10 := x1 * 4
			di := dx * 4

			dRow[di+0] = byte((uint32(row0[i00+0])*w00 + uint32(row0[i10+0])*w10 + uint32(row1[i00+0])*w01 + uint32(row1[i10+0])*w11 + 32768) >> 16)
			dRow[di+1] = byte((uint32(row0[i00+1])*w00 + uint32(row0[i10+1])*w10 + uint32(row1[i00+1])*w01 + uint32(row1[i10+1])*w11 + 32768) >> 16)
			dRow[di+2] = byte((uint32(row0[i00+2])*w00 + uint32(row0[i10+2])*w10 + uint32(row1[i00+2])*w01 + uint32(row1[i10+2])*w11 + 32768) >> 16)
			dRow[di+3] = byte((uint32(row0[i00+3])*w00 + uint32(row0[i10+3])*w10 + uint32(row1[i00+3])*w01 + uint32(row1[i10+3])*w11 + 32768) >> 16)
		}
	}
}

func downscaleBilinear3(dst []byte, dstW, dstH, dstStride int, src []byte, srcW, srcH, srcStride int, scaleX, scaleY uint64) {
	maxX := srcW - 1
	maxY := srcH - 1
	for dy := range dstH {
		syFixed := uint64(dy) * scaleY
		y0 := int(syFixed >> 16)
		wy := uint32((syFixed >> 8) & 0xFF)
		y1 := min(y0+1, maxY)

		row0 := src[y0*srcStride:]
		row1 := src[y1*srcStride:]
		dRow := dst[dy*dstStride : dy*dstStride+dstW*3]

		invWy := 256 - wy
		for dx := range dstW {
			sxFixed := uint64(dx) * scaleX
			x0 := int(sxFixed >> 16)
			wx := uint32((sxFixed >> 8) & 0xFF)
			x1 := min(x0+1, maxX)

			invWx := 256 - wx
			w00 := invWx * invWy
			w10 := wx * invWy
			w01 := invWx * wy
			w11 := wx * wy

			i00 := x0 * 3
			i10 := x1 * 3
			di := dx * 3

			dRow[di+0] = byte((uint32(row0[i00+0])*w00 + uint32(row0[i10+0])*w10 + uint32(row1[i00+0])*w01 + uint32(row1[i10+0])*w11 + 32768) >> 16)
			dRow[di+1] = byte((uint32(row0[i00+1])*w00 + uint32(row0[i10+1])*w10 + uint32(row1[i00+1])*w01 + uint32(row1[i10+1])*w11 + 32768) >> 16)
			dRow[di+2] = byte((uint32(row0[i00+2])*w00 + uint32(row0[i10+2])*w10 + uint32(row1[i00+2])*w01 + uint32(row1[i10+2])*w11 + 32768) >> 16)
		}
	}
}

func downscaleBilinear1(dst []byte, dstW, dstH, dstStride int, src []byte, srcW, srcH, srcStride int, scaleX, scaleY uint64) {
	maxX := srcW - 1
	maxY := srcH - 1
	for dy := range dstH {
		syFixed := uint64(dy) * scaleY
		y0 := int(syFixed >> 16)
		wy := uint32((syFixed >> 8) & 0xFF)
		y1 := min(y0+1, maxY)

		row0 := src[y0*srcStride:]
		row1 := src[y1*srcStride:]
		dRow := dst[dy*dstStride : dy*dstStride+dstW]

		invWy := 256 - wy
		for dx := range dstW {
			sxFixed := uint64(dx) * scaleX
			x0 := int(sxFixed >> 16)
			wx := uint32((sxFixed >> 8) & 0xFF)
			x1 := min(x0+1, maxX)

			invWx := 256 - wx
			w00 := invWx * invWy
			w10 := wx * invWy
			w01 := invWx * wy
			w11 := wx * wy

			dRow[dx] = byte((uint32(row0[x0])*w00 + uint32(row0[x1])*w10 + uint32(row1[x0])*w01 + uint32(row1[x1])*w11 + 32768) >> 16)
		}
	}
}
