package pdf

import (
	"bytes"
	"fmt"
	"image"
	"io"
	"sync"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

// Reader struct represents a PDF reader that holds a *model.Context.
type Reader struct {
	mu sync.Mutex

	ctx *model.Context
	p   int
}

// NewReader creates a new Reader from an io.ReadSeeker and a *model.Configuration.
// If conf is nil, model.NewDefaultConfiguration() will be used.
func NewReader(rs io.ReadSeeker, conf *model.Configuration) (*Reader, error) {
	if conf == nil {
		conf = model.NewDefaultConfiguration()
	}
	ctx, err := pdfcpu.Read(rs, conf)
	if err != nil {
		return nil, err
	}
	if err := api.OptimizeContext(ctx); err != nil {
		return nil, err
	}
	if ctx.PageCount == 0 {
		return nil, fmt.Errorf("page count is zero")
	}
	return &Reader{ctx: ctx}, nil
}

// PageCount returns the number of pages in the PDF.
func (r *Reader) PageCount() int {
	return r.ctx.PageCount
}

// Next advances the Reader to the next page and returns true if there are more pages to read.
func (r *Reader) Next() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.p++
	return r.p <= r.ctx.PageCount
}

// ExtractPage extracts all images from the specified page as []model.Image.
func (r *Reader) ExtractPage(pageNr int) (imgs []model.Image, err error) {
	m, err := pdfcpu.ExtractPageImages(r.ctx, pageNr, false)
	if err != nil {
		return
	}
	for _, v := range m {
		imgs = append(imgs, v)
	}
	return
}

// Extract extracts all images from the current page as []model.Image.
func (r *Reader) Extract() ([]model.Image, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ExtractPage(r.p)
}

var _ error = formatError("")

type formatError string

func (err formatError) Error() string {
	return fmt.Sprintf("image: unknown format: %s", string(err))
}

func (formatError) Unwrap() error {
	return image.ErrFormat
}

// ExtractPageImages extracts all images from the specified page as []image.Image.
func (r *Reader) ExtractPageImages(pageNr int) (imgs []image.Image, err error) {
	res, err := r.ExtractPage(pageNr)
	if err != nil {
		return
	}
	for _, r := range res {
		var img image.Image
		img, _, err = image.Decode(r)
		if err != nil {
			if err == image.ErrFormat && r.FileType != "" {
				err = formatError(r.FileType)
			}
			return
		}
		imgs = append(imgs, img)
	}
	return
}

// ExtractImages extracts all images from the current page as []image.Image.
func (r *Reader) ExtractImages() ([]image.Image, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.ExtractPageImages(r.p)
}

func newReader(r io.Reader) (*Reader, error) {
	if rs, ok := r.(io.ReadSeeker); ok {
		return NewReader(rs, nil)
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return NewReader(bytes.NewReader(b), nil)
}

// Decode decodes a PDF file from r and returns first image as image.Image.
func Decode(r io.Reader) (image.Image, error) {
	reader, err := newReader(r)
	if err != nil {
		return nil, err
	}
	reader.Next()
	imgs, err := reader.ExtractImages()
	if err != nil {
		return nil, err
	}
	return imgs[0], nil
}

// DecodeAll decodes a PDF file from r and returns all images as image.Image.
func DecodeAll(r io.Reader) ([]image.Image, error) {
	reader, err := newReader(r)
	if err != nil {
		return nil, err
	}
	var imgs []image.Image
	for reader.Next() {
		res, err := reader.ExtractImages()
		if err != nil {
			return nil, err
		}
		imgs = append(imgs, res...)
	}
	return imgs, err
}

// DecodeConfig returns the color model and dimensions of a PDF first image without
// decoding the entire image.
func DecodeConfig(r io.Reader) (cfg image.Config, err error) {
	reader, err := newReader(r)
	if err != nil {
		return
	}
	reader.Next()
	rs, err := reader.Extract()
	if err != nil {
		return
	}
	cfg, _, err = image.DecodeConfig(rs[0])
	return
}

func init() {
	image.RegisterFormat("pdf", "%PDF", Decode, DecodeConfig)
}
