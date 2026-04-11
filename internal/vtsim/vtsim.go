// Package vtsim is a tiny terminal simulator sufficient for largo's tests.
//
// It consumes the bytes largo writes (text, newlines, carriage returns,
// cursor-up, and erase-line) and produces a final visible grid. It does
// not implement SGR (colors) — those are stripped. That's enough to
// assert on what the user actually sees after all erase-and-replace
// animations complete.
package vtsim

import (
	"strconv"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

// Screen is an unbounded grid. Rows grow as needed; each row is a slice
// of runes with width-aware placement.
type Screen struct {
	Width int
	rows  [][]rune
	row   int
	col   int
}

func New(width int) *Screen {
	return &Screen{Width: width, rows: [][]rune{{}}, row: 0, col: 0}
}

// Feed processes a byte stream.
func (s *Screen) Feed(p []byte) {
	i := 0
	for i < len(p) {
		b := p[i]

		switch {
		case b == '\n':
			s.row++
			s.col = 0
			s.ensureRow(s.row)
			i++
		case b == '\r':
			s.col = 0
			i++
		case b == '\x1b' && i+1 < len(p) && p[i+1] == '[':
			// CSI sequence: \x1b[ <params> <final>
			j := i + 2
			start := j
			for j < len(p) && !isFinal(p[j]) {
				j++
			}
			if j >= len(p) {
				i = j
				continue
			}
			params := string(p[start:j])
			final := p[j]
			j++
			s.handleCSI(params, final)
			i = j
		case b == '\t':
			// Advance to next multiple of 8.
			advance := 8 - (s.col % 8)
			for k := 0; k < advance; k++ {
				s.putRune(' ')
			}
			i++
		case b < 0x20:
			// Other control chars: ignore.
			i++
		default:
			r, size := utf8.DecodeRune(p[i:])
			s.putRune(r)
			i += size
		}
	}
}

func (s *Screen) handleCSI(params string, final byte) {
	n := 1
	if params != "" {
		if v, err := strconv.Atoi(params); err == nil {
			n = v
		}
	}
	switch final {
	case 'A': // cursor up
		s.row -= n
		if s.row < 0 {
			s.row = 0
		}
	case 'B': // cursor down
		s.row += n
		s.ensureRow(s.row)
	case 'C': // cursor forward
		s.col += n
	case 'D': // cursor back
		s.col -= n
		if s.col < 0 {
			s.col = 0
		}
	case 'K': // erase in line
		// 0 or default: erase from cursor to end of line
		// 1: erase from start of line to cursor
		// 2: erase entire line
		s.ensureRow(s.row)
		row := s.rows[s.row]
		switch n {
		case 0, 1 /* treated as default here, fine for our tests */ :
			// fallthrough handled below; we split 0 and 2.
		}
		// Simplified: treat 0 as "erase to end", 2 as "erase all".
		if params == "" || params == "0" {
			if s.col < len(row) {
				s.rows[s.row] = row[:s.col]
			}
		} else if params == "2" {
			s.rows[s.row] = nil
		}
	case 'm':
		// SGR — ignore (we don't render colors).
	case 'H', 'f':
		// Cursor position — not used by largo.
	}
}

func (s *Screen) ensureRow(row int) {
	for len(s.rows) <= row {
		s.rows = append(s.rows, nil)
	}
}

func (s *Screen) putRune(r rune) {
	w := runewidth.RuneWidth(r)
	if w == 0 {
		return
	}
	if s.col+w > s.Width {
		s.row++
		s.col = 0
		s.ensureRow(s.row)
	}
	s.ensureRow(s.row)
	row := s.rows[s.row]
	// Pad with spaces up to s.col.
	for len(row) < s.col {
		row = append(row, ' ')
	}
	// Overwrite starting at s.col.
	if s.col < len(row) {
		row[s.col] = r
		for k := 1; k < w; k++ {
			if s.col+k < len(row) {
				row[s.col+k] = 0
			} else {
				row = append(row, 0)
			}
		}
	} else {
		row = append(row, r)
		for k := 1; k < w; k++ {
			row = append(row, 0)
		}
	}
	s.rows[s.row] = row
	s.col += w
}

// Lines returns the visible text lines with trailing whitespace trimmed
// on each line, and trailing empty rows removed.
func (s *Screen) Lines() []string {
	out := make([]string, len(s.rows))
	for i, r := range s.rows {
		// Skip zero runes (second cell of wide chars).
		buf := make([]rune, 0, len(r))
		for _, c := range r {
			if c == 0 {
				continue
			}
			buf = append(buf, c)
		}
		out[i] = trimRight(string(buf))
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

func trimRight(s string) string {
	end := len(s)
	for end > 0 && (s[end-1] == ' ' || s[end-1] == '\t') {
		end--
	}
	return s[:end]
}

func isFinal(b byte) bool {
	return b >= 0x40 && b <= 0x7E
}
