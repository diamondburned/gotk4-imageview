package imageview

import (
	"math"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"libdb.so/gotk4-imageview/pkg/imgload"
)

const (
	// MaxZoom is the maximum zoom multiplier in logical pixels per image pixel.
	MaxZoom = 20.0
	// ZoomStep is the discrete zoom multiplier per wheel/button step.
	ZoomStep = 1.25
	// ZoomAnimationDuration is the duration in milliseconds for animated zoom transitions.
	ZoomAnimationDuration = 200
)

// View is a zoomable, pannable, rotatable GTK4 image viewer widget backed by
// Paintable. It incorporates key improvements from GNOME Loupe and Tuba over
// Dissent's zoomView:
//   - Custom Paintable Snapshot with Nearest (pixel-grid when zoomed in) and
//     Trilinear (mipmap when zoomed out) filtering
//   - HiDPI physical-pixel awareness
//   - Smooth 200ms adw.TimedAnimation transitions for double-click and buttons
//   - Hidden scrollbars (gtk.PolicyExternal) and kinetic-scroll cancellation on zoom
//   - Unmodified mouse-wheel zoom around cursor (plus Ctrl+scroll)
//   - "grabbing" cursor feedback while panning
type View struct {
	*gtk.ScrolledWindow
	paintable *Paintable
	picture   *gtk.Picture
	sizer     *gtk.Picture
	frame     *gtk.Overlay

	// zoom is the size of an image pixel in logical pixels, or 0 when fitted.
	zoom    float64
	pointer [2]float64

	zoomAnim   *adw.TimedAnimation
	zoomTarget *adw.CallbackAnimationTarget
	animFrom   float64
	animTo     float64
	animAnchor [2]float64

	onZoomChanged func(effectiveZoom float64, fitted bool)
}

// NewView creates a new zoomable image View.
func NewView() *View {
	p := NewPaintable()
	v := &View{
		paintable: p,
		picture:   gtk.NewPictureForPaintable(p),
		sizer:     gtk.NewPicture(),
		frame:     gtk.NewOverlay(),
	}

	v.picture.SetCanShrink(true)
	v.picture.SetContentFit(gtk.ContentFitContain)
	p.ConnectInvalidateSize(v.Fit)

	v.sizer.SetCanTarget(false)

	v.frame.SetHAlign(gtk.AlignCenter)
	v.frame.SetVAlign(gtk.AlignCenter)
	v.frame.SetChild(v.sizer)
	v.frame.AddOverlay(v.picture)

	v.ScrolledWindow = gtk.NewScrolledWindow()
	v.SetHExpand(true)
	v.SetVExpand(true)
	// Hide scrollbars like GNOME Loupe so panning never causes layout jitter.
	v.SetPolicy(gtk.PolicyExternal, gtk.PolicyExternal)
	v.SetKineticScrolling(true)
	v.SetChild(v.frame)

	// Track HiDPI scale factor changes for accurate physical-pixel filtering.
	updateScale := func() {
		v.paintable.SetScaleFactor(v.ScaleFactor())
	}
	v.ConnectRealize(updateScale)
	v.NotifyProperty("scale-factor", updateScale)

	v.zoomTarget = adw.NewCallbackAnimationTarget(func(t float64) {
		z := v.animFrom + (v.animTo-v.animFrom)*t
		v.applyZoom(z, v.animAnchor[0], v.animAnchor[1])
	})
	v.zoomAnim = adw.NewTimedAnimation(v.ScrolledWindow, 0, 1, ZoomAnimationDuration, v.zoomTarget)
	v.zoomAnim.SetEasing(adw.EaseOutCubic)

	motion := gtk.NewEventControllerMotion()
	motion.ConnectMotion(func(x, y float64) {
		v.pointer = [2]float64{x, y}
	})
	v.AddController(motion)

	// Support both plain scroll wheel and Ctrl+scroll for zooming around cursor.
	scroll := gtk.NewEventControllerScroll(gtk.EventControllerScrollVertical)
	scroll.SetPropagationPhase(gtk.PhaseCapture)
	scroll.ConnectScroll(func(_, dy float64) bool {
		state := scroll.CurrentEventState()
		// Allow Shift+scroll to pass through if needed, otherwise zoom.
		if state&gdk.ShiftMask != 0 && state&gdk.ControlMask == 0 {
			return false
		}
		v.cancelAnimation()
		v.applyZoom(v.CurrentZoom()*math.Pow(ZoomStep, -dy), v.pointer[0], v.pointer[1])
		return true
	})
	v.AddController(scroll)

	var pinchZoom float64
	pinch := gtk.NewGestureZoom()
	pinch.ConnectBegin(func(*gdk.EventSequence) {
		v.cancelAnimation()
		pinchZoom = v.CurrentZoom()
	})
	pinch.ConnectScaleChanged(func(scale float64) {
		if x, y, ok := pinch.BoundingBoxCenter(); ok {
			v.applyZoom(pinchZoom*scale, x, y)
		}
	})
	v.AddController(pinch)

	var dragStart [2]float64
	drag := gtk.NewGestureDrag()
	drag.ConnectDragBegin(func(_, _ float64) {
		dev := drag.CurrentEventDevice()
		if dev != nil && gdk.BaseDevice(dev).Source() == gdk.SourceTouchscreen {
			drag.SetState(gtk.EventSequenceDenied)
			return
		}
		if !v.CanPan() {
			drag.SetState(gtk.EventSequenceDenied)
			return
		}
		v.cancelAnimation()
		v.SetCursorFromName("grabbing")
		dragStart = [2]float64{v.HAdjustment().Value(), v.VAdjustment().Value()}
	})
	drag.ConnectDragUpdate(func(dx, dy float64) {
		v.HAdjustment().SetValue(dragStart[0] - dx)
		v.VAdjustment().SetValue(dragStart[1] - dy)
	})
	drag.ConnectDragEnd(func(_, _ float64) {
		v.SetCursor(nil)
	})
	v.AddController(drag)

	click := gtk.NewGestureClick()
	click.ConnectPressed(func(n int, x, y float64) {
		if n != 2 {
			return
		}
		if v.zoom != 0 {
			v.AnimateFit(x, y)
			return
		}
		v.AnimateZoom(max(1.0, 2.0*v.FitZoom()), x, y)
	})
	v.AddController(click)

	return v
}

