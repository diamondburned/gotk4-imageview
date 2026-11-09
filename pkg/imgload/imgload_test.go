package imgload

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"

	"github.com/diamondburned/gotk4/pkg/gdk/v4"
)

func TestParseEXIFOrientation(t *testing.T) {
	makeJPEGWithEXIF := func(littleEndian bool, orient uint16) []byte {
		var tiff [26]byte
		var bo binary.ByteOrder
		if littleEndian {
			tiff[0], tiff[1] = 'I', 'I'
			bo = binary.LittleEndian
		} else {
			tiff[0], tiff[1] = 'M', 'M'
			bo = binary.BigEndian
		}
		bo.PutUint16(tiff[2:4], 0x002A)
		bo.PutUint32(tiff[4:8], 8) // IFD0 offset
		bo.PutUint16(tiff[8:10], 1)
		bo.PutUint16(tiff[10:12], 0x0112) // Orientation tag
		bo.PutUint16(tiff[12:14], 3)      // SHORT
		bo.PutUint32(tiff[14:18], 1)      // count = 1
		bo.PutUint16(tiff[18:20], orient)

		app1PayloadLen := 2 + 6 + len(tiff)
		var out bytes.Buffer
		out.Write([]byte{0xFF, 0xD8}) // SOI
		out.Write([]byte{0xFF, 0xE1}) // APP1
		var lenBuf [2]byte
		binary.BigEndian.PutUint16(lenBuf[:], uint16(app1PayloadLen))
		out.Write(lenBuf[:])
		out.WriteString("Exif\x00\x00")
		out.Write(tiff[:])
		out.Write([]byte{0xFF, 0xD9}) // EOI
		return out.Bytes()
	}

	tests := []struct {
		name string
		data []byte
		want Orientation
	}{
		{
			name: "empty",
			data: nil,
			want: OrientationNormal,
		},
		{
			name: "non-jpeg",
			data: []byte("\x89PNG\r\n\x1a\n"),
			want: OrientationNormal,
		},
		{
			name: "little-endian-rotate90cw",
			data: makeJPEGWithEXIF(true, uint16(OrientationRotate90CW)),
			want: OrientationRotate90CW,
		},
		{
			name: "big-endian-rotate270cw",
			data: makeJPEGWithEXIF(false, uint16(OrientationRotate270CW)),
			want: OrientationRotate270CW,
		},
		{
			name: "little-endian-transpose",
			data: makeJPEGWithEXIF(true, uint16(OrientationTranspose)),
			want: OrientationTranspose,
		},
		{
			name: "out-of-range-orientation",
			data: makeJPEGWithEXIF(true, 99),
			want: OrientationNormal,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseEXIFOrientation(tc.data)
			if got != tc.want {
				t.Fatalf("ParseEXIFOrientation() = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestConvertImageFormats(t *testing.T) {
	const w, h = 8, 6

	makeRGBA := func() *image.RGBA {
		img := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := range h {
			for x := range w {
				img.SetRGBA(x, y, color.RGBA{R: uint8(x * 20), G: uint8(y * 30), B: 100, A: 255})
			}
		}
		return img
	}

	makeNRGBA := func() *image.NRGBA {
		img := image.NewNRGBA(image.Rect(0, 0, w, h))
		for y := range h {
			for x := range w {
				img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 15), G: uint8(y * 25), B: 200, A: 128})
			}
		}
		return img
	}

	makeGray := func() *image.Gray {
		img := image.NewGray(image.Rect(0, 0, w, h))
		for y := range h {
			for x := range w {
				img.SetGray(x, y, color.Gray{Y: uint8(x*10 + y*20)})
			}
		}
		return img
	}

	makeYCbCr := func(ratio image.YCbCrSubsampleRatio) *image.YCbCr {
		img := image.NewYCbCr(image.Rect(0, 0, w, h), ratio)
		for i := range img.Y {
			img.Y[i] = uint8((i * 17) & 0xFF)
		}
		for i := range img.Cb {
			img.Cb[i] = 120
			img.Cr[i] = 140
		}
		return img
	}

	makePaletted := func() *image.Paletted {
		pal := color.Palette{
			color.RGBA{R: 255, G: 0, B: 0, A: 255},
			color.RGBA{R: 0, G: 255, B: 0, A: 255},
			color.RGBA{R: 0, G: 0, B: 255, A: 255},
			color.RGBA{R: 0, G: 0, B: 0, A: 0},
		}
		img := image.NewPaletted(image.Rect(0, 0, w, h), pal)
		for y := range h {
			for x := range w {
				img.SetColorIndex(x, y, uint8((x+y)%len(pal)))
			}
		}
		return img
	}

	tests := []struct {
		name         string
		img          image.Image
		orient       Orientation
		wantFormat   gdk.MemoryFormat
		wantBorrowed bool
		wantW        int
		wantH        int
	}{
		{
			name:         "RGBA-ZeroCopy",
			img:          makeRGBA(),
			orient:       OrientationNormal,
			wantFormat:   gdk.MemoryR8G8B8A8Premultiplied,
			wantBorrowed: true,
			wantW:        w,
			wantH:        h,
		},
		{
			name:         "NRGBA-ZeroCopy",
			img:          makeNRGBA(),
			orient:       OrientationNormal,
			wantFormat:   gdk.MemoryR8G8B8A8,
			wantBorrowed: true,
			wantW:        w,
			wantH:        h,
		},
		{
			name:         "Gray-ZeroCopy",
			img:          makeGray(),
			orient:       OrientationNormal,
			wantFormat:   gdk.MemoryG8,
			wantBorrowed: true,
			wantW:        w,
			wantH:        h,
		},
		{
			name:         "YCbCr420-RGB",
			img:          makeYCbCr(image.YCbCrSubsampleRatio420),
			orient:       OrientationNormal,
			wantFormat:   gdk.MemoryR8G8B8,
			wantBorrowed: false,
			wantW:        w,
			wantH:        h,
		},
		{
			name:         "YCbCr422-RGB",
			img:          makeYCbCr(image.YCbCrSubsampleRatio422),
			orient:       OrientationNormal,
			wantFormat:   gdk.MemoryR8G8B8,
			wantBorrowed: false,
			wantW:        w,
			wantH:        h,
		},
		{
			name:         "YCbCr444-RGB",
			img:          makeYCbCr(image.YCbCrSubsampleRatio444),
			orient:       OrientationNormal,
			wantFormat:   gdk.MemoryR8G8B8,
			wantBorrowed: false,
			wantW:        w,
			wantH:        h,
		},
		{
			name:         "YCbCr420-Rotate90CW",
			img:          makeYCbCr(image.YCbCrSubsampleRatio420),
			orient:       OrientationRotate90CW,
			wantFormat:   gdk.MemoryR8G8B8,
			wantBorrowed: false,
			wantW:        h,
			wantH:        w,
		},
		{
			name:         "Paletted-Normal",
			img:          makePaletted(),
			orient:       OrientationNormal,
			wantFormat:   gdk.MemoryR8G8B8A8Premultiplied,
			wantBorrowed: false,
			wantW:        w,
			wantH:        h,
		},
		{
			name:         "Paletted-Rotate270CW",
			img:          makePaletted(),
			orient:       OrientationRotate270CW,
			wantFormat:   gdk.MemoryR8G8B8A8Premultiplied,
			wantBorrowed: false,
			wantW:        h,
			wantH:        w,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pb := ConvertImage(tc.img, tc.orient, nil)
			if pb.Format != tc.wantFormat {
				t.Errorf("Format = %v, want %v", pb.Format, tc.wantFormat)
			}
			if pb.Borrowed != tc.wantBorrowed {
				t.Errorf("Borrowed = %v, want %v", pb.Borrowed, tc.wantBorrowed)
			}
			if pb.Width != tc.wantW || pb.Height != tc.wantH {
				t.Errorf("Dimensions = (%d, %d), want (%d, %d)", pb.Width, pb.Height, tc.wantW, tc.wantH)
			}
			tex := pb.ToTexture(false)
			if tex == nil {
				t.Fatal("ToTexture returned nil")
			}
			if tex.Width() != tc.wantW || tex.Height() != tc.wantH {
				t.Errorf("Texture dimensions = (%d, %d), want (%d, %d)", tex.Width(), tex.Height(), tc.wantW, tc.wantH)
			}
		})
	}
}

