## 10:29 · decision · —   — Review fixes: nesting from the process tree, real nested test

The qa-reviewer found five issues. Each was fixed or recorded:

- Nesting trusted `QA_LOCK_HELD`. A gate that ran its children under `env -i` would make a nested
  run wait for its own parent, and with no default timeout that wait never ends. Nesting is now
  decided from the process tree alone. A run that finds the lock held inherits it when two
  things hold: the holder record's pid namespace (now stored in the record, read from
  `/proc/self/ns/pid`) is its own, and the record's pid is among its ancestors. The
  environment variable is gone. Tests cover a holder that is not an ancestor, the same pid in
  another namespace, and an unreadable namespace.
- The real nested chain was only covered through fakes. `TestANestedRunInheritsTheRealLock`
  (cmd/qa-lock) holds the real lock, runs a shell that `env -i`-execs the test binary, and that
  grandchild takes the same lock with `--no-wait`. It must exit 0, so it passes only if it
  inherits the lock rather than being refused.
- The plan claimed a child's I/O class was checked, but no test checked it. The real priority
  test now asserts `ionice -p` of a child is `idle`.
- QA.md overclaimed bind-mount sharing. It now says what the tests prove: two real processes
  exclude each other, and `TestTheLockBelongsToTheInodeNotThePath` shows a hard link, a second
  path to the inode, sees the holder. A real bind mount rests on flock's per-inode semantics.
  New Task 2.3 is a host check: a container run waits for a host run.
- If the holder is SIGKILLed, it frees the lock while its gates (separate process groups) keep
  running. This is documented as a limit in QA.md; Ctrl-C and SIGTERM end the gates first.
  Making the gates inherit the lock fd would change `proc.OS` for every gate, which is out of
  scope here.
- Nit: the "CI" example for `QA_FULL_PRIORITY` was dropped, because CI does not set it.
