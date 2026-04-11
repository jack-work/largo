// Command figaro-synth is a synthetic figaro harness: it produces the
// same kind of output a real figaro agent would emit for a typical
// coding-agent turn (LLM prose, tool markers, fenced or pass-through
// tool output, headings, lists, blockquote), feeds it into a
// largo.Writer one random-size chunk at a time, and lets you see the
// result in your terminal.
//
// The goal: prove that largo is rock-solid in isolation before we
// rewire figaro's CLI to use it. If this looks right here, it will
// look right in figaro.
//
// Usage:
//
//	go run ./cmd/figaro-synth                       # default turn
//	go run ./cmd/figaro-synth -turn tools-stream    # bash pass-through
//	go run ./cmd/figaro-synth -delay 15ms
//	go run ./cmd/figaro-synth -delay 0 -min 20 -max 40
//	go run ./cmd/figaro-synth -list
package main

import (
	_ "embed"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
	"sort"
	"time"

	"github.com/jack-work/largo"
)

//go:embed turns/default.md
var defaultTurn string

//go:embed turns/code-heavy.md
var codeHeavyTurn string

//go:embed turns/tools.md
var toolsTurn string

// turn is a runnable scenario. Markdown-only turns wrap a string;
// programmatic turns (e.g. tools-stream) call Suspend/Resume to
// exercise pass-through.
type turn struct {
	name string
	desc string
	run  func(w *largo.Writer, c chunker)
}

// chunker streams a byte slice through a writer one random-size chunk
// at a time, with a configurable delay between chunks. Used so the
// turn functions don't each reimplement the chunking loop.
type chunker struct {
	rng      *rand.Rand
	delay    time.Duration
	minChunk int
	maxChunk int
}

func (c chunker) stream(dst io.Writer, src []byte) {
	pos := 0
	for pos < len(src) {
		size := c.minChunk + c.rng.Intn(c.maxChunk-c.minChunk+1)
		end := pos + size
		if end > len(src) {
			end = len(src)
		}
		if _, err := dst.Write(src[pos:end]); err != nil {
			log.Fatal(err)
		}
		pos = end
		if c.delay > 0 {
			time.Sleep(c.delay)
		}
	}
}

func mdTurn(name, desc, content string) turn {
	return turn{
		name: name,
		desc: desc,
		run: func(w *largo.Writer, c chunker) {
			c.stream(w, []byte(content))
		},
	}
}

var turns = func() map[string]turn {
	m := map[string]turn{}
	for _, t := range []turn{
		mdTurn("default", "prose + tools (fenced) + summary heading + blockquote", defaultTurn),
		mdTurn("code", "code-heavy explanation with multiple fenced blocks", codeHeavyTurn),
		mdTurn("tools", "multiple sequential tool calls, fenced output (pre-pass-through style)", toolsTurn),
		toolsStreamTurn(),
	} {
		m[t.name] = t
	}
	return m
}()

// toolsStreamTurn exercises the Suspend/Resume pass-through path. It
// streams markdown prose, then suspends largo and writes raw bash-like
// output (with ANSI colors), then resumes for more markdown.
func toolsStreamTurn() turn {
	return turn{
		name: "tools-stream",
		desc: "tool calls with verbatim bash output via Suspend/Resume",
		run: func(w *largo.Writer, c chunker) {
			intro := "---\n\n" +
				"I'll list the contents of `/tmp/figaro` to see what's there.\n\n" +
				"---\n`▶ bash` " + largo.InlineCode("ls -la /tmp/figaro") + "\n\n"
			c.stream(w, []byte(intro))

			raw := w.Suspend()
			bashOutput := []string{
				"total 24\n",
				"drwxr-xr-x  3 jack jack  4096 Apr 11 09:12 \x1b[1;34m.\x1b[0m\n",
				"drwxrwxrwt 18 root root 12288 Apr 11 09:12 \x1b[1;34m..\x1b[0m\n",
				"drwx------  2 jack jack  4096 Apr 11 09:12 \x1b[1;34mfiguros\x1b[0m\n",
				"srw-------  1 jack jack     0 Apr 11 09:12 \x1b[1;35mangelus.sock\x1b[0m\n",
				"-rw-------  1 jack jack    12 Apr 11 09:12 angelus.pid\n",
			}
			for _, line := range bashOutput {
				c.stream(raw, []byte(line))
			}
			if err := w.Resume(); err != nil {
				log.Fatal(err)
			}

			middle := "\n---\n\n" +
				"That's the runtime layout — a Unix socket, a pid file, and a directory of per-agent sockets. " +
				"Let me also show the agent registry by running `figaro list`:\n\n" +
				"---\n`▶ bash` " + largo.InlineCode("figaro list") + "\n\n"
			c.stream(w, []byte(middle))

			raw = w.Suspend()
			listOutput := "ID        STATE    MODEL                       MESSAGES  PIDS\n" +
				"a1b2c3d4  active   claude-sonnet-4-20250514    12        98765\n" +
				"e5f6g7h8  idle     claude-sonnet-4-20250514     3        -\n"
			c.stream(raw, []byte(listOutput))
			if err := w.Resume(); err != nil {
				log.Fatal(err)
			}

			end := "\n---\n\n" +
				"## Summary\n\n" +
				"- Two agents registered, one bound to your shell.\n" +
				"- The angelus is running (you can see its socket and pid file).\n" +
				"- No cleanup needed — this is the expected steady state.\n\n" +
				"> If you ever want to stop the angelus, run " + largo.InlineCode("figaro rest") + ".\n\n"
			c.stream(w, []byte(end))
		},
	}
}

func main() {
	turnName := flag.String("turn", "default", "which canned turn to stream")
	delay := flag.Duration("delay", 10*time.Millisecond, "delay between chunks")
	minChunk := flag.Int("min", 1, "minimum chunk size in bytes")
	maxChunk := flag.Int("max", 12, "maximum chunk size in bytes")
	list := flag.Bool("list", false, "list available turns and exit")
	flag.Parse()

	if *list {
		names := make([]string, 0, len(turns))
		for n := range turns {
			names = append(names, n)
		}
		sort.Strings(names)
		fmt.Fprintln(os.Stderr, "available turns:")
		for _, n := range names {
			fmt.Fprintf(os.Stderr, "  %-14s %s\n", n, turns[n].desc)
		}
		return
	}

	t, ok := turns[*turnName]
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown turn %q\n", *turnName)
		os.Exit(1)
	}

	w, err := largo.NewWriter(os.Stdout, largo.Options{})
	if err != nil {
		log.Fatal(err)
	}

	c := chunker{
		rng:      rand.New(rand.NewSource(time.Now().UnixNano())),
		delay:    *delay,
		minChunk: *minChunk,
		maxChunk: *maxChunk,
	}
	t.run(w, c)

	if err := w.Flush(); err != nil {
		log.Fatal(err)
	}
	fmt.Println()
}
