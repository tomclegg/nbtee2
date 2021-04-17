package nbtee2

import (
	"context"
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
	ch       chan []byte
	todo     []byte
	buf      []byte
	w        *Tee
	lowwater int
	ctx      context.Context
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

// NewReaderContext returns a new io.ReadCloser that reads a copy of
// everything sent to Write(), dropping all buffered writes in order
// to catch up whenever it falls behind by `highwater` writes.
//
// If there is no data ready when Read() is called, Read blocks until
// `lowwater` writes have arrived.
//
// It is safe to call the reader's Close() method while a Read() is in
// progress, and after calling Close(), it is safe (but unnecessary)
// to call Read() until EOF.
func (w *Tee) NewReaderContext(ctx context.Context, lowwater, highwater int) io.ReadCloser {
	w.mtx.Lock()
	defer w.mtx.Unlock()
	ch := make(chan []byte, highwater)
	r := &reader{ch: ch, w: w, lowwater: lowwater, ctx: ctx}
	if w.readers == nil {
		w.readers = make(map[*reader]bool, 1)
	}
	w.readers[r] = true
	return r
}

// NewReader calls NewReaderContext with context.Background().
func (w *Tee) NewReader(lowwater, highwater int) io.ReadCloser {
	return w.NewReaderContext(context.Background(), lowwater, highwater)
}

func (r *reader) WriteTo(w io.Writer) (n int64, err error) {
	defer r.Close()
	for err == nil {
		err = r.fillTodo()
		if len(r.todo) == 0 {
			continue
		}
		var nn int
		nn, err = w.Write(r.todo)
		n += int64(nn)
		r.todo = r.todo[nn:]
	}
	return
}

// Read implements io.Reader.
func (r *reader) Read(p []byte) (int, error) {
	err := r.fillTodo()
	n := copy(p, r.todo)
	r.todo = r.todo[n:]
	return n, err
}

// Fill r.todo with the next incoming buf. If an incoming buf isn't
// ready, block until r.lowwater buffers have been read into r.todo or
// r.ctx is cancelled.
func (r *reader) fillTodo() (err error) {
	if len(r.todo) > 0 {
		return nil
	}
	lowwater := 1
	if r.lowwater > 1 && len(r.ch) == 0 {
		lowwater = r.lowwater
	}
	r.buf = r.buf[:0]
	for i := 0; i < lowwater && err == nil; i++ {
		select {
		case buf, ok := <-r.ch:
			if !ok {
				err = io.EOF
			} else {
				r.buf = append(r.buf, buf...)
			}
		case <-r.ctx.Done():
			err = r.ctx.Err()
		}
	}
	if cap(r.ch) > 2 && len(r.ch) >= cap(r.ch)-1 {
		for len(r.ch) > 0 {
			<-r.ch
		}
	}
	r.todo = r.buf
	return
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
