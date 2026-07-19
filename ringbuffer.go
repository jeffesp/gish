package main

import (
	"os"
	"strconv"
	"sync"
)

/*
- Capped circular byte buffer (default 1MB, configurable via `GISH_JOB_BUFFER_SIZE`)
- Implements `io.Writer`
- `Bytes() []byte` returns contents in order
- Uses `sync.RWMutex` (concurrent writes from PTY reader, reads from `joblog`)
*/

const JobBufferSize = 1 * 1024 * 1024

type RingBuffer struct {
	buf      []byte
	size     int
	mu       sync.RWMutex
	writeIdx int
	length   int
}

func NewRingBuffer(size int) *RingBuffer {
	if size <= 0 {
		size = JobBufferSize
		if v := os.Getenv("GISH_JOB_BUFFER_SIZE"); v != "" {
			if n, err := strconv.Atoi(v); err == nil && n > 0 {
				size = n
			}
		}
	}
	return &RingBuffer{buf: make([]byte, size), size: size}
}

func (rb *RingBuffer) Write(p []byte) (n int, err error) {

	rb.mu.Lock()
	defer rb.mu.Unlock()

	n = len(p)

	// clamp to size of buffer only
	if len(p) > rb.size {
		p = p[len(p)-rb.size:]
	}

	written := copy(rb.buf[rb.writeIdx:], p)
	if written < len(p) {
		written = written + copy(rb.buf, p[written:])
	}
	rb.writeIdx = (rb.writeIdx + written) % rb.size
	rb.length += len(p)
	if rb.length > rb.size {
		rb.length = rb.size
	}

	return n, nil
}

func (rb *RingBuffer) Bytes() []byte {
	rb.mu.RLock()
	defer rb.mu.RUnlock()

	if rb.length < len(rb.buf) {
		return append([]byte(nil), rb.buf[:rb.length]...)
	}

	out := make([]byte, 0, rb.length)
	out = append(out, rb.buf[rb.writeIdx:]...)
	out = append(out, rb.buf[:rb.writeIdx]...)
	return out
}
