---

**> walk me through the actor loop**

---

Sure. The agent's actor loop lives in `internal/figaro/agent.go` and runs on a single goroutine so that all event handling is serialized. Here's the core loop, simplified:

```go
func (a *Agent) drainLoop(ctx context.Context) {
    for {
        select {
        case <-ctx.Done():
            return
        case evt := <-a.inbox:
            a.handle(ctx, evt)
        }
    }
}
```

Every event — user prompts, LLM deltas, tool results, errors — arrives through the same channel. This eliminates races without any locks.

## Event types

There are five event kinds the loop dispatches on:

1. **`eventUserPrompt`** — a new prompt from the CLI. If a turn is already active, it gets buffered in `pendingPrompts` and re-enqueued on `turnComplete`.
2. **`eventLLMDelta`** — a chunk of streaming text from the provider. Fanned out to subscribers (including the CLI's largo writer) as a `stream.delta` notification.
3. **`eventLLMDone`** — the provider finished its stream. If there are tool calls in the final message, the loop schedules them; otherwise the turn ends.
4. **`eventToolResult`** — a tool finished executing. Its result is appended to the store and the loop starts the next LLM stream with the updated context.
5. **`eventLLMError`** — the provider failed. The error is fanned out and the turn ends.

## Turn generation counters

Every event carries a turn generation counter. When a turn ends (via `endTurn`), the generation is bumped. Any stale events still in the inbox from the previous turn get silently dropped:

```go
if evt.gen != a.currentGen {
    continue // drop
}
```

This matters after a panic recovery: the drainLoop restarts with a fresh context and a new generation, and in-flight goroutines from before the panic become no-ops when their events land.

> The invariant is: **at any given moment, exactly one generation is live**. Events from older generations are garbage.

That's the whole pattern. It's remarkably simple compared to the fan-out/fan-in goroutine mesh you'd get trying to do this with multiple workers.

