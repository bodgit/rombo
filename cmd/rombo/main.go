package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/bodgit/rombo"
	"github.com/urfave/cli"
)

var stringToLayout = map[string]rombo.Layout{
	"simple":  rombo.SimpleCompressed{},
	"jaguar":  rombo.JaguarGD{},
	"megasd":  rombo.MegaSD{},
	"sd2snes": rombo.SD2SNES{},
}

type EnumValue struct {
	Enum     []string
	Default  string
	selected string
}

func (e *EnumValue) Set(value string) error {
	for _, enum := range e.Enum {
		if enum == value {
			e.selected = value
			return nil
		}
	}

	return fmt.Errorf("allowed values are %s", strings.Join(e.Enum, ", "))
}

func (e *EnumValue) String() string {
	if e.selected == "" {
		return e.Default
	}
	return e.selected
}

func init() {
	cli.VersionFlag = cli.BoolFlag{
		Name:  "version, V",
		Usage: "print the version",
	}
}

func export(c *cli.Context) error {
	if c.NArg() < 2 {
		cli.ShowCommandHelpAndExit(c, c.Command.FullName(), 1)
	}

	logger := log.New(ioutil.Discard, "", 0)
	if c.Bool("verbose") {
		logger.SetOutput(os.Stderr)
	}

	b, err := ioutil.ReadAll(os.Stdin)
	if err != nil {
		return cli.NewExitError(err, 1)
	}

	datafile, err := rombo.NewDatafile(b)
	if err != nil {
		return cli.NewExitError(err, 1)
	}

	layout := stringToLayout[c.Generic("layout").(*EnumValue).String()]

	r, err := rombo.New(datafile, logger, !c.Bool("dry-run"), layout)
	if err != nil {
		return cli.NewExitError(err, 1)
	}

	start := time.Now()
	if err := r.Export(c.Args().First(), c.Args().Tail()); err != nil {
		return cli.NewExitError(err, 1)
	}
	elapsed := time.Since(start)

	logger.Println("Export finished in", elapsed)

	start = time.Now()
	if err := r.Clean(c.Args().First()); err != nil {
		return cli.NewExitError(err, 1)
	}
	elapsed = time.Since(start)

	logger.Println("Clean finished in", elapsed)

	games, err := datafile.GamesRemaining()
	if err != nil {
		return cli.NewExitError(err, 1)
	}

	if games > 0 {
		output := datafile.Marshal()

		_, err = os.Stdout.Write(output)
		if err != nil {
			return cli.NewExitError(err, 1)
		}

		cli.NewExitError("", 2)
	}

	return nil
}

func merge(c *cli.Context) error {
	if c.NArg() < 1 {
		cli.ShowCommandHelpAndExit(c, c.Command.FullName(), 1)
	}

	b, err := ioutil.ReadFile(c.Args().First())
	if err != nil {
		return cli.NewExitError(err, 1)
	}

	datafile, err := rombo.NewDatafile(b)
	if err != nil {
		return cli.NewExitError(err, 1)
	}

	for _, file := range c.Args().Tail() {
		b, err := ioutil.ReadFile(file)
		if err != nil {
			cli.NewExitError(err, 1)
		}

		if err := datafile.Merge(b); err != nil {
			cli.NewExitError(err, 1)
		}
	}

	_, err = os.Stdout.Write(datafile.Marshal())
	if err != nil {
		log.Fatal(err)
	}

	return nil
}

func verify(c *cli.Context) error {
	if c.NArg() < 1 {
		cli.ShowCommandHelpAndExit(c, c.Command.FullName(), 1)
	}

	logger := log.New(ioutil.Discard, "", 0)
	if c.Bool("verbose") {
		logger.SetOutput(os.Stderr)
	}

	b, err := ioutil.ReadAll(os.Stdin)
	if err != nil {
		return cli.NewExitError(err, 1)
	}

	datafile, err := rombo.NewDatafile(b)
	if err != nil {
		return cli.NewExitError(err, 1)
	}

	r, err := rombo.New(datafile, logger, false, nil)
	if err != nil {
		return cli.NewExitError(err, 1)
	}

	start := time.Now()
	if err := r.Verify(c.Args()); err != nil {
		return cli.NewExitError(err, 1)
	}
	elapsed := time.Since(start)

	logger.Println("Verify finished in", elapsed)

	games, err := datafile.GamesRemaining()
	if err != nil {
		return cli.NewExitError(err, 1)
	}

	if games > 0 {
		output := datafile.Marshal()

		_, err = os.Stdout.Write(output)
		if err != nil {
			return cli.NewExitError(err, 1)
		}

		cli.NewExitError("", 2)
	}

	return nil
}

func main() {
	app := cli.NewApp()

	app.Name = "rombo"
	app.Usage = "ROM management utility"
	app.Version = "1.0.0"

	layouts := make([]string, 0, len(stringToLayout))
	for k := range stringToLayout {
		layouts = append(layouts, k)
	}
	sort.Sort(sort.StringSlice(layouts))

	app.Commands = []cli.Command{
		{
			Name:        "export",
			Usage:       "Create or update a target directory using the ROMs found in one or more source directories",
			Description: "The XML dat file is read from the standard input and a partial XML dat file containing any missing ROM is written to standard output",
			ArgsUsage:   "TARGET SOURCE...",
			Flags: []cli.Flag{
				cli.BoolFlag{
					Name:  "dry-run, n",
					Usage: "don't actually do anything",
				},
				cli.GenericFlag{
					Name: "layout",
					Value: &EnumValue{
						Enum:    layouts,
						Default: "simple",
					},
					Usage: "organise the exported ROMs according to `LAYOUT`. (" + strings.Join(layouts, ", ") + ")",
				},
				cli.BoolFlag{
					Name:  "verbose, v",
					Usage: "increase verbosity",
				},
			},
			Action: export,
		},
		{
			Name:        "merge",
			Usage:       "Merge multiple XML dat files together",
			Description: "The merged XML file is written to standard output",
			ArgsUsage:   "FILE...",
			Flags: []cli.Flag{
				cli.StringFlag{
					Name:  "set-name",
					Usage: "Override the name",
				},
				cli.StringFlag{
					Name:  "set-description",
					Usage: "Override the description",
				},
				cli.StringFlag{
					Name:  "set-version",
					Usage: "Override the version",
				},
				cli.StringFlag{
					Name:  "set-author",
					Usage: "Override the author",
				},
			},
			Action: merge,
		},
		{
			Name:        "verify",
			Usage:       "Verify the contents of one or more directories against an XML dat file",
			Description: "The XML dat file is read from the standard input and a partial XML dat file containing any missing ROM is written to standard output",
			ArgsUsage:   "DIRECTORY...",
			Flags: []cli.Flag{
				cli.BoolFlag{
					Name:  "verbose, v",
					Usage: "increase verbosity",
				},
			},
			Action: verify,
		},
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}
