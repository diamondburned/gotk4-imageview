package imageview

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gdk/v4"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/glib/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"libdb.so/gotk4-imageview/pkg/imgload"
)

const viewerCSS = `
window.imageview-window {
	background-color: #0d0d0d;
	color: white;
}
.imageview-pill {
	padding: 4px 10px;
	border-radius: 9999px;
}
/* AdwWindowTitle uses -6px vertical margins to fit inside headerbars, which
 * makes the text overflow the pill. Reset them and pad the pill instead. */
.imageview-title {
	padding: 6px 4px;
}
.imageview-title windowtitle {
	margin-top: 0;
	margin-bottom: 0;
}
`

const controlsHideSeconds = 3

// Window is a standalone Libadwaita image viewer window with OSD overlay
// controls, zoom percentage indicator, rotation, clipboard copy (<Ctrl>C), and
// zero-re-fetch save (<Ctrl>S).
type Window struct {
	*adw.Window
	toasts     *adw.ToastOverlay
	view       *View
	titleLabel *adw.WindowTitle
	zoomButton *gtk.Button
	fullscreen *gtk.Button
	openURL    *gtk.Button

	controls      []*gtk.Revealer
	controlsShown bool
	hideTimer     glib.SourceHandle
	hovering      bool
	pointer       [2]float64

	filename    string
	rawBytes    []byte
	externalURL string
}

// NewWindow creates a new image viewer Window.
func NewWindow(app *gtk.Application) *Window {
	loadCSS()

	w := &Window{
		view:       NewView(),
		toasts:     adw.NewToastOverlay(),
		titleLabel: adw.NewWindowTitle("Image Viewer", ""),
		filename:   "image.png",
	}

	w.toasts.SetChild(w.view)

	overlay := gtk.NewOverlay()
	overlay.SetChild(w.toasts)

	// Top-left draggable title pill.
	titleBox := gtk.NewBox(gtk.OrientationHorizontal, 8)
	titleBox.AddCSSClass("osd")
	titleBox.AddCSSClass("imageview-pill")
	titleBox.AddCSSClass("imageview-title")
	titleBox.Append(w.titleLabel)

	handle := gtk.NewWindowHandle()
	handle.SetChild(titleBox)
	w.addControl(overlay, handle, gtk.AlignStart, gtk.AlignStart)

	// Top-right window actions.
	topRight := gtk.NewBox(gtk.OrientationHorizontal, 6)
	w.fullscreen = newOSDButton("Toggle Fullscreen (F11)", "view-fullscreen-symbolic", w.ToggleFullscreen)
	w.openURL = newOSDButton("Open in Browser", "web-browser-symbolic", w.OpenExternalURL)
	w.openURL.SetVisible(false)
	topRight.Append(w.openURL)
	topRight.Append(newOSDButton("Copy Image (Ctrl+C)", "edit-copy-symbolic", w.CopyImage))
	topRight.Append(newOSDButton("Save Image (Ctrl+S)", "document-save-symbolic", w.SaveImage))
	topRight.Append(w.fullscreen)
	topRight.Append(newOSDButton("Close (Esc)", "window-close-symbolic", func() { w.Close() }))
	w.addControl(overlay, topRight, gtk.AlignEnd, gtk.AlignStart)

	// Bottom-end zoom & rotation controls.
	bottom := gtk.NewBox(gtk.OrientationHorizontal, 6)
	bottom.Append(newOSDButton("Rotate Counter-Clockwise (Shift+R)", "object-rotate-left-symbolic", func() {
		w.view.RotateBy(-90)
	}))
	bottom.Append(newOSDButton("Rotate Clockwise (R)", "object-rotate-right-symbolic", func() {
		w.view.RotateBy(90)
	}))
	bottom.Append(newOSDButton("Zoom Out (-)", "zoom-out-symbolic", func() {
		w.view.ZoomBy(1.0 / ZoomStep)
	}))

	w.zoomButton = gtk.NewButtonWithLabel("100%")
	w.zoomButton.SetTooltipText("Toggle Fit / 1:1 Original Size (0 / 1)")
	w.zoomButton.AddCSSClass("osd")
	w.zoomButton.AddCSSClass("imageview-pill")
	w.zoomButton.AddCSSClass("numeric")
	w.zoomButton.ConnectClicked(func() {
		if w.view.IsFitted() {
			w.view.ZoomTo(1.0)
		} else {
			w.view.Fit()
		}
	})
	bottom.Append(w.zoomButton)

	bottom.Append(newOSDButton("Zoom In (+)", "zoom-in-symbolic", func() {
		w.view.ZoomBy(ZoomStep)
	}))
	w.addControl(overlay, bottom, gtk.AlignEnd, gtk.AlignEnd)

	w.view.SetOnZoomChanged(func(z float64, _ bool) {
		pct := int(math.Round(z * 100))
		w.zoomButton.SetLabel(fmt.Sprintf("%d%%", pct))
	})

	motion := gtk.NewEventControllerMotion()
	motion.ConnectMotion(func(x, y float64) {
		if p := [2]float64{x, y}; p != w.pointer {
			w.pointer = p
			w.showControls()
		}
	})
	overlay.AddController(motion)

	w.Window = adw.NewWindow()
	if app != nil {
		w.SetApplication(app)
	}
	w.AddCSSClass("imageview-window")
	w.SetTitle("Image Viewer")
	w.SetDefaultSize(1000, 720)
	w.SetContent(overlay)

	w.NotifyProperty("fullscreened", func() {
		if w.IsFullscreen() {
			w.fullscreen.SetIconName("view-restore-symbolic")
		} else {
			w.fullscreen.SetIconName("view-fullscreen-symbolic")
		}
	})

	w.setupKeys()
	w.showControls()
	return w
}

