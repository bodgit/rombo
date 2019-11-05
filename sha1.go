package rombo

import (
	"crypto/sha1"
	"fmt"
	"io/ioutil"
)

func sha1Sum(file string) (string, uint64, error) {
	data, err := ioutil.ReadFile(file)
	if err != nil {
		return "", 0, err
	}

	sha := fmt.Sprintf("%x", sha1.Sum(data))

	return sha, uint64(len(data)), nil
}
