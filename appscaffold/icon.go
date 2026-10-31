package appscaffold

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"strings"
)

// The starting icon is the app's first letter drawn as rounded strokes on its colour. It is a
// placeholder that looks intentional on a home screen until the app gets its real logo.

type point struct{ x, y float64 }

// glyphs holds the letters as polylines on a 4×6 grid, y pointing down.
var glyphs = map[byte][][]point{
	'a': {{{0, 6}, {2, 0}, {4, 6}}, {{0.67, 4}, {3.33, 4}}},
	'b': {{{0, 3}, {3, 3}, {4, 4}, {4, 5}, {3, 6}, {0, 6}, {0, 0}, {3, 0}, {4, 1}, {4, 2}, {3, 3}}},
	'c': {{{4, 1}, {3, 0}, {1, 0}, {0, 1}, {0, 5}, {1, 6}, {3, 6}, {4, 5}}},
	'd': {{{0, 0}, {0, 6}, {2.5, 6}, {4, 4.5}, {4, 1.5}, {2.5, 0}, {0, 0}}},
	'e': {{{4, 0}, {0, 0}, {0, 6}, {4, 6}}, {{0, 3}, {3, 3}}},
	'f': {{{4, 0}, {0, 0}, {0, 6}}, {{0, 3}, {3, 3}}},
	'g': {{{4, 1}, {3, 0}, {1, 0}, {0, 1}, {0, 5}, {1, 6}, {3, 6}, {4, 5}, {4, 3.5}, {2.5, 3.5}}},
	'h': {{{0, 0}, {0, 6}}, {{4, 0}, {4, 6}}, {{0, 3}, {4, 3}}},
	'i': {{{2, 0}, {2, 6}}, {{1, 0}, {3, 0}}, {{1, 6}, {3, 6}}},
	'j': {{{4, 0}, {4, 5}, {3, 6}, {1, 6}, {0, 5}}},
	'k': {{{0, 0}, {0, 6}}, {{4, 0}, {0, 3.5}}, {{1.4, 2.3}, {4, 6}}},
	'l': {{{0, 0}, {0, 6}, {4, 6}}},
	'm': {{{0, 6}, {0, 0}, {2, 3}, {4, 0}, {4, 6}}},
	'n': {{{0, 6}, {0, 0}, {4, 6}, {4, 0}}},
	'o': {{{1, 0}, {3, 0}, {4, 1}, {4, 5}, {3, 6}, {1, 6}, {0, 5}, {0, 1}, {1, 0}}},
	'p': {{{0, 6}, {0, 0}, {3, 0}, {4, 1}, {4, 2}, {3, 3}, {0, 3}}},
	'q': {{{1, 0}, {3, 0}, {4, 1}, {4, 5}, {3, 6}, {1, 6}, {0, 5}, {0, 1}, {1, 0}}, {{2.5, 4.5}, {4, 6}}},
	'r': {{{0, 6}, {0, 0}, {3, 0}, {4, 1}, {4, 2}, {3, 3}, {0, 3}}, {{2, 3}, {4, 6}}},
	's': {{{4, 1}, {3, 0}, {1, 0}, {0, 1}, {0, 2}, {1, 3}, {3, 3}, {4, 4}, {4, 5}, {3, 6}, {1, 6}, {0, 5}}},
	't': {{{0, 0}, {4, 0}}, {{2, 0}, {2, 6}}},
	'u': {{{0, 0}, {0, 5}, {1, 6}, {3, 6}, {4, 5}, {4, 0}}},
	'v': {{{0, 0}, {2, 6}, {4, 0}}},
	'w': {{{0, 0}, {1, 6}, {2, 3}, {3, 6}, {4, 0}}},
	'x': {{{0, 0}, {4, 6}}, {{4, 0}, {0, 6}}},
	'y': {{{0, 0}, {2, 3}, {4, 0}}, {{2, 3}, {2, 6}}},
	'z': {{{0, 0}, {4, 0}, {0, 6}, {4, 6}}},
}

// The letter is 44% of the icon high, centred, so it stays inside the maskable safe zone (80%).
const (
	glyphHeight = 0.44
	strokeWidth = 0.085
)

type iconSpec struct {
	background color.RGBA
	foreground color.RGBA
	strokes    [][]point // in icon units 0..1
}

