package largo

import (
	"bytes"
	"strings"
	"testing"
)

// --- Block detection tests ---

func TestNextBlock_Paragraphs(t *testing.T) {
	sw := &Writer{opts: Options{Width: 80}}
	sw.buf.WriteString("Hello world\n\nSecond paragraph\n\n")
	block, rest, found := sw.nextBlock()
	if !found {
		t.Fatal("expected to find a block")
	}
	if block != "Hello world\n\n" {
		t.Fatalf("unexpected block: %q", block)
	}
	if rest != "Second paragraph\n\n" {
		t.Fatalf("unexpected rest: %q", rest)
	}
}

func TestNextBlock_IncompleteParagraph(t *testing.T) {
	sw := &Writer{opts: Options{Width: 80}}
	sw.buf.WriteString("Hello world\nstill going")
	_, _, found := sw.nextBlock()
	if found {
		t.Fatal("should not find a block without \\n\\n")
	}
}

func TestNextBlock_FencedCodeBlock(t *testing.T) {
	sw := &Writer{opts: Options{Width: 80}}
	sw.buf.WriteString("```go\nfmt.Println(\"hello\")\n```\ntrailing\n\n")
	block, rest, found := sw.nextBlock()
	if !found {
		t.Fatal("expected to find fenced block")
	}
	if block != "```go\nfmt.Println(\"hello\")\n```\n" {
		t.Fatalf("unexpected block: %q", block)
	}
	if rest != "trailing\n\n" {
		t.Fatalf("unexpected rest: %q", rest)
	}
}

func TestNextBlock_FenceWithBlankLinesInside(t *testing.T) {
	sw := &Writer{opts: Options{Width: 80}}
	sw.buf.WriteString("```\nline1\n\nline2\n```\n")
	block, _, found := sw.nextBlock()
	if !found {
		t.Fatal("expected to find fenced block")
	}
	if block != "```\nline1\n\nline2\n```\n" {
		t.Fatalf("fence should include inner blank lines: %q", block)
	}
}

func TestNextBlock_UnfinishedFence(t *testing.T) {
	sw := &Writer{opts: Options{Width: 80}}
	sw.buf.WriteString("```python\nprint('hi')\n")
	_, _, found := sw.nextBlock()
	if found {
		t.Fatal("should not emit an incomplete fence")
	}
	if !sw.inFence {
		t.Fatal("should be in fence state")
	}
}

func TestNextBlock_TildeFence(t *testing.T) {
	sw := &Writer{opts: Options{Width: 80}}
	sw.buf.WriteString("~~~\ncode\n~~~\n")
	block, _, found := sw.nextBlock()
	if !found {
		t.Fatal("expected tilde fence block")
	}
	if block != "~~~\ncode\n~~~\n" {
		t.Fatalf("unexpected block: %q", block)
	}
}

func TestNextBlock_ParagraphBeforeFence(t *testing.T) {
	sw := &Writer{opts: Options{Width: 80}}
	sw.buf.WriteString("Some intro text\n```\ncode\n```\n")
	block, rest, found := sw.nextBlock()
	if !found {
		t.Fatal("expected block")
	}
	if block != "Some intro text\n" {
		t.Fatalf("expected paragraph before fence: %q", block)
	}
	if rest != "```\ncode\n```\n" {
		t.Fatalf("unexpected rest: %q", rest)
	}
}

func TestNextBlock_MultipleBlocks(t *testing.T) {
	sw := &Writer{opts: Options{Width: 80}}
	input := "# Title\n\nParagraph one.\n\n- item\n\n"
	sw.buf.WriteString(input)

	var blocks []string
	for {
		block, rest, found := sw.nextBlock()
		if !found {
			break
		}
		blocks = append(blocks, block)
		sw.buf.Reset()
		sw.buf.WriteString(rest)
	}
	if len(blocks) != 3 {
		t.Fatalf("expected 3 blocks, got %d: %v", len(blocks), blocks)
	}
}

func TestCharByCharBlocks(t *testing.T) {
	input := "# Hello\n\nWorld\n\n"
	sw := &Writer{opts: Options{Width: 80}}

	var blocks []string
	for i := 0; i < len(input); i++ {
		sw.buf.WriteByte(input[i])
		for {
			block, rest, found := sw.nextBlock()
			if !found {
				break
			}
			blocks = append(blocks, block)
			sw.buf.Reset()
			sw.buf.WriteString(rest)
		}
	}
	if len(blocks) != 2 {
		t.Fatalf("expected 2 blocks, got %d: %v", len(blocks), blocks)
	}
}

// --- Raw echo / erase tracking tests ---

func TestEchoRaw_LineTracking(t *testing.T) {
	var out bytes.Buffer
	sw := &Writer{w: &out, opts: Options{Width: 80}}

	sw.echoRaw([]byte("hello\nworld\n"))
	if sw.rawLines != 2 {
		t.Fatalf("expected 2 raw lines, got %d", sw.rawLines)
	}
	if sw.colPos != 0 {
		t.Fatalf("expected colPos 0, got %d", sw.colPos)
	}
}

func TestEchoRaw_SoftWrap(t *testing.T) {
	var out bytes.Buffer
	sw := &Writer{w: &out, opts: Options{Width: 10}}

	sw.echoRaw([]byte("1234567890abcdefghij12345"))
	if sw.rawLines != 2 {
		t.Fatalf("expected 2 soft-wrapped lines, got %d", sw.rawLines)
	}
	if sw.colPos != 5 {
		t.Fatalf("expected colPos 5, got %d", sw.colPos)
	}
}

func TestEraseRaw(t *testing.T) {
	var out bytes.Buffer
	sw := &Writer{w: &out, opts: Options{Width: 80}}

	sw.rawLines = 3
	sw.colPos = 5
	sw.eraseRaw()

	result := out.String()
	if !strings.Contains(result, "\r\033[2K") {
		t.Fatal("missing current-line clear")
	}
	upClears := strings.Count(result, "\033[A\033[2K")
	if upClears != 3 {
		t.Fatalf("expected 3 up-clears, got %d", upClears)
	}
	if sw.rawLines != 0 || sw.colPos != 0 {
		t.Fatal("counters should be reset after erase")
	}
}

func TestEraseRaw_NothingToErase(t *testing.T) {
	var out bytes.Buffer
	sw := &Writer{w: &out, opts: Options{Width: 80}}

	sw.eraseRaw()
	if out.Len() != 0 {
		t.Fatal("should write nothing when there's nothing to erase")
	}
}

// --- Helper tests ---

func TestIsLineStart(t *testing.T) {
	if !isLineStart("hello", 0) {
		t.Error("position 0 should be line start")
	}
	if !isLineStart("a\nb", 2) {
		t.Error("position after \\n should be line start")
	}
	if isLineStart("ab", 1) {
		t.Error("mid-string should not be line start")
	}
}

func TestHasFencePrefix(t *testing.T) {
	if !hasFencePrefix("```go", 0) {
		t.Error("should detect backtick fence")
	}
	if !hasFencePrefix("~~~", 0) {
		t.Error("should detect tilde fence")
	}
	if hasFencePrefix("``x", 0) {
		t.Error("two backticks is not a fence")
	}
}

func TestIndexClosingFence(t *testing.T) {
	s := "fmt.Println()\n```\nafter"
	idx := indexClosingFence(s, 0)
	if idx != 14 {
		t.Fatalf("expected 14, got %d", idx)
	}

	s2 := "no fence here\nstill no fence"
	idx2 := indexClosingFence(s2, 0)
	if idx2 != -1 {
		t.Fatalf("expected -1, got %d", idx2)
	}
}
