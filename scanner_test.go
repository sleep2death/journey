package journey

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestScannerInit(t *testing.T) {
	src := []byte(`
# This is a journey comment. Boom!

str0 = "abc string"
str1 = 'a raw line with new line
here...' # This is a comment at the end of the line.

float0 = 12.35
int0 = 1234

bool0 = true
bool1 = false
	`)

	// Initialize the scanner.
	var s Scanner

	fset := NewFileSet()
	file := fset.AddFile("", fset.Base(), len(src))

	s.Init(file, src, nil)

	for {
		pos, tok, lit := s.Scan()
		if tok == EOF {
			break
		}
		t.Logf("%s\t%s\t%q\n", fset.Position(pos), tok, lit)
	}

	assert.Equal(t, int32(-1), s.ch)
}
