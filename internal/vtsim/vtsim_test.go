package vtsim

import (
	"strings"
	"testing"
)

func TestBasicText(t *testing.T) {
	s := New(80)
	s.Feed([]byte("hello\nworld\n"))
	lines := s.Lines()
	if len(lines) != 2 || lines[0] != "hello" || lines[1] != "world" {
		t.Fatalf("unexpected: %#v", lines)
	}
}

func TestEraseAndReplace(t *testing.T) {
	s := New(80)
	s.Feed([]byte("temp\r\x1b[2Kreal\n"))
	lines := s.Lines()
	if len(lines) != 1 || lines[0] != "real" {
		t.Fatalf("unexpected: %#v", lines)
	}
}

func TestUpAndClear(t *testing.T) {
	s := New(80)
	// Write 3 lines, go up 2, clear, write new
	s.Feed([]byte("one\ntwo\nthree\n"))
	s.Feed([]byte("\x1b[A\x1b[A\r\x1b[2Kreplaced\n"))
	lines := s.Lines()
	// After: row 0 "one", row 1 "replaced", row 2 "three" or overwritten?
	// The \x1b[A moves up from row 3 to row 2 then row 1 (index 1 = "two").
	// Then \r clears col, \x1b[2K clears row 1 entirely, then "replaced\n".
	// After the \n, cursor is at row 2 col 0.
	expected := []string{"one", "replaced", "three"}
	if !equal(lines, expected) {
		t.Fatalf("expected %#v, got %#v", expected, lines)
	}
}

func TestSoftWrap(t *testing.T) {
	s := New(10)
	s.Feed([]byte("1234567890abc"))
	lines := s.Lines()
	if len(lines) != 2 || lines[0] != "1234567890" || lines[1] != "abc" {
		t.Fatalf("unexpected: %#v", lines)
	}
}

func TestSGRStripped(t *testing.T) {
	s := New(80)
	s.Feed([]byte("\x1b[31mred\x1b[0m\n"))
	lines := s.Lines()
	if len(lines) != 1 || lines[0] != "red" {
		t.Fatalf("unexpected: %#v", lines)
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func Render(s *Screen) string {
	return strings.Join(s.Lines(), "\n")
}
