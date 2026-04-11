// Package largo provides a streaming io.Writer adapter for the glamour
// markdown terminal renderer. As tokens arrive they are written to the
// terminal immediately as raw text so the user sees output in real time.
// When a markdown block boundary is detected the raw output is erased and
// replaced with glamour-rendered output.
//
// Usage:
//
//	r, _ := glamour.NewTermRenderer(glamour.WithAutoStyle())
//	w := largo.New(os.Stdout, r, largo.Options{Width: 80})
//	for token := range llmTokens {
//	    w.Write([]byte(token))
//	}
//	w.Flush()
package largo

import (
	"bytes"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/styles"
	"github.com/mattn/go-runewidth"
	"golang.org/x/term"
)

// Options configures the streaming writer.
type Options struct {
	// Width is the terminal width in columns, used to calculate how many
	// terminal rows a line of raw text occupies (for accurate erasure).
	// Defaults to auto-detected terminal width, falling back to 80.
	Width int

	// Margin is deprecated. The correct word-wrap width is now computed
	// automatically from the glamour style's document margin. Callers
	// should remove any Margin override.
	Margin int
}

// blockRenderer is the minimal surface largo needs from a markdown
// renderer. *glamour.TermRenderer satisfies this. Defining it as an
// interface (rather than holding the concrete type) lets tests inject
// stub renderers to exercise the render-error fallback path.
type blockRenderer interface {
	Render(in string) (string, error)
}

// Writer implements io.Writer. It streams raw bytes to the terminal
// immediately and replaces them with glamour-rendered output when a
// block boundary is detected.
type Writer struct {
	w        io.Writer
	renderer blockRenderer
	opts     Options

	buf     bytes.Buffer // accumulated markdown source
	inFence bool

	// rawLines tracks the number of terminal rows consumed by raw output
	// for the current (incomplete) block. When a block completes we erase
	// this many rows before writing the rendered replacement.
	rawLines int
	// colPos tracks the column position within the current terminal row
	// so we can account for soft wrapping.
	colPos int
}

// terminalWidth attempts to detect the terminal width from w. Returns 0 if
// w is not a terminal.
func terminalWidth(w io.Writer) int {
	if f, ok := w.(*os.File); ok {
		if w, _, err := term.GetSize(int(f.Fd())); err == nil {
			return w
		}
	}
	return 0
}

// New creates a Writer that streams to w with glamour rendering.
// If opts.Width is 0, it is auto-detected from w (if w is a terminal),
// falling back to 80.
func New(w io.Writer, renderer *glamour.TermRenderer, opts Options) *Writer {
	if opts.Width <= 0 {
		opts.Width = terminalWidth(w)
	}
	if opts.Width <= 0 {
		opts.Width = 80
	}
	return &Writer{
		w:        w,
		renderer: renderer,
		opts:     opts,
	}
}

// streamingStyle returns a glamour style tuned for block-by-block rendering.
// It removes the document-level and heading padding that glamour adds for
// full-document renders, since those cause doubled spacing when blocks are
// rendered independently. It also strips the literal "##" / "###" prefixes
// glamour's default H2–H6 styling bakes in — in a streaming context those
// look like unrendered markdown syntax bleeding through. Returns the style
// option and the document margin (in columns) that glamour will add to
// each line.
func streamingStyle() (glamour.TermRendererOption, int) {
	style := styles.DarkStyleConfig
	style.Document.BlockPrefix = ""
	style.Document.BlockSuffix = ""
	style.Heading.BlockSuffix = ""

	// H1 keeps its pill background. H2–H6 have their literal "##" prefixes
	// cleared so they render as bold/colored text without the marker.
	style.H2.Prefix = ""
	style.H3.Prefix = ""
	style.H4.Prefix = ""
	style.H5.Prefix = ""
	style.H6.Prefix = ""

	docMargin := 0
	if style.Document.Margin != nil {
		docMargin = int(*style.Document.Margin)
	}

	return glamour.WithStyles(style), docMargin
}

// NewWriter creates a Writer with its own glamour renderer, using the
// detected (or provided) terminal width for both line tracking and rendering.
// The word-wrap width is computed automatically: terminal width minus the
// glamour style's document margin, so rendered output fits the terminal
// exactly without overflow or wasted columns.
func NewWriter(w io.Writer, opts Options) (*Writer, error) {
	if opts.Width <= 0 {
		opts.Width = terminalWidth(w)
	}
	if opts.Width <= 0 {
		opts.Width = 80
	}

	styleOpt, docMargin := streamingStyle()
	wrapWidth := opts.Width - docMargin

	r, err := glamour.NewTermRenderer(
		styleOpt,
		glamour.WithWordWrap(wrapWidth),
	)
	if err != nil {
		return nil, err
	}
	return &Writer{
		w:        w,
		renderer: r,
		opts:     opts,
	}, nil
}

