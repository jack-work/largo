package largo

import (
	"bytes"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/jack-work/largo/internal/vtsim"
)

// streamThrough writes input bytes one or two at a time through a Writer
// (forcing multi-chunk behavior), then flushes, and returns the bytes
// that were written to the underlying writer.
func streamThrough(t *testing.T, input []byte, width, chunk int) []byte {
	t.Helper()
	var out bytes.Buffer
	w, err := NewWriter(&out, Options{Width: width})
	if err != nil {
		t.Fatalf("NewWriter: %v", err)
	}
	for i := 0; i < len(input); i += chunk {
		end := i + chunk
		if end > len(input) {
			end = len(input)
		}
		if _, err := w.Write(input[i:end]); err != nil {
			t.Fatalf("write: %v", err)
		}
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}
	return out.Bytes()
}

// renderVisible runs the byte stream through a terminal simulator and
// returns the final visible lines. Width matches the writer's width.
func renderVisible(raw []byte, width int) []string {
	s := vtsim.New(width)
	s.Feed(raw)
	return s.Lines()
}

// dumpVisible is a convenience for tests: returns the visible screen as
// a joined string, newline-separated.
func dumpVisible(raw []byte, width int) string {
	return strings.Join(renderVisible(raw, width), "\n")
}

func TestScenario_OnlyParagraph(t *testing.T) {
	// Single paragraph ending without a trailing blank line — flushed
	// at stream end. Should still render cleanly.
	input := []byte("A single sentence.")
	raw := streamThrough(t, input, 80, 3)
	vis := renderVisible(raw, 80)
	joined := strings.Join(vis, "\n")
	if !strings.Contains(joined, "A single sentence.") {
		t.Errorf("missing text:\n%s", joined)
	}
	mustNotLeakMarkdown(t, vis)
}

func TestScenario_MultipleBlankLines(t *testing.T) {
	// Three blank lines between paragraphs should collapse to one
	// (largo's normalize applies uniformly; glamour may or may not
	// squash extra blanks).
	input := []byte("First.\n\n\n\nSecond.\n\n")
	raw := streamThrough(t, input, 80, 1)
	vis := renderVisible(raw, 80)
	joined := strings.Join(vis, "\n")
	if !strings.Contains(joined, "First.") || !strings.Contains(joined, "Second.") {
		t.Errorf("missing content:\n%s", joined)
	}
	// Count blank rows between First and Second.
	firstIdx, secondIdx := -1, -1
	for i, line := range vis {
		if strings.Contains(line, "First.") {
			firstIdx = i
		}
		if strings.Contains(line, "Second.") {
			secondIdx = i
		}
	}
	blanks := 0
	for i := firstIdx + 1; i < secondIdx; i++ {
		if strings.TrimSpace(vis[i]) == "" {
			blanks++
		}
	}
	if blanks != 1 {
		t.Errorf("expected 1 blank row between paragraphs, got %d\n%s", blanks, joined)
	}
}

func TestScenario_NarrowTerminal(t *testing.T) {
	// Very narrow width exercises soft-wrap tracking in echoRaw.
	input := []byte("The quick brown fox jumps over the lazy dog.\n\nAnother line.\n\n")
	raw := streamThrough(t, input, 20, 2)
	vis := renderVisible(raw, 20)
	joined := strings.Join(vis, "\n")
	if !strings.Contains(joined, "quick") || !strings.Contains(joined, "Another") {
		t.Errorf("missing content:\n%s", joined)
	}
}

func TestScenario_CodeFenceOnly(t *testing.T) {
	input := []byte("```go\nfunc main() {}\n```\n\n")
	raw := streamThrough(t, input, 80, 3)
	vis := renderVisible(raw, 80)
	joined := strings.Join(vis, "\n")
	if !strings.Contains(joined, "func main()") {
		t.Errorf("missing code:\n%s", joined)
	}
	// The closing triple-backticks should not be visible as literal text.
	for _, line := range vis {
		if strings.Contains(line, "```") {
			t.Errorf("closing fence leaked into visible output: %q", line)
		}
	}
}

