package rombo

import (
	"archive/zip"
	"context"
	"errors"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"sync"

	"github.com/gabriel-vasile/mimetype"
	"github.com/uwedeportivo/torrentzip"
)

type ioCounter struct {
	bytesRx uint64
	bytesTx uint64
}

func (r *Rombo) findFiles(ctx context.Context, dir string) (<-chan string, <-chan error, error) {
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
			if info.Name()[0] == '.' || (r.layout != nil && r.layout.ignorePath(relpath)) {
				if info.Name()[0] != '.' {
					r.logger.Printf("Skipping \"%s\"\n", file)
				}
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

func (r *Rombo) mergeFiles(ctx context.Context, in ...<-chan string) (<-chan string, <-chan error, error) {
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

func (r *Rombo) mimeSplitter(ctx context.Context, in <-chan string) (<-chan string, <-chan string, <-chan error, error) {
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
			case "zip", "xlsx": // One zip so far has been misidentified as a .xlsx
				select {
				case zip <- file:
				case <-ctx.Done():
					return
				}
			case "7z": // Some archives have zip extension but are actually 7zip
				// TODO
				r.logger.Printf("Ignoring \"%s\" as we can't read it\n", file)
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

func (r *Rombo) cleanFile(ctx context.Context, dir, file, sha string, size uint64, roms []ROM) error {
	matched := false
	for _, rom := range roms {
		relpath, _, _, err := r.layout.exportPath(rom)
		if err != nil {
			return err
		}

		fullpath := filepath.Join(dir, relpath)

		if fullpath == file {
			matched = true
			break
		}
	}

	if !matched {
		r.logger.Printf("No matches for \"%s\", deleting\n", file)

		if r.destructive {
			if err := os.Remove(file); err != nil {
				return err
			}
		}
	}

	return nil
}

func (r *Rombo) exportFile(ctx context.Context, dir, file, sha string, size uint64, roms []ROM) error {
	for _, rom := range roms {
		relpath, zipped, name, err := r.layout.exportPath(rom)
		if err != nil {
			return err
		}

		fullpath := filepath.Join(dir, relpath)

		if zipped {
			ok, rcrc, rsize, err := fileExistsInZip(fullpath, name)
			if err != nil && !os.IsNotExist(err) {
				return err
			}

			if os.IsNotExist(err) || !ok || rcrc != rom.CRC || rsize != size {
				r.logger.Printf("Archiving \"%s\" to \"%s\" as \"%s\"\n", file, fullpath, name)
				if r.destructive {
					f, err := os.Open(file)
					if err != nil {
						return err
					}

					if err := createOrUpdateZip(fullpath, name, f); err != nil {
						f.Close()
						return err
					}

					f.Close()
				}
			}
		} else {
			rsha, rsize, err := sha1Sum(fullpath)
			if err != nil && !os.IsNotExist(err) {
				return err
			}

			if os.IsNotExist(err) || rsha != sha || rsize != size {
				r.logger.Printf("Copying \"%s\" to \"%s\"\n", file, fullpath)
				if r.destructive {
					if err := copyFile(file, fullpath); err != nil {
						return err
					}
				}
			}
		}

		if err := r.datafile.seenROM(rom); err != nil {
			return err
		}
	}

	return nil
}

func (r *Rombo) verifyFile(ctx context.Context, dir, file, sha string, size uint64, roms []ROM) error {
	for _, rom := range roms {
		if err := r.datafile.seenROM(rom); err != nil {
			return err
		}
	}

	return nil
}

func (r *Rombo) fileWorker(ctx context.Context, dir string, f func(context.Context, string, string, string, uint64, []ROM) error, in <-chan string) (<-chan error, error) {
	errc := make(chan error, 1)
	go func() {
		defer close(errc)
		for file := range in {
			sha, size, err := sha1Sum(file)
			if err != nil {
				errc <- err
				return
			}

			roms, _, err := r.datafile.findROMBySHA1(size, sha)
			if err != nil {
				errc <- err
				return
			}

			r.logger.Printf("Working on file \"%s\" with SHA1 %s\n", file, sha)

			if err := f(ctx, dir, file, sha, size, roms); err != nil {
				errc <- err
				return
			}
		}
	}()
	return errc, nil
}

func (r *Rombo) cleanZip(ctx context.Context, dir, file string) error {
	reader, err := zip.OpenReader(file)
	if err != nil {
		return err
	}
	defer reader.Close()

	evictees := make([]string, 0, len(reader.File))

File:
	for _, f := range reader.File {
		roms, _, err := r.datafile.findROMByCRC(f.UncompressedSize64, zipCRC(f))
		if err != nil {
			return err
		}

		for _, rom := range roms {
			relpath, _, name, err := r.layout.exportPath(rom)
			if err != nil {
				return err
			}

			fullpath := filepath.Join(dir, relpath)

			if fullpath == file && name == f.Name {
				continue File
			}
		}

		evictees = append(evictees, f.Name)
	}

	reader.Close()

	switch len(evictees) {
	case len(reader.File): // XXX Might not work if there are directories
		r.logger.Printf("Deleting \"%s\"\n", file)
		if r.destructive {
			return os.Remove(file)
		}
		return nil
	case 0:
		// Nothing to delete so check for torrentzip correctness
		sha, _, err := sha1Sum(file)
		if err != nil {
			return err
		}
		tmpfile, nsha, err := recreateZip(file)
		if err != nil {
			return err
		}
		defer os.Remove(tmpfile)
		if sha != nsha {
			r.logger.Printf("Replacing \"%s\"\n", file)
			if r.destructive {
				return copyFile(tmpfile, file)
			}
		}
		return nil
	default:
		// Prune
	}

	var tmpfile *os.File
	var w *torrentzip.Writer

	if r.destructive {
		var err error

		tmpfile, err = ioutil.TempFile(filepath.Dir(file), "."+filepath.Base(file))
		if err != nil {
			return err
		}
		defer os.Remove(tmpfile.Name())

		w, err = torrentzip.NewWriter(tmpfile)
		if err != nil {
			return err
		}
	}

	reader, err = zip.OpenReader(file)
	if err != nil {
		return err
	}
	defer reader.Close()

	for _, f := range reader.File {
		if len(evictees) > 0 && evictees[0] == f.Name {
			r.logger.Printf("Removing \"%s\" from \"%s\"\n", f.Name, file)
			evictees = evictees[1:]
		} else if r.destructive {
			fr, err := f.Open()
			if err != nil {
				return err
			}

			fw, err := w.Create(f.Name)
			if err != nil {
				fr.Close()
				return err
			}

			_, err = io.Copy(fw, fr)
			if err != nil {
				fr.Close()
				return err
			}

			fr.Close()
		}
	}

	reader.Close()

	if r.destructive {
		if err := w.Close(); err != nil {
			return err
		}

		if err := tmpfile.Close(); err != nil {
			return err
		}

		if err := os.Rename(tmpfile.Name(), file); err != nil {
			return err
		}
	}

	return nil
}

func (r *Rombo) exportZip(ctx context.Context, dir, file string) error {
	reader, err := zip.OpenReader(file)
	if err != nil {
		return err
	}
	defer reader.Close()

	for _, f := range reader.File {
		roms, _, err := r.datafile.findROMByCRC(f.UncompressedSize64, zipCRC(f))
		if err != nil {
			return err
		}

		for _, rom := range roms {
			relpath, zipped, name, err := r.layout.exportPath(rom)
			if err != nil {
				return err
			}

			fullpath := filepath.Join(dir, relpath)

			if zipped {
				ok, rcrc, rsize, err := fileExistsInZip(fullpath, name)
				if err != nil && !os.IsNotExist(err) {
					return err
				}

				if os.IsNotExist(err) || !ok || rcrc != zipCRC(f) || rsize != f.UncompressedSize64 {
					r.logger.Printf("Extracting \"%s\" from \"%s\" and archiving to \"%s\" as \"%s\"\n", f.Name, file, fullpath, name)
					if r.destructive {
						fr, err := f.Open()
						if err != nil {
							return err
						}

						if err := createOrUpdateZip(fullpath, name, fr); err != nil {
							fr.Close()
							return err
						}

						fr.Close()
					}
				}
			} else {
				rsha, rlength, err := sha1Sum(fullpath)
				if err != nil && !os.IsNotExist(err) {
					return err
				}

				if os.IsNotExist(err) || rsha != rom.SHA1 || rlength != f.UncompressedSize64 {
					r.logger.Printf("Extracting \"%s\" from \"%s\" to \"%s\"\n", f.Name, file, fullpath)
					if r.destructive {
						fr, err := f.Open()
						if err != nil {
							return err
						}

						if err := writeFile(fr, fullpath); err != nil {
							return err
						}

						fr.Close()
					}
				}
			}

			if err := r.datafile.seenROM(rom); err != nil {
				return err
			}
		}
	}

	reader.Close()

	return nil
}

func (r *Rombo) verifyZip(ctx context.Context, dir, file string) error {
	reader, err := zip.OpenReader(file)
	if err != nil {
		return err
	}
	defer reader.Close()

	for _, f := range reader.File {
		roms, _, err := r.datafile.findROMByCRC(f.UncompressedSize64, zipCRC(f))
		if err != nil {
			return err
		}

		for _, rom := range roms {
			if err := r.datafile.seenROM(rom); err != nil {
				return err
			}
		}
	}

	reader.Close()

	return nil
}

func (r *Rombo) zipWorker(ctx context.Context, dir string, f func(context.Context, string, string) error, in <-chan string) (<-chan error, error) {
	errc := make(chan error, 1)
	go func() {
		defer close(errc)
		for file := range in {
			r.logger.Printf("Working on archive \"%s\"\n", file)
			if err := f(ctx, dir, file); err != nil {
				errc <- err
				return
			}
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

func (r *Rombo) Clean(dir string) error {
	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()

	var errcList []<-chan error

	findc, errc, err := r.findFiles(ctx, dir)
	if err != nil {
		return err
	}
	errcList = append(errcList, errc)

	filec, zipc, errc, err := r.mimeSplitter(ctx, findc)
	if err != nil {
		return err
	}
	errcList = append(errcList, errc)

	for i := 0; i < 10; i++ {
		errc, err := r.fileWorker(ctx, dir, r.cleanFile, filec)
		if err != nil {
			return err
		}
		errcList = append(errcList, errc)

		errc, err = r.zipWorker(ctx, dir, r.cleanZip, zipc)
		if err != nil {
			return err
		}
		errcList = append(errcList, errc)
	}

	return waitForPipeline(errcList...)
}

func (r *Rombo) Export(dir string, dirs []string) error {
	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()

	var filecList []<-chan string
	var errcList []<-chan error

	for _, dir := range dirs {
		filec, errc, err := r.findFiles(ctx, dir)
		if err != nil {
			return err
		}
		filecList = append(filecList, filec)
		errcList = append(errcList, errc)
	}

	mergec, errc, err := r.mergeFiles(ctx, filecList...)
	if err != nil {
		return err
	}
	errcList = append(errcList, errc)

	filec, zipc, errc, err := r.mimeSplitter(ctx, mergec)
	if err != nil {
		return err
	}
	errcList = append(errcList, errc)

	for i := 0; i < 10; i++ {
		errc, err := r.fileWorker(ctx, dir, r.exportFile, filec)
		if err != nil {
			return err
		}
		errcList = append(errcList, errc)

		errc, err = r.zipWorker(ctx, dir, r.exportZip, zipc)
		if err != nil {
			return err
		}
		errcList = append(errcList, errc)
	}

	return waitForPipeline(errcList...)
}

func (r *Rombo) Verify(dirs []string) error {
	ctx, cancelFunc := context.WithCancel(context.Background())
	defer cancelFunc()

	var filecList []<-chan string
	var errcList []<-chan error

	for _, dir := range dirs {
		filec, errc, err := r.findFiles(ctx, dir)
		if err != nil {
			return err
		}
		filecList = append(filecList, filec)
		errcList = append(errcList, errc)
	}

	mergec, errc, err := r.mergeFiles(ctx, filecList...)
	if err != nil {
		return err
	}
	errcList = append(errcList, errc)

	filec, zipc, errc, err := r.mimeSplitter(ctx, mergec)
	if err != nil {
		return err
	}
	errcList = append(errcList, errc)

	for i := 0; i < 10; i++ {
		errc, err := r.fileWorker(ctx, "", r.verifyFile, filec)
		if err != nil {
			return err
		}
		errcList = append(errcList, errc)

		errc, err = r.zipWorker(ctx, "", r.verifyZip, zipc)
		if err != nil {
			return err
		}
		errcList = append(errcList, errc)
	}

	return waitForPipeline(errcList...)
}