func newIconSpec(name, hex string) (iconSpec, error) {
	bg, err := parseHex(hex)
	if err != nil {
		return iconSpec{}, err
	}
	glyph, ok := glyphs[name[0]]
	if !ok {
		return iconSpec{}, fmt.Errorf("no glyph for %q", name[0])
	}
	fg := color.RGBA{255, 255, 255, 255}
	if luminance(bg) > 0.6 {
		fg = color.RGBA{0x17, 0x20, 0x33, 255}
	}
	scale := glyphHeight / 6
	left, top := 0.5-2*scale, 0.5-3*scale
	strokes := make([][]point, len(glyph))
	for i, line := range glyph {
		for _, p := range line {
			strokes[i] = append(strokes[i], point{left + p.x*scale, top + p.y*scale})
		}
	}
	return iconSpec{background: bg, foreground: fg, strokes: strokes}, nil
}

// svg is the browser icon: rounded corners, since browsers show it as is.
func (s iconSpec) svg() []byte {
	var b strings.Builder
	b.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 512 512">`)
	fmt.Fprintf(&b, `<rect width="512" height="512" rx="112" fill="%s"/>`, hexColor(s.background))
	fmt.Fprintf(&b, `<path fill="none" stroke="%s" stroke-width="%.1f" stroke-linecap="round" stroke-linejoin="round" d="`, hexColor(s.foreground), strokeWidth*512)
	for _, line := range s.strokes {
		for i, p := range line {
			if i == 0 {
				fmt.Fprintf(&b, "M%.1f %.1f", p.x*512, p.y*512)
			} else {
				fmt.Fprintf(&b, "L%.1f %.1f", p.x*512, p.y*512)
			}
		}
	}
	b.WriteString(`"/></svg>` + "\n")
	return []byte(b.String())
}

// png is a full-bleed square: iOS and Android mask the corners themselves.
func (s iconSpec) png(size int) ([]byte, error) {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	half := strokeWidth * float64(size) / 2
	scaled := make([][]point, len(s.strokes))
	for i, line := range s.strokes {
		for _, p := range line {
			scaled[i] = append(scaled[i], point{p.x * float64(size), p.y * float64(size)})
		}
	}
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			c := point{float64(x) + 0.5, float64(y) + 0.5}
			d := math.Inf(1)
			for _, line := range scaled {
				for i := 1; i < len(line); i++ {
					d = math.Min(d, segmentDistance(c, line[i-1], line[i]))
				}
			}
			cover := math.Max(0, math.Min(1, half-d+0.5))
			img.SetRGBA(x, y, mix(s.background, s.foreground, cover))
		}
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func segmentDistance(p, a, b point) float64 {
	dx, dy := b.x-a.x, b.y-a.y
	t := 0.0
	if l := dx*dx + dy*dy; l > 0 {
		t = math.Max(0, math.Min(1, ((p.x-a.x)*dx+(p.y-a.y)*dy)/l))
	}
	return math.Hypot(p.x-(a.x+t*dx), p.y-(a.y+t*dy))
}

func mix(a, b color.RGBA, t float64) color.RGBA {
	m := func(x, y uint8) uint8 { return uint8(math.Round(float64(x)*(1-t) + float64(y)*t)) }
	return color.RGBA{m(a.R, b.R), m(a.G, b.G), m(a.B, b.B), 255}
}

func luminance(c color.RGBA) float64 {
	lin := func(v uint8) float64 {
		s := float64(v) / 255
		if s <= 0.04045 {
			return s / 12.92
		}
		return math.Pow((s+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(c.R) + 0.7152*lin(c.G) + 0.0722*lin(c.B)
}

func parseHex(hex string) (color.RGBA, error) {
	var r, g, b uint8
	if len(hex) != 7 || hex[0] != '#' {
		return color.RGBA{}, fmt.Errorf("colour %q must be #rrggbb", hex)
	}
	if _, err := fmt.Sscanf(strings.ToLower(hex[1:]), "%02x%02x%02x", &r, &g, &b); err != nil {
		return color.RGBA{}, fmt.Errorf("colour %q must be #rrggbb", hex)
	}
	return color.RGBA{r, g, b, 255}, nil
}

func hexColor(c color.RGBA) string { return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B) }
