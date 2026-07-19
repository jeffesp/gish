package main

import (
	"bytes"
	"sync"
	"testing"
)

func TestRingBufferWrite(t *testing.T) {
	x := NewRingBuffer(10)

	count, err := x.Write(bytes.NewBufferString("abcdefg").Bytes())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 7 {
		t.Fatal("Did not write expected number of characters")
	}

}

// TODO: test default size of RingBuffer allocated

func TestRingBufferWriteWraps(t *testing.T) {
	x := NewRingBuffer(5)

	count, err := x.Write(bytes.NewBufferString("abcdefg").Bytes())

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if count != 7 {
		t.Fatal("Did not write expected number of characters")
	}

	if string(x.buf) != "cdefg" {
		t.Fatalf("Did not truncate characters in writing: %v", string(x.buf))
	}
}

func TestRingBufferMultipleWritesWrap(t *testing.T) {
	x := NewRingBuffer(5)

	x.Write(bytes.NewBufferString("ab").Bytes())
	x.Write(bytes.NewBufferString("cdefg").Bytes())

	if string(x.buf) != "fgcde" {
		t.Fatalf("Did not wrap characters in writing: %v", string(x.buf))
	}
}

func TestRingBufferMultipleWritesWrapMultipleTimes(t *testing.T) {
	x := NewRingBuffer(4)

	x.Write(bytes.NewBufferString("abcdefghijklmno").Bytes())

	if string(x.buf) != "lmno" {
		t.Fatalf("Did not wrap characters in writing: %v", string(x.buf))
	}
}

func TestRingBufferBytesCallReturnsData(t *testing.T) {
	x := NewRingBuffer(10)

	x.Write(bytes.NewBufferString("abcdefg").Bytes())

	if string(x.Bytes()) != "abcdefg" {

		t.Fatalf("Did not read characters back from buffer: %v", string(x.Bytes()))
	}
}

func TestRingBufferBytesCallUnwrapsData(t *testing.T) {
	x := NewRingBuffer(5)

	x.Write(bytes.NewBufferString("ab").Bytes())
	x.Write(bytes.NewBufferString("cdefg").Bytes())

	if string(x.Bytes()) != "cdefg" {
		t.Fatalf("Did not unwrap data in writing: %v", string(x.Bytes()))
	}

}

func TestRingBufferBytesOnEmptyBuffer(t *testing.T) {
	x := NewRingBuffer(5)

	if x.Bytes() != nil {
		t.Fatal("Read on empty RingBuffer did not return nil")
	}
}

func TestRingBufferBytesReturnsIdempotent(t *testing.T) {
	x := NewRingBuffer(5)
	x.Write(bytes.NewBufferString("cdefg").Bytes())

	_ = x.Bytes()
	if string(x.Bytes()) != "cdefg" {
		t.Fatal("Read on empty RingBuffer did not return nil")
	}
}

func TestRingBufferBytesIsACopy(t *testing.T) {
	x := NewRingBuffer(5)
	x.Write(bytes.NewBufferString("cdefg").Bytes())

	copied := x.Bytes()

	x.Write(bytes.NewBufferString("vwxyz").Bytes())
	if string(copied) != "cdefg" {
		t.Fatal("")
	}

}

func TestRingBufferConcurrentWrites(t *testing.T) {
	x := NewRingBuffer(64)

	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 50 {
				x.Write(bytes.NewBufferString("abcdefg").Bytes())
			}
		}()
	}
	wg.Wait()

	if x.length > x.size {
		t.Fatalf("length exceeded buffer size: %d > %d", x.length, x.size)
	}
	if x.writeIdx < 0 || x.writeIdx >= x.size {
		t.Fatalf("writeIdx out of bounds: %d", x.writeIdx)
	}
}

func TestRingBufferConcurrentReadsAndWrites(t *testing.T) {
	x := NewRingBuffer(64)

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 200 {
			x.Write(bytes.NewBufferString("abcdefg").Bytes())
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 200 {
			_ = x.Bytes()
		}
	}()

	wg.Wait()
}