// Paintable returns the underlying Paintable.
func (v *View) Paintable() *Paintable {
	return v.paintable
}

// SetImage loads an *imgload.Image into the view and resets zoom/rotation.
func (v *View) SetImage(img *imgload.Image) {
	v.cancelAnimation()
	v.paintable.SetRotation(0)
	v.paintable.SetImage(img)
	v.Fit()
}

// SetOnZoomChanged registers a callback invoked whenever the effective zoom changes.
func (v *View) SetOnZoomChanged(fn func(effectiveZoom float64, fitted bool)) {
	v.onZoomChanged = fn
}

// IsFitted reports whether the view is currently in fit-to-window mode.
func (v *View) IsFitted() bool {
	return v.zoom == 0
}

// CanPan reports whether the zoomed image exceeds the viewport along either axis.
func (v *View) CanPan() bool {
	if v.zoom == 0 {
		return false
	}
	hadj := v.HAdjustment()
	vadj := v.VAdjustment()
	return hadj.Upper() > hadj.PageSize() || vadj.Upper() > vadj.PageSize()
}

// ImageSize returns the rotated intrinsic dimensions of the current image.
func (v *View) ImageSize() (w, h float64, ok bool) {
	iw := v.paintable.intrinsicWidth()
	ih := v.paintable.intrinsicHeight()
	w, h = float64(iw), float64(ih)
	return w, h, w > 0 && h > 0
}

// FitZoom returns the zoom level at which the image fits inside the viewport
// without upscaling past 1:1 logical pixels.
func (v *View) FitZoom() float64 {
	w, h, ok := v.ImageSize()
	if !ok {
		return 1
	}
	vw, vh := float64(v.Width()), float64(v.Height())
	if vw <= 0 || vh <= 0 {
		return 1
	}
	return min(1.0, vw/w, vh/h)
}

// CurrentZoom returns the active logical zoom ratio.
func (v *View) CurrentZoom() float64 {
	if v.zoom == 0 {
		return v.FitZoom()
	}
	return v.zoom
}

