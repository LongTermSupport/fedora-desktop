# Plan 00266 Phase A, part 2: report

Commit `3a09d28ee` (pushed to v2-smoke), ccy 3.124.107. p266-c3 took 3.124.106 first.

## Done: item 1, the saved launch choices (`.claude/ccy/.last-launch.conf`)

- checkoutpin K5 (`lastlaunch.go`) does four things: it parses the file strictly; it writes through one
  `os.Root`, a temp file then a rename, so a planted link is replaced and never followed; it
  records the digest of the bytes it wrote; and it backs `ccy-teams approve last-launch`.
- The claude-yolo front (F16) is the launcher's only way to the file. It offers `--read-launch-config CHECKOUT attended|unattended`, `--write-launch-config` and `--launch-network [--forget]`. A launch
  nobody attends refuses content ccy did not write, naming the file and the verb. An attended
  launch warns. A forget of an unapproved file removes the file.
- Defence Before Fix:
  - The semgrep rule was red on HEAD at `claude-yolo:542,3370,3386` and
    `lib/network-management.bash:124`.
  - The Go rule was red on its fixture.
  - Both are green now and documented.
- The bash shrank, and the ratchet is tightened.
- Checks:
  - suites: test-ccy-teams 281/0, test-ccy-network-disconnect 145/0;
  - `go test -race` on ccylaunch, difftest, checkoutpin, registry and teams;
  - golangci-lint, the project rules and qa-patterns are all clean.
- Difftest goldens were re-recorded for one intended change: the approval record each launch now
  writes. Planted files can now be pinned, and there is a new scenario, `launch-quick-unapproved`.

## Left

Items 2 to 7 (A.4, A.5, git hardening, optional judgeRestore, the security review, the ticks). The
detail is in [261126-p266-A2-handoff.md](261126-p266-A2-handoff.md).
