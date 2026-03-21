# largo

A lightweight streaming adapter for [glamour](https://github.com/charmbracelet/glamour). Feed it markdown tokens as they arrive from an LLM — raw text appears immediately, then gets replaced with glamour-rendered output when each block completes.

## Install

```
go get github.com/jack-work/largo
```

## Usage

```go
r, _ := glamour.NewTermRenderer(glamour.WithAutoStyle(), glamour.WithWordWrap(80))
w := largo.New(os.Stdout, r, largo.Options{Width: 80})

for token := range llmResponseChannel {
    w.Write([]byte(token))
}
w.Flush()
```

It implements `io.Writer`, so it plugs in anywhere:

```go
io.Copy(w, resp.Body)
w.Flush()
```

## Demo

Stream a markdown file with random chunking to see it in action:

```
go run ./cmd/largo-demo testdata/sample.md
go run ./cmd/largo-demo -delay 30ms -min 1 -max 20 testdata/sample.md
```

## How it works

1. **Echo raw** — every `Write()` call immediately prints raw bytes so the user sees text appearing in real time.
2. **Track rows** — counts terminal rows consumed (including soft wraps at the configured width).
3. **Detect boundary** — when `\n\n` or a closed fenced code block is found, the raw rows are erased using ANSI escape codes.
4. **Render and replace** — the completed block is rendered through glamour and written in place.
5. **Flush** — call at stream end to render whatever is left.

## License

MIT