// Fit resets the view to fit-to-window mode immediately.
func (v *View) Fit() {
	v.cancelAnimation()
	v.fitImmediate()
}

func (v *View) fitImmediate() {
	v.cancelKineticScroll()
	v.zoom = 0
	v.sizer.SetCanShrink(true)
	if w, h, ok := v.ImageSize(); ok {
		v.sizer.SetPaintable(gdk.NewPaintableEmpty(int(w), int(h)))
	}
	v.notifyZoom()
}

// AnimateFit smoothly animates back to fit-to-window mode around (x, y).
func (v *View) AnimateFit(x, y float64) {
	if v.zoom == 0 {
		return
	}
	v.AnimateZoom(v.FitZoom(), x, y)
}

// ZoomBy multiplies the current zoom by factor around the center of the view
// using a smooth animation.
func (v *View) ZoomBy(factor float64) {
	v.ZoomTo(v.CurrentZoom() * factor)
}

// ZoomTo smoothly animates the zoom to targetZoom around the center of the view.
func (v *View) ZoomTo(targetZoom float64) {
	v.AnimateZoom(targetZoom, float64(v.Width())/2, float64(v.Height())/2)
}

// AnimateZoom smoothly animates from the current zoom to targetZoom while
// keeping the point (x, y) anchored.
func (v *View) AnimateZoom(targetZoom, x, y float64) {
	_, _, ok := v.ImageSize()
	if !ok {
		return
	}
	fit := v.FitZoom()
	targetZoom = max(fit, min(targetZoom, max(MaxZoom, fit)))

	v.cancelAnimation()
	v.animFrom = v.CurrentZoom()
	v.animTo = targetZoom
	v.animAnchor = [2]float64{x, y}

	if math.Abs(v.animTo-v.animFrom) < 1e-4 {
		v.applyZoom(targetZoom, x, y)
		return
	}
	v.zoomAnim.Play()
}

// RotateBy rotates the image by deltaDeg (e.g. 90 or -90) and re-fits.
func (v *View) RotateBy(deltaDeg int) {
	v.cancelAnimation()
	v.paintable.RotateBy(deltaDeg)
	v.fitImmediate()
}

func (v *View) cancelAnimation() {
	if v.zoomAnim != nil && v.zoomAnim.State() == adw.AnimationPlaying {
		v.zoomAnim.Skip()
	}
}

// cancelKineticScroll toggles kinetic scrolling off and back on to immediately
// halt any in-flight deceleration before adjusting scroll bounds (matching Loupe).
func (v *View) cancelKineticScroll() {
	v.SetKineticScrolling(false)
	v.SetKineticScrolling(true)
}

func (v *View) applyZoom(zoom, x, y float64) {
	w, h, ok := v.ImageSize()
	if !ok {
		return
	}

	fit := v.FitZoom()
	if zoom <= fit*1.001 {
		v.fitImmediate()
		return
	}
	zoom = min(zoom, max(MaxZoom, fit))

	v.cancelKineticScroll()

	hadj := v.HAdjustment()
	vadj := v.VAdjustment()

	old := v.CurrentZoom()
	ix := viewToContent(hadj, x, w*old) / old
	iy := viewToContent(vadj, y, h*old) / old

	v.zoom = zoom
	v.sizer.SetCanShrink(false)
	v.sizer.SetPaintable(gdk.NewPaintableEmpty(int(math.Round(w*zoom)), int(math.Round(h*zoom))))

	scrollContentTo(hadj, ix*zoom-x, w*zoom)
	scrollContentTo(vadj, iy*zoom-y, h*zoom)
	v.notifyZoom()
}

func (v *View) notifyZoom() {
	if v.onZoomChanged != nil {
		v.onZoomChanged(v.CurrentZoom(), v.zoom == 0)
	}
}

func viewToContent(adj *gtk.Adjustment, pos, size float64) float64 {
	if page := adj.PageSize(); size < page {
		return pos - (page-size)/2
	}
	return adj.Value() + pos
}

func scrollContentTo(adj *gtk.Adjustment, value, size float64) {
	adj.SetUpper(max(size, adj.PageSize()))
	adj.SetValue(value)
}
