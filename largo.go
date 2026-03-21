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

	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/glamour/styles"
	"golang.org/x/term"
)

// Options configures the streaming writer.
type Options struct {
	// Width is the terminal width in columns, used to calculate how many
	// terminal rows a line of raw text occupies (for accurate erasure).
	// Defaults to 80 if zero.
	Width int

	// Margin is the number of columns to subtract from Width when
	// configuring glamour's word wrap. Glamour's default style adds a
	// 2-char left margin, so Margin should be at least 2 to avoid
	// wrapping. Defaults to 2 if zero.
	Margin int
}

// Writer implements io.Writer. It streams raw bytes to the terminal
// immediately and replaces them with glamour-rendered output when a
// block boundary is detected.
type Writer struct {
	w        io.Writer
	renderer *glamour.TermRenderer
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
// rendered independently.
func streamingStyle() glamour.TermRendererOption {
	style := styles.DarkStyleConfig
	style.Document.BlockPrefix = ""
	style.Document.BlockSuffix = ""
	style.Heading.BlockSuffix = ""
	return glamour.WithStyles(style)
}

// NewWriter creates a Writer with its own glamour renderer, using the
// detected (or provided) terminal width for both line tracking and rendering.
// This ensures the two widths are always in sync.
func NewWriter(w io.Writer, opts Options) (*Writer, error) {
	if opts.Width <= 0 {
		opts.Width = terminalWidth(w)
	}
	if opts.Width <= 0 {
		opts.Width = 80
	}
	if opts.Margin <= 0 {
		opts.Margin = 2
	}
	r, err := glamour.NewTermRenderer(
		streamingStyle(),
		glamour.WithWordWrap(opts.Width-opts.Margin),
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
func (sw *Writer) echoRaw(p []byte) error {
	if _, err := sw.w.Write(p); err != nil {
		return err
	}
	// Track terminal rows consumed.
	for _, b := range p {
		if b == '\n' {
			sw.rawLines++
			sw.colPos = 0
		} else {
			sw.colPos++
			if sw.colPos >= sw.opts.Width {
				// Soft wrap: terminal moves to next row.
				sw.rawLines++
				sw.colPos = 0
			}
		}
	}
	return nil
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
		return err
	}
	// Glamour wraps each render with leading/trailing newlines even with
	// our custom style. Trim them so block-by-block output doesn't
	// accumulate extra blank lines.
	rendered = strings.TrimLeft(rendered, "\n")
	rendered = strings.TrimRight(rendered, "\n") + "\n"

	// Add a blank line before headings for visual separation.
	trimmed := strings.TrimSpace(block)
	if len(trimmed) > 0 && trimmed[0] == '#' {
		rendered = "\n" + rendered
	}

	_, err = io.WriteString(sw.w, rendered)
	return err
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
