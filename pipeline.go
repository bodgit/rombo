package rombo

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"sync"

	"github.com/gabriel-vasile/mimetype"
)

func findFiles(ctx context.Context, dir string, layout Layout) (<-chan string, <-chan error, error) {
	out := make(chan string)
	errc := make(chan error, 1)
	go func() {
		defer close(out)
		defer close(errc)
		errc <- filepath.Walk(dir, func(file string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}

			// Work out the path relative to the base directory
			relpath, err := filepath.Rel(dir, file)
			if err != nil {
				return err
			}

			// Ignore any hidden files or directories, otherwise we end up fighting with things like Spotlight, etc.
			// Also ignore any layout-specific files or directories
			if info.Name()[0] == '.' || (layout != nil && layout.ignorePath(relpath)) {
				if info.Mode().IsDir() {
					return filepath.SkipDir
				}
				return nil
			}

			// Ignore anything that isn't a normal file
			if !info.Mode().IsRegular() {
				return nil
			}

			select {
			case out <- file:
			case <-ctx.Done():
				return errors.New("walk cancelled")
			}

			return nil
		})
	}()
	return out, errc, nil
}

func mergeFiles(ctx context.Context, in ...<-chan string) (<-chan string, <-chan error, error) {
	var wg sync.WaitGroup
	out := make(chan string)
	errc := make(chan error, 1)
	wg.Add(len(in))
	for _, c := range in {
		go func(c <-chan string) {
			defer wg.Done()
			for n := range c {
				select {
				case out <- n:
				case <-ctx.Done():
					return
				}
			}
		}(c)
	}
	go func() {
		wg.Wait()
		close(out)
		close(errc)
	}()
	return out, errc, nil
}

func mimeSplitter(ctx context.Context, in <-chan string) (<-chan string, <-chan string, <-chan error, error) {
	out := make(chan string)
	zip := make(chan string)
	errc := make(chan error, 1)
	go func() {
		defer close(out)
		defer close(zip)
		defer close(errc)
		for file := range in {
			_, extension, err := mimetype.DetectFile(file)
			if err != nil {
				errc <- err
				return
			}
			switch extension {
			case "zip":
				select {
				case zip <- file:
				case <-ctx.Done():
					return
				}
			default:
				select {
				case out <- file:
				case <-ctx.Done():
					return
				}
			}
		}
	}()
	return out, zip, errc, nil
}

func processFile(ctx context.Context, d *Datafile, target *string, layout Layout, in <-chan string) (<-chan error, error) {
	errc := make(chan error, 1)
	go func() {
		defer close(errc)
		for file := range in {
			sha, length, err := sha1Sum(file)
			if err != nil {
				errc <- err
				return
			}

			roms, ok, err := d.findROMBySHA1(length, sha)
			if err != nil {
				errc <- err
				return
			}

			if !ok {
				// Delete if within target directory
				if target != nil {
					if _, err := filepath.Rel(*target, file); err != nil {
						continue
					}
					fmt.Println("would delete invalid file", file)
				}
				continue
			}

			matched := false

			for _, rom := range roms {
				if target != nil {
					relpath, _, _, err := layout.exportPath(rom.Game, rom.Filename)
					if err != nil {
						errc <- err
						return
					}

					fullpath := filepath.Join(*target, relpath)

					if fullpath == file {
						// File is in the correct location, do nothing but mark it as seen
						matched = true
					} else {
						_, err := os.Stat(fullpath)
						if err != nil {
							// Copy file
							fmt.Println("would copy", file, "to", fullpath, "as it doesn't exist")
						} else {
							rsha, rlength, err := sha1Sum(fullpath)
							if err != nil {
								errc <- err
								return
							}

							if rsha != sha || rlength != length {
								// Copy the file
								fmt.Println("would overwrite", fullpath, "with", file, "as it doesn't match")
							}
						}
					}
				}

				// Mark the ROM as seen
				if err := d.deleteROM(rom); err != nil {
					errc <- err
					return
				}
			}

			if target != nil && !matched {
				// File was valid but is not in the right place
				fmt.Println("would delete valid file", file)
			}
		}
	}()
	return errc, nil
}

func processZip(ctx context.Context, d *Datafile, target *string, layout Layout, in <-chan string) (<-chan error, error) {
	errc := make(chan error, 1)
	go func() {
		defer close(errc)
		for file := range in {
			r, err := zip.OpenReader(file)
			if err != nil {
				errc <- err
				return
			}

			for _, f := range r.File {
				crc := fmt.Sprintf("%.*x", crc32.Size<<1, f.CRC32)
				fmt.Println("zip:", crc, file, f.Name)

				roms, ok, err := d.findROMByCRC(f.UncompressedSize64, crc)
				if err != nil {
					errc <- err
					return
				}

				if !ok {
					continue
				}

				for _, rom := range roms {
					if err := d.deleteROM(rom); err != nil {
						errc <- err
						return
					}
				}
			}

			r.Close()
		}
	}()
	return errc, nil
}

func waitForPipeline(errs ...<-chan error) error {
	errc := mergeErrors(errs...)
	for err := range errc {
		if err != nil {
			return err
		}
	}
	return nil
}

func mergeErrors(cs ...<-chan error) <-chan error {
	var wg sync.WaitGroup
	out := make(chan error, len(cs))
	wg.Add(len(cs))
	for _, c := range cs {
		go func(c <-chan error) {
			for n := range c {
				out <- n
			}
			wg.Done()
		}(c)
	}
	go func() {
		wg.Wait()
		close(out)
	}()
	return out
}

func Pipeline(datafile *Datafile, dirs []string, target *string, layout Layout) error {
	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()

	var filecList []<-chan string
	var errcList []<-chan error

	for _, dir := range dirs {
		filec, errc, err := findFiles(ctx, dir, layout)
		if err != nil {
			return err
		}
		filecList = append(filecList, filec)
		errcList = append(errcList, errc)
	}

	if target != nil {
		filec, errc, err := findFiles(ctx, *target, layout)
		if err != nil {
			return err
		}
		filecList = append(filecList, filec)
		errcList = append(errcList, errc)
	}

	mergec, errc, err := mergeFiles(ctx, filecList...)
	if err != nil {
		return err
	}
	errcList = append(errcList, errc)

	filec, zipc, errc, err := mimeSplitter(ctx, mergec)
	if err != nil {
		return err
	}
	errcList = append(errcList, errc)

	for i := 0; i < 10; i++ {
		errc, err = processFile(ctx, datafile, target, layout, filec)
		if err != nil {
			return err
		}
		errcList = append(errcList, errc)

		errc, err = processZip(ctx, datafile, target, layout, zipc)
		if err != nil {
			return err
		}
		errcList = append(errcList, errc)
	}

	return waitForPipeline(errcList...)
}
