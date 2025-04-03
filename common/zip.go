package common

import (
	"archive/zip"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
)

// CreateZip is a utility function which creates a destination zip file with
// the specified file(s) as its content.
func CreateZip(outfile string, infiles []string) error {
	buf := new(bytes.Buffer)
	w := zip.NewWriter(buf)
	for _, file := range infiles {
		f, err := w.Create(filepath.Base(file))
		if err != nil {
			return fmt.Errorf("createzip: create: %w", err)
		}
		contents, err := os.ReadFile(file)
		if err != nil {
			return fmt.Errorf("createzip: readfile: %w", err)
		}
		_, err = f.Write(contents)
		if err != nil {
			return fmt.Errorf("createzip: write: %w", err)
		}
	}
	err := w.Close()
	return err
}

type InternalZipWriter struct {
	w *zip.Writer
	b *bytes.Buffer
}

func NewInternalZipWriter() InternalZipWriter {
	izw := InternalZipWriter{}
	izw.b = new(bytes.Buffer)
	izw.w = zip.NewWriter(izw.b)
	return izw
}

func (izw *InternalZipWriter) Store(name string, contents []byte) error {
	f, err := izw.w.Create(name)
	if err != nil {
		return err
	}
	_, err = f.Write(contents)
	return err
}

func (izw *InternalZipWriter) GetData() ([]byte, error) {
	if izw.w == nil {
		return []byte{}, fmt.Errorf("nil InternalZipWriter")
	}
	err := izw.w.Close()
	if err != nil {
		return []byte{}, err
	}
	return izw.b.Bytes(), nil
}
