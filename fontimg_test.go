package fontimg

import (
	"bytes"
	"image/color"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/tdewolff/canvas"
)

func TestOpen(t *testing.T) {
	font, err := Open("sans-serif", canvas.FontRegular, nil)
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	t.Logf("font: %+v", font)
}

func TestRasterize(t *testing.T) {
	var (
		size    = 48
		style   = canvas.FontRegular
		variant = canvas.FontNormal
		fg      = color.Black
		bg      = color.White
		dpi     = 100.0
		margin  = 5.0
	)
	for _, test := range testFonts(t) {
		t.Run(test.name, func(t *testing.T) {
			f := New(nil, test.path)
			img, err := f.Rasterize(
				nil,
				size,
				style,
				variant,
				fg,
				bg,
				dpi,
				margin,
			)
			if err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			var b bytes.Buffer
			if err := png.Encode(&b, img); err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			buf := b.Bytes()
			t.Logf("buf: %d", len(buf))
			base := strings.TrimSuffix(test.path, filepath.Ext(test.path))
			if err := os.WriteFile(base+".png", buf, 0o644); err != nil {
				t.Fatalf("expected no error, got: %v", err)
			}
			if bytes.Compare(test.exp, buf) != 0 {
				t.Errorf("expected %s to match rasterized image", test.golden)
			}
		})
	}
}

type testFont struct {
	path   string
	golden string
	name   string
	exp    []byte
}

func testFonts(t *testing.T) []testFont {
	var tests []testFont
	err := fs.WalkDir(os.DirFS("testdata"), ".", func(name string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir(), !extRE.MatchString(name):
			return nil
		}
		pathstr := filepath.Join("testdata", name)
		golden := strings.TrimSuffix(pathstr, filepath.Ext(pathstr)) + ".png.golden"
		exp, err := os.ReadFile(golden)
		if err != nil {
			t.Fatalf("expected no error, got: %v", err)
		}
		tests = append(tests, testFont{
			path:   pathstr,
			golden: golden,
			name:   name,
			exp:    exp,
		})
		return nil
	})
	if err != nil {
		t.Fatalf("expected no error, got: %v", err)
	}
	return tests
}
