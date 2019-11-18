package rombo

import (
	"archive/zip"
	"fmt"
	"hash/crc32"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/bodgit/rombo/internal/plumbing"
	"github.com/uwedeportivo/torrentzip"
)

func zipCRC(f *zip.File) string {
	return fmt.Sprintf("%.*x", crc32.Size<<1, f.CRC32)
}

func fileExistsInZip(path, name string) (bool, string, uint64, error) {
	file, err := os.Open(path)
	if err != nil {
		return false, "", 0, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return false, "", 0, err
	}

	reader, err := zip.NewReader(plumbing.TeeReaderAt(file, &plumbing.WriteCounter{}), info.Size())
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

func createOrUpdateZip(path, name string, fr io.Reader) (uint64, uint64, error) {
	tmpfile, err := ioutil.TempFile(filepath.Dir(path), "."+filepath.Base(path))
	if err != nil {
		return 0, 0, err
	}
	defer os.Remove(tmpfile.Name())

	bytesIn := &plumbing.WriteCounter{}
	bytesOut := &plumbing.WriteCounter{}

	w, err := torrentzip.NewWriter(io.MultiWriter(tmpfile, bytesOut))
	if err != nil {
		return 0, 0, err
	}

	file, err := os.Open(path)
	if err != nil && !os.IsNotExist(err) {
		return 0, 0, err
	}

	if err == nil {
		defer file.Close()

		info, err := file.Stat()
		if err != nil {
			return 0, 0, err
		}

		reader, err := zip.NewReader(plumbing.TeeReaderAt(file, bytesIn), info.Size())
		if err != nil {
			return 0, 0, err
		}

		for _, f := range reader.File {
			if name == f.Name {
				continue
			}

			fr, err := f.Open()
			if err != nil {
				return 0, 0, err
			}

			fw, err := w.Create(f.Name)
			if err != nil {
				return 0, 0, err
			}

			_, err = io.Copy(fw, fr)
			if err != nil {
				return 0, 0, err
			}

			fr.Close()
		}

		file.Close()
	}

	fw, err := w.Create(name)
	if err != nil {
		return 0, 0, err
	}

	_, err = io.Copy(fw, fr)
	if err != nil {
		return 0, 0, err
	}

	if err := w.Close(); err != nil {
		return 0, 0, err
	}

	if err := tmpfile.Close(); err != nil {
		return 0, 0, err
	}

	if err := os.Rename(tmpfile.Name(), path); err != nil {
		return 0, 0, err
	}

	return bytesIn.Count(), bytesOut.Count(), nil
}