func TestScenario_SuspendResume(t *testing.T) {
	// Streams: markdown intro, then suspend + verbatim bytes (simulating
	// bash output with ANSI), then resume + more markdown. Verifies the
	// pass-through region is preserved on screen and the markdown that
	// follows renders correctly without overlapping.
	var out bytes.Buffer
	w, err := NewWriter(&out, Options{Width: 80})
	if err != nil {
		t.Fatal(err)
	}

	w.Write([]byte("Running a command:\n\n"))

	raw := w.Suspend()
	// Simulate bash output, including a line that bash colored red.
	raw.Write([]byte("file1.txt\n"))
	raw.Write([]byte("\x1b[31mfile2.txt\x1b[0m\n"))
	raw.Write([]byte("file3.txt\n"))
	if err := w.Resume(); err != nil {
		t.Fatalf("resume: %v", err)
	}

	w.Write([]byte("Three files found.\n\n"))
	if err := w.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	vis := renderVisible(out.Bytes(), 80)
	joined := strings.Join(vis, "\n")

	for _, want := range []string{"Running a command", "file1.txt", "file2.txt", "file3.txt", "Three files found"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in:\n%s", want, joined)
		}
	}
	if os.Getenv("V") != "" {
		t.Logf("visible (%d rows):\n%s", len(vis), joined)
	}
}

func TestScenario_SuspendNoTrailingNewline(t *testing.T) {
	// If the suspended region writes bytes without a trailing newline,
	// Resume must inject one so the next markdown block starts on a
	// fresh row instead of overlapping.
	var out bytes.Buffer
	w, err := NewWriter(&out, Options{Width: 80})
	if err != nil {
		t.Fatal(err)
	}

	w.Write([]byte("Before.\n\n"))
	raw := w.Suspend()
	raw.Write([]byte("no newline at end"))
	w.Resume()
	w.Write([]byte("After.\n\n"))
	w.Flush()

	vis := renderVisible(out.Bytes(), 80)

	// Find rows containing the three markers and assert they are all
	// on distinct rows (no overlap).
	rowOf := func(s string) int {
		for i, line := range vis {
			if strings.Contains(line, s) {
				return i
			}
		}
		return -1
	}
	rBefore := rowOf("Before")
	rRaw := rowOf("no newline")
	rAfter := rowOf("After")
	if rBefore == -1 || rRaw == -1 || rAfter == -1 {
		t.Fatalf("missing content: rBefore=%d rRaw=%d rAfter=%d\n%s",
			rBefore, rRaw, rAfter, strings.Join(vis, "\n"))
	}
	if rBefore == rRaw || rRaw == rAfter || rBefore == rAfter {
		t.Errorf("rows collided: rBefore=%d rRaw=%d rAfter=%d\n%s",
			rBefore, rRaw, rAfter, strings.Join(vis, "\n"))
	}
}

func TestScenario_ToolsStreamPattern(t *testing.T) {
	// End-to-end test of the figaro pass-through pattern: markdown
	// header for the tool call, suspended verbatim bash output (with
	// ANSI), resume, more markdown.
	var out bytes.Buffer
	w, err := NewWriter(&out, Options{Width: 80})
	if err != nil {
		t.Fatal(err)
	}

	w.Write([]byte("---\nI'll list the directory:\n\n"))
	w.Write([]byte("---\n`▶ bash` " + InlineCode("ls -la /tmp") + "\n\n"))

	raw := w.Suspend()
	raw.Write([]byte("total 24\n"))
	raw.Write([]byte("\x1b[1;34mdir1\x1b[0m\n"))
	raw.Write([]byte("file1.txt\n"))
	w.Resume()

	w.Write([]byte("\n---\n\nThree entries found.\n\n"))
	w.Flush()

	vis := renderVisible(out.Bytes(), 80)
	joined := strings.Join(vis, "\n")

	// All content from both the markdown and pass-through regions
	// must appear, and pass-through bytes must NOT be glamour-margin
	// indented (they should be flush-left).
	for _, want := range []string{"list the directory", "▶ bash", "total 24", "dir1", "file1.txt", "Three entries found"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q\n%s", want, joined)
		}
	}

	// Find the bash-output rows and confirm they begin at column 0,
	// not at column 2 (glamour's margin).
	for _, line := range vis {
		if strings.Contains(line, "total 24") || strings.Contains(line, "file1.txt") {
			if strings.HasPrefix(line, "  ") {
				t.Errorf("pass-through row was glamour-indented: %q", line)
			}
		}
	}

	if os.Getenv("V") != "" {
		t.Logf("visible (%d rows):\n%s", len(vis), joined)
	}
}

