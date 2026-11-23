# gotk4-imageview

[![Go Reference](https://pkg.go.dev/badge/libdb.so/gotk4-imageview.svg)](https://pkg.go.dev/libdb.so/gotk4-imageview)
[![AI Slop Inside](https://sladge.net/badge.svg)](https://sladge.net)

A pure-Go image decoder and a GTK4/Libadwaita image viewer widget for
[gotk4](https://github.com/diamondburned/gotk4).

Images are decoded with Go's `image/*` packages (plus `golang.org/x/image`) and
uploaded straight into `gdk.MemoryTexture`s, so there is no dependency on
gdk-pixbuf or its loaders.

## Packages

- [`pkg/imgload`](https://pkg.go.dev/libdb.so/gotk4-imageview/pkg/imgload)
  decodes PNG, JPEG, GIF, WebP (including animated WebP), BMP and TIFF into
  GDK textures.
  - Go image types map directly to `gdk.MemoryFormat`s. `RGBA`, `NRGBA` and
    `Gray` are zero-copy, `YCbCr` is converted to RGB in a single pass, and
    `Paletted` goes through a lookup table.
  - JPEG EXIF orientation is applied during conversion.
  - Animations reuse one canvas, so `Step()` doesn't allocate, and only the
    dirty region is re-uploaded via `gdk.MemoryTextureBuilder`.
  - Decoding hot paths run at 0 allocs/op (see `bench_test.go`).
- [`pkg/imageview`](https://pkg.go.dev/libdb.so/gotk4-imageview/pkg/imageview)
  provides the viewer:
  - `Paintable` draws the image and handles rotation and HiDPI-aware scaling
    filters. It switches to nearest-neighbor when zoomed past 1:1.
  - `View` is a zoomable, pannable widget with scroll and pinch zoom,
    double-click to zoom, and kinetic panning.
  - `Window` is a standalone viewer window with auto-hiding OSD controls,
    rotation, copy to clipboard, saving the original bytes, fullscreen, and an
    optional "Open in Browser" button (`SetExternalURL`).

## Usage

```go
import "libdb.so/gotk4-imageview/pkg/imageview"

win := imageview.NewWindow(app)
if err := win.LoadFile("cat.png"); err != nil {
	log.Fatal(err)
}
win.SetExternalURL("https://example.com/original-post")
win.Present()
```

To decode without the viewer:

```go
img, err := imgload.DecodeBytes(data, imgload.Options{})
if err != nil {
	return err
}
picture.SetPaintable(img.Texture())
```

## Keybindings

| Key                    | Action                         |
| ---------------------- | ------------------------------ |
| `+` / `-`              | Zoom in / out                  |
| `0`                    | Fit to window                  |
| `1` / `2`              | Zoom to 100% / 200%            |
| `R` / `Shift+R`        | Rotate clockwise / counter-clockwise |
| `Ctrl+C`               | Copy image                     |
| `Ctrl+S`               | Save image                     |
| `F11` / `F`            | Toggle fullscreen              |
| `Esc`                  | Leave fullscreen or close      |
| `Ctrl+W` / `Ctrl+Q`    | Close                          |

## Running

```sh
nix develop
go run ./cmd/gotk4-imageview [image...]
```

The [`example/`](example) directory has one runnable program per format, each
with a bundled image:

```sh
go run ./example/png
go run ./example/jpeg-exif
go run ./example/gif-anim
go run ./example/webp-anim
go run ./example/bmp-tiff -format=bmp
```

## Testing

```sh
go test ./...
go test -bench=. -benchmem ./pkg/imgload
nix build .#checks.x86_64-linux.wayland-hidpi  # HiDPI Wayland NixOS VM test
```
