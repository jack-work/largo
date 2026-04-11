// Command figaro-synth is a synthetic figaro harness: it produces the
// same kind of markdown stream a real figaro agent would emit for a
// typical coding-agent turn (user prompt header, LLM prose, tool
// markers, fenced tool results, headings, lists, blockquote), feeds it
// into a largo.Writer one random-size chunk at a time, and lets you
// see the result in your terminal.
//
// The goal: prove that largo is rock-solid in isolation before we
// rewire figaro's CLI to use it. If this looks right here, it will
// look right in figaro.
//
// Usage:
//
//	go run ./cmd/figaro-synth
//	go run ./cmd/figaro-synth -delay 15ms
//	go run ./cmd/figaro-synth -delay 0 -min 20 -max 40
//	go run ./cmd/figaro-synth -turn bash   # specific turn scenario
package main

import (
	_ "embed"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"os"
	"time"

	"github.com/jack-work/largo"
)

//go:embed turns/default.md
var defaultTurn string

//go:embed turns/code-heavy.md
var codeHeavyTurn string

//go:embed turns/tools.md
var toolsTurn string

var turns = map[string]string{
	"default": defaultTurn,
	"code":    codeHeavyTurn,
	"tools":   toolsTurn,
}

func main() {
	turnName := flag.String("turn", "default", "which canned turn to stream (default|code|tools)")
	delay := flag.Duration("delay", 10*time.Millisecond, "delay between chunks")
	minChunk := flag.Int("min", 1, "minimum chunk size in bytes")
	maxChunk := flag.Int("max", 12, "maximum chunk size in bytes")
	list := flag.Bool("list", false, "list available turns and exit")
	flag.Parse()

	if *list {
		fmt.Fprintln(os.Stderr, "available turns:")
		for name, content := range turns {
			fmt.Fprintf(os.Stderr, "  %-10s %d bytes\n", name, len(content))
		}
		return
	}

	content, ok := turns[*turnName]
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown turn %q; available: ", *turnName)
		for name := range turns {
			fmt.Fprintf(os.Stderr, "%s ", name)
		}
		fmt.Fprintln(os.Stderr)
		os.Exit(1)
	}

	sw, err := largo.NewWriter(os.Stdout, largo.Options{})
	if err != nil {
		log.Fatal(err)
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	buf := []byte(content)
	pos := 0
	for pos < len(buf) {
		chunkSize := *minChunk + rng.Intn(*maxChunk-*minChunk+1)
		end := pos + chunkSize
		if end > len(buf) {
			end = len(buf)
		}
		if _, err := sw.Write(buf[pos:end]); err != nil {
			log.Fatal(err)
		}
		pos = end
		if *delay > 0 {
			time.Sleep(*delay)
		}
	}
	if err := sw.Flush(); err != nil {
		log.Fatal(err)
	}
	fmt.Println()
}