func TestScenario_SuspendEmpty(t *testing.T) {
	// Suspend immediately followed by Resume with no writes in between.
	// Should be a clean no-op.
	var out bytes.Buffer
	w, err := NewWriter(&out, Options{Width: 80})
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte("Before.\n\n"))
	w.Suspend()
	w.Resume()
	w.Write([]byte("After.\n\n"))
	w.Flush()

	vis := renderVisible(out.Bytes(), 80)
	joined := strings.Join(vis, "\n")
	if !strings.Contains(joined, "Before") || !strings.Contains(joined, "After") {
		t.Errorf("missing content:\n%s", joined)
	}
}

func TestScenario_SuspendDoubleSuspendPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on double Suspend")
		}
	}()
	w, _ := NewWriter(&bytes.Buffer{}, Options{Width: 80})
	w.Suspend()
	w.Suspend()
}

func TestScenario_WriteWhileSuspendedPanics(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on Write while suspended")
		}
	}()
	w, _ := NewWriter(&bytes.Buffer{}, Options{Width: 80})
	w.Suspend()
	w.Write([]byte("nope"))
}

// brokenRenderer always returns a render error. Used to verify the
// fallback path falls back to raw markdown rather than swallowing the
// block.
type brokenRenderer struct{}

func (brokenRenderer) Render(string) (string, error) {
	return "", fmt.Errorf("synthetic render failure")
}

func TestScenario_RenderErrorFallback(t *testing.T) {
	var out bytes.Buffer
	w, err := NewWriter(&out, Options{Width: 80})
	if err != nil {
		t.Fatal(err)
	}
	w.renderer = brokenRenderer{}

	input := []byte("# Title\n\nA paragraph.\n\n")
	if _, err := w.Write(input); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := w.Flush(); err != nil {
		t.Fatalf("flush: %v", err)
	}

	vis := renderVisible(out.Bytes(), 80)
	joined := strings.Join(vis, "\n")

	// Both blocks should still be visible — as raw markdown — even
	// though glamour failed.
	if !strings.Contains(joined, "# Title") {
		t.Errorf("missing raw heading:\n%s", joined)
	}
	if !strings.Contains(joined, "A paragraph.") {
		t.Errorf("missing raw paragraph:\n%s", joined)
	}
}

func TestScenario_PartialFenceAtEnd(t *testing.T) {
	// An unterminated fence at stream end — Flush should still render
	// whatever is in the buffer without hanging.
	input := []byte("```\nincomplete code")
	raw := streamThrough(t, input, 80, 3)
	vis := renderVisible(raw, 80)
	joined := strings.Join(vis, "\n")
	if !strings.Contains(joined, "incomplete code") {
		t.Errorf("missing content:\n%s", joined)
	}
}

func TestScenario_HeadingThenParagraph(t *testing.T) {
	input := []byte("# Title\n\nA paragraph of text.\n\n")
	raw := streamThrough(t, input, 80, 3)
	vis := renderVisible(raw, 80)

	// The heading and paragraph should each occupy their own lines,
	// with a blank line between them (glamour padding), and no text
	// from one bleeding into the next.
	joined := strings.Join(vis, "\n")
	if !strings.Contains(joined, "Title") {
		t.Errorf("missing heading text:\n%s", joined)
	}
	if !strings.Contains(joined, "A paragraph of text.") {
		t.Errorf("missing paragraph text:\n%s", joined)
	}

	// Check that no line contains both the heading text and the start
	// of the paragraph — that would mean they overlapped.
	for _, line := range vis {
		if strings.Contains(line, "Title") && strings.Contains(line, "A paragraph") {
			t.Errorf("heading and paragraph collided on one line: %q", line)
		}
	}

	if os.Getenv("V") != "" {
		t.Logf("visible:\n%s", joined)
	}
}

