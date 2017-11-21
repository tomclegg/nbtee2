package nbtee2

import (
	"io"
	"sync"
)

// Tee is an asynchronous one-to-any pipe. New readers can be added at
// any time. When a reader isn't reading fast enough to keep up with
// writes, it misses some data in order to catch up.
//
// Each []byte sent to Write() is either received entirely or not at
// all by any given reader, assuming it keeps reading until EOF.
type Tee struct {
	readers map[*reader]bool
	mtx     sync.Mutex
}

type reader struct {
	ch   chan []byte
	todo []byte
	buf  []byte
	w    *Tee
}

// Write sends p to all readers that aren't overflowing. Write never
// blocks. The returned error is always nil.
func (w *Tee) Write(p []byte) (int, error) {
	buf := make([]byte, len(p))
	copy(buf, p)
	w.mtx.Lock()
	defer w.mtx.Unlock()
	for r := range w.readers {
		select {
		case r.ch <- buf:
		default:
		}
	}
	return len(p), nil
}

// Close causes all readers to reach EOF when they finish reading
// what's in their buffers.
func (w *Tee) Close() error {
	w.mtx.Lock()
	defer w.mtx.Unlock()
	for r := range w.readers {
		close(r.ch)
	}
	w.readers = nil
	return nil
}

// NewReader returns a new io.ReadCloser that reads a copy of
// everything sent to Write(), dropping all buffered writes in order
// to catch up whenever it falls behind by `highwater` writes.
//
// It is safe to call the reader's Close() method while a Read() is in
// progress, and after calling Close(), it is safe (but unnecessary)
// to call Read() until EOF.
func (w *Tee) NewReader(highwater int) io.ReadCloser {
	w.mtx.Lock()
	defer w.mtx.Unlock()
	ch := make(chan []byte, highwater)
	r := &reader{ch: ch, w: w}
	if w.readers == nil {
		w.readers = make(map[*reader]bool, 1)
	}
	w.readers[r] = true
	return r
}

func (r *reader) WriteTo(w io.Writer) (n int64, err error) {
	for {
		r.emptyIfFull()
		buf, ok := <-r.ch
		if !ok {
			err = io.EOF
			return
		}
		var nn int
		nn, err = w.Write(<-r.ch)
		n += int64(nn)
		if err != nil {
			return
		}
	}
}

// Read implements io.Reader.
func (r *reader) Read(p []byte) (int, error) {
	if len(r.todo) > 0 {
		n := copy(p, r.todo)
		r.todo = r.todo[n:]
		return n, nil
	}
	incoming, ok := <-r.ch
	if !ok {
		return 0, io.EOF
	}
	if cap(r.ch) > 1 && len(r.ch) >= cap(r.ch)-1 {
		for len(r.ch) > 0 {
			<-r.ch
		}
	}
	if len(incoming) <= len(p) {
		r.todo = nil
	} else {
		if len(incoming)-len(p) > cap(r.buf) {
			r.buf = make([]byte, 0, len(incoming)-len(p))
		}
		r.buf = r.buf[:copy(r.buf, incoming[len(p):])]
		r.todo = r.buf
	}
	return copy(p, incoming), nil
}

// Close releases resources. Readers should be closed after use.
func (r *reader) Close() error {
	r.w.mtx.Lock()
	defer r.w.mtx.Unlock()
	if r.w.readers[r] {
		close(r.ch)
		delete(r.w.readers, r)
	}
	return nil
}
