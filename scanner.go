package journey

import (
	"fmt"
	"path/filepath"
	"unicode"
	"unicode/utf8"
)

// An ErrorHandler may be provided to Scanner.Init. If a syntax error is
// encountered and a handler was installed, the handler is called with a
// position and an error message. The position points to the beginning of
// the offending token.
//
type ErrorHandler func(pos Position, msg string)

// A Scanner holds the scanner's internal state while processing
// a given text. It can be allocated as part of another data
// structure but must be initialized via Init before use.
//
type Scanner struct {
	// immutable state
	file *File        // source file handle
	dir  string       // directory portion of file.Name()
	src  []byte       // source
	err  ErrorHandler // error reporting; or nil

	// scanning state
	ch         rune // current character
	offset     int  // character offset
	rdOffset   int  // reading offset (position after current character)
	lineOffset int  // current line offset
	insertSemi bool // insert a semicolon before next newline

	// public state - ok to modify
	ErrorCount int // number of errors encountered
}

const bom = 0xFEFF // byte order mark, only permitted as very first character

// Read the next Unicode char into s.ch.
// s.ch < 0 means end-of-file.
//
func (s *Scanner) next() {
	if s.rdOffset < len(s.src) {
		s.offset = s.rdOffset
		if s.ch == '\n' {
			s.lineOffset = s.offset
			s.file.AddLine(s.offset)
		}
		r, w := rune(s.src[s.rdOffset]), 1
		switch {
		case r == 0:
			s.error(s.offset, "illegal character NUL")
		case r >= utf8.RuneSelf:
			// not ASCII
			r, w = utf8.DecodeRune(s.src[s.rdOffset:])
			if r == utf8.RuneError && w == 1 {
				s.error(s.offset, "illegal UTF-8 encoding")
			} else if r == bom && s.offset > 0 {
				s.error(s.offset, "illegal byte order mark")
			}
		}
		s.rdOffset += w
		s.ch = r
	} else {
		s.offset = len(s.src)
		if s.ch == '\n' {
			s.lineOffset = s.offset
			s.file.AddLine(s.offset)
		}
		s.ch = -1 // eof
	}
}

// peek returns the byte following the most recently read character without
// advancing the scanner. If the scanner is at EOF, peek returns 0.
func (s *Scanner) peek() byte {
	if s.rdOffset < len(s.src) {
		return s.src[s.rdOffset]
	}
	return 0
}

// Init prepares the scanner s to tokenize the text src by setting the
// scanner at the beginning of src. The scanner uses the file set file
// for position information and it adds line information for each line.
// It is ok to re-use the same file when re-scanning the same file as
// line information which is already present is ignored. Init causes a
// panic if the file size does not match the src size.
//
// Calls to Scan will invoke the error handler err if they encounter a
// syntax error and err is not nil. Also, for each error encountered,
// the Scanner field ErrorCount is incremented by one. The mode parameter
// determines how comments are handled.
//
// Note that Init may call err if there is an error in the first character
// of the file.
//
func (s *Scanner) Init(file *File, src []byte, err ErrorHandler) {
	initToken()
	// Explicitly initialize all fields since a scanner may be reused.
	if file.Size() != len(src) {
		panic(fmt.Sprintf("file size (%d) does not match src len (%d)", file.Size(), len(src)))
	}
	s.file = file
	s.dir, _ = filepath.Split(file.Name())
	s.src = src
	s.err = err

	s.ch = ' '
	s.offset = 0
	s.rdOffset = 0
	s.lineOffset = 0
	s.insertSemi = false
	s.ErrorCount = 0

	s.next()
	if s.ch == bom {
		s.next() // ignore BOM at file beginning
	}
}

func (s *Scanner) error(offs int, msg string) {
	if s.err != nil {
		s.err(s.file.Position(s.file.Pos(offs)), msg)
	}
	s.ErrorCount++
}

