package imgload

import (
	"encoding/binary"
	"image"
	"image/color"
	"image/draw"
	"sync"
	"unsafe"

	"github.com/diamondburned/gotk4/pkg/gdk/v4"
)

// PixelBuffer holds raw pixel bytes in a layout directly supported by
// gdk.MemoryTexture without further conversion.
type PixelBuffer struct {
	Pix    []byte
	Width  int
	Height int
	Stride int
	Format gdk.MemoryFormat
	// Borrowed is true when Pix aliases the underlying image.Image's slice
	// directly without an allocation or copy.
	Borrowed bool
}

// BufferPool pools backing byte slices for image conversion to avoid repeated
// allocations when decoding or converting multiple images.
type BufferPool struct {
	pool sync.Pool
}

// Get returns a byte slice of at least n bytes.
func (p *BufferPool) Get(n int) []byte {
	if p != nil {
		if v := p.pool.Get(); v != nil {
			buf := *(v.(*[]byte))
			if cap(buf) >= n {
				return buf[:n]
			}
		}
	}
	return make([]byte, n)
}

// Put returns a byte slice to the pool.
func (p *BufferPool) Put(buf []byte) {
	if p == nil || cap(buf) == 0 {
		return
	}
	b := buf[:cap(buf)]
	p.pool.Put(&b)
}

// ConvertImage converts an arbitrary image.Image (with optional EXIF
// orientation) into a PixelBuffer ready for gdk.NewMemoryTexture.
//
// When orient is OrientationNormal (or 0) and img is already an in-memory
// *image.RGBA, *image.NRGBA, *image.Gray, *image.RGBA64, *image.NRGBA64, or
// *image.Gray16, ConvertImage aliases img's backing slice directly with zero
// allocations and zero copies.
//
// For *image.YCbCr (decoded by image/jpeg and golang.org/x/image/webp) and
// *image.Paletted (decoded by image/gif and image/png), ConvertImage uses
// specialized tight slice loops and fuses any EXIF orientation transform into
// the same single pass.
func ConvertImage(img image.Image, orient Orientation, dst []byte) PixelBuffer {
	if !orient.Valid() {
		orient = OrientationNormal
	}

	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return PixelBuffer{Format: gdk.MemoryR8G8B8A8Premultiplied}
	}

	if orient == OrientationNormal {
		switch src := img.(type) {
		case *image.RGBA:
			start := src.PixOffset(b.Min.X, b.Min.Y)
			end := src.PixOffset(b.Max.X, b.Max.Y-1)
			return PixelBuffer{
				Pix:      src.Pix[start:end],
				Width:    w,
				Height:   h,
				Stride:   src.Stride,
				Format:   gdk.MemoryR8G8B8A8Premultiplied,
				Borrowed: true,
			}
		case *image.NRGBA:
			start := src.PixOffset(b.Min.X, b.Min.Y)
			end := src.PixOffset(b.Max.X, b.Max.Y-1)
			return PixelBuffer{
				Pix:      src.Pix[start:end],
				Width:    w,
				Height:   h,
				Stride:   src.Stride,
				Format:   gdk.MemoryR8G8B8A8,
				Borrowed: true,
			}
		case *image.Gray:
			start := src.PixOffset(b.Min.X, b.Min.Y)
			end := src.PixOffset(b.Max.X, b.Max.Y-1)
			return PixelBuffer{
				Pix:      src.Pix[start:end],
				Width:    w,
				Height:   h,
				Stride:   src.Stride,
				Format:   gdk.MemoryG8,
				Borrowed: true,
			}
		case *image.RGBA64:
			start := src.PixOffset(b.Min.X, b.Min.Y)
			end := src.PixOffset(b.Max.X, b.Max.Y-1)
			pix := src.Pix[start:end]
			swap16InPlace(pix, w, h, src.Stride, 8)
			return PixelBuffer{
				Pix:      pix,
				Width:    w,
				Height:   h,
				Stride:   src.Stride,
				Format:   gdk.MemoryR16G16B16A16Premultiplied,
				Borrowed: true,
			}
		case *image.NRGBA64:
			start := src.PixOffset(b.Min.X, b.Min.Y)
			end := src.PixOffset(b.Max.X, b.Max.Y-1)
			pix := src.Pix[start:end]
			swap16InPlace(pix, w, h, src.Stride, 8)
			return PixelBuffer{
				Pix:      pix,
				Width:    w,
				Height:   h,
				Stride:   src.Stride,
				Format:   gdk.MemoryR16G16B16A16,
				Borrowed: true,
			}
		case *image.Gray16:
			start := src.PixOffset(b.Min.X, b.Min.Y)
			end := src.PixOffset(b.Max.X, b.Max.Y-1)
			pix := src.Pix[start:end]
			swap16InPlace(pix, w, h, src.Stride, 2)
			return PixelBuffer{
				Pix:      pix,
				Width:    w,
				Height:   h,
				Stride:   src.Stride,
				Format:   gdk.MemoryG16,
				Borrowed: true,
			}
		}
	}

	switch src := img.(type) {
	case *image.YCbCr:
		return convertYCbCrRGB(src, orient, dst)
	case *image.NYCbCrA:
		return convertNYCbCrA(src, orient, dst)
	case *image.Paletted:
		return convertPaletted(src, orient, dst)
	case *image.RGBA:
		return orientBytes4(src.Pix[src.PixOffset(b.Min.X, b.Min.Y):], w, h, src.Stride, orient, gdk.MemoryR8G8B8A8Premultiplied, dst)
	case *image.NRGBA:
		return orientBytes4(src.Pix[src.PixOffset(b.Min.X, b.Min.Y):], w, h, src.Stride, orient, gdk.MemoryR8G8B8A8, dst)
	case *image.Gray:
		return orientBytes1(src.Pix[src.PixOffset(b.Min.X, b.Min.Y):], w, h, src.Stride, orient, dst)
	default:
		return convertGenericRGBA(img, orient, dst)
	}
}

