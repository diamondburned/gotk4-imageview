package imgload

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"
)

func BenchmarkConvertRGBAZeroCopy(b *testing.B) {
	img := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
	b.SetBytes(1920 * 1080 * 4)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		pb := ConvertImage(img, OrientationNormal, nil)
		if !pb.Borrowed {
			b.Fatal("expected borrowed")
		}
	}
}

func BenchmarkConvertYCbCr420ToRGB(b *testing.B) {
	img := image.NewYCbCr(image.Rect(0, 0, 1920, 1080), image.YCbCrSubsampleRatio420)
	dst := make([]byte, 1920*1080*3)
	b.SetBytes(1920 * 1080 * 3)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = ConvertImage(img, OrientationNormal, dst)
	}
}

func BenchmarkConvertPaletted(b *testing.B) {
	pal := make(color.Palette, 256)
	for i := range pal {
		pal[i] = color.RGBA{R: uint8(i), G: uint8(255 - i), B: 128, A: 255}
	}
	img := image.NewPaletted(image.Rect(0, 0, 1024, 1024), pal)
	dst := make([]byte, 1024*1024*4)
	b.SetBytes(1024 * 1024 * 4)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = ConvertImage(img, OrientationNormal, dst)
	}
}

func BenchmarkGIFAnimationStep(b *testing.B) {
	pal := color.Palette{
		color.RGBA{0, 0, 0, 0},
		color.RGBA{255, 0, 0, 255},
		color.RGBA{0, 255, 0, 255},
		color.RGBA{0, 0, 255, 255},
	}
	frames := make([]*image.Paletted, 8)
	delays := make([]int, 8)
	disposals := make([]byte, 8)
	for i := range frames {
		f := image.NewPaletted(image.Rect(i*10, i*10, i*10+256, i*10+256), pal)
		for p := range f.Pix {
			f.Pix[p] = uint8((p + i) & 3)
		}
		frames[i] = f
		delays[i] = 4
		disposals[i] = gif.DisposalBackground
	}
	g := &gif.GIF{
		Image:    frames,
		Delay:    delays,
		Disposal: disposals,
		Config:   image.Config{ColorModel: pal, Width: 512, Height: 512},
	}
	anim := NewGIFAnimation(g)

	b.SetBytes(256 * 256 * 4)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		anim.Step()
	}
}

func BenchmarkDownscale4KTo1080p(b *testing.B) {
	src := PixelBuffer{
		Pix:    make([]byte, 3840*2160*4),
		Width:  3840,
		Height: 2160,
		Stride: 3840 * 4,
	}
	dst := make([]byte, 1920*1080*4)
	b.SetBytes(1920 * 1080 * 4)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		_ = Downscale(src, 1920, 1080, dst)
	}
}

func BenchmarkDecodePNGToTexture(b *testing.B) {
	img := image.NewNRGBA(image.Rect(0, 0, 512, 512))
	for i := range img.Pix {
		img.Pix[i] = uint8(i)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		b.Fatal(err)
	}
	data := buf.Bytes()

	b.SetBytes(512 * 512 * 4)
	b.ReportAllocs()
	b.ResetTimer()
	var decoded Image
	for b.Loop() {
		if err := DecodeBytesInto(&decoded, data, Options{}); err != nil {
			b.Fatal(err)
		}
		tex := decoded.Texture()
		if tex == nil {
			b.Fatal("nil texture")
		}
	}
}

func BenchmarkDecodeJPEGToTextureWithPool(b *testing.B) {
	img := image.NewRGBA(image.Rect(0, 0, 512, 512))
	for i := range img.Pix {
		img.Pix[i] = uint8(i)
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 85}); err != nil {
		b.Fatal(err)
	}
	data := buf.Bytes()
	var pool BufferPool

	b.SetBytes(512 * 512 * 3)
	b.ReportAllocs()
	b.ResetTimer()
	var decoded Image
	for b.Loop() {
		if err := DecodeBytesInto(&decoded, data, Options{Pool: &pool}); err != nil {
			b.Fatal(err)
		}
		tex := decoded.Static.ToTexture(true)
		if tex == nil {
			b.Fatal("nil texture")
		}
		pool.Put(decoded.Static.Pix)
	}
}
