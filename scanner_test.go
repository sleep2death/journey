package journey

import "testing"

func TestScannerInit(t *testing.T) {
	src := []byte("hello, world!")

	// Initialize the scanner.
	var s Scanner

	fset := NewFileSet()
	file := fset.AddFile("", fset.Base(), len(src))

	s.Init(file, src, nil)
}
