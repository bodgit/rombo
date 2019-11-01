package rombo

import (
	"errors"
	"path/filepath"
	"regexp"
	"strings"
)

type Layout interface {
	exportPath(string, string, string) (string, bool, string, error)
	ignorePath(string) bool
}

func firstAlphanumeric(s string) (string, error) {
	for _, c := range s {
		switch {
		case 'A' <= c && c <= 'Z':
			fallthrough
		case 'a' <= c && c <= 'z':
			fallthrough
		case '0' <= c && c <= '9':
			return strings.ToUpper(string(c)), nil
		}
	}

	return "", errors.New("no alphanumberic character")
}

type SimpleCompressed struct{}

func (SimpleCompressed) exportPath(name, description, filename string) (string, bool, string, error) {
	// Create a zip using the name of the game containing the filename
	return name + ".zip", true, filename, nil
}

func (SimpleCompressed) ignorePath(relpath string) bool {
	// Don't ignore any files
	return false
}

type MegaSD struct{}

func (MegaSD) exportPath(name, description, filename string) (string, bool, string, error) {
	parent, err := firstAlphanumeric(name)
	if err != nil {
		return "", false, "", err
	}

	switch filepath.Ext(filename) {
	case ".cue", ".bin":
		re := regexp.MustCompile(`\s+\(Disc\s\d+\)\s*`)
		dir := re.ReplaceAllString(name, "")

		return filepath.Join(parent, dir, filename), false, "", nil
	default:
		return filepath.Join(parent, filename), false, "", nil
	}
}

func (MegaSD) ignorePath(relpath string) bool {
	switch relpath {
	case "BUP", "CHEATS", "STATES", "lastmsd.cfg": // System files
		fallthrough
	case filepath.Join("BIOS", "bios.cfg"): // Mega CD BIOS configuration
		return true
	}

	switch filepath.Ext(relpath) {
	case ".upd": // Firmware update (filename contains serial number)
		return true
	}

	return false
}

type JaguarSD struct{}

func (JaguarSD) exportPath(name, description, filename string) (string, bool, string, error) {
	return filename, false, "", nil
}

func (JaguarSD) ignorePath(relpath string) bool {
	switch relpath {
	case "firmware.upd": // Firmware update
		return true
	}

	switch filepath.Ext(relpath) {
	case ".e2p", ".mrq": // Ignore any saved state & marquee files
		return true
	}

	return false
}

type SD2SNES struct{}

func (SD2SNES) exportPath(name, description, filename string) (string, bool, string, error) {
	return filename, false, "", nil
}

func (SD2SNES) ignorePath(relpath string) bool {
	switch relpath {
	case "sd2snes": // Ignore the system directory entirely
		return true
	}
	return false
}
