// Command largo-demo streams a markdown file to the terminal with random
// chunk sizes, simulating LLM token-by-token output. Raw text appears
// immediately and gets replaced with glamour-rendered markdown as each
// block completes.
//
// Usage:
//
//	go run ./cmd/largo-demo testdata/sample.md
//	go run ./cmd/largo-demo -delay 30ms testdata/sample.md
//	echo '# Hello\n\nWorld' | go run ./cmd/largo-demo
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
	"time"

	"github.com/jack-work/largo"
)

func main() {
	delay := flag.Duration("delay", 20*time.Millisecond, "delay between chunks")
	minChunk := flag.Int("min", 1, "minimum chunk size in bytes")
	maxChunk := flag.Int("max", 12, "maximum chunk size in bytes")
	flag.Parse()

	// Read input: file arg or stdin.
	var input []byte
	var err error
	if flag.NArg() > 0 {
		input, err = os.ReadFile(flag.Arg(0))
	} else {
		input, err = io.ReadAll(os.Stdin)
	}
	if err != nil {
		log.Fatal(err)
	}
	if len(input) == 0 {
		fmt.Fprintln(os.Stderr, "no input")
		os.Exit(1)
	}

	sw, err := largo.NewWriter(os.Stdout, largo.Options{Margin: 4})
	if err != nil {
		log.Fatal(err)
	}

	// Stream with random chunk sizes.
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	pos := 0
	for pos < len(input) {
		chunkSize := *minChunk + rng.Intn(*maxChunk-*minChunk+1)
		end := pos + chunkSize
		if end > len(input) {
			end = len(input)
		}
		if _, err := sw.Write(input[pos:end]); err != nil {
			log.Fatal(err)
		}
		pos = end
		time.Sleep(*delay)
	}

	if err := sw.Flush(); err != nil {
		log.Fatal(err)
	}
}