func ensureSlice(dst []byte, n int) []byte {
	if cap(dst) >= n {
		return dst[:n]
	}
	return make([]byte, n)
}

// swap16InPlace converts big-endian uint16 components (used by Go's RGBA64,
// NRGBA64, and Gray16) into native-endian uint16 components expected by
// GDK_MEMORY_R16G16B16A16* and GDK_MEMORY_G16 on little-endian machines.
func swap16InPlace(pix []byte, w, h, stride, bytesPerPixel int) {
	rowBytes := w * bytesPerPixel
	for y := range h {
		row := pix[y*stride : y*stride+rowBytes]
		for i := 0; i+1 < len(row); i += 2 {
			row[i], row[i+1] = row[i+1], row[i]
		}
	}
}

// convertYCbCrRGB converts *image.YCbCr into 3-byte packed GDK_MEMORY_R8G8B8
// (saving 25% memory compared to 4-byte RGBA), fusing EXIF orientation into a
// single pass.
func convertYCbCrRGB(src *image.YCbCr, orient Orientation, dst []byte) PixelBuffer {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	outW, outH := orient.Dimensions(w, h)
	outStride := outW * 3
	out := ensureSlice(dst, outH*outStride)

	if orient == OrientationNormal && b.Min.X == 0 && b.Min.Y == 0 {
		switch src.SubsampleRatio {
		case image.YCbCrSubsampleRatio444:
			ycbcr444ToRGB(out, src.Y, src.Cb, src.Cr, w, h, src.YStride, src.CStride)
			return PixelBuffer{Pix: out, Width: outW, Height: outH, Stride: outStride, Format: gdk.MemoryR8G8B8}
		case image.YCbCrSubsampleRatio422:
			ycbcr422ToRGB(out, src.Y, src.Cb, src.Cr, w, h, src.YStride, src.CStride)
			return PixelBuffer{Pix: out, Width: outW, Height: outH, Stride: outStride, Format: gdk.MemoryR8G8B8}
		case image.YCbCrSubsampleRatio420:
			ycbcr420ToRGB(out, src.Y, src.Cb, src.Cr, w, h, src.YStride, src.CStride)
			return PixelBuffer{Pix: out, Width: outW, Height: outH, Stride: outStride, Format: gdk.MemoryR8G8B8}
		}
	}

	// Single-pass oriented or arbitrary subsampled YCbCr conversion.
	for y := range h {
		sy := b.Min.Y + y
		yRowOff := (sy - src.Rect.Min.Y) * src.YStride
		for x := range w {
			sx := b.Min.X + x
			yi := yRowOff + (sx - src.Rect.Min.X)
			ci := src.COffset(sx, sy)
			r, g, bl := ycbcrToRGB8(src.Y[yi], src.Cb[ci], src.Cr[ci])
			dx, dy := orientCoord(x, y, w, h, orient)
			di := dy*outStride + dx*3
			out[di+0] = r
			out[di+1] = g
			out[di+2] = bl
		}
	}

	return PixelBuffer{
		Pix:    out,
		Width:  outW,
		Height: outH,
		Stride: outStride,
		Format: gdk.MemoryR8G8B8,
	}
}

