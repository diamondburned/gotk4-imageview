// Package imageview provides an improved GTK4/Libadwaita image viewer widget
// and window backed by pkg/imgload and gdk.MemoryTexture, without gdk-pixbuf.
package imageview

import (
	"math"
	"time"

	coreglib "github.com/diamondburned/gotk4/pkg/core/glib"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/graphene"
	"github.com/diamondburned/gotk4/pkg/gsk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"libdb.so/gotk4-imageview/pkg/imgload"
)

// MaxFPS caps animation playback to 50 FPS (20ms minimum frame delay), matching
// web browser GIF/WebP timing behavior.
const MaxFPS = 50

const minFrameDelay = time.Second / MaxFPS

// Paintable is a custom gdk.Paintable that renders static or animated
// imgload.Image values using gdk.MemoryTexture and gdk.MemoryTextureBuilder,
// with adaptive gsk.ScalingFilter selection and 90-degree rotation support.
type Paintable struct {
	coreglib.Object
	*gdk.Paintable

	texture  gdk.Texturer
	anim     *imgload.Animation
	timer    glib.SourceHandle
	playing  bool
	rotation int // 0, 90, 180, 270 degrees clockwise
	scale    int // HiDPI device scale factor (default 1)
	pt       *graphene.Point
	bounds   *graphene.Rect
}

var paintableType = coreglib.RegisterSubclass[*Paintable](
	coreglib.WithGTypeName("Gotk4ImageviewPaintable"),
	coreglib.WithOverrides(func(p *Paintable) coreglib.ObjectOverrides {
		return coreglib.ObjectOverrides{
			Init: func() {
				p.Paintable = &gdk.Paintable{Object: &p.Object}
				p.playing = true
				p.scale = 1
				p.pt = graphene.NewPointAlloc()
				p.bounds = graphene.RectAlloc()
			},
			Dispose: p.Clear,
		}
	}),
	coreglib.WithOverrides(func(p *Paintable) gdk.PaintableOverrides {
		return gdk.PaintableOverrides{
			IntrinsicWidth:       p.intrinsicWidth,
			IntrinsicHeight:      p.intrinsicHeight,
			IntrinsicAspectRatio: p.intrinsicAspectRatio,
			Snapshot:             p.snapshot,
		}
	}),
)

// NewPaintable creates an empty Paintable.
func NewPaintable() *Paintable {
	return paintableType.New()
}

// NewPaintableFromImage creates a Paintable initialized with img.
func NewPaintableFromImage(img *imgload.Image) *Paintable {
	p := NewPaintable()
	p.SetImage(img)
	return p
}

// Clear stops any active animation timer and releases the current texture.
func (p *Paintable) Clear() {
	p.stopTimer()
	p.texture = nil
	p.anim = nil
}

// SetImage replaces the paintable's image content.
func (p *Paintable) SetImage(img *imgload.Image) {
	p.stopTimer()
	p.texture = nil
	p.anim = nil

	if img != nil {
		if img.IsAnimated() {
			p.anim = img.Anim
			p.anim.Reset()
			p.texture = p.anim.FrameTexture(nil)
		} else {
			p.texture = img.Texture()
		}
	}

	p.InvalidateSize()
	p.InvalidateContents()
}

// SetTexture sets a raw gdk.Texturer directly.
func (p *Paintable) SetTexture(tex gdk.Texturer) {
	p.stopTimer()
	p.anim = nil
	p.texture = tex
	p.InvalidateSize()
	p.InvalidateContents()
}

// Texture returns the currently displayed gdk.Texturer (either the static
// texture or the latest animation frame), suitable for clipboard copy.
func (p *Paintable) Texture() gdk.Texturer {
	return p.texture
}

// HasImage reports whether a texture is loaded.
func (p *Paintable) HasImage() bool {
	return p.texture != nil
}

// IsAnimated reports whether the loaded image is a multi-frame animation.
func (p *Paintable) IsAnimated() bool {
	return p.anim != nil && p.anim.FrameCount() > 1
}

// Playing reports whether animation playback is enabled.
func (p *Paintable) Playing() bool {
	return p.playing
}

// SetPlaying enables or pauses animation playback.
func (p *Paintable) SetPlaying(playing bool) {
	if p.playing == playing {
		return
	}
	p.playing = playing
	if !playing {
		p.stopTimer()
	} else if p.anim != nil {
		p.InvalidateContents()
	}
}

// Rotation returns the current clockwise rotation in degrees (0, 90, 180, 270).
func (p *Paintable) Rotation() int {
	return p.rotation
}

// SetRotation sets the clockwise rotation in degrees (snapped to multiples of 90).
func (p *Paintable) SetRotation(deg int) {
	deg = ((deg % 360) + 360) % 360
	deg = (deg / 90) * 90
	if p.rotation == deg {
		return
	}
	oldSwapped := p.swapsDimensions()
	p.rotation = deg
	if p.swapsDimensions() != oldSwapped {
		p.InvalidateSize()
	}
	p.InvalidateContents()
}

