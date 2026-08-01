//go:build windows

package tray

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/png"
)

func iconBytes() []byte {
	ico, err := pngToICO(iconPNG, 16, 32)
	if err != nil {
		return iconPNG
	}
	return ico
}

func pngToICO(pngBytes []byte, sizes ...int) ([]byte, error) {
	src, err := png.Decode(bytes.NewReader(pngBytes))
	if err != nil {
		return nil, err
	}
	if len(sizes) == 0 {
		sizes = []int{32}
	}
	type entry struct {
		w, h int
		png  []byte
	}
	entries := make([]entry, 0, len(sizes))
	for _, size := range sizes {
		dst := image.NewRGBA(image.Rect(0, 0, size, size))
		scaleNearest(dst, src)
		var buf bytes.Buffer
		if err := png.Encode(&buf, dst); err != nil {
			return nil, err
		}
		entries = append(entries, entry{w: size, h: size, png: buf.Bytes()})
	}

	var out bytes.Buffer
	_ = binary.Write(&out, binary.LittleEndian, uint16(0))
	_ = binary.Write(&out, binary.LittleEndian, uint16(1))
	_ = binary.Write(&out, binary.LittleEndian, uint16(len(entries)))
	offset := 6 + 16*len(entries)
	for _, e := range entries {
		w, h := e.w, e.h
		if w >= 256 {
			w = 0
		}
		if h >= 256 {
			h = 0
		}
		out.WriteByte(byte(w))
		out.WriteByte(byte(h))
		out.WriteByte(0)
		out.WriteByte(0)
		_ = binary.Write(&out, binary.LittleEndian, uint16(1))
		_ = binary.Write(&out, binary.LittleEndian, uint16(32))
		_ = binary.Write(&out, binary.LittleEndian, uint32(len(e.png)))
		_ = binary.Write(&out, binary.LittleEndian, uint32(offset))
		offset += len(e.png)
	}
	for _, e := range entries {
		out.Write(e.png)
	}
	return out.Bytes(), nil
}

func scaleNearest(dst *image.RGBA, src image.Image) {
	db := dst.Bounds()
	sb := src.Bounds()
	sw := sb.Dx()
	sh := sb.Dy()
	dw := db.Dx()
	dh := db.Dy()
	for y := range dh {
		sy := sb.Min.Y + y*sh/dh
		for x := range dw {
			sx := sb.Min.X + x*sw/dw
			dst.Set(db.Min.X+x, db.Min.Y+y, src.At(sx, sy))
		}
	}
}
