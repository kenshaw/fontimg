// Package fontimg provides a preview image of a font file (ttf, otf, woff, ...).
package fontimg

import (
	"bytes"
	_ "embed"
	"fmt"
	"image"
	"image/color"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"unicode"

	"github.com/tdewolff/canvas"
	"github.com/tdewolff/canvas/renderers/rasterizer"
	fontpkg "github.com/tdewolff/font"
)

// Open opens fonts as either a path on disk or from the system fonts. When
// sysfonts is nil, the default system fonts will be loaded.
func Open(name string, style canvas.FontStyle, sysfonts *fontpkg.SystemFonts) ([]*Font, error) {
	if sysfonts == nil {
		var err error
		once.Do(func() {
			sfonts, err = fontpkg.FindSystemFonts(fontpkg.DefaultFontDirs())
		})
		if err != nil {
			return nil, err
		}
		sysfonts = sfonts
	}
	var v []*Font
	switch fi, err := os.Stat(name); {
	case err == nil && fi.IsDir():
		entries, err := os.ReadDir(name)
		if err != nil {
			return nil, fmt.Errorf("unable to open directory %q: %v", name, err)
		}
		for _, entry := range entries {
			if s := entry.Name(); !entry.IsDir() && extRE.MatchString(s) {
				v = append(v, New(nil, filepath.Join(name, s)))
			}
		}
		sort.Slice(v, func(i, j int) bool {
			return strings.ToLower(v[i].Family) < strings.ToLower(v[j].Family)
		})
	case err == nil:
		v = append(v, New(nil, name))
	default:
		if font := Match(name, style, sysfonts); font != nil {
			v = append(v, font)
		}
	}
	if len(v) == 0 {
		return nil, fmt.Errorf("unable to locate font %q", name)
	}
	return v, nil
}

// Match creates a font image for a matching font name from the system fonts.
func Match(name string, style canvas.FontStyle, sysfonts *fontpkg.SystemFonts) *Font {
	md, ok := sysfonts.Match(name, fontpkg.ParseStyle(style.String()))
	if !ok {
		return nil
	}
	return NewFont(md)
}

// Font is a font image.
type Font struct {
	Buf        []byte
	Path       string
	Family     string
	Name       string
	Style      string
	SampleText string
	once       sync.Once
}

// NewFont creates a new font image.
func NewFont(md fontpkg.FontMetadata) *Font {
	family := md.Family
	if family == "" {
		family = titleCase(strings.TrimSuffix(filepath.Base(md.Filename), filepath.Ext(md.Filename)))
	}
	return &Font{
		Path:   md.Filename,
		Family: family,
		Style:  md.Style.String(),
	}
}

// New creates a font image from a path.
func New(buf []byte, path string) *Font {
	return &Font{
		Buf:    buf,
		Path:   path,
		Family: titleCase(strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))),
	}
}

// BestName returns the best name for a font.
func (font *Font) BestName() string {
	if font.Name != "" {
		return font.Name
	}
	return font.Family
}

// String satisfies the [fmt.Stringer] interface.
func (font *Font) String() string {
	name := font.BestName()
	if font.Style != "" {
		name += " (" + font.Style + ")"
	}
	return fmt.Sprintf("%q: %s", name, font.Path)
}

// WriteYAML writes YAML information to w.
func (font *Font) WriteYAML(w io.Writer) {
	fmt.Fprintln(w, "---")
	fmt.Fprintf(w, "path: %s\n", font.Path)
	fmt.Fprintf(w, "family: %q\n", font.BestName())
	fmt.Fprintf(w, "style: %q\n", font.Style)
}

// Load loads the font style.
func (font *Font) Load(style canvas.FontStyle) (*canvas.FontFamily, error) {
	ff := canvas.NewFontFamily(font.Family)
	switch {
	case font.Buf != nil:
		if err := ff.LoadFont(font.Buf, 0, style); err != nil {
			return nil, err
		}
	case font.Path != "":
		if err := ff.LoadFontFile(font.Path, style); err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("font.Buf and font.Path not set")
	}
	font.once.Do(func() {
		face := ff.Face(16)
		if v := face.Font.SFNT.Name.Get(fontpkg.NameFontFamily); 0 < len(v) {
			font.Name = v[0].String()
		}
		if v := face.Font.SFNT.Name.Get(fontpkg.NameFontSubfamily); 0 < len(v) {
			font.Style = fontpkg.ParseStyle(v[0].String()).String()
		}
		if v := face.Font.SFNT.Name.Get(fontpkg.NameSampleText); 0 < len(v) {
			font.SampleText = v[0].String()
		}
	})
	return ff, nil
}

