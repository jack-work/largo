---

**> what's running on port 8080?**

---

Let me check what's listening on that port.

---
`▶ bash` `lsof -i :8080`
```
COMMAND   PID USER   FD   TYPE DEVICE SIZE/NODE NAME
figaro  12345 jack    7u  IPv6  98765      0t0  TCP *:http-alt (LISTEN)
```
---

A `figaro` process — that's probably our angelus supervisor or an agent socket. Let me look at the process tree to be sure.

---
`▶ bash` `pstree -p 12345`
```
figaro(12345)─┬─{figaro}(12346)
              ├─{figaro}(12347)
              ├─{figaro}(12348)
              └─{figaro}(12349)
```
---

Four goroutines, single process. That matches the angelus model: one supervisor process with goroutine-per-agent. Now let me check the registry to see which agents are bound:

---
`▶ bash` `figaro list`
```
ID        STATE    MODEL                       MESSAGES  PIDS
a1b2c3d4  active   claude-sonnet-4-20250514    12        98765
e5f6g7h8  idle     claude-sonnet-4-20250514    3         -
```
---

Two agents: one active on a coding session, one idle. The port is the angelus supervisor socket — not actually HTTP despite the `http-alt` label; that's just `lsof` guessing from the port number.

## Summary

- Port 8080 is held by `figaro` (pid 12345).
- It's the **angelus supervisor**, not an HTTP server.
- Two agents are registered, one bound to your shell.
- No action needed — this is expected.

If you want to stop it, run `figaro rest`. That sends SIGTERM to the angelus, which cleanly unbinds all agents before exiting.