// View returns the underlying *View widget.
func (w *Window) View() *View {
	return w.view
}

// LoadFile reads, decodes, and displays the image at path, caching the raw
// file bytes so SaveImage never needs to re-read or re-encode the source.
func (w *Window) LoadFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	return w.LoadBytes(filepath.Base(path), data)
}

// LoadBytes decodes data and displays it under the given display filename.
func (w *Window) LoadBytes(filename string, data []byte) error {
	decoded, err := imgload.DecodeBytes(data, imgload.Options{})
	if err != nil {
		return err
	}
	w.filename = filename
	w.rawBytes = data
	w.SetTitle(filename)
	w.titleLabel.SetTitle(filename)
	w.titleLabel.SetSubtitle(fmt.Sprintf("%d × %d (%s)", decoded.Width(), decoded.Height(), decoded.Format))
	w.view.SetImage(decoded)
	return nil
}

// SetExternalURL sets the URL opened by the "Open in Browser" button. It is
// independent of the image source, so it can point at e.g. the original post
// or message. An empty url hides the button.
func (w *Window) SetExternalURL(url string) {
	w.externalURL = url
	w.openURL.SetVisible(url != "")
	if url != "" {
		w.openURL.SetTooltipText("Open in Browser (" + url + ")")
	}
}

// OpenExternalURL opens the URL set by SetExternalURL in the default handler.
func (w *Window) OpenExternalURL() {
	if w.externalURL == "" {
		return
	}
	launcher := gtk.NewURILauncher(w.externalURL)
	launcher.Launch(context.Background(), &w.Window.Window, func(res gio.AsyncResulter) {
		if err := launcher.LaunchFinish(res); err != nil {
			w.toast("Failed to open link: " + err.Error())
		}
	})
}

// CopyImage copies the currently displayed GdkTexture directly to the system
// clipboard (<Ctrl>C), matching Tuba and GNOME Loupe.
func (w *Window) CopyImage() {
	tex := w.view.Paintable().Texture()
	if tex == nil {
		return
	}
	gdk.DisplayGetDefault().Clipboard().SetTexture(tex)
	w.toast("Copied image to clipboard")
}

// SaveImage saves the cached original image bytes to a user-chosen path
// without re-downloading or re-encoding (<Ctrl>S).
func (w *Window) SaveImage() {
	if len(w.rawBytes) == 0 {
		return
	}
	dialog := gtk.NewFileDialog()
	dialog.SetTitle("Save Image")
	dialog.SetInitialName(w.filename)
	dialog.Save(context.Background(), &w.Window.Window, func(res gio.AsyncResulter) {
		file, err := dialog.SaveFinish(res)
		if err != nil {
			return
		}
		dst := file.Path()
		if err := os.WriteFile(dst, w.rawBytes, 0o644); err != nil {
			w.toast("Failed to save image: " + err.Error())
			return
		}
		w.toast("Saved " + filepath.Base(dst))
	})
}

// ToggleFullscreen enters or leaves fullscreen mode.
func (w *Window) ToggleFullscreen() {
	if w.IsFullscreen() {
		w.Unfullscreen()
	} else {
		w.Fullscreen()
	}
}

