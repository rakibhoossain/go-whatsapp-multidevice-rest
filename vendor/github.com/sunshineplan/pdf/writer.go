package pdf

import (
	"bytes"
	"image"
	"image/jpeg"
	"io"
	"os"

	"github.com/pdfcpu/pdfcpu/pkg/api"
)

// DefaultQuality is the default quality encoding parameter.
const DefaultQuality = 75

// Options are the encoding parameters. Quality ranges from 1 to 100 inclusive, higher is better.
type Options struct {
	Quality int
}

// EncodeReader writes r to w.
func EncodeReader(w io.Writer, r []io.Reader) error {
	return api.ImportImages(nil, w, r, nil, nil)
}

// Append appends images to w.
func Append(rs io.ReadSeeker, w io.Writer, imgs []io.Reader) error {
	return api.ImportImages(rs, w, imgs, nil, nil)
}

func processImage(imgs []image.Image, o *Options) ([]io.Reader, error) {
	quality := DefaultQuality
	if o != nil {
		quality = o.Quality
		if quality < 1 {
			quality = 1
		} else if quality > 100 {
			quality = 100
		}
	}

	var r []io.Reader
	for _, i := range imgs {
		var buf bytes.Buffer
		if err := jpeg.Encode(&buf, i, &jpeg.Options{Quality: quality}); err != nil {
			return nil, err
		}
		r = append(r, &buf)
	}

	return r, nil
}

// Encode writes images to w.
func Encode(w io.Writer, imgs []image.Image, o *Options) error {
	r, err := processImage(imgs, o)
	if err != nil {
		return err
	}

	return EncodeReader(w, r)
}

func fileExists(filename string) bool {
	if _, err := os.Stat(filename); err == nil {
		return true
	}
	return false
}

// EncodeFile appends images to outFile which will be created if necessary.
func EncodeFile(outFile string, imgFiles []string, o *Options) (err error) {
	var f1, f2 *os.File
	var rs io.ReadSeeker
	tmpFile := outFile
	if fileExists(outFile) {
		if f1, err = os.Open(outFile); err != nil {
			return err
		}
		rs = f1
		tmpFile += ".tmp"
	}

	var imgs []image.Image
	for _, i := range imgFiles {
		f, err := os.Open(i)
		if err != nil {
			return err
		}
		defer f.Close()
		img, _, err := image.Decode(f)
		if err != nil {
			return err
		}
		imgs = append(imgs, img)
	}

	r, err := processImage(imgs, o)
	if err != nil {
		return err
	}

	if f2, err = os.Create(tmpFile); err != nil {
		return err
	}

	defer func() {
		if err != nil {
			f2.Close()
			if f1 != nil {
				f1.Close()
				os.Remove(tmpFile)
			}
			return
		}
		if err = f2.Close(); err != nil {
			return
		}
		if f1 != nil {
			if err = f1.Close(); err != nil {
				return
			}
			if err = os.Rename(tmpFile, outFile); err != nil {
				return
			}
		}
	}()

	return Append(rs, f2, r)
}