func TestOrientationTransforms(t *testing.T) {
	// 3x2 asymmetric image with distinct pixel markers.
	src := image.NewRGBA(image.Rect(0, 0, 3, 2))
	src.SetRGBA(0, 0, color.RGBA{R: 10, G: 0, B: 0, A: 255}) // top-left
	src.SetRGBA(2, 0, color.RGBA{R: 20, G: 0, B: 0, A: 255}) // top-right
	src.SetRGBA(0, 1, color.RGBA{R: 30, G: 0, B: 0, A: 255}) // bottom-left
	src.SetRGBA(2, 1, color.RGBA{R: 40, G: 0, B: 0, A: 255}) // bottom-right

	tests := []struct {
		name           string
		orient         Orientation
		wantW, wantH   int
		checkX, checkY int
		wantR          byte
	}{
		{"Normal", OrientationNormal, 3, 2, 0, 0, 10},
		{"FlipH", OrientationFlipH, 3, 2, 2, 0, 10},
		{"Rotate180", OrientationRotate180, 3, 2, 2, 1, 10},
		{"FlipV", OrientationFlipV, 3, 2, 0, 1, 10},
		{"Transpose", OrientationTranspose, 2, 3, 0, 0, 10},
		{"Rotate90CW", OrientationRotate90CW, 2, 3, 1, 0, 10},
		{"Transverse", OrientationTransverse, 2, 3, 1, 2, 10},
		{"Rotate270CW", OrientationRotate270CW, 2, 3, 0, 2, 10},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			pb := ConvertImage(src, tc.orient, nil)
			if pb.Width != tc.wantW || pb.Height != tc.wantH {
				t.Fatalf("size = %dx%d, want %dx%d", pb.Width, pb.Height, tc.wantW, tc.wantH)
			}
			gotR := pb.Pix[tc.checkY*pb.Stride+tc.checkX*4]
			if gotR != tc.wantR {
				t.Errorf("pixel(%d,%d).R = %d, want %d", tc.checkX, tc.checkY, gotR, tc.wantR)
			}
		})
	}
}

