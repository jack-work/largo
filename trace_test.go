package largo

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

// decodeTrace produces a compact human-readable log of a largo byte
// stream, collapsing SGR and repeated spaces/newlines so control flow
// is visible without drowning in color codes.
func decodeTrace(p []byte) string {
	var b strings.Builder
	i := 0
	for i < len(p) {
		c := p[i]
		switch {
		case c == '\n':
			b.WriteString("\\n\n")
			i++
		case c == '\r':
			b.WriteString("\\r")
			i++
		case c == '\x1b' && i+1 < len(p) && p[i+1] == '[':
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
			switch final {
			case 'm':
				// SGR — skip.
			case 'K':
				fmt.Fprintf(&b, "<EL%s>", params)
			case 'A':
				fmt.Fprintf(&b, "<UP%s>", params)
			default:
				fmt.Fprintf(&b, "<%s%c>", params, final)
			}
			i = j
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

func TestTrace_HeadingThenParagraph(t *testing.T) {
	input := []byte("# Title\n\nHello world.\n\n")
	var out bytes.Buffer
	w, err := NewWriter(&out, Options{Width: 80})
	if err != nil {
		t.Fatal(err)
	}
	w.Write(input)
	w.Flush()

	trace := decodeTrace(out.Bytes())
	t.Logf("trace (%d bytes, %d stripped):\n%s", out.Len(), len(trace), trace)
}

func TestTrace_HruleThenQuote(t *testing.T) {
	// The figaro scenario shows two blank lines between --- and the next
	// block. Reproduce the pattern.
	input := []byte("---\n\n**> user prompt**\n\n---\n\n")
	var out bytes.Buffer
	w, err := NewWriter(&out, Options{Width: 80})
	if err != nil {
		t.Fatal(err)
	}
	w.Write(input)
	w.Flush()
	trace := decodeTrace(out.Bytes())
	t.Logf("trace:\n%s", trace)

	vis := renderVisible(out.Bytes(), 80)
	t.Logf("visible:\n%s", strings.Join(vis, "\n"))
}

func TestTrace_ThreeBlocks(t *testing.T) {
	input := []byte("# H\n\nA.\n\nB.\n\n")
	var out bytes.Buffer
	w, err := NewWriter(&out, Options{Width: 80})
	if err != nil {
		t.Fatal(err)
	}
	w.Write(input)
	w.Flush()
	trace := decodeTrace(out.Bytes())
	t.Logf("trace:\n%s", trace)

	vis := renderVisible(out.Bytes(), 80)
	t.Logf("visible:\n%s", strings.Join(vis, "\n"))
	t.Logf("row count: %d", len(vis))
}

func TestTrace_SummaryHeading(t *testing.T) {
	// Reproduces the case where "## Summary" appeared as raw markdown
	// in the figaro turn scenario. Feed only the problem region.
	input := []byte("---\n\n## Summary\n\nThe function works.\n\n")
	var out bytes.Buffer
	w, err := NewWriter(&out, Options{Width: 80})
	if err != nil {
		t.Fatal(err)
	}
	w.Write(input)
	w.Flush()

	trace := decodeTrace(out.Bytes())
	t.Logf("trace:\n%s", trace)
}
