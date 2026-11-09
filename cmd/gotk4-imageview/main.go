package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"github.com/diamondburned/gotk4/pkg/gtk/v4"
	"libdb.so/gotk4-imageview/pkg/imageview"
)

var appID = "com.github.diamondburned.gotk4-imageview"

func main() {
	adw.Init()

	app := gtk.NewApplication(appID, gio.ApplicationHandlesOpen|gio.ApplicationNonUnique)
	app.ConnectActivate(func() {
		win := imageview.NewWindow(app)
		if err := win.LoadBytes("demo-checkerboard.png", generateDemoPNG()); err != nil {
			fmt.Fprintf(os.Stderr, "failed to load demo image: %v\n", err)
		}
		win.Present()
	})
	app.ConnectOpen(func(files []gio.Filer, _ string) {
		for _, f := range files {
			win := imageview.NewWindow(app)
			if path := f.Path(); path != "" {
				if err := win.LoadFile(path); err != nil {
					fmt.Fprintf(os.Stderr, "failed to load %s: %v\n", path, err)
				}
			}
			win.Present()
		}
	})

	os.Exit(app.Run(os.Args))
}

func generateDemoPNG() []byte {
	const w, h = 640, 480
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w {
			cell := ((x / 32) + (y / 32)) & 1
			r := uint8((x * 255) / w)
			g := uint8((y * 255) / h)
			b := uint8(180)
			if cell == 0 {
				r /= 2
				g /= 2
				b /= 2
			}
			img.SetNRGBA(x, y, color.NRGBA{R: r, G: g, B: b, A: 255})
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}
