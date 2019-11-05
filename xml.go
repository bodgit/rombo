package rombo

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/jbowtie/gokogiri/xml"
)

type ROM struct {
	Game     string
	Filename string
}

type Datafile struct {
	input  *xml.XmlDocument
	output *xml.XmlDocument
	mutex  sync.Mutex
}

func loadXMLReader(r io.Reader) (*xml.XmlDocument, error) {
	b, err := ioutil.ReadAll(r)
	if err != nil {
		return nil, err
	}

	return xml.Parse(b, xml.DefaultEncodingBytes, nil, xml.XML_PARSE_NOBLANKS, xml.DefaultEncodingBytes)
}

func loadXMLFile(file string) (*xml.XmlDocument, error) {
	f, err := os.Open(file)
	if err != nil {
		return nil, err
	}

	return loadXMLReader(f)
}

func xmlParse(b []byte) (*xml.XmlDocument, error) {
	return xml.Parse(b, xml.DefaultEncodingBytes, nil, xml.XML_PARSE_NOBLANKS, xml.DefaultEncodingBytes)
}

func NewDatafile(b []byte) (*Datafile, error) {
	d := Datafile{}

	document, err := xmlParse(b)
	if err != nil {
		return nil, err
	}
	d.input = document

	// In the absence of a way to clone a document...
	document, err = xmlParse(b)
	if err != nil {
		return nil, err
	}
	d.output = document

	return &d, nil
}

func (d *Datafile) Marshal() []byte {
	b, _ := d.output.ToXml(nil, nil)

	// Phantom trailing null bytes can appear for some reason
	return bytes.TrimRight(b, "\x00")
}

func (d *Datafile) Merge(b []byte) error {
	input, err := xmlParse(b)
	if err != nil {
		return err
	}

Game:
	for game := input.Root().FirstChild(); game != nil; game = game.NextSibling() {
		switch game.Name() {
		case "header":
			continue Game
		case "game":
			if err := d.output.Root().LastChild().InsertAfter(game.Duplicate(-1)); err != nil {
				return err
			}
		default:
			return fmt.Errorf("unknown element: %s", game.Name())
		}
	}

	return nil
}

func (d *Datafile) findROMByCRC(size uint64, crc string) ([]ROM, bool, error) {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	nodes, err := d.input.Search("/datafile/game/rom[@size='" + strconv.FormatUint(size, 10) + "' and (@crc='" + strings.ToLower(crc) + "' or @crc='" + strings.ToUpper(crc) + "')]")
	if err != nil {
		return nil, false, err
	}

	if len(nodes) > 0 {
		roms := make([]ROM, 0, len(nodes))
		for _, node := range nodes {
			roms = append(roms, ROM{Game: node.Parent().Attr("name"), Filename: node.Attr("name")})
		}
		return roms, true, nil
	}

	return nil, false, nil
}

func (d *Datafile) deleteROMByCRC(size uint64, crc string) error {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	nodes, err := d.output.Search("/datafile/game/rom[@size='" + strconv.FormatUint(size, 10) + "' and (@crc='" + strings.ToLower(crc) + "' or @crc='" + strings.ToUpper(crc) + "')]")
	if err != nil {
		return err
	}

	for _, rom := range nodes {
		game := rom.Parent()
		rom.Unlink()

		roms, err := game.Search("rom")
		if err != nil {
			return err
		}

		if len(roms) == 0 {
			game.Unlink()
		}
	}

	return nil
}

func (d *Datafile) findROMBySHA1(size uint64, sha string) ([]ROM, bool, error) {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	nodes, err := d.input.Search("/datafile/game/rom[@size='" + strconv.FormatUint(size, 10) + "' and (@sha1='" + strings.ToLower(sha) + "' or @sha1='" + strings.ToUpper(sha) + "')]")
	if err != nil {
		return nil, false, err
	}

	if len(nodes) > 0 {
		roms := make([]ROM, 0, len(nodes))
		for _, node := range nodes {
			roms = append(roms, ROM{Game: node.Parent().Attr("name"), Filename: node.Attr("name")})
		}
		return roms, true, nil
	}

	return nil, false, nil
}

func (d *Datafile) deleteROM(rom ROM) error {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	nodes, err := d.output.Search("/datafile/game[@name=\"" + rom.Game + "\"]/rom[@name=\"" + rom.Filename + "\"]")
	if err != nil {
		return err
	}

	if len(nodes) > 1 {
		return errors.New("more than one matched ROM")
	}

	for _, rom := range nodes {
		game := rom.Parent()
		rom.Unlink()

		roms, err := game.Search("rom")
		if err != nil {
			return err
		}

		if len(roms) == 0 {
			game.Unlink()
		}
	}

	return nil
}

func (d *Datafile) deleteROMBySHA1(size uint64, sha string) error {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	nodes, err := d.output.Search("/datafile/game/rom[@size='" + strconv.FormatUint(size, 10) + "' and (@sha1='" + strings.ToLower(sha) + "' or @sha1='" + strings.ToUpper(sha) + "')]")
	if err != nil {
		return err
	}

	for _, rom := range nodes {
		game := rom.Parent()
		rom.Unlink()

		roms, err := game.Search("rom")
		if err != nil {
			return err
		}

		if len(roms) == 0 {
			game.Unlink()
		}
	}

	return nil
}

func (d *Datafile) Games() (int, error) {
	d.mutex.Lock()
	defer d.mutex.Unlock()

	nodes, err := d.output.Search("/datafile/game")
	if err != nil {
		return 0, err
	}

	return len(nodes), nil
}