func ycbcr444ToRGB(dst, yPlane, cbPlane, crPlane []byte, w, h, yStride, cStride int) {
	outStride := w * 3
	for y := range h {
		yRow := yPlane[y*yStride : y*yStride+w]
		cbRow := cbPlane[y*cStride : y*cStride+w]
		crRow := crPlane[y*cStride : y*cStride+w]
		dRow := dst[y*outStride : (y+1)*outStride]
		di := 0
		for x := range w {
			r, g, b := ycbcrToRGB8(yRow[x], cbRow[x], crRow[x])
			dRow[di+0] = r
			dRow[di+1] = g
			dRow[di+2] = b
			di += 3
		}
	}
}

func ycbcr422ToRGB(dst, yPlane, cbPlane, crPlane []byte, w, h, yStride, cStride int) {
	outStride := w * 3
	cw := (w + 1) / 2
	for y := range h {
		yRow := yPlane[y*yStride : y*yStride+w]
		cbRow := cbPlane[y*cStride : y*cStride+cw]
		crRow := crPlane[y*cStride : y*cStride+cw]
		dRow := dst[y*outStride : (y+1)*outStride]
		di := 0
		pairs := w / 2
		for cx := range pairs {
			cb := cbRow[cx]
			cr := crRow[cx]
			x0 := cx * 2
			r0, g0, b0 := ycbcrToRGB8(yRow[x0], cb, cr)
			r1, g1, b1 := ycbcrToRGB8(yRow[x0+1], cb, cr)
			dRow[di+0] = r0
			dRow[di+1] = g0
			dRow[di+2] = b0
			dRow[di+3] = r1
			dRow[di+4] = g1
			dRow[di+5] = b1
			di += 6
		}
		if w&1 != 0 {
			r, g, b := ycbcrToRGB8(yRow[w-1], cbRow[cw-1], crRow[cw-1])
			dRow[di+0] = r
			dRow[di+1] = g
			dRow[di+2] = b
		}
	}
}

func ycbcr420ToRGB(dst, yPlane, cbPlane, crPlane []byte, w, h, yStride, cStride int) {
	outStride := w * 3
	cw := (w + 1) / 2
	for y := range h {
		cy := y / 2
		yRow := yPlane[y*yStride : y*yStride+w]
		cbRow := cbPlane[cy*cStride : cy*cStride+cw]
		crRow := crPlane[cy*cStride : cy*cStride+cw]
		dRow := dst[y*outStride : (y+1)*outStride]
		di := 0
		pairs := w / 2
		for cx := range pairs {
			cb := cbRow[cx]
			cr := crRow[cx]
			x0 := cx * 2
			r0, g0, b0 := ycbcrToRGB8(yRow[x0], cb, cr)
			r1, g1, b1 := ycbcrToRGB8(yRow[x0+1], cb, cr)
			dRow[di+0] = r0
			dRow[di+1] = g0
			dRow[di+2] = b0
			dRow[di+3] = r1
			dRow[di+4] = g1
			dRow[di+5] = b1
			di += 6
		}
		if w&1 != 0 {
			r, g, b := ycbcrToRGB8(yRow[w-1], cbRow[cw-1], crRow[cw-1])
			dRow[di+0] = r
			dRow[di+1] = g
			dRow[di+2] = b
		}
	}
}

