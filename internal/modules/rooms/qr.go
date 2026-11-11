package rooms

import (
	"fmt"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
)

// QR draws the room's invite link as an SVG the design can print beside the code
// (P14). It is inline SVG rather than an image so it takes the page's own ink colour
// in both themes and costs no extra request.
//
// An empty string means "no code to show": the template then leaves the square out
// instead of drawing an empty box.
func QR(link string) string {
	if link == "" {
		return ""
	}
	code, err := qrcode.New(link, qrcode.Medium)
	if err != nil {
		return ""
	}
	bitmap := code.Bitmap()
	n := len(bitmap)
	if n == 0 {
		return ""
	}
	// go-qrcode's bitmap already carries the quiet zone, so the whole square is the
	// viewBox and no padding has to be invented here.
	var path strings.Builder
	for y, row := range bitmap {
		for x, on := range row {
			if !on {
				continue
			}
			// One run of dark modules becomes one rectangle, which keeps the path
			// short enough to sit in the page without a second request.
			width := 1
			for x+width < len(row) && row[x+width] {
				width++
			}
			fmt.Fprintf(&path, "M%d %dh%dv1h-%dz", x, y, width, width)
			row[x] = false
			for i := 1; i < width; i++ {
				row[x+i] = false
			}
		}
	}
	return fmt.Sprintf(`<svg class="qr" viewBox="0 0 %d %d" role="img" aria-hidden="true" shape-rendering="crispEdges"><path d="%s" fill="currentColor"/></svg>`, n, n, path.String())
}