func TestGIFAnimationDisposalsAndZeroAllocStep(t *testing.T) {
	pal := color.Palette{
		color.RGBA{R: 0, G: 0, B: 0, A: 0},     // 0: transparent
		color.RGBA{R: 255, G: 0, B: 0, A: 255}, // 1: red
		color.RGBA{R: 0, G: 255, B: 0, A: 255}, // 2: green
		color.RGBA{R: 0, G: 0, B: 255, A: 255}, // 3: blue
	}

	f0 := image.NewPaletted(image.Rect(0, 0, 16, 16), pal)
	for i := range f0.Pix {
		f0.Pix[i] = 1 // solid red
	}

	f1 := image.NewPaletted(image.Rect(4, 4, 12, 12), pal)
	for i := range f1.Pix {
		f1.Pix[i] = 2 // green inner square, disposal = Background
	}

	f2 := image.NewPaletted(image.Rect(2, 2, 8, 8), pal)
	for i := range f2.Pix {
		f2.Pix[i] = 3 // blue square, disposal = Previous
	}

	g := &gif.GIF{
		Image:    []*image.Paletted{f0, f1, f2},
		Delay:    []int{5, 10, 15},
		Disposal: []byte{gif.DisposalNone, gif.DisposalBackground, gif.DisposalPrevious},
		Config: image.Config{
			ColorModel: pal,
			Width:      16,
			Height:     16,
		},
	}

	anim := NewGIFAnimation(g)
	if anim.FrameCount() != 3 {
		t.Fatalf("FrameCount = %d, want 3", anim.FrameCount())
	}

	// Verify zero heap allocations per Step().
	allocs := testing.AllocsPerRun(100, func() {
		anim.Step()
	})
	if allocs != 0 {
		t.Fatalf("Animation.Step() allocated %v times per run, want 0", allocs)
	}

	// Verify FrameTexture with update region.
	anim.Reset()
	tex0 := anim.FrameTexture(nil)
	if tex0 == nil {
		t.Fatal("FrameTexture(nil) returned nil")
	}
	anim.Step()
	tex1 := anim.FrameTexture(tex0)
	if tex1 == nil {
		t.Fatal("FrameTexture(tex0) returned nil")
	}
}

