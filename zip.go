package rombo

import (
	"archive/zip"
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
	defer reader.Close()

	for _, f := range reader.File {
		if f.Name == name {
			return true, zipCRC(f), f.UncompressedSize64, nil
		}
	}

	return false, "", 0, nil
}

func createOrUpdateZip(path, file, name string) (uint64, error) {
	tmpfile, err := ioutil.TempFile(filepath.Dir(path), "."+filepath.Base(path))
	if err != nil {
		return 0, err
	}
	defer os.Remove(tmpfile.Name())

	w, err := torrentzip.NewWriter(tmpfile)
	if err != nil {
		return 0, err
	}

	reader, err := zip.OpenReader(path)
	if err != nil && !os.IsNotExist(err) {
		return 0, err
	}

	if err == nil {
		defer reader.Close()

		for _, f := range reader.File {
			if name == f.Name {
				continue
			}

			fr, err := f.Open()
			if err != nil {
				return 0, err
			}

			fw, err := w.Create(f.Name)
			if err != nil {
				return 0, err
			}

			_, err = io.Copy(fw, fr)
			if err != nil {
				return 0, err
			}

			fr.Close()
		}

		reader.Close()
	}

	fr, err := os.Open(file)
	if err != nil {
		return 0, err
	}
	defer fr.Close()

	fw, err := w.Create(name)
	if err != nil {
		return 0, err
	}

	_, err = io.Copy(fw, fr)
	if err != nil {
		return 0, err
	}

	if err := fr.Close(); err != nil {
		return 0, err
	}

	if err := w.Close(); err != nil {
		return 0, err
	}

	if err := tmpfile.Close(); err != nil {
		return 0, err
	}

	info, err := os.Stat(tmpfile.Name())
	if err != nil {
		return 0, err
	}

	if err := os.Rename(tmpfile.Name(), file); err != nil {
		return 0, err
	}

	return uint64(info.Size()), nil
}
