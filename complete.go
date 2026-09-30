package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

// completionOut, if non-nil, is the real terminal that a Tab writes an
// ambiguous completion's candidate list to. This has to be the real fd,
// not term.Terminal's own Write: printing a listing here works entirely
// by moving the real cursor with raw escapes and never touches the
// current prompt/line, which term.Terminal's Write can't do (its erase
// logic assumes it owns everything on screen and always redraws the
// prompt/line right after whatever it prints). Wired to the raw-mode
// REPL's real output in runRawREPL; left nil in tests and in the
// scanner REPL, where there's no cursor to move.
var completionOut io.Writer

// completionPrompt is the prompt string currently shown by the raw-mode
// REPL (runRawREPL keeps it in sync with each t.SetPrompt(JSPrompt())
// call), used with line and pos to compute the cursor's current column
// so printCandidatesBelow can return to it after printing a listing.
var completionPrompt string

// lastListingLines is how many lines the previous candidate listing
// printed, so a following Tab that's still ambiguous can erase exactly
// that many rows instead of stacking a new listing underneath. Reset to
// 0 whenever a line is submitted (runRawREPL), since rows printed while
// editing a previous line are no longer where this tracks them.
var lastListingLines int

// eraseLinesBelow deletes the n terminal rows directly below the
// cursor's current row using ANSI's Delete Line (DL) sequence, which
// shifts whatever is further below up to fill the gap rather than just
// blanking them — otherwise the erased rows would stay behind as a
// permanent empty gap instead of disappearing. It leaves the cursor
// back on its starting row.
func eraseLinesBelow(out io.Writer, n int) {
	if n <= 0 {
		return
	}
	fmt.Fprintf(out, "\x1b[1B\x1b[%dM\x1b[1A", n) //nolint:errcheck
}

// clearListing erases a completion listing still on screen below the
// prompt, if any. Called at the top of every completeLine invocation —
// so typing past a listing, or a later Tab landing on a different,
// non-listing outcome (a unique match or a prefix extension), cleans it
// up — and from enterFilter just before Enter/LF reaches
// term.Terminal's decoder, since submitting the line isn't otherwise
// routed through completeLine at all and would otherwise run the
// command right on top of a listing nothing ever cleared.
func clearListing() {
	if lastListingLines > 0 && completionOut != nil {
		eraseLinesBelow(completionOut, lastListingLines)
		lastListingLines = 0
	}
}

// printCandidatesBelow prints matches, arranged into columns, on fresh
// rows directly below the current prompt/line, and returns the cursor
// to exactly where it started (same row and column). It assumes any
// previous listing has already been cleared (completeLine calls
// clearListing before ever reaching here). Everything here is relative
// cursor movement against the real terminal: no move depends on the
// terminal having scrolled or not, and none of it touches the
// prompt/line's own row, so the prompt never visibly shifts around the
// way it would if this went through term.Terminal's Write (which always
// erases and redraws the current line around whatever it's given).
//
// line and pos are the full line and cursor position completeLine was
// called with — used with completionPrompt to compute the cursor's
// current column (assuming, like the rest of completion, that the
// prompt+line fits on one terminal row without wrapping).
func printCandidatesBelow(out io.Writer, matches []string, line string, pos int) {
	listing := candidateList(matches)
	rows := strings.Count(listing, "\n")

	var b strings.Builder
	b.WriteString("\r\n")
	b.WriteString(strings.ReplaceAll(listing, "\n", "\r\n"))
	fmt.Fprintf(&b, "\x1b[%dA", rows+1)
	if col := utf8.RuneCountInString(completionPrompt) + utf8.RuneCountInString(line[:pos]); col > 0 {
		fmt.Fprintf(&b, "\x1b[%dC", col)
	}
	io.WriteString(out, b.String()) //nolint:errcheck

	lastListingLines = rows
}

// word describes the token the cursor sits inside, as found by
// wordAtCursor.
type word struct {
	Start     int    // byte offset in line where the word begins
	Text      string // partial text, quotes/backslashes stripped like tokenize does
	Quote     byte   // 0, '\'', or '"' if the cursor is inside an unterminated quote
	IsCommand bool   // true if this word is in command position (first word, or right after a pipe)
}

