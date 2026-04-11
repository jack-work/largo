package largo

import "strings"

// EscapeInline escapes CommonMark special characters in s so it can be
// embedded as plain text inside a paragraph or other inline context
// without being parsed as markdown syntax. Use this whenever you take
// a string from outside (user input, LLM-generated text, an external
// tool's argument) and want to display it verbatim in prose.
//
// Note that escaping is the wrong tool when the content needs to be
// shown as code; for that, use InlineCode (a string with backtick
// delimiters that don't collide with the content) or FenceCode (a
// multi-line fenced block).
func EscapeInline(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch c {
		case '\\', '`', '*', '_', '{', '}', '[', ']', '(', ')',
			'#', '+', '-', '.', '!', '<', '>', '|', '~':
			b.WriteByte('\\')
		}
		b.WriteByte(c)
	}
	return b.String()
}

// InlineCode wraps s in inline-code delimiters, automatically choosing
// a delimiter length one greater than the longest backtick run inside
// s, so the content cannot accidentally be parsed as a closing
// delimiter.
//
// If s starts or ends with a backtick or whitespace, a single padding
// space is added inside each delimiter (and stripped on render),
// per CommonMark § 6.1.
//
// Use this for arbitrary single-line content that should appear in
// monospace inline — file paths, command names, identifiers,
// short shell snippets.
func InlineCode(s string) string {
	longest := 0
	cur := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '`' {
			cur++
			if cur > longest {
				longest = cur
			}
		} else {
			cur = 0
		}
	}
	delim := strings.Repeat("`", longest+1)

	pad := ""
	if len(s) > 0 {
		first, last := s[0], s[len(s)-1]
		if first == '`' || last == '`' || first == ' ' || last == ' ' {
			pad = " "
		}
	}
	return delim + pad + s + pad + delim
}

// FenceCode wraps s in a fenced code block, automatically choosing a
// fence length longer than the longest backtick run on any line of s,
// so content cannot accidentally close the fence early. lang is an
// optional language hint ("go", "python", "json", or "" for none).
//
// The returned block ends with a newline so it can be concatenated
// directly into a markdown stream.
func FenceCode(s, lang string) string {
	longest := 2 // ensure at least 3 backticks
	for _, line := range strings.Split(s, "\n") {
		n := 0
		for n < len(line) && line[n] == '`' {
			n++
		}
		if n > longest {
			longest = n
		}
	}
	delim := strings.Repeat("`", longest+1)

	var b strings.Builder
	b.Grow(len(s) + 2*len(delim) + len(lang) + 4)
	b.WriteString(delim)
	b.WriteString(lang)
	b.WriteByte('\n')
	b.WriteString(s)
	if !strings.HasSuffix(s, "\n") {
		b.WriteByte('\n')
	}
	b.WriteString(delim)
	b.WriteByte('\n')
	return b.String()
}
