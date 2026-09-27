package main

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

// completeLine is the AutoCompleteCallback hook for the raw-mode REPL's
// term.Terminal. It fires for any key not already bound by x/term; Tab
// ('\t') is the only key it cares about for now. See
// plans/unlikely/possible-readline.md for the hook point and
// plans/tab-completion.md for the completion design.
//
// Returning ok=false leaves the line untouched and lets x/term handle the
// key normally (a no-op for Tab, since it isn't otherwise bound).
func completeLine(line string, pos int, key rune) (string, int, bool) {
	if key != '\t' {
		return "", 0, false
	}

	_ = wordAtCursor(line, pos)

	// TODO: candidate generation (builtins, aliases, $PATH, filesystem)
	// and common-prefix/listing logic.
	return "", 0, false
}