// mustContain checks that every expected substring is present in the
// joined screen text.
func mustContain(t *testing.T, joined string, needles []string) {
	t.Helper()
	for _, m := range needles {
		if !strings.Contains(joined, m) {
			t.Errorf("missing expected text %q", m)
		}
	}
}

// mustNotLeakMarkdown checks that no visible line starts with raw
// markdown emphasis or heading syntax (which would indicate glamour
// failed to render a block).
func mustNotLeakMarkdown(t *testing.T, vis []string) {
	t.Helper()
	for i, line := range vis {
		trim := strings.TrimSpace(line)
		if trim == "" {
			continue
		}
		if strings.HasPrefix(trim, "**>") {
			t.Errorf("line %d leaks raw **> marker: %q", i, line)
		}
		if strings.HasPrefix(trim, "##") {
			t.Errorf("line %d leaks raw ## heading: %q", i, line)
		}
	}
}

func TestScenario_SynthTurns(t *testing.T) {
	// Run all canned turns from the figaro-synth harness through largo
	// at several chunk sizes. Each turn asserts its own required text.
	turns := map[string]struct {
		path string
		must []string
	}{
		"default": {
			path: "cmd/figaro-synth/turns/default.md",
			must: []string{"help me understand", "▶ bash", "▶ read", "Summary", "key insight"},
		},
		"code-heavy": {
			path: "cmd/figaro-synth/turns/code-heavy.md",
			must: []string{"actor loop", "drainLoop", "Event types", "Turn generation", "invariant"},
		},
		"tools": {
			path: "cmd/figaro-synth/turns/tools.md",
			must: []string{"port 8080", "▶ bash", "lsof", "pstree", "Summary", "figaro rest"},
		},
	}

	for name, tc := range turns {
		t.Run(name, func(t *testing.T) {
			input, err := os.ReadFile(tc.path)
			if err != nil {
				t.Fatalf("read %s: %v", tc.path, err)
			}
			for _, chunk := range []int{1, 7, 40} {
				t.Run(fmt.Sprintf("chunk=%d", chunk), func(t *testing.T) {
					raw := streamThrough(t, input, 80, chunk)
					vis := renderVisible(raw, 80)
					joined := strings.Join(vis, "\n")
					mustContain(t, joined, tc.must)
					mustNotLeakMarkdown(t, vis)
					if os.Getenv("V") != "" {
						t.Logf("visible (%d rows):\n%s", len(vis), joined)
					}
				})
			}
		})
	}
}

func TestScenario_FiguroTurn(t *testing.T) {
	input, err := os.ReadFile("cmd/figaro-synth/turns/default.md")
	if err != nil {
		t.Fatalf("read turn: %v", err)
	}

	// Exercise a range of chunk sizes to verify output is independent
	// of how the stream is sliced.
	chunkSizes := []int{1, 3, 5, 17, 64, len(input)}
	must := []string{
		"help me understand this function",
		"I'll take a look",
		"boundary check runs on every write",
		"▶ bash",
		"ls -la /tmp/figaro",
		"angelus.sock",
		"▶ read",
		"default_provider",
		"Summary",
		"Buffering",
		"Flushing",
		"key insight",
	}

	for _, chunk := range chunkSizes {
		t.Run(fmt.Sprintf("chunk=%d", chunk), func(t *testing.T) {
			raw := streamThrough(t, input, 80, chunk)
			vis := renderVisible(raw, 80)
			joined := strings.Join(vis, "\n")

			mustContain(t, joined, must)
			mustNotLeakMarkdown(t, vis)

			if os.Getenv("V") != "" {
				t.Logf("visible (%d rows, %d raw bytes):\n%s",
					len(vis), len(raw), joined)
			}
		})
	}
}
