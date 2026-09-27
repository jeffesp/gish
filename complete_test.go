package main

import "testing"

func TestWordAtCursor(t *testing.T) {
	cases := []struct {
		line      string
		pos       int
		wantStart int
		wantText  string
		wantQuote byte
		wantCmd   bool
	}{
		{"", 0, 0, "", 0, true},
		{"ec", 2, 0, "ec", 0, true},
		{"echo hel", 8, 5, "hel", 0, false},
		{"echo ", 5, 5, "", 0, false},
		{"cat file.out | gr", 17, 15, "gr", 0, true},
		{"cat file.out |& gr", 18, 16, "gr", 0, true},
		{"echo 'hello wor", 15, 5, "hello wor", '\'', false},
		{`echo "hello wor`, 15, 5, "hello wor", '"', false},
		{`echo a\ b`, 9, 5, "a b", 0, false},
		{"echo one two", 8, 5, "one", 0, false},
		// cursor in the middle of a word only sees the prefix before it
		{"echo hello", 7, 5, "he", 0, false},
		// cursor right after a completed pipe, before typing anything
		{"cat file.out |", 14, 14, "", 0, true},
	}

	for _, c := range cases {
		got := wordAtCursor(c.line, c.pos)
		if got.Start != c.wantStart || got.Text != c.wantText || got.Quote != c.wantQuote || got.IsCommand != c.wantCmd {
			t.Errorf("wordAtCursor(%q, %d) = %+v, want {Start:%d Text:%q Quote:%q IsCommand:%v}",
				c.line, c.pos, got, c.wantStart, c.wantText, c.wantQuote, c.wantCmd)
		}
	}
}
