package main

import "io"

const (
	byteCtrlC = 0x03
	byteCtrlU = 0x15 // erase from line start to cursor
	byteCtrlK = 0x0b // erase from cursor to line end
)

// ctrlCFilter rewrites a raw Ctrl+C byte (0x03) in the input stream into
// Ctrl+U followed by Ctrl+K before it ever reaches term.Terminal's key
// decoder — Ctrl+U erases everything before the cursor and leaves it at
// column 0, so the following Ctrl+K (erase to end of line) always clears
// the whole line regardless of where the cursor was.
//
// This has to happen here, on the raw bytes, rather than by handling
// Ctrl+C after the fact: term.Terminal.readLine's own Ctrl+C case returns
// "", io.EOF immediately, skipping the bookkeeping that advances its
// internal remainder buffer past the consumed byte. That leaves the same
// 0x03 sitting in the buffer, so the very next ReadLine call decodes it
// again — straight from the buffer, without reading anything new — and
// returns io.EOF again. There's no public API to clear that leftover
// state, so once a real Ctrl+C reaches term.Terminal's decoder even
// once, every later ReadLine call is permanently poisoned. Rewriting the
// byte before it gets there avoids that path entirely.
type ctrlCFilter struct {
	io.Reader
	pending []byte

	// echo, if non-nil, receives a "^C" notice for each Ctrl+C seen.
	// term.Terminal.readLine unlocks its mutex around the Read call this
	// wraps, so writing through echo (typically the same *term.Terminal)
	// here is safe rather than reentrant.
	echo io.Writer
}

func (f *ctrlCFilter) Read(p []byte) (int, error) {
	if len(f.pending) > 0 {
		n := copy(p, f.pending)
		f.pending = f.pending[n:]
		return n, nil
	}

	buf := make([]byte, len(p))
	n, err := f.Reader.Read(buf)
	if n == 0 {
		return 0, err
	}

	out := make([]byte, 0, n)
	for _, b := range buf[:n] {
		if b == byteCtrlC {
			if f.echo != nil {
				io.WriteString(f.echo, "^C\n") //nolint:errcheck
			}
			out = append(out, byteCtrlU, byteCtrlK)
		} else {
			out = append(out, b)
		}
	}

	m := copy(p, out)
	f.pending = out[m:]
	if len(f.pending) > 0 {
		// Hold err back until the expanded bytes are fully drained,
		// since a Reader shouldn't pair a non-nil err with more data
		// still to come.
		return m, nil
	}
	return m, err
}