func (s *Scanner) errorf(offs int, format string, args ...interface{}) {
	s.error(offs, fmt.Sprintf(format, args...))
}

func (s *Scanner) skipWhitespace() {
	for s.ch == ' ' || s.ch == '\t' || s.ch == '\n' && !s.insertSemi || s.ch == '\r' {
		s.next()
	}
}

// Scan scans the next token and returns the token position, the token,
// and its literal string if applicable. The source end is indicated by
// token.EOF.
//
func (s *Scanner) Scan() (pos Pos, tok Token, lit string) {
	s.skipWhitespace()

	// current token start
	pos = s.file.Pos(s.offset)

	switch ch := s.ch; {
	case isLetter(ch):
		lit = s.scanIdentifier()
		if len(lit) > 1 {
			// keywords are longer than one letter - avoid lookup otherwise
			tok = Lookup(lit)
		} else {
			tok = IDENT
		}
	case isDecimal(ch) || ch == '.' && isDecimal(rune(s.peek())):
		tok, lit = s.scanNumber()
	default:
		s.next() // always make progress
		switch ch {
		case -1:
			if s.insertSemi {
				s.insertSemi = false // EOF consumed
				return pos, SEMICOLON, "\n"
			}
			tok = EOF
		case '\n':
			// we only reach here if s.insertSemi was
			// set in the first place and exited early
			// from s.skipWhitespace()
			return pos, SEMICOLON, "\n"
		case '"':
			tok = STRING
			lit = s.scanString()
		case '\'':
			tok = STRING
			lit = s.scanRawString()
		case '#':
			tok = COMMENT
			lit = s.scanComment()
		case '=':
			tok = ASSIGN
			lit = "="
		case '[':
			tok = LBRACK
			lit = "["
		case ']':
			tok = RBRACK
			lit = "]"
		}

	}
	return
}

// scanEscape parsj
// escaped quote. In case of a syntax error, it stops at the offending
// character (without consuming it) and returns false. Otherwise
// it returns true.
func (s *Scanner) scanEscape(quote rune) bool {
	offs := s.offset

	var n int
	var base, max uint32
	switch s.ch {
	case 'a', 'b', 'f', 'n', 'r', 't', 'v', '\\', quote:
		s.next()
		return true
	case '0', '1', '2', '3', '4', '5', '6', '7':
		n, base, max = 3, 8, 255
	case 'x':
		s.next()
		n, base, max = 2, 16, 255
	case 'u':
		s.next()
		n, base, max = 4, 16, unicode.MaxRune
	case 'U':
		s.next()
		n, base, max = 8, 16, unicode.MaxRune
	default:
		msg := "unknown escape sequence"
		if s.ch < 0 {
			msg = "escape sequence not terminated"
		}
		s.error(offs, msg)
		return false
	}

	var x uint32
	for n > 0 {
		d := uint32(digitVal(s.ch))
		if d >= base {
			msg := fmt.Sprintf("illegal character %#U in escape sequence", s.ch)
			if s.ch < 0 {
				msg = "escape sequence not terminated"
			}
			s.error(s.offset, msg)
			return false
		}
		x = x*base + d
		s.next()
		n--
	}

	if x > max || 0xD800 <= x && x < 0xE000 {
		s.error(offs, "escape sequence is invalid Unicode code point")
		return false
	}

	return true
}

func (s *Scanner) scanRune() string {
	// '\'' opening already consumed
	offs := s.offset - 1

	valid := true
	n := 0
	for {
		ch := s.ch
		if ch == '\n' || ch < 0 {
			// only report error if we don't have one already
			if valid {
				s.error(offs, "rune literal not terminated")
				valid = false
			}
			break
		}
		s.next()
		if ch == '\'' {
			break
		}
		n++
		if ch == '\\' {
			if !s.scanEscape('\'') {
				valid = false
			}
			// continue to read to closing quote
		}
	}

	if valid && n != 1 {
		s.error(offs, "illegal rune literal")
	}

	return string(s.src[offs:s.offset])
}

