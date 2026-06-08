package largo

import (
	"bytes"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/mattn/go-runewidth"
)

// dwsim is a minimal terminal simulator that models DEFERRED auto-wrap
// (xterm/VTE/kitty/alacritty behavior). It is intentionally separate
// from internal/vtsim, which models eager wrap — the same rule largo
// itself uses in echoRaw. Because both vtsim and largo are eager,
// vtsim-based tests cannot detect largo's wrap-counting drift against
// real terminals. dwsim is the differential reference.
//
// Deferred-wrap rule:
//   - Filling the last column of a row sets a "pending wrap" flag.
//     The cursor stays parked on that row at column W.
//   - The NEXT printable rune (only printables) consumes the pending
//     wrap: cursor advances to (row+1, 0) BEFORE the rune is placed.
//   - \r clears pending wrap and parks col=0 on the same row.
//   - \n clears pending wrap and advances to (row+1, 0) — but does
//     not double-advance.
//
// dwsim implements just enough CSI to follow largo's emitted erase
// sequence: \033[2K (erase line), \033[A (cursor up), and ignored SGR.
type dwsim struct {
	width   int
	rows    [][]rune
	row     int
	col     int
	pending bool // deferred wrap armed
}

func newDW(width int) *dwsim {
	return &dwsim{width: width, rows: [][]rune{{}}}
}

func (s *dwsim) ensureRow(r int) {
	for len(s.rows) <= r {
		s.rows = append(s.rows, nil)
	}
}

func (s *dwsim) put(r rune) {
	w := runewidth.RuneWidth(r)
	if w == 0 {
		return
	}
	if s.pending {
		s.row++
		s.col = 0
		s.pending = false
		s.ensureRow(s.row)
	}
	if s.col+w > s.width {
		// Wide char doesn't fit on current row; xterm wraps it down.
		s.row++
		s.col = 0
		s.ensureRow(s.row)
	}
	row := s.rows[s.row]
	for len(row) < s.col {
		row = append(row, ' ')
	}
	if s.col < len(row) {
		row[s.col] = r
	} else {
		row = append(row, r)
	}
	for k := 1; k < w; k++ {
		if s.col+k < len(row) {
			row[s.col+k] = 0
		} else {
			row = append(row, 0)
		}
	}
	s.rows[s.row] = row
	s.col += w
	if s.col == s.width {
		s.pending = true // park; do not advance yet
	}
}

func (s *dwsim) feed(p []byte) {
	i := 0
	for i < len(p) {
		b := p[i]
		switch {
		case b == '\n':
			s.pending = false
			s.row++
			s.col = 0
			s.ensureRow(s.row)
			i++
		case b == '\r':
			s.pending = false
			s.col = 0
			i++
		case b == '\x1b' && i+1 < len(p) && p[i+1] == '[':
			j := i + 2
			start := j
			for j < len(p) && !(p[j] >= 0x40 && p[j] <= 0x7e) {
				j++
			}
			if j >= len(p) {
				i = j
				continue
			}
			params := string(p[start:j])
			final := p[j]
			j++
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
				s.pending = false
			case 'K': // erase in line
				s.ensureRow(s.row)
				if params == "" || params == "0" {
					if s.col < len(s.rows[s.row]) {
						s.rows[s.row] = s.rows[s.row][:s.col]
					}
				} else if params == "2" {
					s.rows[s.row] = nil
				}
			case 'm': // SGR, ignore
			}
			i = j
		case b < 0x20:
			i++
		default:
			r, size := utf8.DecodeRune(p[i:])
			s.put(r)
			i += size
		}
	}
}

func (s *dwsim) lines() []string {
	out := make([]string, len(s.rows))
	for i, r := range s.rows {
		buf := make([]rune, 0, len(r))
		for _, c := range r {
			if c == 0 {
				continue
			}
			buf = append(buf, c)
		}
		// trim right
		end := len(buf)
		for end > 0 && (buf[end-1] == ' ' || buf[end-1] == '\t') {
			end--
		}
		out[i] = string(buf[:end])
	}
	for len(out) > 0 && out[len(out)-1] == "" {
		out = out[:len(out)-1]
	}
	return out
}

