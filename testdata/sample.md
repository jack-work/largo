# Welcome to Largo

This is a **streaming markdown renderer** for terminal applications. It wraps [glamour](https://github.com/charmbracelet/glamour) to provide real-time feedback.

## Features

- Stream tokens as they arrive from an LLM
- Raw text appears *immediately*
- Blocks get replaced with styled output
- Handles fenced code blocks correctly

## Code Example

Here's some Go code:

```go
package main

import "fmt"

func main() {
    fmt.Println("Hello from largo!")
    for i := 0; i < 5; i++ {
        fmt.Printf("  iteration %d\n", i)
    }
}
```

And here's some Python:

```python
def greet(name: str) -> str:
    """Return a greeting."""
    return f"Hello, {name}!"

if __name__ == "__main__":
    print(greet("largo"))
```

## A Table

| Feature      | Status |
|-------------|--------|
| Paragraphs  | ✅     |
| Code blocks | ✅     |
| Headers     | ✅     |
| Lists       | ✅     |
| Tables      | ✅     |

## Blockquote

> "The best way to predict the future is to invent it."
> — Alan Kay

## Final Notes

That's it! The rendered output should look much nicer than the raw markdown streaming across your terminal. Each block gets replaced as soon as its boundary is detected.