func (s *Scanner) scanString() string {
	// '"' opening already consumed
	offs := s.offset - 1

	for {
		ch := s.ch
		if ch == '\n' || ch < 0 {
			s.error(offs, "string literal not terminated")
			break
		}
		s.next()
		if ch == '"' {
			break
		}
		if ch == '\\' {
			s.scanEscape('"')
		}
	}

	return string(s.src[offs:s.offset])
}

func (s *Scanner) scanComment() string {
	// '#' opening already consumed
	offs := s.offset - 1

	for {
		ch := s.ch
		if ch == '\n' || ch < 0 {
			break
		}
		s.next()
		if ch == '\\' {
			s.scanEscape('"')
		}
	}

	return string(s.src[offs:s.offset])
}

func stripCR(b []byte, comment bool) []byte {
	c := make([]byte, len(b))
	i := 0
	for j, ch := range b {
		// In a /*-style comment, don't strip \r from *\r/ (incl.
		// sequences of \r from *\r\r...\r/) since the resulting
		// */ would terminate the comment too early unless the \r
		// is immediately following the opening /* in which case
		// it's ok because /*/ is not closed yet (issue #11151).
		if ch != '\r' || comment && i > len("/*") && c[i-1] == '*' && j+1 < len(b) && b[j+1] == '/' {
			c[i] = ch
			i++
		}
	}
	return c[:i]
}

func (s *Scanner) scanRawString() string {
	// ' opening already consumed
	offs := s.offset - 1

	hasCR := false
	for {
		ch := s.ch
		if ch < 0 {
			s.error(offs, "raw string literal not terminated")
			break
		}
		s.next()
		if ch == '\'' {
			break
		}
		if ch == '\r' {
			hasCR = true
		}
	}

	lit := s.src[offs:s.offset]
	if hasCR {
		lit = stripCR(lit, false)
	}

	return string(lit)
}

func isLetter(ch rune) bool {
	return 'a' <= lower(ch) && lower(ch) <= 'z' || ch == '_' || ch >= utf8.RuneSelf && unicode.IsLetter(ch)
}

func isDigit(ch rune) bool {
	return isDecimal(ch) || ch >= utf8.RuneSelf && unicode.IsDigit(ch)
}

func (s *Scanner) scanIdentifier() string {
	offs := s.offset
	for isLetter(s.ch) || isDigit(s.ch) {
		s.next()
	}
	return string(s.src[offs:s.offset])
}

func digitVal(ch rune) int {
	switch {
	case '0' <= ch && ch <= '9':
		return int(ch - '0')
	case 'a' <= lower(ch) && lower(ch) <= 'f':
		return int(lower(ch) - 'a' + 10)
	}
	return 16 // larger than any legal digit val
}

func lower(ch rune) rune     { return ('a' - 'A') | ch } // returns lower-case ch iff ch is ASCII letter
func isDecimal(ch rune) bool { return '0' <= ch && ch <= '9' }
func isHex(ch rune) bool     { return '0' <= ch && ch <= '9' || 'a' <= lower(ch) && lower(ch) <= 'f' }

// digits accepts the sequence { digit | '_' }.
// If base <= 10, digits accepts any decimal digit but records
// the offset (relative to the source start) of a digit >= base
// in *invalid, if *invalid < 0.
// digits returns a bitset describing whether the sequence contained
// digits (bit 0 is set), or separators '_' (bit 1 is set).
func (s *Scanner) digits(base int, invalid *int) (digsep int) {
	if base <= 10 {
		max := rune('0' + base)
		for isDecimal(s.ch) || s.ch == '_' {
			ds := 1
			if s.ch == '_' {
				ds = 2
			} else if s.ch >= max && *invalid < 0 {
				*invalid = int(s.offset) // record invalid rune offset
			}
			digsep |= ds
			s.next()
		}
	} else {
		for isHex(s.ch) || s.ch == '_' {
			ds := 1
			if s.ch == '_' {
				ds = 2
			}
			digsep |= ds
			s.next()
		}
	}
	return
}