// ycbcrToRGB8 matches color.YCbCrToRGB's exact integer JFIF arithmetic.
func ycbcrToRGB8(yy, cb, cr byte) (byte, byte, byte) {
	yy1 := int32(yy) * 0x10101
	cb1 := int32(cb) - 128
	cr1 := int32(cr) - 128

	r := (yy1 + 91881*cr1) >> 16
	g := (yy1 - 22554*cb1 - 46802*cr1) >> 16
	b := (yy1 + 116130*cb1) >> 16

	return clampU8(r), clampU8(g), clampU8(b)
}

func clampU8(v int32) byte {
	if uint32(v) <= 255 {
		return byte(v)
	}
	if v < 0 {
		return 0
	}
	return 255
}

func convertNYCbCrA(src *image.NYCbCrA, orient Orientation, dst []byte) PixelBuffer {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	outW, outH := orient.Dimensions(w, h)
	outStride := outW * 4
	out := ensureSlice(dst, outH*outStride)

	for y := range h {
		sy := b.Min.Y + y
		yRowOff := (sy - src.Rect.Min.Y) * src.YStride
		aRowOff := (sy - src.Rect.Min.Y) * src.AStride
		for x := range w {
			sx := b.Min.X + x
			yi := yRowOff + (sx - src.Rect.Min.X)
			ai := aRowOff + (sx - src.Rect.Min.X)
			ci := src.COffset(sx, sy)
			r, g, bl := ycbcrToRGB8(src.Y[yi], src.Cb[ci], src.Cr[ci])
			a := src.A[ai]
			dx, dy := orientCoord(x, y, w, h, orient)
			di := dy*outStride + dx*4
			out[di+0] = r
			out[di+1] = g
			out[di+2] = bl
			out[di+3] = a
		}
	}

	return PixelBuffer{
		Pix:    out,
		Width:  outW,
		Height: outH,
		Stride: outStride,
		Format: gdk.MemoryR8G8B8A8,
	}
}

// convertPaletted expands an *image.Paletted into premultiplied RGBA using a
// stack-allocated 256-entry uint32 lookup table (zero heap allocations for the
// LUT).
func convertPaletted(src *image.Paletted, orient Orientation, dst []byte) PixelBuffer {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	outW, outH := orient.Dimensions(w, h)
	outStride := outW * 4
	out := ensureSlice(dst, outH*outStride)

	var lut [256]uint32
	buildPaletteLUT(&lut, src.Palette)

	outU32 := unsafe.Slice((*uint32)(unsafe.Pointer(&out[0])), outW*outH)

	if orient == OrientationNormal {
		for y := range h {
			sOff := src.PixOffset(b.Min.X, b.Min.Y+y)
			sRow := src.Pix[sOff : sOff+w]
			dRow := outU32[y*outW : (y+1)*outW]
			for x, idx := range sRow {
				dRow[x] = lut[idx]
			}
		}
	} else {
		for y := range h {
			sOff := src.PixOffset(b.Min.X, b.Min.Y+y)
			sRow := src.Pix[sOff : sOff+w]
			for x, idx := range sRow {
				dx, dy := orientCoord(x, y, w, h, orient)
				outU32[dy*outW+dx] = lut[idx]
			}
		}
	}

	return PixelBuffer{
		Pix:    out,
		Width:  outW,
		Height: outH,
		Stride: outStride,
		Format: gdk.MemoryR8G8B8A8Premultiplied,
	}
}

