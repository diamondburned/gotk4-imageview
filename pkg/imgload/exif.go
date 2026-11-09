// Package imgload decodes static and animated images using pure Go decoders
// and converts them into GTK4 gdk.MemoryTexture objects with minimal
// allocations and no gdk-pixbuf dependency.
package imgload

import "encoding/binary"

// Orientation represents an EXIF orientation tag value (1..8).
type Orientation uint8

const (
	OrientationNormal      Orientation = 1 // 0 deg
	OrientationFlipH       Orientation = 2 // mirror horizontal
	OrientationRotate180   Orientation = 3 // 180 deg
	OrientationFlipV       Orientation = 4 // mirror vertical
	OrientationTranspose   Orientation = 5 // mirror horizontal + rotate 270 CW
	OrientationRotate90CW  Orientation = 6 // rotate 90 CW
	OrientationTransverse  Orientation = 7 // mirror horizontal + rotate 90 CW
	OrientationRotate270CW Orientation = 8 // rotate 270 CW
)

// Valid reports whether o is in the standard EXIF range [1, 8].
func (o Orientation) Valid() bool {
	return o >= OrientationNormal && o <= OrientationRotate270CW
}

// SwapsDimensions reports whether applying o swaps width and height.
func (o Orientation) SwapsDimensions() bool {
	return o >= OrientationTranspose && o <= OrientationRotate270CW
}

// Dimensions returns (w, h) after applying orientation o.
func (o Orientation) Dimensions(w, h int) (int, int) {
	if o.SwapsDimensions() {
		return h, w
	}
	return w, h
}

// ParseEXIFOrientation scans a JPEG byte stream for an APP1 EXIF Orientation
// tag without allocating on the heap. If no valid tag is found, it returns
// OrientationNormal.
func ParseEXIFOrientation(data []byte) Orientation {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return OrientationNormal
	}

	i := 2
	for i+4 <= len(data) {
		if data[i] != 0xFF {
			return OrientationNormal
		}
		// Skip any padding 0xFF bytes.
		for i < len(data) && data[i] == 0xFF {
			i++
		}
		if i >= len(data) {
			return OrientationNormal
		}

		marker := data[i]
		i++

		// Standalone markers without length payload.
		if marker == 0xD8 || (marker >= 0xD0 && marker <= 0xD7) || marker == 0x01 {
			continue
		}
		// Stop at Start Of Scan (SOS) or End Of Image (EOI).
		if marker == 0xDA || marker == 0xD9 {
			return OrientationNormal
		}

		if i+2 > len(data) {
			return OrientationNormal
		}
		segLen := int(binary.BigEndian.Uint16(data[i : i+2]))
		if segLen < 2 || i+segLen > len(data) {
			return OrientationNormal
		}

		// APP1 marker (0xE1) with "Exif\x00\x00" header.
		if marker == 0xE1 && segLen >= 14 {
			payload := data[i+2 : i+segLen]
			if len(payload) >= 6 &&
				payload[0] == 'E' && payload[1] == 'x' &&
				payload[2] == 'i' && payload[3] == 'f' &&
				payload[4] == 0 && payload[5] == 0 {
				if o := parseTIFFOrientation(payload[6:]); o.Valid() {
					return o
				}
			}
		}

		i += segLen
	}

	return OrientationNormal
}

func parseTIFFOrientation(tiff []byte) Orientation {
	if len(tiff) < 8 {
		return OrientationNormal
	}

	var bo binary.ByteOrder
	switch {
	case tiff[0] == 'I' && tiff[1] == 'I':
		bo = binary.LittleEndian
	case tiff[0] == 'M' && tiff[1] == 'M':
		bo = binary.BigEndian
	default:
		return OrientationNormal
	}

	if bo.Uint16(tiff[2:4]) != 0x002A {
		return OrientationNormal
	}

	ifdOffset := int(bo.Uint32(tiff[4:8]))
	if ifdOffset < 8 || ifdOffset+2 > len(tiff) {
		return OrientationNormal
	}

	numEntries := int(bo.Uint16(tiff[ifdOffset : ifdOffset+2]))
	pos := ifdOffset + 2

	for range numEntries {
		if pos+12 > len(tiff) {
			return OrientationNormal
		}
		tag := bo.Uint16(tiff[pos : pos+2])
		if tag == 0x0112 { // Orientation tag
			fieldType := bo.Uint16(tiff[pos+2 : pos+4])
			count := bo.Uint32(tiff[pos+4 : pos+8])
			if fieldType == 3 && count == 1 { // SHORT
				val := bo.Uint16(tiff[pos+8 : pos+10])
				o := Orientation(val)
				if o.Valid() {
					return o
				}
			}
			return OrientationNormal
		}
		pos += 12
	}

	return OrientationNormal
}
