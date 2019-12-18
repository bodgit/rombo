package rombo

import (
	"archive/zip"
	"crypto/sha1"
	"fmt"
	"hash/crc32"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/uwedeportivo/torrentzip"
)

func zipCRC(f *zip.File) string {
	return fmt.Sprintf("%.*x", crc32.Size<<1, f.CRC32)
}

func fileExistsInZip(path, name string) (bool, string, uint64, error) {
	reader, err := zip.OpenReader(path)
	if err != nil {
		return false, "", 0, err
	}

	for _, f := range reader.File {
		if f.Name == name {
			return true, zipCRC(f), f.UncompressedSize64, nil
		}
	}

	return false, "", 0, nil
}

func createOrUpdateZip(path, name string, fr io.Reader) error {
	tmpfile, err := ioutil.TempFile(filepath.Dir(path), "."+filepath.Base(path))
	if err != nil {
		return err
	}
	defer os.Remove(tmpfile.Name())

	w, err := torrentzip.NewWriter(tmpfile)
	if err != nil {
		return err
	}

	reader, err := zip.OpenReader(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}

	if err == nil {
		defer reader.Close()

		for _, f := range reader.File {
			if name == f.Name {
				continue
			}

			fr, err := f.Open()
			if err != nil {
				return err
			}

			fw, err := w.Create(f.Name)
			if err != nil {
				return err
			}

			_, err = io.Copy(fw, fr)
			if err != nil {
				return err
			}

			fr.Close()
		}

		reader.Close()
	}

	fw, err := w.Create(name)
	if err != nil {
		return err
	}

	_, err = io.Copy(fw, fr)
	if err != nil {
		return err
	}

	if err := w.Close(); err != nil {
		return err
	}

	if err := tmpfile.Close(); err != nil {
		return err
	}

	if err := os.Rename(tmpfile.Name(), path); err != nil {
		return err
	}

	return nil
}

func recreateZip(path string) (string, string, error) {
	tmpfile, err := ioutil.TempFile(os.TempDir(), filepath.Base(path))
	if err != nil {
		return "", "", err
	}

	h := sha1.New()

	// Create new zip and compute SHA1 at the same time
	w, err := torrentzip.NewWriter(io.MultiWriter(tmpfile, h))
	if err != nil {
		return "", "", err
	}

	reader, err := zip.OpenReader(path)
	if err != nil {
		return "", "", err
	}
	defer reader.Close()

	for _, f := range reader.File {
		fr, err := f.Open()
		if err != nil {
			return "", "", err
		}

		fw, err := w.Create(f.Name)
		if err != nil {
			return "", "", err
		}

		_, err = io.Copy(fw, fr)
		if err != nil {
			return "", "", err
		}

		fr.Close()
	}

	if err := w.Close(); err != nil {
		return "", "", err
	}

	if err := tmpfile.Close(); err != nil {
		return "", "", err
	}

	return tmpfile.Name(), fmt.Sprintf("%x", h.Sum(nil)), nil
}
