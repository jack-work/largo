# Largo Rendering Issues Observed in Figaro CLI

## Context

Figaro uses largo to render streaming LLM output. Deltas arrive as small text
chunks via JSON-RPC notifications and are written to `largo.Writer`. The writer
echoes raw text immediately, then erases and replaces with glamour-styled
markdown when block boundaries are detected.

## Issue: Raw + Rendered Text Both Visible (Ghosting)

### Symptom

In some cases, both the raw text AND the glamour-rendered replacement are
visible simultaneously. The user sees the same phrase twice — once as plain
text, once as styled markdown — stacked vertically.

This was observed with multi-turn tool-call responses where the LLM produces
text, then a tool call, then more text. The text before and after the tool
call both appeared doubled.

### Suspected Cause

`rawLines` tracking in `echoRaw()` miscounts terminal rows consumed. When
`eraseRaw()` moves the cursor up by `rawLines`, it doesn't go far enough,
leaving some raw text visible above the glamour replacement.

Possible triggers:

1. **Terminal width mismatch.** `terminalWidth()` reads the width once at
   construction. If the terminal is resized, or if the width detected from
   `os.Stdout` doesn't match reality (e.g., running inside tmux with a
   different pane width), `colPos` soft-wrap tracking diverges from actual
   terminal behavior.

2. **ANSI sequences in raw text.** The `echoRaw` byte counter treats ANSI
   escape sequences as visible characters. If any raw text contains escape
   codes (which it shouldn't for LLM deltas, but might for tool output that
   got mixed in), the column count will be wrong.

3. **Multi-byte characters.** `echoRaw` counts bytes, not runes. A multi-byte
   UTF-8 character (emoji, CJK, etc.) occupies one or two columns but multiple
   bytes. The column position diverges from reality.

4. **Glamour output height differs from raw text height.** When glamour renders
   a block, it may produce more or fewer lines than the raw text occupied
   (due to word wrapping, margins, etc.). If the rendered output is shorter
   than the raw text was, the previous raw text from an earlier block may
   still be visible above.

5. **Block boundary between Flush calls.** When `sw.Flush()` is called
   (e.g., before tool output), it renders the current buffer. But if the
   LLM's text didn't end at a clean block boundary, the partial block gets
   rendered as a standalone block. The next batch of deltas starts a new
   `rawLines` count from zero, but visually the cursor may not be at the
   position largo thinks it is.

### How to Reproduce

Use figaro with a prompt that triggers tool use + text output:

```
q list the files here and explain what each one does
```

The LLM will produce text, then call `bash ls -la`, then produce more text
interpreting the results. The text after the tool call may appear doubled.

### Suggested Investigation

1. Add debug logging of `rawLines` and `colPos` before/after `eraseRaw()`.
2. Compare the number of `\033[A` (cursor up) sequences emitted by `eraseRaw`
   with the actual number of terminal rows the raw text occupied.
3. Test with a fixed-width string of known length to isolate whether the
   count is off by a constant or proportional to content length.