// TestWrapDriftAgainstDeferredVT streams a long markdown paragraph at a
// narrow terminal width, then renders the captured bytes through a
// faithful deferred-wrap simulator. The bug: when raw output soft-wraps
// at the exact column boundary, largo's echoRaw eager-bumps rawLines
// while a real (deferred-wrap) terminal does not yet visit a new row.
// On block boundary, eraseRaw therefore clears the wrong number of
// rows, leaving stale raw markdown above the glamour-rendered output.
//
// The assertion: after the full stream + flush, no fragment of the
// original raw markdown source should remain visible on screen. If the
// erase under-runs, a partial word from the raw echo survives above
// the rendered replacement.
func TestWrapDriftAgainstDeferredVT(t *testing.T) {
	const width = 20
	// Crafted so multiple lines hit the column-20 boundary exactly.
	// "The quick brown fox " is exactly 20 chars (incl. trailing space).
	// Streamed one byte at a time, every wrap point arms a deferred
	// wrap that largo accounts for eagerly.
	input := []byte("The quick brown fox jumps over the lazy dog. " +
		"Pack my box with five dozen liquor jugs.\n\n" +
		"How vexingly quick daft zebras jump.\n\n")

	var out bytes.Buffer
	w, err := NewWriter(&out, Options{Width: width})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < len(input); i++ {
		if _, err := w.Write(input[i : i+1]); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	sim := newDW(width)
	sim.feed(out.Bytes())
	visible := strings.Join(sim.lines(), "\n")

	// Sanity: the rendered content IS present (glamour ran).
	if !strings.Contains(visible, "vexingly") {
		t.Fatalf("rendered content missing entirely:\n%s", visible)
	}

	// Failing assertion: any of the source words should appear AT MOST
	// once in the final visible buffer. If echoRaw's row count drifts
	// from the deferred-wrap terminal, eraseRaw under-clears and a
	// fragment of the raw echo survives — so a word appears twice.
	for _, word := range []string{"vexingly", "liquor", "Pack", "zebras"} {
		n := strings.Count(visible, word)
		if n > 1 {
			t.Errorf("word %q appears %d times — raw markdown leaked past erase\n--- visible (%d rows, width %d) ---\n%s\n--- end ---",
				word, n, width, len(sim.lines()), visible)
		}
	}
}

// TestWrapDriftExactFillNewline isolates the simplest reproduction:
// fill a row to exactly the column boundary, then a newline arrives,
// then a short word, then \n\n. On a deferred-wrap terminal the
// exact fill does NOT advance — the \n is what advances. Old (buggy)
// largo advanced on the exact fill AND on the \n, double-counting by
// one row — so eraseRaw cleared one row too few and a fragment of
// the raw echo survived above the rendered output.
//
// We assert via differential rendering: stream byte-by-byte through
// largo (the path that hits echoRaw's incremental counting), then
// stream the same input as a single Write (which doesn't soft-wrap
// the echo because the \n\n arrives in the same call and drain
// erases immediately). The two visible buffers must match — if
// echoRaw's wrap counting drifts, the byte-by-byte stream leaves
// stale rows that the single-write stream does not.
func TestWrapDriftExactFillNewline(t *testing.T) {
	const width = 10
	// "1234567890" is exactly 10 chars — fills row to the column
	// boundary without crossing it. On deferred-wrap the cursor stays
	// parked at col 10, row 0. The "\n" advances to row 1.
	input := []byte("1234567890\ntail\n\n")

	render := func(chunk int) string {
		var out bytes.Buffer
		w, err := NewWriter(&out, Options{Width: width})
		if err != nil {
			t.Fatal(err)
		}
		for i := 0; i < len(input); i += chunk {
			end := i + chunk
			if end > len(input) {
				end = len(input)
			}
			w.Write(input[i:end])
		}
		w.Flush()
		sim := newDW(width)
		sim.feed(out.Bytes())
		return strings.Join(sim.lines(), "\n")
	}

	oneByte := render(1)
	bulk := render(len(input))

	if oneByte != bulk {
		t.Errorf("byte-by-byte and bulk rendering disagree — wrap counting drift\n--- byte-by-byte ---\n%s\n--- bulk ---\n%s\n--- end ---", oneByte, bulk)
	}
}

// TestEchoRawMatchesDeferredVT is the strict invariant test: largo's
// echoRaw row counter must equal the number of physical rows the
// cursor has descended on a faithful deferred-wrap VT. Any divergence
// is the bug — it directly causes eraseRaw to clear the wrong number
// of rows. Run against a battery of edge cases that exercise exact
// fills, exact fills followed by newlines, CR cancellation, ANSI
// non-consumption, and wide-character wrap.
func TestEchoRawMatchesDeferredVT(t *testing.T) {
	type tc struct {
		name  string
		width int
		in    string
	}
	cases := []tc{
		{"exact fill no follow", 10, "1234567890"},
		{"exact fill then newline", 10, "1234567890\n"},
		{"exact fill then more text", 10, "1234567890tail"},
		{"exact fill then double newline", 10, "1234567890\n\n"},
		{"two consecutive exact fills", 10, "1234567890abcdefghij"},
		{"wrap-overshoot", 10, "1234567890a"},
		{"CR cancels pending wrap", 10, "1234567890\rx"},
		{"ANSI does not consume pending", 10, "1234567890\x1b[0mx"},
		{"wide-char overshoot", 5, "1234漢"},
		{"wide-char exact fill", 10, "漢字漢字漢"},
		{"paragraph at width 20", 20, "The quick brown fox jumps over the lazy dog. Pack my box."},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Largo's view.
			sw := &Writer{w: &bytes.Buffer{}, opts: Options{Width: c.width}}
			sw.echoRaw([]byte(c.in))

			// Deferred-VT's view.
			sim := newDW(c.width)
			sim.feed([]byte(c.in))

			// rawLines is "rows descended from the start row." On the
			// VT that's sim.row. pendingWrap is the VT's own pending
			// flag. colPos is sim.col (or width when pending).
			vtCol := sim.col
			if sim.pending {
				vtCol = c.width
			}
			if sw.rawLines != sim.row {
				t.Errorf("rawLines: largo=%d vt=%d", sw.rawLines, sim.row)
			}
			if sw.colPos != vtCol {
				t.Errorf("colPos: largo=%d vt=%d", sw.colPos, vtCol)
			}
			if sw.pendingWrap != sim.pending {
				t.Errorf("pendingWrap: largo=%v vt=%v", sw.pendingWrap, sim.pending)
			}
		})
	}
}