// Rasterize rasterizes the font image.
func (font *Font) Rasterize(
	tpl *template.Template,
	fontSize int, style canvas.FontStyle, variant canvas.FontVariant,
	fg, bg color.Color,
	dpi, margin float64,
) (*image.RGBA, error) {
	// default template
	if tpl == nil {
		tpl = tplDefault
	}
	// load font family
	ff, err := font.Load(style)
	if err != nil {
		return nil, err
	}
	// generate text
	buf := new(bytes.Buffer)
	if err := tpl.Execute(buf, TemplateData{
		Size:       fontSize,
		Name:       font.BestName(),
		Style:      font.Style,
		SampleText: font.SampleText,
	}); err != nil {
		return nil, err
	}
	// create canvas and context
	c := canvas.New(100, 100)
	ctx := canvas.NewContext(c)
	ctx.SetZIndex(1)
	ctx.SetFillColor(fg)
	// draw text
	lines, sizes := breakLines(buf.Bytes(), fontSize)
	for i, y := 0, float64(0); i < len(lines); i++ {
		face := ff.Face(float64(sizes[i]), fg, style, variant)
		txt := canvas.NewTextBox(face, strings.TrimSpace(lines[i]), 0, 0, canvas.Left, canvas.Top, 0, 0)
		b := txt.Bounds()
		ctx.DrawText(0, y, txt)
		y += b.Y0 - b.Y1
	}
	// fit canvas to context
	c.Fit(margin)
	// draw background
	ctx.SetZIndex(-1)
	ctx.SetFillColor(bg)
	width, height := ctx.Size()
	ctx.DrawPath(0, 0, canvas.Rectangle(width, height))
	// close drawing context
	ctx.Close()
	// rasterize
	return rasterizer.Draw(c, canvas.DPI(dpi), canvas.DefaultColorSpace), nil
}

// TemplateData is the data passed to the text template.
type TemplateData struct {
	Size       int
	Name       string
	Style      string
	SampleText string
}

// breakLines breaks the text up by lines, returning the lines and the font
// size for each line.
func breakLines(buf []byte, size int) ([]string, []int) {
	var lines []string
	var sizes []int
	for _, line := range bytes.Split(buf, []byte{'\n'}) {
		sz := size
		if m := sizeRE.FindSubmatch(line); m != nil {
			if s, err := strconv.Atoi(string(m[1])); err == nil {
				sz = s
			}
			line = m[2]
		}
		lines, sizes = append(lines, string(line)), append(sizes, sz)
	}
	return lines, sizes
}

// titleCase returns the title case for a name.
func titleCase(name string) string {
	var prev rune
	var s []rune
	r := []rune(name)
	for i, c := range r {
		switch {
		case unicode.IsLower(prev) && unicode.IsUpper(c):
			s = append(s, ' ')
		case !unicode.IsLetter(c):
			c = ' '
		}
		if unicode.IsUpper(prev) && unicode.IsUpper(c) && unicode.IsLower(peek(r, i+1)) {
			s = append(s, ' ')
		}
		s = append(s, c)
		prev = c
	}
	return spaceRE.ReplaceAllString(strings.TrimSpace(string(s)), " ")
}

// peek peeks a rune.
func peek(r []rune, i int) rune {
	if i < len(r) {
		return r[i]
	}
	return 0
}

var (
	extRE   = regexp.MustCompile(`(?i)\.(ttf|ttc|otf|woff|woff2|sfnt)$`)
	sizeRE  = regexp.MustCompile(`^\x00([0-9]+)\x00(.*)$`)
	spaceRE = regexp.MustCompile(`\s+`)
)

var (
	sfonts     *fontpkg.SystemFonts
	once       sync.Once
	tplDefault *template.Template
)

func init() {
	var err error
	if tplDefault, err = NewTemplate(string(textTpl)); err != nil {
		panic(err)
	}
}

// NewTemplate creates a text template.
func NewTemplate(text string) (*template.Template, error) {
	return template.New("").Funcs(map[string]interface{}{
		"size": func(size int) string {
			return fmt.Sprintf("\x00%d\x00", size)
		},
		"inc": func(a, b int) int {
			return a + b
		},
	}).Parse(text)
}

//go:embed text.tpl
var textTpl []byte