// Write appends p to the buffer, echoes it as raw text, and renders any
// completed blocks. Always returns len(p), nil unless the underlying
// writer fails.
func (sw *Writer) Write(p []byte) (int, error) {
	sw.buf.Write(p)

	// Echo raw bytes and track terminal rows consumed.
	if err := sw.echoRaw(p); err != nil {
		return len(p), err
	}

	if err := sw.drain(); err != nil {
		return len(p), err
	}
	return len(p), nil
}

// Flush renders whatever remains in the buffer. Call when the stream ends
// or before switching to a different output mode (e.g., tool output).
func (sw *Writer) Flush() error {
	if sw.buf.Len() == 0 {
		return nil
	}
	content := sw.buf.String()
	sw.buf.Reset()
	if err := sw.eraseRaw(); err != nil {
		return err
	}
	return sw.renderAndWrite(content)
}

// echoRaw writes raw bytes to the terminal and updates the line counter.
// Column tracking accounts for ANSI escape sequences (zero width),
// tab stops, and multi-column characters (CJK, emoji).
func (sw *Writer) echoRaw(p []byte) error {
	if _, err := sw.w.Write(p); err != nil {
		return err
	}
	// Track terminal rows consumed using rune widths.
	i := 0
	for i < len(p) {
		b := p[i]

		// Newline: always moves to next row.
		if b == '\n' {
			sw.rawLines++
			sw.colPos = 0
			i++
			continue
		}

		// Carriage return: moves to column 0 without advancing row.
		if b == '\r' {
			sw.colPos = 0
			i++
			continue
		}

		// ANSI escape sequence: skip entirely (zero visual width).
		if b == '\x1b' && i+1 < len(p) && p[i+1] == '[' {
			// CSI sequence: \x1b[ ... <terminator>
			j := i + 2
			for j < len(p) && !isCSITerminator(p[j]) {
				j++
			}
			if j < len(p) {
				j++ // skip the terminator byte
			}
			i = j
			continue
		}

		// Tab: advance to next 8-column tab stop.
		if b == '\t' {
			advance := 8 - (sw.colPos % 8)
			sw.colPos += advance
			if sw.colPos >= sw.opts.Width {
				sw.rawLines++
				sw.colPos = 0
			}
			i++
			continue
		}

		// Other control characters (bell, backspace, etc.): skip.
		if b < 0x20 {
			i++
			continue
		}

		// Decode a full rune and measure its display width.
		r, size := utf8.DecodeRune(p[i:])
		w := runewidth.RuneWidth(r)
		sw.colPos += w
		if sw.colPos > sw.opts.Width {
			// Character doesn't fit on current line — terminal wraps
			// before printing it, so it starts on the next row.
			sw.rawLines++
			sw.colPos = w
		} else if sw.colPos == sw.opts.Width {
			// Exactly filled the row. Terminal may or may not wrap yet
			// (deferred wrap). Treat as wrapped — the next visible
			// character will be on a new row.
			sw.rawLines++
			sw.colPos = 0
		}
		i += size
	}
	return nil
}

// isCSITerminator returns true if b is the final byte of a CSI escape
// sequence (the range 0x40–0x7E, i.e. @ through ~).
func isCSITerminator(b byte) bool {
	return b >= 0x40 && b <= 0x7E
}

// eraseRaw moves the cursor up and clears each row of raw output, leaving
// the cursor at the position where the raw block started.
func (sw *Writer) eraseRaw() error {
	if sw.rawLines == 0 && sw.colPos == 0 {
		return nil
	}

	var esc bytes.Buffer
	// Clear the current (possibly partial) line.
	esc.WriteString("\r\033[2K")
	// Move up and clear each previous line.
	for i := 0; i < sw.rawLines; i++ {
		esc.WriteString("\033[A\033[2K")
	}

	sw.rawLines = 0
	sw.colPos = 0

	_, err := sw.w.Write(esc.Bytes())
	return err
}

// drain scans the buffer for complete blocks, erases the raw output, and
// writes glamour-rendered output.
func (sw *Writer) drain() error {
	for {
		block, rest, found := sw.nextBlock()
		if !found {
			return nil
		}

		// Erase all raw output (covers the completed block + any remainder
		// that was echoed).
		if err := sw.eraseRaw(); err != nil {
			return err
		}

		// Render and write the completed block.
		if err := sw.renderAndWrite(block); err != nil {
			return err
		}

		// Replace the buffer with the remainder.
		sw.buf.Reset()
		sw.buf.WriteString(rest)

		// Re-echo the unconsumed remainder as raw text so the user
		// continues to see the in-progress next block.
		if len(rest) > 0 {
			if err := sw.echoRaw([]byte(rest)); err != nil {
				return err
			}
		}
	}
}

