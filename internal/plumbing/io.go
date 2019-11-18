package plumbing

import (
	"io"
	"sync/atomic"
)

type teeReaderAt struct {
	r io.ReaderAt
	w io.WriterAt
}

func TeeReaderAt(r io.ReaderAt, w io.WriterAt) io.ReaderAt {
	return &teeReaderAt{r, w}
}

func (t *teeReaderAt) ReadAt(p []byte, off int64) (n int, err error) {
	n, err = t.r.ReadAt(p, off)
	if n > 0 {
		if n, err := t.w.WriteAt(p[:n], off); err != nil {
			return n, err
		}
	}
	return
}

type WriteCounter struct {
	count uint64
}

func (wc *WriteCounter) write(p []byte) (int, error) {
	n := len(p)
	atomic.AddUint64(&wc.count, uint64(n))
	return n, nil
}

func (wc *WriteCounter) Write(p []byte) (int, error) {
	return wc.write(p)
}

func (wc *WriteCounter) WriteAt(p []byte, off int64) (int, error) {
	return wc.write(p)
}

func (wc *WriteCounter) Count() uint64 {
	return atomic.LoadUint64(&wc.count)
}
