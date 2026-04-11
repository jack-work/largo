package largo

import (
	"strings"
	"testing"

	"github.com/charmbracelet/glamour"
)

// TestGlamour_TrailingNewlines reports how many trailing newlines
// glamour emits for various block types under our streaming style.
// This tells us what normalizeTrailingNewlines should target.
func TestGlamour_TrailingNewlines(t *testing.T) {
	styleOpt, _ := streamingStyle()
	r, err := glamour.NewTermRenderer(styleOpt, glamour.WithWordWrap(78))
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{
		"paragraph":  "Hello world.\n\n",
		"heading1":   "# Title\n\n",
		"heading2":   "## Title\n\n",
		"list":       "- a\n- b\n\n",
		"codefence":  "```\nx := 1\n```\n",
		"blockquote": "> quote text\n\n",
		"hrule":      "---\n\n",
	}
	for name, in := range cases {
		out, err := r.Render(in)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		trailing := 0
		for i := len(out) - 1; i >= 0 && out[i] == '\n'; i-- {
			trailing++
		}
		leading := 0
		for i := 0; i < len(out) && out[i] == '\n'; i++ {
			leading++
		}
		t.Logf("%-11s in=%d out=%d lead=%d trail=%d",
			name, len(in), len(out), leading, trailing)
	}
}

// Confirm whether glamour's streaming style can render h2 headings
// in isolation — or whether it's largo's block splitting that's
// causing "## Summary" to come out as literal text.
func TestGlamour_HeadingsIsolated(t *testing.T) {
	styleOpt, _ := streamingStyle()
	r, err := glamour.NewTermRenderer(styleOpt, glamour.WithWordWrap(78))
	if err != nil {
		t.Fatal(err)
	}

	cases := []string{
		"# H1 heading\n\n",
		"## H2 heading\n\n",
		"### H3 heading\n\n",
		"# H1\n",
		"## H2\n",
	}
	for _, c := range cases {
		out, err := r.Render(c)
		if err != nil {
			t.Errorf("render %q: %v", c, err)
			continue
		}
		// Does the rendered output still contain literal "##" markers?
		if strings.Contains(out, "##") {
			t.Errorf("rendered %q contains literal '##': %q", c, strings.ReplaceAll(out, "\x1b", "ESC"))
		} else {
			t.Logf("OK %q -> %q", c, strings.ReplaceAll(out, "\x1b", "ESC"))
		}
	}
}