// wordAtCursor scans line[:pos] using the same quoting/escaping rules as
// tokenize (token.go) to find the word the cursor is in the middle of, or
// just after. Unlike tokenize it never errors on an unclosed quote —
// that's the normal state while a quoted argument is still being typed —
// and it only looks at text up to pos, since completion replaces the
// prefix under the cursor, not anything typed after it.
func wordAtCursor(line string, pos int) word {
	if pos < 0 {
		pos = 0
	}
	if pos > len(line) {
		pos = len(line)
	}
	sub := line[:pos]

	var cur []byte
	inQuote := false
	quoteChar := byte(0)
	wordStart := -1
	expectingCommand := true

	emitWord := func() {
		if wordStart != -1 {
			expectingCommand = false
		}
		cur = cur[:0]
		wordStart = -1
	}
	emitPipe := func() {
		cur = cur[:0]
		wordStart = -1
		expectingCommand = true
	}

	for i := 0; i < len(sub); i++ {
		ch := sub[i]
		switch {
		case !inQuote && ch == '\\':
			if wordStart == -1 {
				wordStart = i
			}
			if i+1 < len(sub) {
				i++
				cur = append(cur, sub[i])
			}
		case !inQuote && (ch == '\'' || ch == '"'):
			if wordStart == -1 {
				wordStart = i
			}
			inQuote = true
			quoteChar = ch
		case inQuote && quoteChar == '"' && ch == '\\' && i+1 < len(sub) &&
			(sub[i+1] == '"' || sub[i+1] == '$' || sub[i+1] == '\\'):
			i++
			cur = append(cur, sub[i])
		case inQuote && ch == quoteChar:
			inQuote = false
		case !inQuote && (ch == ' ' || ch == '\t'):
			emitWord()
		case !inQuote && ch == '|':
			if i+1 < len(sub) && sub[i+1] == '&' {
				i++
			}
			emitPipe()
		default:
			if wordStart == -1 {
				wordStart = i
			}
			cur = append(cur, ch)
		}
	}

	if wordStart == -1 {
		wordStart = pos
	}
	if !inQuote {
		quoteChar = 0
	}

	return word{
		Start:     wordStart,
		Text:      string(cur),
		Quote:     quoteChar,
		IsCommand: expectingCommand,
	}
}

// fileCandidates lists directory entries under filepath.Split(partial)'s
// directory part whose name starts with its base part, in the same
// dir+name shape as partial itself (so the result can be dropped straight
// back into the command line). Directories get a trailing separator so a
// following Tab can complete straight into them. Matches whose name
// starts with "." are excluded unless the typed base part also starts
// with ".", mirroring the dotfile rule in expandGlobs (token.go).
func fileCandidates(partial string) []string {
	dirPart, basePart := filepath.Split(partial)
	readDir := dirPart
	if readDir == "" {
		readDir = "."
	}

	entries, err := os.ReadDir(readDir)
	if err != nil {
		return nil
	}

	includeDot := strings.HasPrefix(basePart, ".")

	var matches []string
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, basePart) {
			continue
		}
		if !includeDot && strings.HasPrefix(name, ".") {
			continue
		}

		full := dirPart + name
		if isDirEntry(readDir, e) {
			full += string(filepath.Separator)
		}
		matches = append(matches, full)
	}

	sort.Strings(matches)
	return matches
}

// isDirEntry reports whether e names a directory, following a symlink if
// e is one (os.DirEntry.IsDir does not resolve symlinks).
func isDirEntry(dir string, e os.DirEntry) bool {
	if e.IsDir() {
		return true
	}
	if e.Type()&os.ModeSymlink == 0 {
		return false
	}
	info, err := os.Stat(filepath.Join(dir, e.Name()))
	return err == nil && info.IsDir()
}

// dirOnlyCommands lists commands whose arguments should only complete to
// directories (files are filtered out of the candidate list). Edit this
// map to add more.
var dirOnlyCommands = map[string]bool{
	"cd": true,
}

