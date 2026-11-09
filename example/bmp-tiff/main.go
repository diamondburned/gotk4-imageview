// Command bmp-tiff opens a 24-bit BMP or an 8-bit grayscale TIFF (which imgload
// maps zero-copy to gdk.MemoryG8).
//
// Pass -format=bmp or -format=tiff to pick the bundled image, or pass a file
// path as an argument to open a different image instead.
package main

import (
	_ "embed"
	"flag"
	"fmt"
	"os"

	"github.com/diamondburned/gotk4-adwaita/pkg/adw"
	"github.com/diamondburned/gotk4/pkg/gio/v2"
	"libdb.so/gotk4-imageview/pkg/imageview"
)

var (
	//go:embed color-bars.bmp
	bmpData []byte
	//go:embed concentric-gray8.tiff
	tiffData []byte
)

func main() {
	format := flag.String("format", "tiff", "bundled image to display: bmp or tiff")
	flag.Parse()

	var name string
	var data []byte
	switch *format {
	case "bmp":
		name, data = "color-bars.bmp", bmpData
	case "tiff":
		name, data = "concentric-gray8.tiff", tiffData
	default:
		fmt.Fprintf(os.Stderr, "unknown -format=%q (want bmp or tiff)\n", *format)
		os.Exit(1)
	}

	app := adw.NewApplication("com.github.diamondburned.gotk4-imageview.example.bmp-tiff", gio.ApplicationFlagsNone)
	app.ConnectActivate(func() {
		w := imageview.NewWindow(&app.Application)

		var err error
		if flag.NArg() > 0 {
			err = w.LoadFile(flag.Arg(0))
		} else {
			err = w.LoadBytes(name, data)
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
