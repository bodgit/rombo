package rombo

import (
	"crypto/sha1"
	"fmt"
	"io"
	"os"
)

func sha1Sum(file string) (string, uint64, error) {
	f, err := os.Open(file)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()

	h := sha1.New()
	length, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}

	return fmt.Sprintf("%x", h.Sum(nil)), uint64(length), nil
}