func buildPaletteLUT(lut *[256]uint32, pal color.Palette) {
	for i, c := range pal {
		if i >= 256 {
			break
		}
		if c == nil {
			continue
		}
		r, g, b, a := c.RGBA()
		var px [4]byte
		px[0] = byte(r >> 8)
		px[1] = byte(g >> 8)
		px[2] = byte(b >> 8)
		px[3] = byte(a >> 8)
		lut[i] = binary.NativeEndian.Uint32(px[:])
	}
}

func orientBytes4(src []byte, w, h, srcStride int, orient Orientation, format gdk.MemoryFormat, dst []byte) PixelBuffer {
	outW, outH := orient.Dimensions(w, h)
	outStride := outW * 4
	out := ensureSlice(dst, outH*outStride)
	outU32 := unsafe.Slice((*uint32)(unsafe.Pointer(&out[0])), outW*outH)

	for y := range h {
		row := src[y*srcStride : y*srcStride+w*4]
		rowU32 := unsafe.Slice((*uint32)(unsafe.Pointer(&row[0])), w)
		for x, v := range rowU32 {
			dx, dy := orientCoord(x, y, w, h, orient)
			outU32[dy*outW+dx] = v
		}
	}

	return PixelBuffer{
		Pix:    out,
		Width:  outW,
		Height: outH,
		Stride: outStride,
		Format: format,
	}
}

func orientBytes1(src []byte, w, h, srcStride int, orient Orientation, dst []byte) PixelBuffer {
	outW, outH := orient.Dimensions(w, h)
	out := ensureSlice(dst, outH*outW)

	for y := range h {
		row := src[y*srcStride : y*srcStride+w]
		for x, v := range row {
			dx, dy := orientCoord(x, y, w, h, orient)
			out[dy*outW+dx] = v
		}
	}

	return PixelBuffer{
		Pix:    out,
		Width:  outW,
		Height: outH,
		Stride: outW,
		Format: gdk.MemoryG8,
	}
}

func convertGenericRGBA(img image.Image, orient Orientation, dst []byte) PixelBuffer {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if orient == OrientationNormal {
		outStride := w * 4
		out := ensureSlice(dst, h*outStride)
		rgba := image.RGBA{
			Pix:    out,
			Stride: outStride,
			Rect:   image.Rect(0, 0, w, h),
		}
		draw.Draw(&rgba, rgba.Rect, img, b.Min, draw.Src)
		return PixelBuffer{
			Pix:    out,
			Width:  w,
			Height: h,
			Stride: outStride,
			Format: gdk.MemoryR8G8B8A8Premultiplied,
		}
	}

	outW, outH := orient.Dimensions(w, h)
	outStride := outW * 4
	out := ensureSlice(dst, outH*outStride)
	for y := range h {
		for x := range w {
			r, g, bl, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			dx, dy := orientCoord(x, y, w, h, orient)
			di := dy*outStride + dx*4
			out[di+0] = byte(r >> 8)
			out[di+1] = byte(g >> 8)
			out[di+2] = byte(bl >> 8)
			out[di+3] = byte(a >> 8)
		}
	}
	return PixelBuffer{
		Pix:    out,
		Width:  outW,
		Height: outH,
		Stride: outStride,
		Format: gdk.MemoryR8G8B8A8Premultiplied,
	}
}

// orientCoord maps source coordinates (x, y) in a (w, h) image to destination
// coordinates after applying EXIF orientation o.
func orientCoord(x, y, w, h int, o Orientation) (int, int) {
	switch o {
	case OrientationFlipH:
		return w - 1 - x, y
	case OrientationRotate180:
		return w - 1 - x, h - 1 - y
	case OrientationFlipV:
		return x, h - 1 - y
	case OrientationTranspose:
		return y, x
	case OrientationRotate90CW:
		return h - 1 - y, x
	case OrientationTransverse:
		return h - 1 - y, w - 1 - x
	case OrientationRotate270CW:
		return y, w - 1 - x
	default:
		return x, y
	}
}
