package imgload

import (
	"image"
	"image/color"
	"image/gif"
	"time"
	"unsafe"

	"github.com/diamondburned/gotk4/pkg/gdk/v4"
)

// Animation represents a decoded multi-frame animation (such as an animated
// GIF or animated WebP) that composites frames onto a reusable canvas buffer
// with zero Go heap allocations per frame step.
type Animation struct {
	width  int
	height int

	frames    []*image.Paletted
	rgba      []*image.RGBA // used by non-paletted animations such as WebP
	delays    []time.Duration
	disposals []byte
	blend     []bool // true = alpha-over blend, false = src copy

	canvas   []byte
	prev     []byte // allocated only when DisposalPrevious is used
	bgColor  uint32
	hasBg    bool
	loopRest bool

	index        int
	prevDisposal byte
	prevRect     image.Rectangle
	dirtyRect    image.Rectangle
}

// NewGIFAnimation creates a zero-alloc-per-step Animation from a decoded
// *gif.GIF.
func NewGIFAnimation(g *gif.GIF) *Animation {
	w := g.Config.Width
	h := g.Config.Height
	for _, f := range g.Image {
		fb := f.Bounds()
		if fb.Max.X > w {
			w = fb.Max.X
		}
		if fb.Max.Y > h {
			h = fb.Max.Y
		}
	}
	if w <= 0 || h <= 0 {
		w, h = 1, 1
	}

	n := len(g.Image)
	delays := make([]time.Duration, n)
	disposals := make([]byte, n)
	blend := make([]bool, n)
	needsPrev := false

	for i := range n {
		if i < len(g.Delay) {
			d := time.Duration(g.Delay[i]) * 10 * time.Millisecond
			if d <= 0 {
				d = 100 * time.Millisecond
			}
			delays[i] = d
		} else {
			delays[i] = 100 * time.Millisecond
		}
		if i < len(g.Disposal) {
			disposals[i] = g.Disposal[i]
			if g.Disposal[i] == gif.DisposalPrevious {
				needsPrev = true
			}
		}
		blend[i] = true
	}

	var prev []byte
	if needsPrev {
		prev = make([]byte, w*h*4)
	}

	var bgColor uint32
	var hasBg bool
	if pal, ok := g.Config.ColorModel.(color.Palette); ok && int(g.BackgroundIndex) < len(pal) {
		var lut [256]uint32
		buildPaletteLUT(&lut, pal)
		bgColor = lut[g.BackgroundIndex]
		hasBg = true
	}
	// Per the GIF89a spec, if a frame has a transparent color index,
	// DisposalBackground clears to transparent rather than BackgroundIndex.
	for _, f := range g.Image {
		if transparentIndex(f.Palette) >= 0 {
			bgColor = 0
			break
		}
	}

	a := &Animation{
		width:     w,
		height:    h,
		frames:    g.Image,
		delays:    delays,
		disposals: disposals,
		blend:     blend,
		canvas:    make([]byte, w*h*4),
		prev:      prev,
		bgColor:   bgColor,
		hasBg:     hasBg,
		index:     -1,
	}
	a.Step()
	return a
}

// Width returns the canvas width in pixels.
func (a *Animation) Width() int { return a.width }

// Height returns the canvas height in pixels.
func (a *Animation) Height() int { return a.height }

// FrameCount returns the number of frames in the animation.
func (a *Animation) FrameCount() int {
	if len(a.frames) > 0 {
		return len(a.frames)
	}
	return len(a.rgba)
}

// Index returns the current frame index.
func (a *Animation) Index() int {
	if a.index < 0 {
		return 0
	}
	return a.index
}

// Delay returns the display duration of the current frame.
func (a *Animation) Delay() time.Duration {
	idx := a.Index()
	if idx >= 0 && idx < len(a.delays) {
		return a.delays[idx]
	}
	return 100 * time.Millisecond
}

// DirtyRect returns the bounding rectangle modified by the most recent Step.
func (a *Animation) DirtyRect() image.Rectangle {
	return a.dirtyRect
}

// Buffer returns the current composited canvas as a PixelBuffer.
// The returned slice aliases the animation's reusable canvas buffer.
func (a *Animation) Buffer() PixelBuffer {
	return PixelBuffer{
		Pix:      a.canvas,
		Width:    a.width,
		Height:   a.height,
		Stride:   a.width * 4,
		Format:   gdk.MemoryR8G8B8A8Premultiplied,
		Borrowed: true,
	}
}

// Reset resets the animation back to the first frame.
func (a *Animation) Reset() {
	clear(a.canvas)
	if a.prev != nil {
		clear(a.prev)
	}
	a.index = -1
	a.prevDisposal = 0
	a.prevRect = image.Rectangle{}
	a.Step()
}

