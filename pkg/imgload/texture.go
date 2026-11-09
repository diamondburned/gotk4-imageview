package imgload

import (
	"image"

	"github.com/diamondburned/gotk4/pkg/cairo"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
)

// ToTexture creates an immutable *gdk.MemoryTexture from a PixelBuffer.
//
// When copyBytes is false, ToTexture uses glib.NewBytesWithGo so GTK reads
// directly from pb.Pix without copying the pixel buffer across the cgo
// boundary. Callers must not mutate pb.Pix after passing copyBytes=false.
//
// When copyBytes is true (for example, when pb.Pix comes from a reusable
// BufferPool or Animation canvas), ToTexture uses glib.NewBytes, which copies
// into C-managed memory so pb.Pix can be reused immediately.
func (pb PixelBuffer) ToTexture(copyBytes bool) *gdk.MemoryTexture {
	if pb.Width <= 0 || pb.Height <= 0 || len(pb.Pix) == 0 {
		return nil
	}
	var gbytes *glib.Bytes
	if copyBytes {
		gbytes = glib.NewBytes(pb.Pix)
	} else {
		gbytes = glib.NewBytesWithGo(pb.Pix)
	}
	return gdk.NewMemoryTexture(pb.Width, pb.Height, pb.Format, gbytes, uint(pb.Stride))
}

// FrameTexture builds a *gdk.MemoryTexture for the animation's current frame,
// optionally attaching prev and the frame's DirtyRect via
// gdk.MemoryTextureBuilder so GTK4's renderer only uploads the damaged sub-rect
// to the GPU.
func (a *Animation) FrameTexture(prev gdk.Texturer) gdk.Texturer {
	if a.width <= 0 || a.height <= 0 || len(a.canvas) == 0 {
		return nil
	}

	// Copy canvas bytes into C GBytes so a.canvas can be mutated in-place on
	// the next Step() with zero Go heap allocations.
	gbytes := glib.NewBytes(a.canvas)

	dirty := a.dirtyRect.Intersect(image.Rect(0, 0, a.width, a.height))
	if prev == nil || dirty.Empty() {
		return gdk.NewMemoryTexture(
			a.width,
			a.height,
			gdk.MemoryR8G8B8A8Premultiplied,
			gbytes,
			uint(a.width*4),
		)
	}

	builder := gdk.NewMemoryTextureBuilder()
	builder.SetWidth(a.width)
	builder.SetHeight(a.height)
	builder.SetFormat(gdk.MemoryR8G8B8A8Premultiplied)
	builder.SetStride(uint(a.width * 4))
	builder.SetBytes(gbytes)
	builder.SetUpdateTexture(prev)

	if region, err := (*cairo.Region)(nil).CreateRectangle(&cairo.Rectangle{
		X:      dirty.Min.X,
		Y:      dirty.Min.Y,
		Width:  dirty.Dx(),
		Height: dirty.Dy(),
	}); err == nil {
		builder.SetUpdateRegion(region)
	}

	return builder.Build()
}