// fileOnlyCommands lists commands whose arguments should only complete to
// plain files (directories are filtered out of the candidate list). Edit
// this map to add more.
var fileOnlyCommands = map[string]bool{}

// commandForWord returns the command name w belongs to: the first word of
// the current pipeline segment (the tokens since the last unquoted "|" or
// "|&", or since the start of the line). Returns "" if w is itself in
// command position, i.e. there is no command yet to look up.
func commandForWord(line string, w word) string {
	tokens, err := tokenize(line[:w.Start])
	if err != nil {
		return ""
	}

	segStart := 0
	for i, t := range tokens {
		if t.Kind == TokenPipe || t.Kind == TokenMergePipe {
			segStart = i + 1
		}
	}
	if segStart >= len(tokens) {
		return ""
	}
	return tokens[segStart].Value
}

// filterEntries keeps only the fileCandidates results that are
// directories (keepDirs true) or that are not (keepDirs false).
func filterEntries(matches []string, keepDirs bool) []string {
	var out []string
	for _, m := range matches {
		if strings.HasSuffix(m, string(filepath.Separator)) == keepDirs {
			out = append(out, m)
		}
	}
	return out
}

// completionWidth is the terminal's current column count, kept in sync
// with the raw-mode REPL's term.Terminal size in runRawREPL (initial
// size and SIGWINCH updates alike), for sizing candidateList's columns.
// 0 (its zero value, and its value in tests and the scanner REPL, where
// there's no real terminal to measure) means "unknown," and candidateList
// falls back to one entry per line.
var completionWidth int

// candidateList formats matches for display below the prompt, arranged
// into as many columns as fit completionWidth (see columnate), and
// terminated by a trailing newline so a following prompt redraw starts
// on its own line.
func candidateList(matches []string) string {
	return columnate(matches, completionWidth)
}

