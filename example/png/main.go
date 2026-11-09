// Command png opens an RGBA PNG with alpha transparency. Zoom past 200% to see
// nearest-neighbor scaling kick in.
//
// Pass a file path as an argument to open a different image instead.
package main

import (
	_ "embed"
	"fmt"
	"os"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"libdb.so/gotk4-imageview/pkg/imageview"
)

//go:embed dog.png
var imageData []byte

func main() {
	app := adw.NewApplication("com.github.diamondburned.gotk4-imageview.example.png", gio.ApplicationFlagsNone)
	app.ConnectActivate(func() {
		w := imageview.NewWindow(&app.Application)

		var err error
		if len(os.Args) > 1 {
			err = w.LoadFile(os.Args[1])
		} else {
			err = w.LoadBytes("dog.png", imageData)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "failed to load image:", err)
			os.Exit(1)
		}

		w.Present()
	})

	if code := app.Run(os.Args[:1]); code != 0 {
		os.Exit(code)
	}
}
