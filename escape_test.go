package largo

import "testing"

func TestEscapeInline(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"plain text", "plain text"},
		{"**bold**", "\\*\\*bold\\*\\*"},
		{"_under_", "\\_under\\_"},
		{"`tick`", "\\`tick\\`"},
		{"# heading", "\\# heading"},
		{"a [link](url)", "a \\[link\\]\\(url\\)"},
		{"path/to/file.txt", "path/to/file\\.txt"},
		{"backslash\\foo", "backslash\\\\foo"},
		{"", ""},
		{"|", "\\|"},
		{"~strike~", "\\~strike\\~"},
	}
	for _, c := range cases {
		got := EscapeInline(c.in)
		if got != c.want {
			t.Errorf("EscapeInline(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestInlineCode(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"plain", "`plain`"},
		{"with backtick `inside`", "`` with backtick `inside` ``"},
		{"`leading", "`` `leading ``"},
		{"trailing`", "`` trailing` ``"},
		{"```triple```", "```` ```triple``` ````"},
		{" leadingspace", "`  leadingspace `"},
		{"", "``"},
	}
	for _, c := range cases {
		got := InlineCode(c.in)
		if got != c.want {
			t.Errorf("InlineCode(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFenceCode(t *testing.T) {
	cases := []struct {
		name      string
		in        string
		lang      string
		wantStart string
		wantEnd   string
	}{
		{
			name:      "plain code",
			in:        "x := 1\nfmt.Println(x)\n",
			lang:      "go",
			wantStart: "```go\n",
			wantEnd:   "```\n",
		},
		{
			name:      "no language",
			in:        "hello\n",
			lang:      "",
			wantStart: "```\n",
			wantEnd:   "```\n",
		},
		{
			name:      "content with triple backticks",
			in:        "before\n```\ninside\n```\nafter\n",
			lang:      "",
			wantStart: "````\n",
			wantEnd:   "````\n",
		},
		{
			name:      "content without trailing newline",
			in:        "no trailing",
			lang:      "",
			wantStart: "```\n",
			wantEnd:   "```\n",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := FenceCode(c.in, c.lang)
			if got[:len(c.wantStart)] != c.wantStart {
				t.Errorf("start: got %q want prefix %q", got, c.wantStart)
			}
			if got[len(got)-len(c.wantEnd):] != c.wantEnd {
				t.Errorf("end: got %q want suffix %q", got, c.wantEnd)
			}
		})
	}
}

func TestFenceCode_LongestBacktickRun(t *testing.T) {
	// 5 backticks at the start of a line means we need 6 in the fence.
	in := "before\n`````\nstuff\n`````\nafter\n"
	got := FenceCode(in, "")
	if got[:7] != "``````\n" {
		t.Errorf("expected 6-backtick fence, got %q", got[:10])
	}
}
