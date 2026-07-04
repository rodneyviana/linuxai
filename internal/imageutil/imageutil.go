// Package imageutil loads a manually attached image (from a file path or
// the local clipboard), downscales it, and encodes it as a data URL small
// enough for the hosted endpoint's inline image limit.
package imageutil

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	_ "image/png"
	"os"
	"os/exec"
)

const (
	maxWidth  = 1024
	maxBytes  = 170 * 1024 // stay under the ~180KB hosted-endpoint cap
	minWidth  = 256
	startQual = 85
	minQual   = 20
)

// LoadFile decodes an image from a file path.
func LoadFile(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("opening image: %w", err)
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return nil, fmt.Errorf("decoding image: %w", err)
	}
	return img, nil
}

// HasLocalDisplay reports whether a local X11 or Wayland display is
// available, i.e. whether reading the clipboard makes sense at all. Over
// plain SSH there is no local clipboard.
func HasLocalDisplay() bool {
	return os.Getenv("WAYLAND_DISPLAY") != "" || os.Getenv("DISPLAY") != ""
}

// LoadClipboard reads an image from the local clipboard via wl-paste
// (Wayland) or xclip (X11). Callers should check HasLocalDisplay first.
func LoadClipboard() (image.Image, error) {
	var cmd *exec.Cmd
	switch {
	case os.Getenv("WAYLAND_DISPLAY") != "":
		cmd = exec.Command("wl-paste", "--type", "image/png")
	case os.Getenv("DISPLAY") != "":
		cmd = exec.Command("xclip", "-selection", "clipboard", "-t", "image/png", "-o")
	default:
		return nil, fmt.Errorf("no local display; use --image PATH instead")
	}

	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("reading clipboard (is an image copied?): %w", err)
	}

	img, _, err := image.Decode(bytes.NewReader(out))
	if err != nil {
		return nil, fmt.Errorf("decoding clipboard image: %w", err)
	}
	return img, nil
}

// downscale resizes img so its width is at most width, preserving aspect
// ratio, using nearest-neighbor sampling (stdlib only, no golang.org/x/image).
func downscale(img image.Image, width int) image.Image {
	b := img.Bounds()
	srcW, srcH := b.Dx(), b.Dy()
	if srcW <= width {
		return img
	}

	dstW := width
	dstH := srcH * dstW / srcW
	if dstH < 1 {
		dstH = 1
	}

	dst := image.NewRGBA(image.Rect(0, 0, dstW, dstH))
	for y := 0; y < dstH; y++ {
		srcY := b.Min.Y + y*srcH/dstH
		for x := 0; x < dstW; x++ {
			srcX := b.Min.X + x*srcW/dstW
			dst.Set(x, y, img.At(srcX, srcY))
		}
	}
	return dst
}

// encodeJPEG encodes img as JPEG at the given quality.
func encodeJPEG(img image.Image, quality int) ([]byte, error) {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: quality}); err != nil {
		return nil, fmt.Errorf("encoding jpeg: %w", err)
	}
	return buf.Bytes(), nil
}

// ToDataURL downscales img to a width suitable for the hosted endpoint and
// encodes it as a data: URL, shrinking quality and then dimensions until it
// fits under the inline size cap.
func ToDataURL(img image.Image) (string, error) {
	width := maxWidth
	if b := img.Bounds().Dx(); b < width {
		width = b
	}

	for {
		scaled := downscale(img, width)

		for quality := startQual; quality >= minQual; quality -= 15 {
			data, err := encodeJPEG(scaled, quality)
			if err != nil {
				return "", err
			}
			if len(data) <= maxBytes {
				return "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(data), nil
			}
		}
		if width <= minWidth {
			break
		}
		width = width * 3 / 4
		if width < minWidth {
			width = minWidth
		}
	}

	return "", fmt.Errorf("could not shrink image under %d KB", maxBytes/1024)
}