// columnate arranges entries into columns sized to fit width, filling
// down each column before moving to the next (matching ls -C's layout),
// and returns them as complete rows terminated by "\n". Each column is
// as wide as its widest entry, with 2 spaces between columns. If width
// is unknown (<= 0), or even a single column of entries is wider than
// width, it falls back to one entry per line.
func columnate(entries []string, width int) string {
	if len(entries) == 0 {
		return ""
	}

	const spacing = 2
	n := len(entries)

	// Fallback: one entry per line.
	rows, cols, colWidths := n, 1, columnWidths(entries, 1, n)

	if width > 0 {
		for r := 1; r <= n; r++ {
			c := (n + r - 1) / r // columns needed for this many rows
			w := columnWidths(entries, c, r)
			total := (c - 1) * spacing
			for _, cw := range w {
				total += cw
			}
			if total <= width {
				rows, cols, colWidths = r, c, w
				break
			}
		}
	}

	var b strings.Builder
	for r := range rows {
		for c := range cols {
			i := c*rows + r
			if i >= n {
				break
			}
			if c > 0 {
				b.WriteString("  ")
			}
			entry := entries[i]
			b.WriteString(entry)
			if c < cols-1 && i+rows < n {
				b.WriteString(strings.Repeat(" ", colWidths[c]-utf8.RuneCountInString(entry)))
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// columnWidths returns, for a down-then-across layout of entries into
// cols columns of up to rows entries each, the display width of each
// column's widest entry.
func columnWidths(entries []string, cols, rows int) []int {
	w := make([]int, cols)
	for i, e := range entries {
		c := i / rows
		if l := utf8.RuneCountInString(e); l > w[c] {
			w[c] = l
		}
	}
	return w
}

// commonPrefix returns the longest byte-string prefix shared by every
// element of matches, trimmed back to a full rune if it would otherwise
// split one in the middle.
func commonPrefix(matches []string) string {
	if len(matches) == 0 {
		return ""
	}

	prefix := matches[0]
	for _, m := range matches[1:] {
		n := 0
		for n < len(prefix) && n < len(m) && prefix[n] == m[n] {
			n++
		}
		prefix = prefix[:n]
		if prefix == "" {
			return ""
		}
	}

	for len(prefix) > 0 && !utf8.ValidString(prefix) {
		prefix = prefix[:len(prefix)-1]
	}
	return prefix
}

// shellEscape backslash-escapes the characters tokenize (token.go) treats
// specially outside of quotes, so inserting text back into an unquoted
// word round-trips through tokenize unchanged.
func shellEscape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case ' ', '\t', '\\', '\'', '"', '|', '&':
			b.WriteByte('\\')
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

// spliceCompletion replaces the word w (which spans [w.Start:pos] in
// line) with text, re-quoting or re-escaping it to match how the word was
// originally typed, and appends trailing (a trailing space for a
// finished, unique completion, or "" to leave the word open for another
// Tab — e.g. after completing into a directory, or into an ambiguous
// prefix). It returns the values completeLine's AutoCompleteCallback
// signature expects.
func spliceCompletion(line string, pos int, w word, text string, trailing string) (string, int, bool) {
	var b strings.Builder
	b.WriteString(line[:w.Start])

	if w.Quote != 0 {
		b.WriteByte(w.Quote)
		b.WriteString(text)
		if trailing != "" {
			b.WriteByte(w.Quote)
		}
	} else {
		b.WriteString(shellEscape(text))
	}
	b.WriteString(trailing)

	newPos := b.Len()
	b.WriteString(line[pos:])

	return b.String(), newPos, true
}

// completeLine is the AutoCompleteCallback hook for the raw-mode REPL's
// term.Terminal. It fires for any key not already bound by x/term —
// which includes every plain printable character typed, not just Tab
// ('\t'), so that clearListing runs (and cleans up a stale listing) as
// soon as the user keeps typing past one, not only on another Tab. See
// plans/unlikely/possible-readline.md for the hook point and
// plans/tab-completion.md for the completion design.
//
// For now every position is completed as a filesystem path, including
// command position — no builtin/alias/$PATH lookup yet.
//
// Returning ok=false leaves the line untouched and lets x/term handle the
// key normally (a no-op for Tab, since it isn't otherwise bound).
func completeLine(line string, pos int, key rune) (string, int, bool) {
	clearListing()

	if key != '\t' {
		return "", 0, false
	}

	w := wordAtCursor(line, pos)
	matches := fileCandidates(w.Text)

	switch cmd := commandForWord(line, w); {
	case dirOnlyCommands[cmd]:
		matches = filterEntries(matches, true)
	case fileOnlyCommands[cmd]:
		matches = filterEntries(matches, false)
	}

	if len(matches) == 0 {
		return "", 0, false
	}

	if len(matches) == 1 {
		trailing := " "
		if strings.HasSuffix(matches[0], string(filepath.Separator)) {
			trailing = ""
		}
		return spliceCompletion(line, pos, w, matches[0], trailing)
	}

	prefix := commonPrefix(matches)
	if prefix == "" || prefix == w.Text {
		// Ambiguous with nothing new to add: list the candidates instead
		// of silently doing nothing.
		if completionOut != nil {
			printCandidatesBelow(completionOut, matches, line, pos)
		}
		return "", 0, false
	}

	return spliceCompletion(line, pos, w, prefix, "")
}

// enterFilter wraps a reader and, just before a raw '\r' or '\n' byte
// (Enter) reaches term.Terminal's decoder, calls clearListing. Enter is
// handled directly inside term.Terminal's key-handling switch, never
// through completeLine/AutoCompleteCallback, so without this nothing
// would clear a completion listing still on screen before the
// submitted line runs and prints output right on top of it.
//
// Like ctrlCFilter, this relies on term.Terminal.readLine unlocking its
// mutex around the Read call this wraps, which makes calling
// clearListing (and its write through completionOut) here safe rather
// than reentrant.
type enterFilter struct {
	io.Reader
}

func (f *enterFilter) Read(p []byte) (int, error) {
	n, err := f.Reader.Read(p)
	for _, b := range p[:n] {
		if b == '\r' || b == '\n' {
			clearListing()
			break
		}
	}
	return n, err
}
