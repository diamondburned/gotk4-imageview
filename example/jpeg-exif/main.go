// Command jpeg-exif opens a 400x640 JPEG tagged with EXIF Orientation=6, which
// imgload rotates upright (640x400) while converting YCbCr to RGB.
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

//go:embed exif-orient6.jpg
var imageData []byte

func main() {
	app := adw.NewApplication("com.github.diamondburned.gotk4-imageview.example.jpeg-exif", gio.ApplicationFlagsNone)
	app.ConnectActivate(func() {
		w := imageview.NewWindow(&app.Application)

		var err error
		if len(os.Args) > 1 {
			err = w.LoadFile(os.Args[1])
		} else {
			err = w.LoadBytes("exif-orient6.jpg", imageData)
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
