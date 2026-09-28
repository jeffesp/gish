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

// completionLister, if non-nil, receives the candidate list when Tab
// completion is ambiguous and can't extend the word any further — mirroring
// zsh's default of listing completions below the prompt instead of just
// beeping. Wired to the raw-mode REPL's term.Terminal in runRawREPL; left
// nil in tests and in the scanner REPL, where there's no cursor/prompt to
// redraw around a printed list.
var completionLister io.Writer

// completionRawOut, if non-nil, is the real terminal — bypassing
// completionLister's Write — that a repeated ambiguous Tab erases the
// previous listing through before printing the new one. completionLister
// (term.Terminal.Write) only knows how to erase its own current
// prompt/line row; it has no memory of a listing it printed on an earlier
// call, so removing that requires raw cursor movement against the real
// fd instead. Wired alongside completionLister in runRawREPL.
var completionRawOut io.Writer

// lastListingLines is how many lines the previous candidate listing
// printed, so a following Tab that's still ambiguous can erase exactly
// that many rows instead of stacking a new listing underneath. Reset to
// 0 whenever a line is submitted (runRawREPL), since rows printed while
// editing a previous line are no longer where this tracks them.
var lastListingLines int

// eraseLinesAbove deletes the n terminal rows directly above the
// cursor's current row using ANSI's Delete Line (DL) sequence, which
// shifts everything below those rows up to fill the gap rather than
// just blanking them — otherwise the erased rows would stay behind as a
// permanent empty gap instead of disappearing. Because content shifts
// up to fill the deleted rows, the cursor ends up at the same column it
// started at, on whatever now occupies its original row (i.e., wherever
// what used to be below the deleted block shifted up to).
func eraseLinesAbove(out io.Writer, n int) {
	if n <= 0 {
		return
	}
	fmt.Fprintf(out, "\x1b[%dA\x1b[%dM", n, n) //nolint:errcheck
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

// candidateList formats matches for display below the prompt, one per
// line, terminated by a trailing newline so a following prompt redraw
// starts on its own line.
func candidateList(matches []string) string {
	return strings.Join(matches, "\n") + "\n"
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
// term.Terminal. It fires for any key not already bound by x/term; Tab
// ('\t') is the only key it cares about for now. See
// plans/unlikely/possible-readline.md for the hook point and
// plans/tab-completion.md for the completion design.
//
// For now every position is completed as a filesystem path, including
// command position — no builtin/alias/$PATH lookup yet.
//
// Returning ok=false leaves the line untouched and lets x/term handle the
// key normally (a no-op for Tab, since it isn't otherwise bound).
func completeLine(line string, pos int, key rune) (string, int, bool) {
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
		if completionLister != nil {
			if completionRawOut != nil {
				eraseLinesAbove(completionRawOut, lastListingLines)
			}
			io.WriteString(completionLister, candidateList(matches)) //nolint:errcheck
			lastListingLines = len(matches)
		}
		return "", 0, false
	}

	return spliceCompletion(line, pos, w, prefix, "")
}
