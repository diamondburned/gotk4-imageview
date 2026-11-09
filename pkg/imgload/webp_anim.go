package imgload

import (
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"image/gif"
	"time"

	"golang.org/x/image/webp"
)

// IsAnimatedWebP reports whether data begins with a RIFF WEBP container that
// has the VP8X animation flag set and contains an ANMF frame chunk.
// IsAnimatedWebP performs zero heap allocations.
func IsAnimatedWebP(data []byte) bool {
	if len(data) < 30 {
		return false
	}
	if string(data[0:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
		return false
	}
	if string(data[12:16]) != "VP8X" {
		return false
	}
	flags := data[20]
	const animFlag = 0x02
	return flags&animFlag != 0
}

// DecodeAnimatedWebP parses an animated WebP container into an *Animation by
// extracting each ANMF frame's elementary VP8/VP8L/ALPH chunks into a minimal
// standalone RIFF WEBP buffer and decoding it via golang.org/x/image/webp.
func DecodeAnimatedWebP(data []byte) (*Animation, error) {
	if !IsAnimatedWebP(data) {
		return nil, errors.New("imgload: not an animated WebP")
	}

	// VP8X payload at offset 20..30:
	// [0] flags, [1..3] reserved, [4..6] canvasWidth-1 (24-bit LE), [7..9] canvasHeight-1 (24-bit LE)
	canvasW := readUint24LE(data[24:27]) + 1
	canvasH := readUint24LE(data[27:30]) + 1
	if canvasW <= 0 || canvasH <= 0 {
		return nil, errors.New("imgload: invalid WebP canvas dimensions")
	}

	var (
		rgbaFrames []*image.RGBA
		delays     []time.Duration
		disposals  []byte
		blends     []bool
		bgColor    uint32
		hasBg      bool
		scratch    []byte
		reader     bytes.Reader
	)

	pos := 12
	for pos+8 <= len(data) {
		fourCC := string(data[pos : pos+4])
		chunkLen := int(binary.LittleEndian.Uint32(data[pos+4 : pos+8]))
		payloadStart := pos + 8
		if chunkLen < 0 || payloadStart+chunkLen > len(data) {
			break
		}
		payload := data[payloadStart : payloadStart+chunkLen]

		switch fourCC {
		case "ANIM":
			if len(payload) >= 6 {
				// ANIM background color is stored in [Blue, Green, Red, Alpha] byte order.
				b, g, r, a := payload[0], payload[1], payload[2], payload[3]
				var px [4]byte
				px[0] = byte((uint32(r)*uint32(a) + 127) / 255)
				px[1] = byte((uint32(g)*uint32(a) + 127) / 255)
				px[2] = byte((uint32(b)*uint32(a) + 127) / 255)
				px[3] = a
				bgColor = binary.NativeEndian.Uint32(px[:])
				hasBg = true
			}

		case "ANMF":
			if len(payload) < 16 {
				break
			}
			frameX := readUint24LE(payload[0:3]) * 2
			frameY := readUint24LE(payload[3:6]) * 2
			frameW := readUint24LE(payload[6:9]) + 1
			frameH := readUint24LE(payload[9:12]) + 1
			durationMS := readUint24LE(payload[12:15])
			flags := payload[15]

			disposeBg := (flags & 0x01) != 0
			noBlend := (flags & 0x02) != 0

			subChunks := payload[16:]
			scratch = buildSingleFrameWebP(scratch[:0], frameW, frameH, subChunks)
			reader.Reset(scratch)

			decoded, err := webp.Decode(&reader)
			if err != nil {
				return nil, err
			}

			pb := ConvertImage(decoded, OrientationNormal, nil)
			rgba := toPremultipliedRGBA(pb, frameX, frameY)

			delay := time.Duration(durationMS) * time.Millisecond
			if delay <= 10*time.Millisecond {
				delay = 100 * time.Millisecond
			}

			disp := byte(gif.DisposalNone)
			if disposeBg {
				disp = gif.DisposalBackground
			}

			rgbaFrames = append(rgbaFrames, rgba)
			delays = append(delays, delay)
			disposals = append(disposals, disp)
			blends = append(blends, !noBlend)
		}

		// RIFF chunks are padded to even byte boundaries.
		paddedLen := chunkLen + (chunkLen & 1)
		pos = payloadStart + paddedLen
	}

	if len(rgbaFrames) == 0 {
		return nil, errors.New("imgload: animated WebP contained no ANMF frames")
	}

	a := &Animation{
		width:     canvasW,
		height:    canvasH,
		rgba:      rgbaFrames,
		delays:    delays,
		disposals: disposals,
		blend:     blends,
		canvas:    make([]byte, canvasW*canvasH*4),
		bgColor:   bgColor,
		hasBg:     hasBg,
		index:     -1,
	}
	a.Step()
	return a, nil
}

func readUint24LE(b []byte) int {
	return int(b[0]) | int(b[1])<<8 | int(b[2])<<16
}

func writeUint24LE(b []byte, v int) {
	b[0] = byte(v)
	b[1] = byte(v >> 8)
	b[2] = byte(v >> 16)
}

func buildSingleFrameWebP(dst []byte, w, h int, subChunks []byte) []byte {
	hasAlpha := false
	pos := 0
	for pos+8 <= len(subChunks) {
		fourCC := string(subChunks[pos : pos+4])
		cLen := int(binary.LittleEndian.Uint32(subChunks[pos+4 : pos+8]))
		if fourCC == "ALPH" {
			hasAlpha = true
			break
		}
		pos += 8 + cLen + (cLen & 1)
	}

	const vp8xChunkSize = 18 // "VP8X" (4) + size=10 (4) + payload (10)
	riffPayloadSize := 4 + vp8xChunkSize + len(subChunks)
	totalSize := 8 + riffPayloadSize

	if cap(dst) < totalSize {
		dst = make([]byte, totalSize)
	} else {
		dst = dst[:totalSize]
	}

	copy(dst[0:4], "RIFF")
	binary.LittleEndian.PutUint32(dst[4:8], uint32(riffPayloadSize))
	copy(dst[8:12], "WEBP")

	copy(dst[12:16], "VP8X")
	binary.LittleEndian.PutUint32(dst[16:20], 10)
	clear(dst[20:30])
	if hasAlpha {
		dst[20] = 0x10 // Alpha flag
	}
	writeUint24LE(dst[24:27], w-1)
	writeUint24LE(dst[27:30], h-1)

	copy(dst[30:], subChunks)
	return dst
}

func toPremultipliedRGBA(pb PixelBuffer, offsetX, offsetY int) *image.RGBA {
	w, h := pb.Width, pb.Height
	rect := image.Rect(offsetX, offsetY, offsetX+w, offsetY+h)

	switch pb.Format {
	case 0: // fallback
		return image.NewRGBA(rect)
	default:
		out := make([]byte, w*h*4)
		switch pb.Stride {
		case w * 3: // R8G8B8 from YCbCr
			for y := range h {
				sRow := pb.Pix[y*pb.Stride : (y+1)*pb.Stride]
				dRow := out[y*w*4 : (y+1)*w*4]
				si, di := 0, 0
				for range w {
					dRow[di+0] = sRow[si+0]
					dRow[di+1] = sRow[si+1]
					dRow[di+2] = sRow[si+2]
					dRow[di+3] = 255
					si += 3
					di += 4
				}
			}
		case w * 4: // NRGBA or RGBA
			for y := range h {
				sRow := pb.Pix[y*pb.Stride : (y+1)*pb.Stride]
				dRow := out[y*w*4 : (y+1)*w*4]
				for i := 0; i < len(sRow); i += 4 {
					a := uint32(sRow[i+3])
					if a == 255 {
						dRow[i+0] = sRow[i+0]
						dRow[i+1] = sRow[i+1]
						dRow[i+2] = sRow[i+2]
						dRow[i+3] = 255
					} else if a > 0 {
						dRow[i+0] = byte((uint32(sRow[i+0])*a + 127) / 255)
						dRow[i+1] = byte((uint32(sRow[i+1])*a + 127) / 255)
						dRow[i+2] = byte((uint32(sRow[i+2])*a + 127) / 255)
						dRow[i+3] = byte(a)
					}
				}
			}
		}
		return &image.RGBA{
			Pix:    out,
			Stride: w * 4,
			Rect:   rect,
		}
	}
}