// RotateBy rotates the image by deltaDeg (e.g. +90 or -90).
func (p *Paintable) RotateBy(deltaDeg int) {
	p.SetRotation(p.rotation + deltaDeg)
}

// ScaleFactor returns the HiDPI scale factor used for physical pixel math.
func (p *Paintable) ScaleFactor() int {
	if p.scale < 1 {
		return 1
	}
	return p.scale
}

// SetScaleFactor updates the HiDPI scale factor (1x, 2x, etc.).
func (p *Paintable) SetScaleFactor(scale int) {
	scale = max(scale, 1)
	if p.scale == scale {
		return
	}
	p.scale = scale
	p.InvalidateContents()
}

func (p *Paintable) swapsDimensions() bool {
	return p.rotation == 90 || p.rotation == 270
}

func (p *Paintable) rawSize() (int, int) {
	if p.texture == nil {
		return 0, 0
	}
	b := gdk.BaseTexture(p.texture)
	return b.Width(), b.Height()
}

func (p *Paintable) intrinsicWidth() int {
	w, h := p.rawSize()
	if p.swapsDimensions() {
		return h
	}
	return w
}

func (p *Paintable) intrinsicHeight() int {
	w, h := p.rawSize()
	if p.swapsDimensions() {
		return w
	}
	return h
}

func (p *Paintable) intrinsicAspectRatio() float64 {
	w := p.intrinsicWidth()
	h := p.intrinsicHeight()
	if w <= 0 || h <= 0 {
		return 0
	}
	return float64(w) / float64(h)
}

// SelectScalingFilter chooses the optimal GSK scaling filter for drawing an
// image of (texW, texH) physical pixels into a target box of (drawnW, drawnH)
// logical pixels at the given HiDPI scaleFactor:
//   - Nearest when zoomed in past 1:1 physical pixels (crisp pixel grid, like Loupe)
//   - Linear when between 0.5x and 1.0x physical size (avoids trilinear mipmap blur on HiDPI)
//   - Trilinear when downscaled below 0.5x physical size (eliminates moire/shimmering)
func SelectScalingFilter(texW, texH int, drawnW, drawnH float64, scaleFactor int) gsk.ScalingFilter {
	if texW <= 0 || texH <= 0 || drawnW <= 0 || drawnH <= 0 {
		return gsk.ScalingFilterLinear
	}
	scale := float64(max(scaleFactor, 1))
	physW := drawnW * scale
	physH := drawnH * scale

	ratio := max(physW/float64(texW), physH/float64(texH))
	switch {
	case ratio > 1.05:
		return gsk.ScalingFilterNearest
	case ratio < 0.5:
		return gsk.ScalingFilterTrilinear
	default:
		return gsk.ScalingFilterLinear
	}
}

func (p *Paintable) snapshot(snapshotter gdk.Snapshotter, width, height float64) {
	if p.texture == nil || width <= 0 || height <= 0 {
		return
	}

	if p.anim != nil && p.playing && p.timer == 0 {
		delay := max(p.anim.Delay(), minFrameDelay)
		ms := uint(max(delay.Milliseconds(), 1))
		p.timer = glib.TimeoutAdd(ms, func() {
			p.timer = 0
			if p.anim == nil || !p.playing {
				return
			}
			p.anim.Step()
			p.texture = p.anim.FrameTexture(p.texture)
			p.InvalidateContents()
		})
	}

	snap, ok := snapshotter.(*gtk.Snapshot)
	if !ok {
		gdk.BaseTexture(p.texture).Snapshot(snapshotter, width, height)
		return
	}

	rawW, rawH := p.rawSize()
	rotW, rotH := float64(p.intrinsicWidth()), float64(p.intrinsicHeight())
	if rotW <= 0 || rotH <= 0 {
		return
	}

	// Preserve aspect ratio within (width, height) like ContentFitContain.
	scale := min(width/rotW, height/rotH)
	drawRotW := math.Round(rotW * scale)
	drawRotH := math.Round(rotH * scale)
	offsetX := math.Floor((width - drawRotW) / 2)
	offsetY := math.Floor((height - drawRotH) / 2)

	drawRawW, drawRawH := drawRotW, drawRotH
	if p.swapsDimensions() {
		drawRawW, drawRawH = drawRotH, drawRotW
	}

	filter := SelectScalingFilter(rawW, rawH, drawRawW, drawRawH, p.scale)

	snap.Save()
	p.pt.Init(float32(offsetX+drawRotW/2), float32(offsetY+drawRotH/2))
	snap.Translate(p.pt)
	if p.rotation != 0 {
		snap.Rotate(float32(p.rotation))
	}
	p.bounds.Init(
		float32(-drawRawW/2),
		float32(-drawRawH/2),
		float32(drawRawW),
		float32(drawRawH),
	)
	snap.AppendScaledTexture(p.texture, filter, p.bounds)
	snap.Restore()
}

func (p *Paintable) stopTimer() {
	if p.timer != 0 {
		glib.SourceRemove(p.timer)
		p.timer = 0
	}
}
