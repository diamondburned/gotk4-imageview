package imgload

import (
	"bytes"
	"fmt"
	"image"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"sync"

	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	"golang.org/x/image/webp"
)

// Options configures image decoding and conversion.
type Options struct {
	// MaxWidth and MaxHeight optionally cap the decoded image dimensions.
	// When > 0 and the decoded image exceeds either bound, it is downscaled
	// preserving aspect ratio.
	MaxWidth  int
	MaxHeight int

	// IgnoreEXIF disables automatic JPEG APP1 EXIF orientation correction.
	IgnoreEXIF bool

	// FirstFrameOnly decodes only the first frame of animated GIFs or WebPs.
	FirstFrameOnly bool

	// Pool optionally provides a reusable byte slice pool for pixel conversion.
	Pool *BufferPool
}

// Image holds a decoded static or animated image ready for GTK4 display.
type Image struct {
	Format      string
	Orientation Orientation
	Static      PixelBuffer
	Anim        *Animation
}

// Width returns the display width in pixels.
func (img *Image) Width() int {
	if img.Anim != nil {
		return img.Anim.Width()
	}
	return img.Static.Width
}

// Height returns the display height in pixels.
func (img *Image) Height() int {
	if img.Anim != nil {
		return img.Anim.Height()
	}
	return img.Static.Height
}

// IsAnimated reports whether the image has multiple animation frames.
func (img *Image) IsAnimated() bool {
	return img.Anim != nil && img.Anim.FrameCount() > 1
}

// Texture creates a *gdk.MemoryTexture for a static image (or the first frame
// of an animation).
func (img *Image) Texture() *gdk.MemoryTexture {
	if img.Anim != nil {
		return img.Anim.Buffer().ToTexture(true)
	}
	return img.Static.ToTexture(false)
}

var readerPool = sync.Pool{
	New: func() any {
		return new(bytes.Reader)
	},
}

func acquireReader(data []byte) *bytes.Reader {
	r := readerPool.Get().(*bytes.Reader)
	r.Reset(data)
	return r
}

func releaseReader(r *bytes.Reader) {
	r.Reset(nil)
	readerPool.Put(r)
}

// DecodeFile reads and decodes an image file from disk.
func DecodeFile(path string, opts Options) (*Image, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return DecodeBytes(data, opts)
}

// Decode reads all bytes from r and decodes the image.
func Decode(r io.Reader, opts Options) (*Image, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return DecodeBytes(data, opts)
}

// DecodeBytes decodes a static or animated image from an in-memory byte slice
// with minimal allocations.
func DecodeBytes(data []byte, opts Options) (*Image, error) {
	var out Image
	if err := DecodeBytesInto(&out, data, opts); err != nil {
		return nil, err
	}
	return &out, nil
}

// DecodeBytesInto decodes a static or animated image from data into a
// caller-supplied *Image struct, avoiding heap allocation of the Image wrapper
// and bytes.Reader.
func DecodeBytesInto(dst *Image, data []byte, opts Options) error {
	if len(data) == 0 {
		return fmt.Errorf("imgload: empty image data")
	}

	if !opts.FirstFrameOnly {
		if isGIF(data) {
			r := acquireReader(data)
			g, err := gif.DecodeAll(r)
			releaseReader(r)
			if err == nil && len(g.Image) > 1 {
				*dst = Image{
					Format:      "gif",
					Orientation: OrientationNormal,
					Anim:        NewGIFAnimation(g),
				}
				return nil
			}
		}
		if IsAnimatedWebP(data) {
			anim, err := DecodeAnimatedWebP(data)
			if err == nil && anim.FrameCount() > 1 {
				*dst = Image{
					Format:      "webp",
					Orientation: OrientationNormal,
					Anim:        anim,
				}
				return nil
			}
		}
	}

	orient := OrientationNormal
	if !opts.IgnoreEXIF {
		orient = ParseEXIFOrientation(data)
	}

	r := acquireReader(data)
	decoded, format, err := decodeKnown(r, data)
	releaseReader(r)
	if err != nil {
		return fmt.Errorf("imgload: decode: %w", err)
	}

	var scratch []byte
	if opts.Pool != nil {
		b := decoded.Bounds()
		scratch = opts.Pool.Get(b.Dx() * b.Dy() * 4)
	}

	pb := ConvertImage(decoded, orient, scratch)
	if opts.MaxWidth > 0 || opts.MaxHeight > 0 {
		pb = Downscale(pb, opts.MaxWidth, opts.MaxHeight, nil)
	}

	*dst = Image{
		Format:      format,
		Orientation: orient,
		Static:      pb,
	}
	return nil
}

// decodeKnown dispatches directly to image/jpeg, image/png, image/gif, or
// golang.org/x/image/webp based on magic bytes, avoiding image.Decode's
// bufio.NewReader(r) 4 KB buffer allocation when the format is known.
func decodeKnown(r *bytes.Reader, data []byte) (image.Image, string, error) {
	switch {
	case len(data) >= 3 && data[0] == 0xFF && data[1] == 0xD8 && data[2] == 0xFF:
		img, err := jpeg.Decode(r)
		return img, "jpeg", err
	case len(data) >= 8 && string(data[:8]) == "\x89PNG\r\n\x1a\n":
		img, err := png.Decode(r)
		return img, "png", err
	case isGIF(data):
		img, err := gif.Decode(r)
		return img, "gif", err
	case len(data) >= 12 && string(data[0:4]) == "RIFF" && string(data[8:12]) == "WEBP":
		img, err := webp.Decode(r)
		return img, "webp", err
	default:
		return image.Decode(r)
	}
}

func isGIF(data []byte) bool {
	return len(data) >= 6 &&
		data[0] == 'G' && data[1] == 'I' && data[2] == 'F' &&
		data[3] == '8' && (data[4] == '7' || data[4] == '9') && data[5] == 'a'
}
