package imageutil

import (
	"encoding/base64"
	"image"
	"image/color"
	"image/jpeg"
	"strings"
	"testing"
)

// solidImage builds a synthetic w x h image filled with a repeating
// pattern, avoiding any dependency on test fixture files.
func solidImage(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			c := color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 128, A: 255}
			img.Set(x, y, c)
		}
	}
	return img
}

func TestDownscaleShrinksWideImages(t *testing.T) {
	src := solidImage(2000, 1000)
	got := downscale(src, 1024)

	b := got.Bounds()
	if b.Dx() != 1024 {
		t.Errorf("width = %d, want 1024", b.Dx())
	}
	wantHeight := 1000 * 1024 / 2000
	if b.Dy() != wantHeight {
		t.Errorf("height = %d, want %d", b.Dy(), wantHeight)
	}
}

func TestDownscaleLeavesSmallImagesAlone(t *testing.T) {
	src := solidImage(500, 300)
	got := downscale(src, 1024)

	if got != src {
		t.Error("downscale should return the original image unchanged when already narrower than the target width")
	}
}

func TestToDataURLProducesValidJPEGDataURLUnderSizeCap(t *testing.T) {
	src := solidImage(2000, 1200)

	url, err := ToDataURL(src)
	if err != nil {
		t.Fatalf("ToDataURL: %v", err)
	}

	const prefix = "data:image/jpeg;base64,"
	if !strings.HasPrefix(url, prefix) {
		t.Fatalf("data URL missing expected prefix, got %q", url[:min(40, len(url))])
	}

	encoded := strings.TrimPrefix(url, prefix)
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		t.Fatalf("base64 payload does not decode: %v", err)
	}
	if len(raw) > maxBytes {
		t.Errorf("encoded size = %d bytes, want <= %d", len(raw), maxBytes)
	}

	decoded, err := jpeg.Decode(strings.NewReader(string(raw)))
	if err != nil {
		t.Fatalf("payload is not a valid JPEG: %v", err)
	}
	if decoded.Bounds().Dx() > maxWidth {
		t.Errorf("decoded width = %d, want <= %d", decoded.Bounds().Dx(), maxWidth)
	}
}

func TestToDataURLSmallImagePassesThrough(t *testing.T) {
	src := solidImage(100, 80)

	url, err := ToDataURL(src)
	if err != nil {
		t.Fatalf("ToDataURL: %v", err)
	}
	if !strings.HasPrefix(url, "data:image/jpeg;base64,") {
		t.Errorf("unexpected data URL prefix: %q", url[:min(40, len(url))])
	}
}

func TestHasLocalDisplay(t *testing.T) {
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	if HasLocalDisplay() {
		t.Error("HasLocalDisplay() = true with no DISPLAY/WAYLAND_DISPLAY set")
	}

	t.Setenv("DISPLAY", ":0")
	if !HasLocalDisplay() {
		t.Error("HasLocalDisplay() = false with DISPLAY set")
	}

	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	if !HasLocalDisplay() {
		t.Error("HasLocalDisplay() = false with WAYLAND_DISPLAY set")
	}
}