func (s *Scanner) scanNumber() (Token, string) {
	offs := s.offset
	tok := ILLEGAL

	base := 10        // number base
	prefix := rune(0) // one of 0 (decimal), '0' (0-octal), 'x', 'o', or 'b'
	digsep := 0       // bit 0: digit present, bit 1: '_' present
	invalid := -1     // index of invalid digit in literal, or < 0

	// integer part
	if s.ch != '.' {
		tok = INT
		if s.ch == '0' {
			s.next()
			switch lower(s.ch) {
			case 'x':
				s.next()
				base, prefix = 16, 'x'
			case 'o':
				s.next()
				base, prefix = 8, 'o'
			case 'b':
				s.next()
				base, prefix = 2, 'b'
			default:
				base, prefix = 8, '0'
				digsep = 1 // leading 0
			}
		}
		digsep |= s.digits(base, &invalid)
	}

	// fractional part
	if s.ch == '.' {
		tok = FLOAT
		if prefix == 'o' || prefix == 'b' {
			s.error(s.offset, "invalid radix point in "+litname(prefix))
		}
		s.next()
		digsep |= s.digits(base, &invalid)
	}

	if digsep&1 == 0 {
		s.error(s.offset, litname(prefix)+" has no digits")
	}

	// exponent
	if e := lower(s.ch); e == 'e' || e == 'p' {
		switch {
		case e == 'e' && prefix != 0 && prefix != '0':
			s.errorf(s.offset, "%q exponent requires decimal mantissa", s.ch)
		case e == 'p' && prefix != 'x':
			s.errorf(s.offset, "%q exponent requires hexadecimal mantissa", s.ch)
		}
		s.next()
		tok = FLOAT
		if s.ch == '+' || s.ch == '-' {
			s.next()
		}
		ds := s.digits(10, nil)
		digsep |= ds
		if ds&1 == 0 {
			s.error(s.offset, "exponent has no digits")
		}
	} else if prefix == 'x' && tok == FLOAT {
		s.error(s.offset, "hexadecimal mantissa requires a 'p' exponent")
	}

	// suffix 'i'
	if s.ch == 'i' {
		tok = IMAG
		s.next()
	}

	lit := string(s.src[offs:s.offset])
	if tok == INT && invalid >= 0 {
		s.errorf(invalid, "invalid digit %q in %s", lit[invalid-offs], litname(prefix))
	}
	if digsep&2 != 0 {
		if i := invalidSep(lit); i >= 0 {
			s.error(offs+i, "'_' must separate successive digits")
		}
	}

	return tok, lit
}

func litname(prefix rune) string {
	switch prefix {
	case 'x':
		return "hexadecimal literal"
	case 'o', '0':
		return "octal literal"
	case 'b':
		return "binary literal"
	}
	return "decimal literal"
}

// invalidSep returns the index of the first invalid separator in x, or -1.
func invalidSep(x string) int {
	x1 := ' ' // prefix char, we only care if it's 'x'
	d := '.'  // digit, one of '_', '0' (a digit), or '.' (anything else)
	i := 0

	// a prefix counts as a digit
	if len(x) >= 2 && x[0] == '0' {
		x1 = lower(rune(x[1]))
		if x1 == 'x' || x1 == 'o' || x1 == 'b' {
			d = '0'
			i = 2
		}
	}

	// mantissa and exponent
	for ; i < len(x); i++ {
		p := d // previous digit
		d = rune(x[i])
		switch {
		case d == '_':
			if p != '0' {
				return i
			}
		case isDecimal(d) || x1 == 'x' && isHex(d):
			d = '0'
		default:
			if p == '_' {
				return i - 1
			}
			d = '.'
		}
	}
	if d == '_' {
		return len(x) - 1
	}

	return -1
}