func TestDecodeBytesEndToEnd(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 32, 24))
	for y := range 24 {
		for x := range 32 {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x * 8), G: uint8(y * 10), B: 180, A: 255})
		}
	}

	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, img); err != nil {
		t.Fatal(err)
	}

	var jpgBuf bytes.Buffer
	if err := jpeg.Encode(&jpgBuf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name       string
		data       []byte
		opts       Options
		wantFormat string
		wantW      int
		wantH      int
	}{
		{
			name:       "PNG-Full",
			data:       pngBuf.Bytes(),
			opts:       Options{},
			wantFormat: "png",
			wantW:      32,
			wantH:      24,
		},
		{
			name:       "JPEG-Full",
			data:       jpgBuf.Bytes(),
			opts:       Options{},
			wantFormat: "jpeg",
			wantW:      32,
			wantH:      24,
		},
		{
			name:       "PNG-Downscaled",
			data:       pngBuf.Bytes(),
			opts:       Options{MaxWidth: 16, MaxHeight: 16},
			wantFormat: "png",
			wantW:      16,
			wantH:      12,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			decoded, err := DecodeBytes(tc.data, tc.opts)
			if err != nil {
				t.Fatalf("DecodeBytes error: %v", err)
			}
			if decoded.Format != tc.wantFormat {
				t.Errorf("Format = %q, want %q", decoded.Format, tc.wantFormat)
			}
			if decoded.Width() != tc.wantW || decoded.Height() != tc.wantH {
				t.Errorf("size = %dx%d, want %dx%d", decoded.Width(), decoded.Height(), tc.wantW, tc.wantH)
			}
			tex := decoded.Texture()
			if tex == nil {
				t.Fatal("Texture() returned nil")
			}
		})
	}
}

func TestDecodeAnimatedWebP(t *testing.T) {
	encodeSolidVP8L := func(w, h int, c color.NRGBA) []byte {
		var (
			buf   []byte
			bits  uint64
			nBits uint
		)
		writeBits := func(val uint32, n uint) {
			bits |= (uint64(val) & ((1 << n) - 1)) << nBits
			nBits += n
			for nBits >= 8 {
				buf = append(buf, byte(bits))
				bits >>= 8
				nBits -= 8
			}
		}
		writeBits(0x2f, 8)
		writeBits(uint32(w-1), 14)
		writeBits(uint32(h-1), 14)
		writeBits(1, 1)
		writeBits(0, 3)
		writeBits(0, 1)
		writeBits(0, 1)
		writeBits(0, 1)
		for _, sym := range [5]uint8{c.G, c.R, c.B, c.A, 0} {
			writeBits(1, 1)
			writeBits(0, 1)
			writeBits(1, 1)
			writeBits(uint32(sym), 8)
		}
		writeBits(0, 8)
		if nBits > 0 {
			buf = append(buf, byte(bits))
		}
		return buf
	}

	writeChunk := func(dst *bytes.Buffer, fourCC string, payload []byte) {
		dst.WriteString(fourCC[:4])
		var sz [4]byte
		binary.LittleEndian.PutUint32(sz[:], uint32(len(payload)))
		dst.Write(sz[:])
		dst.Write(payload)
		if len(payload)&1 != 0 {
			dst.WriteByte(0)
		}
	}

	var chunks bytes.Buffer
	var vp8x [10]byte
	vp8x[0] = 0x02 | 0x10
	writeUint24LE(vp8x[4:7], 32-1)
	writeUint24LE(vp8x[7:10], 32-1)
	writeChunk(&chunks, "VP8X", vp8x[:])

	anim := [6]byte{0, 0, 0, 255, 0, 0}
	writeChunk(&chunks, "ANIM", anim[:])

	for i, col := range []color.NRGBA{{R: 255, A: 255}, {G: 255, A: 255}} {
		vp8l := encodeSolidVP8L(16, 16, col)
		var anmf bytes.Buffer
		var hdr [16]byte
		writeUint24LE(hdr[0:3], (i*8)/2)
		writeUint24LE(hdr[3:6], (i*8)/2)
		writeUint24LE(hdr[6:9], 16-1)
		writeUint24LE(hdr[9:12], 16-1)
		writeUint24LE(hdr[12:15], 80)
		anmf.Write(hdr[:])
		writeChunk(&anmf, "VP8L", vp8l)
		writeChunk(&chunks, "ANMF", anmf.Bytes())
	}

	var riff bytes.Buffer
	riff.WriteString("RIFF")
	var sz [4]byte
	binary.LittleEndian.PutUint32(sz[:], uint32(4+chunks.Len()))
	riff.Write(sz[:])
	riff.WriteString("WEBP")
	riff.Write(chunks.Bytes())

	decoded, err := DecodeBytes(riff.Bytes(), Options{})
	if err != nil {
		t.Fatalf("DecodeBytes(animated WebP) error: %v", err)
	}
	if !decoded.IsAnimated() || decoded.Anim.FrameCount() != 2 {
		t.Fatalf("expected 2-frame animated WebP, got animated=%v frames=%d", decoded.IsAnimated(), decoded.Anim.FrameCount())
	}
}