func (w *Window) setupKeys() {
	keys := gtk.NewEventControllerKey()
	keys.SetPropagationPhase(gtk.PhaseCapture)
	keys.ConnectKeyPressed(func(keyval, _ uint, state gdk.ModifierType) bool {
		w.showControls()
		ctrl := state&gdk.ControlMask != 0
		shift := state&gdk.ShiftMask != 0

		switch keyval {
		case gdk.KEY_Escape:
			if w.IsFullscreen() {
				w.Unfullscreen()
			} else {
				w.Close()
			}
			return true
		case gdk.KEY_F11, gdk.KEY_f, gdk.KEY_F:
			if !ctrl {
				w.ToggleFullscreen()
				return true
			}
		case gdk.KEY_plus, gdk.KEY_equal, gdk.KEY_KP_Add:
			w.view.ZoomBy(ZoomStep)
			return true
		case gdk.KEY_minus, gdk.KEY_underscore, gdk.KEY_KP_Subtract:
			w.view.ZoomBy(1.0 / ZoomStep)
			return true
		case gdk.KEY_0, gdk.KEY_KP_0:
			w.view.Fit()
			return true
		case gdk.KEY_1, gdk.KEY_KP_1:
			w.view.ZoomTo(1.0)
			return true
		case gdk.KEY_2, gdk.KEY_KP_2:
			w.view.ZoomTo(2.0)
			return true
		case gdk.KEY_r, gdk.KEY_R:
			if !ctrl {
				if shift {
					w.view.RotateBy(-90)
				} else {
					w.view.RotateBy(90)
				}
				return true
			}
		case gdk.KEY_c, gdk.KEY_C:
			if ctrl {
				w.CopyImage()
				return true
			}
		case gdk.KEY_s, gdk.KEY_S:
			if ctrl {
				w.SaveImage()
				return true
			}
		case gdk.KEY_w, gdk.KEY_W, gdk.KEY_q, gdk.KEY_Q:
			if ctrl {
				w.Close()
				return true
			}
		}
		return false
	})
	w.AddController(keys)
}

func (w *Window) addControl(overlay *gtk.Overlay, child gtk.Widgetter, halign, valign gtk.Align) *gtk.Revealer {
	rev := gtk.NewRevealer()
	rev.SetTransitionType(gtk.RevealerTransitionTypeCrossfade)
	rev.SetChild(child)
	rev.SetHAlign(halign)
	rev.SetVAlign(valign)
	rev.SetMarginTop(18)
	rev.SetMarginBottom(18)
	rev.SetMarginStart(18)
	rev.SetMarginEnd(18)

	hover := gtk.NewEventControllerMotion()
	hover.ConnectEnter(func(_, _ float64) { w.hovering = true })
	hover.ConnectLeave(func() { w.hovering = false })
	rev.AddController(hover)

	overlay.AddOverlay(rev)
	w.controls = append(w.controls, rev)
	return rev
}

func (w *Window) showControls() {
	w.revealControls(true)
	if w.hideTimer != 0 {
		glib.SourceRemove(w.hideTimer)
	}
	w.hideTimer = glib.TimeoutSecondsAdd(controlsHideSeconds, func() {
		w.hideTimer = 0
		if !w.hovering {
			w.revealControls(false)
		}
	})
}

func (w *Window) revealControls(reveal bool) {
	w.controlsShown = reveal
	for _, r := range w.controls {
		r.SetRevealChild(reveal)
	}
	if reveal {
		w.SetCursor(nil)
	} else {
		w.SetCursorFromName("none")
	}
}

func (w *Window) toast(msg string) {
	w.toasts.AddToast(adw.NewToast(msg))
}

func newOSDButton(tooltip, icon string, onClick func()) *gtk.Button {
	btn := gtk.NewButtonFromIconName(icon)
	btn.SetTooltipText(tooltip)
	btn.AddCSSClass("osd")
	btn.AddCSSClass("circular")
	btn.ConnectClicked(onClick)
	return btn
}

var cssLoaded bool

func loadCSS() {
	if cssLoaded {
		return
	}
	display := gdk.DisplayGetDefault()
	if display == nil {
		return
	}
	cssLoaded = true
	prov := gtk.NewCSSProvider()
	prov.LoadFromString(viewerCSS)
	gtk.StyleContextAddProviderForDisplay(display, prov, gtk.STYLE_PROVIDER_PRIORITY_APPLICATION)
}
