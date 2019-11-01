package rombo

import (
	"archive/zip"
	"context"
	"crypto/sha1"
	"errors"
	"fmt"
	"hash/crc32"
	"io/ioutil"
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

			// Ignore any hidden files or directories, otherwise we end up fighting with things like Spotlight, etc.
			if info.Name()[0] == '.' {
				if info.IsDir() {
					return filepath.SkipDir
				} else {
					return nil
				}
			}

			// Work out the path relative to the base directory
			relpath, err := filepath.Rel(dir, file)
			if err != nil {
				return err
			}

			// Ignore any layout-specific files or directories
			if layout != nil && layout.ignorePath(relpath) {
				if info.Mode().IsDir() {
					return filepath.SkipDir
				} else {
					return nil
				}
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

func processFile(ctx context.Context, d *Datafile, clean bool, in <-chan string) (<-chan error, error) {
	errc := make(chan error, 1)
	go func() {
		defer close(errc)
		for file := range in {
			data, err := ioutil.ReadFile(file)
			if err != nil {
				errc <- err
				return
			}
			sha := fmt.Sprintf("%x", sha1.Sum(data))
			fmt.Println("file:", sha, file)

			ok, err := d.findROMBySHA1(uint64(len(data)), sha)
			if err != nil {
				errc <- err
				return
			}

			if ok {
				if err := d.deleteROMBySHA1(uint64(len(data)), sha); err != nil {
					errc <- err
					return
				}
			} else if clean {
				fmt.Println("deleting", file)
			}
		}
	}()
	return errc, nil
}

func processZip(ctx context.Context, d *Datafile, clean bool, in <-chan string) (<-chan error, error) {
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

				ok, err := d.findROMByCRC(f.UncompressedSize64, crc)
				if err != nil {
					errc <- err
					return
				}

				if ok {
					if err := d.deleteROMByCRC(f.UncompressedSize64, crc); err != nil {
						errc <- err
						return
					}
				} else if clean {
					fmt.Println("deleting", f.Name, "from", file)
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

func Pipeline(datafile *Datafile, dirs []string, clean bool, layout Layout) error {
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
		errc, err = processFile(ctx, datafile, clean, filec)
		if err != nil {
			return err
		}
		errcList = append(errcList, errc)

		errc, err = processZip(ctx, datafile, clean, zipc)
		if err != nil {
			return err
		}
		errcList = append(errcList, errc)
	}

	return waitForPipeline(errcList...)
}
