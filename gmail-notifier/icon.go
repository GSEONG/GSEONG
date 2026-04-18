package main

import (
	"encoding/binary"
)

// generateTrayIcon builds a 16x16 32-bit ICO binary containing an envelope icon.
func generateTrayIcon() []byte {
	const size = 16

	// BGRA pixel values
	type px struct{ b, g, r, a uint8 }
	blue  := px{244, 133, 66, 255}  // Google Blue (#4285F4)
	white := px{255, 255, 255, 255}

	// XOR mask: 16×16 BGRA pixels, bottom-up row order (BMP convention)
	var xorMask [size * size * 4]byte
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			bmpY := size - 1 - y // BMP rows are bottom-up
			idx := (bmpY*size + x) * 4

			p := blue

			// White filled envelope body
			if x >= 2 && x <= 13 && y >= 3 && y <= 12 {
				p = white
			}

			// Blue diagonal lines forming the envelope flap (V shape)
			for i := 0; i <= 5; i++ {
				if (x == 2+i || x == 13-i) && y == 3+i {
					p = blue
				}
			}

			xorMask[idx+0] = p.b
			xorMask[idx+1] = p.g
			xorMask[idx+2] = p.r
			xorMask[idx+3] = p.a
		}
	}

	// AND mask: all zeros (alpha channel in the XOR mask handles transparency).
	// Each row is padded to a DWORD boundary: 16px → 2 bytes data + 2 bytes pad = 4 bytes/row.
	andMask := make([]byte, size*4)

	// BITMAPINFOHEADER (40 bytes)
	var bmpHdr [40]byte
	binary.LittleEndian.PutUint32(bmpHdr[0:], 40)           // biSize
	binary.LittleEndian.PutUint32(bmpHdr[4:], uint32(size)) // biWidth
	binary.LittleEndian.PutUint32(bmpHdr[8:], uint32(size*2)) // biHeight: doubled (XOR + AND)
	binary.LittleEndian.PutUint16(bmpHdr[12:], 1)           // biPlanes
	binary.LittleEndian.PutUint16(bmpHdr[14:], 32)          // biBitCount

	bmpDataLen := uint32(len(bmpHdr) + len(xorMask) + len(andMask))

	// ICO file header (6 bytes)
	icoHdr := [6]byte{
		0, 0, // idReserved = 0
		1, 0, // idType     = 1 (icon)
		1, 0, // idCount    = 1
	}

	// ICONDIRENTRY (16 bytes)
	var dirEntry [16]byte
	dirEntry[0] = size // bWidth
	dirEntry[1] = size // bHeight
	dirEntry[2] = 0    // bColorCount (0 = more than 8bpp)
	dirEntry[3] = 0    // bReserved
	binary.LittleEndian.PutUint16(dirEntry[4:], 1)            // wPlanes
	binary.LittleEndian.PutUint16(dirEntry[6:], 32)           // wBitCount
	binary.LittleEndian.PutUint32(dirEntry[8:], bmpDataLen)   // dwBytesInRes
	binary.LittleEndian.PutUint32(dirEntry[12:], 22)          // dwImageOffset = 6 + 16

	out := make([]byte, 0, 22+int(bmpDataLen))
	out = append(out, icoHdr[:]...)
	out = append(out, dirEntry[:]...)
	out = append(out, bmpHdr[:]...)
	out = append(out, xorMask[:]...)
	out = append(out, andMask...)
	return out
}
