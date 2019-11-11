package rombo

import (
	"errors"
	"path/filepath"
	"regexp"
	"strings"
)

const (
	noIntroBIOS = "[BIOS] " // No-Intro dat file prefix for BIOS images
)

type Layout interface {
	exportPath(ROM) (string, bool, string, error)
	ignorePath(string) bool
}

func firstAlphanumeric(s string) (string, error) {
	s = strings.TrimPrefix(s, noIntroBIOS)
	for _, c := range s {
		switch {
		case 'A' <= c && c <= 'Z':
			fallthrough
		case 'a' <= c && c <= 'z':
			return strings.ToUpper(string(c)), nil
		case '0' <= c && c <= '9':
			return "#", nil
		}
	}

	return "", errors.New("no alphanumberic character")
}

type SimpleCompressed struct{}

func (SimpleCompressed) exportPath(rom ROM) (string, bool, string, error) {
	// Create a zip using the name of the game containing the filename
	return rom.Game + ".zip", true, rom.Filename, nil
}

func (SimpleCompressed) ignorePath(relpath string) bool {
	// Don't ignore any files
	return false
}

type MegaSD struct{}

func (MegaSD) exportPath(rom ROM) (string, bool, string, error) {
	parent, err := firstAlphanumeric(rom.Game)
	if err != nil {
		return "", false, "", err
	}

	// Keep any machine BIOS images in a separate BIOS directory, as the
	// MegaSD needs the Mega CD/Sega CD BIOS stored there at least, but
	// let any built-in games fall through and get stored as normal
	if strings.HasPrefix(rom.Filename, noIntroBIOS) {
		switch {
		case strings.Contains(rom.Filename, "32X"):
			fallthrough
		case strings.Contains(rom.Filename, "LaserActive"):
			fallthrough
		case strings.Contains(rom.Filename, "Mega-CD"):
			fallthrough
		case strings.Contains(rom.Filename, "Sega CD"):
			fallthrough
		case strings.Contains(rom.Filename, "WonderMega"):
			return filepath.Join("BIOS", rom.Filename), false, "", nil
		}
	}

	switch filepath.Ext(rom.Filename) {
	case ".sms":
		return filepath.Join("Master System & Mark III", parent, rom.Filename), false, "", nil
	case ".md":
		return filepath.Join("Mega Drive & Genesis", parent, rom.Filename), false, "", nil
	case ".32x":
		return filepath.Join("32X", parent, rom.Filename), false, "", nil
	case ".cue", ".bin":
		re := regexp.MustCompile(`\s+\(Disc\s\d+\)`)
		dir := re.ReplaceAllString(rom.Game, "")

		return filepath.Join("Mega CD & Sega CD", parent, dir, rom.Filename), false, "", nil
	default:
		return filepath.Join(parent, rom.Filename), false, "", nil
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

func (JaguarSD) exportPath(rom ROM) (string, bool, string, error) {
	return rom.Filename, false, "", nil
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

func (SD2SNES) exportPath(rom ROM) (string, bool, string, error) {
	return rom.Filename, false, "", nil
}

func (SD2SNES) ignorePath(relpath string) bool {
	switch relpath {
	case "sd2snes": // Ignore the system directory entirely
		return true
	}
	return false
}