// nextBlock finds the first complete block in the buffer.
// Block boundaries are blank lines (\n\n) outside fenced code blocks.
// Fenced code blocks (``` or ~~~) are emitted as a single block.
func (sw *Writer) nextBlock() (block, rest string, found bool) {
	s := sw.buf.String()

	// If we're inside a fenced code block, look for the closing fence.
	if sw.inFence {
		firstNL := strings.IndexByte(s, '\n')
		if firstNL == -1 {
			return "", "", false
		}
		closeIdx := indexClosingFence(s, firstNL+1)
		if closeIdx == -1 {
			return "", "", false
		}
		end := closeIdx
		if nl := strings.IndexByte(s[closeIdx:], '\n'); nl != -1 {
			end = closeIdx + nl + 1
		} else {
			return "", "", false
		}
		sw.inFence = false
		return s[:end], s[end:], true
	}

	i := 0
	for i < len(s) {
		// Check for fence start at line boundaries.
		if isLineStart(s, i) && hasFencePrefix(s, i) {
			before := s[:i]
			// If there's content before the fence, emit it as a block first.
			if len(strings.TrimSpace(before)) > 0 {
				return before, s[i:], true
			}
			// Find end of opening fence line.
			closeSearch := i
			if nl := strings.IndexByte(s[i:], '\n'); nl != -1 {
				closeSearch = i + nl + 1
			} else {
				return "", "", false
			}
			// Find closing fence.
			closeIdx := indexClosingFence(s, closeSearch)
			if closeIdx == -1 {
				sw.inFence = true
				sw.buf.Reset()
				sw.buf.WriteString(s[i:])
				return "", "", false
			}
			end := closeIdx
			if nl := strings.IndexByte(s[closeIdx:], '\n'); nl != -1 {
				end = closeIdx + nl + 1
			} else {
				return "", "", false
			}
			return s[i:end], s[end:], true
		}

		// Blank line = block boundary (outside fences).
		if i+1 < len(s) && s[i] == '\n' && s[i+1] == '\n' {
			block := s[:i+2]
			if len(strings.TrimSpace(block)) > 0 {
				return block, s[i+2:], true
			}
			i += 2
			continue
		}
		i++
	}

	return "", "", false
}

func (sw *Writer) renderAndWrite(block string) error {
	rendered, err := sw.renderer.Render(block)
	if err != nil {
		// Glamour failed to render this block. Fall back to writing the
		// raw markdown source so the user sees something instead of
		// losing a chunk of the response. The cursor-safety contract
		// (block ends on a fresh row) still applies.
		rendered = stripBlankLines(block) + "\n\n"
		_, werr := io.WriteString(sw.w, rendered)
		if werr != nil {
			return werr
		}
		return nil
	}
	// Normalize the rendered block so every block has a uniform shape
	// regardless of what glamour emits for this block type. Different
	// block types wrap their output differently: hrules emit a leading
	// row of pad spaces; headings emit zero trailing newlines; paragraphs
	// emit one. Without normalization this bleeds into cursor drift
	// (because the erase-replace math assumes the rendered block ends
	// on a fresh row) and inconsistent vertical spacing between blocks.
	//
	// Target shape:
	//   - No leading blank-only lines (block starts flush against prev).
	//   - No trailing blank-only lines, then exactly two trailing
	//     newlines: one for cursor safety (so the next raw echo lands
	//     on a fresh row) and one blank line for readability.
	rendered = stripBlankLines(rendered)
	rendered += "\n\n"

	_, err = io.WriteString(sw.w, rendered)
	return err
}

// stripBlankLines removes leading and trailing blank-only lines from s.
// A blank-only line is one whose visible content (after whitespace trim,
// ANSI escapes considered non-visible) is empty. The returned string
// has no leading or trailing newlines.
func stripBlankLines(s string) string {
	lines := strings.Split(s, "\n")
	for len(lines) > 0 && isVisuallyBlank(lines[0]) {
		lines = lines[1:]
	}
	for len(lines) > 0 && isVisuallyBlank(lines[len(lines)-1]) {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

// isVisuallyBlank reports whether a line contains no visible characters —
// i.e., after stripping ANSI SGR sequences, only whitespace remains.
func isVisuallyBlank(line string) bool {
	i := 0
	for i < len(line) {
		c := line[i]
		if c == '\x1b' && i+1 < len(line) && line[i+1] == '[' {
			j := i + 2
			for j < len(line) && !(line[j] >= 0x40 && line[j] <= 0x7e) {
				j++
			}
			if j < len(line) {
				j++
			}
			i = j
			continue
		}
		if c != ' ' && c != '\t' {
			return false
		}
		i++
	}
	return true
}

func isLineStart(s string, i int) bool {
	return i == 0 || (i > 0 && s[i-1] == '\n')
}

func hasFencePrefix(s string, i int) bool {
	if i+3 > len(s) {
		return false
	}
	return s[i:i+3] == "```" || s[i:i+3] == "~~~"
}

func indexClosingFence(s string, start int) int {
	i := start
	for i < len(s) {
		nl := strings.IndexByte(s[i:], '\n')
		var line string
		if nl == -1 {
			line = s[i:]
		} else {
			line = s[i : i+nl]
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "```" || trimmed == "~~~" {
			return i
		}
		if nl == -1 {
			break
		}
		i += nl + 1
	}
	return -1
}
