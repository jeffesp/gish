package main

import (
	"io"
	"strings"
	"testing"
)

// readAll drains r via repeated small reads (to exercise ctrlCFilter's
// pending-byte buffering, not just a single big Read call).
func readAll(t *testing.T, r io.Reader) []byte {
	t.Helper()
	var out []byte
	buf := make([]byte, 1)
	for {
		n, err := r.Read(buf)
		out = append(out, buf[:n]...)
		if err != nil {
			if err != io.EOF {
				t.Fatalf("Read: %v", err)
			}
			return out
		}
	}
}

func TestCtrlCFilter(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []byte
	}{
		{"plain text", "hello", []byte("hello")},
		{"ctrl-c alone", "\x03", []byte{byteCtrlU, byteCtrlK}},
		{"ctrl-c among other bytes", "ab\x03c", []byte{'a', 'b', byteCtrlU, byteCtrlK, 'c'}},
		{"ctrl-d is untouched", "\x04", []byte{0x04}},
	}

	for _, c := range cases {
		f := &ctrlCFilter{Reader: strings.NewReader(c.in)}
		got := readAll(t, f)
		if string(got) != string(c.want) {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
}

func TestCtrlCFilterEchoesOnCtrlC(t *testing.T) {
	var echo strings.Builder
	f := &ctrlCFilter{Reader: strings.NewReader("a\x03b"), echo: &echo}
	readAll(t, f)

	if echo.String() != "^C\n" {
		t.Errorf("echo = %q, want %q", echo.String(), "^C\n")
	}
}