// Step advances the animation by one frame in-place on the canvas buffer.
// Step performs zero heap allocations.
func (a *Animation) Step() time.Duration {
	count := a.FrameCount()
	if count == 0 {
		return 0
	}

	next := a.index + 1
	if next >= count {
		next = 0
		clear(a.canvas)
		a.prevDisposal = 0
		a.prevRect = image.Rectangle{}
		a.loopRest = true
	}

	canvasBounds := image.Rect(0, 0, a.width, a.height)
	canvasU32 := unsafe.Slice((*uint32)(unsafe.Pointer(&a.canvas[0])), a.width*a.height)

	// 1. Apply the previous frame's disposal action.
	var disposedRect image.Rectangle
	switch a.prevDisposal {
	case gif.DisposalBackground:
		r := a.prevRect.Intersect(canvasBounds)
		if !r.Empty() {
			fillRectU32(canvasU32, a.width, r, a.bgColor)
			disposedRect = r
		}
	case gif.DisposalPrevious:
		r := a.prevRect.Intersect(canvasBounds)
		if !r.Empty() && a.prev != nil {
			prevU32 := unsafe.Slice((*uint32)(unsafe.Pointer(&a.prev[0])), a.width*a.height)
			copyRectU32(canvasU32, prevU32, a.width, r)
			disposedRect = r
		}
	}

	// 2. Determine the current frame's bounds and disposal mode.
	var frameBounds image.Rectangle
	if len(a.frames) > 0 {
		frameBounds = a.frames[next].Bounds()
	} else {
		frameBounds = a.rgba[next].Bounds()
	}
	r := frameBounds.Intersect(canvasBounds)

	disp := byte(0)
	if next < len(a.disposals) {
		disp = a.disposals[next]
	}

	// Save canvas region if this frame specifies DisposalPrevious.
	if disp == gif.DisposalPrevious && a.prev != nil && !r.Empty() {
		prevU32 := unsafe.Slice((*uint32)(unsafe.Pointer(&a.prev[0])), a.width*a.height)
		copyRectU32(prevU32, canvasU32, a.width, r)
	}

	// 3. Composite the current frame onto the canvas.
	if !r.Empty() {
		if len(a.frames) > 0 {
			a.compositePaletted(canvasU32, a.frames[next], r, a.blend[next])
		} else {
			a.compositeRGBA(a.rgba[next], r, a.blend[next])
		}
	}

	if a.loopRest || next == 0 {
		a.dirtyRect = canvasBounds
		a.loopRest = false
	} else if !disposedRect.Empty() {
		a.dirtyRect = disposedRect.Union(r)
	} else {
		a.dirtyRect = r
	}

	a.index = next
	a.prevDisposal = disp
	a.prevRect = r

	return a.Delay()
}

func (a *Animation) compositePaletted(canvasU32 []uint32, f *image.Paletted, r image.Rectangle, over bool) {
	var lut [256]uint32
	buildPaletteLUT(&lut, f.Palette)
	transIdx := transparentIndex(f.Palette)

	w := r.Dx()
	for y := r.Min.Y; y < r.Max.Y; y++ {
		sOff := f.PixOffset(r.Min.X, y)
		sRow := f.Pix[sOff : sOff+w]
		dRow := canvasU32[y*a.width+r.Min.X : y*a.width+r.Max.X]
		if !over || transIdx < 0 {
			for x, idx := range sRow {
				dRow[x] = lut[idx]
			}
		} else {
			tIdx := uint8(transIdx)
			for x, idx := range sRow {
				if idx != tIdx {
					dRow[x] = lut[idx]
				}
			}
		}
	}
}

func (a *Animation) compositeRGBA(f *image.RGBA, r image.Rectangle, over bool) {
	stride := a.width * 4
	rowBytes := r.Dx() * 4
	for y := r.Min.Y; y < r.Max.Y; y++ {
		sOff := f.PixOffset(r.Min.X, y)
		sRow := f.Pix[sOff : sOff+rowBytes]
		dOff := y*stride + r.Min.X*4
		dRow := a.canvas[dOff : dOff+rowBytes]
		if !over {
			copy(dRow, sRow)
			continue
		}
		for i := 0; i < rowBytes; i += 4 {
			sa := uint32(sRow[i+3])
			if sa == 0 {
				continue
			}
			if sa == 255 {
				dRow[i+0] = sRow[i+0]
				dRow[i+1] = sRow[i+1]
				dRow[i+2] = sRow[i+2]
				dRow[i+3] = 255
				continue
			}
			invA := 255 - sa
			dRow[i+0] = byte(uint32(sRow[i+0]) + (uint32(dRow[i+0])*invA+127)/255)
			dRow[i+1] = byte(uint32(sRow[i+1]) + (uint32(dRow[i+1])*invA+127)/255)
			dRow[i+2] = byte(uint32(sRow[i+2]) + (uint32(dRow[i+2])*invA+127)/255)
			dRow[i+3] = byte(sa + (uint32(dRow[i+3])*invA+127)/255)
		}
	}
}

func transparentIndex(pal color.Palette) int {
	for i, c := range pal {
		if c == nil {
			return i
		}
		if _, _, _, a := c.RGBA(); a == 0 {
			return i
		}
	}
	return -1
}

func fillRectU32(dst []uint32, stride int, r image.Rectangle, val uint32) {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		row := dst[y*stride+r.Min.X : y*stride+r.Max.X]
		for i := range row {
			row[i] = val
		}
	}
}

func copyRectU32(dst, src []uint32, stride int, r image.Rectangle) {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		dRow := dst[y*stride+r.Min.X : y*stride+r.Max.X]
		sRow := src[y*stride+r.Min.X : y*stride+r.Max.X]
		copy(dRow, sRow)
	}
}
