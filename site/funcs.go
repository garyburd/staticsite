package site

import (
	"encoding/json"
	"errors"
	"fmt"
	htemplate "html/template"
	"image"
	"io/ioutil"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type slice []interface{}

// functionContext is the context for template functions.
type functionContext struct {
	// The site.
	site *site

	// modTime is maxiumum modification time of any referenced file.
	modTime time.Time

	// Current page URL directory.
	udir string

	// Current page file directory.
	fdir string
}

func (fc *functionContext) funcs() htemplate.FuncMap {
	return htemplate.FuncMap{
		"slice": func(v ...interface{}) []interface{} { return slice(v) },

		"pathBase": path.Base,
		"pathDir":  path.Dir,
		"pathJoin": path.Join,

		"stringTrimPrefix": strings.TrimPrefix,
		"stringTrimSuffix": strings.TrimSuffix,
		"stringTrimSpace":  strings.TrimSpace,

		"timeNow": time.Now,

		"glob":            fc.glob,
		"include":         fc.include,
		"includeCSS":      fc.includeCSS,
		"includeHTML":     fc.includeHTML,
		"includeHTMLAttr": fc.includeHTMLAttr,
		"includeJS":       fc.includeJS,
		"includeJSStr":    fc.includeJSStr,
		"readJSON":        fc.readJSON,
		"readPage":        fc.readPage,
		"readPages":       fc.readPages,
		"readImage":       fc.readImage,
		"readImageSrcSet": fc.readImageSrcSet,
	}
}

func (fc *functionContext) updateModTime(fpath string) error {
	fi, err := os.Stat(fpath)
	if err != nil {
		return err
	}
	fc.modTime = maxTime(fc.modTime, fi.ModTime())
	return nil
}

func (fc *functionContext) toFilePath(upath string) string {
	if !strings.HasPrefix(upath, "/") {
		upath = path.Join(fc.udir, upath)
	}
	return fc.site.toFilePath(upath)
}

func (fc *functionContext) toURLPath(abs bool, fpath string) (string, error) {
	if abs {
		p, err := filepath.Rel(fc.site.dir, fpath)
		if err != nil {
			return "", err
		}
		return "/" + filepath.ToSlash(p), nil
	}

	p, err := filepath.Rel(fc.fdir, fpath)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(p), nil
}

func (fc *functionContext) globInternal(uglob string) (fpaths []string, upaths []string, err error) {
	fglob := fc.toFilePath(uglob)
	fpaths, err = filepath.Glob(fglob)
	if err != nil {
		return nil, nil, err
	}

	upaths = make([]string, len(fpaths))
	abs := strings.HasPrefix(uglob, "/")
	for i, fpath := range fpaths {
		upaths[i], err = fc.toURLPath(abs, fpath)
		if err != nil {
			return nil, nil, err
		}
	}

	return fpaths, upaths, nil
}

func (fc *functionContext) glob(uglob string) ([]string, error) {
	_, upaths, err := fc.globInternal(uglob)
	return upaths, err
}

func (fc *functionContext) include(upath string) (string, error) {
	fpath := fc.toFilePath(upath)
	fc.updateModTime(fpath)
	b, err := ioutil.ReadFile(fpath)
	return string(b), err
}

func (fc *functionContext) includeCSS(upath string) (htemplate.CSS, error) {
	s, err := fc.include(upath)
	return htemplate.CSS(s), err
}

func (fc *functionContext) includeHTML(upath string) (htemplate.HTML, error) {
	s, err := fc.include(upath)
	return htemplate.HTML(s), err
}

func (fc *functionContext) includeHTMLAttr(upath string) (htemplate.HTMLAttr, error) {
	s, err := fc.include(upath)
	return htemplate.HTMLAttr(s), err
}

func (fc *functionContext) includeJS(upath string) (htemplate.JS, error) {
	s, err := fc.include(upath)
	return htemplate.JS(s), err
}

func (fc *functionContext) includeJSStr(upath string) (htemplate.JSStr, error) {
	s, err := fc.include(upath)
	return htemplate.JSStr(s), err
}

func (fc *functionContext) readJSON(upath string) (interface{}, error) {
	fpath := fc.toFilePath(upath)
	fc.updateModTime(fpath)
	f, err := os.Open(fpath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var v interface{}
	err = json.NewDecoder(f).Decode(&v)
	return v, err
}

func (fc *functionContext) readPage(upath string) (*Page, error) {
	if strings.HasSuffix(upath, "/") {
		upath += "index.html"
	}

	fpath := fc.toFilePath(upath)
	fc.updateModTime(fpath)

	if strings.HasSuffix(upath, "/index.html") {
		upath = upath[:len(upath)-len("index.html")]
	}

	p := &Page{Path: upath}
	_, _, err := readFileWithFrontMatter(fpath, p)
	return p, err
}

func (fc *functionContext) readPages(uglob string, options ...string) ([]*Page, error) {
	if strings.HasSuffix(uglob, "/") {
		uglob += "index.html"
	}

	fpaths, upaths, err := fc.globInternal(uglob)
	if err != nil {
		return nil, err
	}

	var pages []*Page
	for i, fpath := range fpaths {
		upath := upaths[i]

		fc.updateModTime(fpath)
		if strings.HasSuffix(upath, "/index.html") {
			upath = upath[:len(upath)-len("index.html")]
		}

		page := &Page{Path: upath}
		_, _, err := readFileWithFrontMatter(fpath, page)
		if err != nil {
			return nil, err
		}
		pages = append(pages, page)
	}

	for _, option := range options {
		switch {
		case option == "sort:-Created":
			sort.Slice(pages, func(i, j int) bool {
				return pages[j].Created.Before(pages[i].Created)
			})
		case strings.HasPrefix(option, "limit:"):
			s := option[len("limit:"):]
			n, err := strconv.Atoi(s)
			if err != nil {
				return nil, fmt.Errorf("readPages: invalid limit %q", s)
			}
			if n < len(pages) {
				pages = pages[:n]
			}
		default:
			return nil, fmt.Errorf("readPages: invalid option %q", option)
		}
	}

	return pages, nil
}

type Image struct {
	W   int
	H   int
	Src string
}

func (img *Image) SrcWidthHeight() htemplate.HTMLAttr {
	return htemplate.HTMLAttr(fmt.Sprintf(`src="%s" width="%d" height="%d"`, img.Src, img.W, img.H))
}

func (fc *functionContext) readImage(upath string) (*Image, error) {
	config, err := readImageConfig(fc.toFilePath(upath))
	return &Image{Src: upath, W: config.Width, H: config.Height}, err
}

type ImageSrcSet struct {
	Image
	SrcSet string
}

func (fc *functionContext) readImageSrcSet(src string) (*ImageSrcSet, error) {
	fsrc := fc.toFilePath(src)
	config, err := readImageConfig(fsrc)
	if err != nil {
		return nil, err
	}
	result := &ImageSrcSet{Image: Image{Src: src, W: config.Width, H: config.Height}}
	var buf strings.Builder
	fmt.Fprintf(&buf, "%s %dw", src, config.Width)

	sdir, sfile := path.Split(src)
	dot := strings.LastIndexByte(sfile, '.')
	dash := strings.LastIndexByte(sfile, '-')
	if dot < 0 || dash < 0 || dot < dash {
		return nil, errors.New("src name must be of form <id>-<variant>.<ext>")
	}
	uglob := path.Join(sdir, sfile[:dash+1]+"*"+sfile[dot:])

	fpaths, upaths, err := fc.globInternal(uglob)
	for i, fpath := range fpaths {
		if fpath == fsrc {
			continue
		}
		config, err := readImageConfig(fpath)
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&buf, ",%s %dw", upaths[i], config.Width)
	}
	result.SrcSet = buf.String()
	return result, err
}

func readImageConfig(fpath string) (image.Config, error) {
	f, err := os.Open(fpath)
	if err != nil {
		return image.Config{}, err
	}
	defer f.Close()
	config, _, err := image.DecodeConfig(f)
	if err != nil {
		err = fmt.Errorf("error reading %s: %w", fpath, err)
	}
	return config, err
}