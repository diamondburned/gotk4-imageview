// Command gif-anim plays an animated GIF using zero-allocation frame stepping and
// partial gdk.MemoryTextureBuilder updates.
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

//go:embed glup.gif
var imageData []byte

func main() {
	app := adw.NewApplication("com.github.diamondburned.gotk4-imageview.example.gif-anim", gio.ApplicationFlagsNone)
	app.ConnectActivate(func() {
		w := imageview.NewWindow(&app.Application)

		var err error
		if len(os.Args) > 1 {
			err = w.LoadFile(os.Args[1])
		} else {
			err = w.LoadBytes("glup.gif", imageData)
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
