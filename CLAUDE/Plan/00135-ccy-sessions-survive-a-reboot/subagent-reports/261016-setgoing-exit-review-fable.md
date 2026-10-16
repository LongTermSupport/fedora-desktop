# Review: set-going fails only for a fault (fix/setgoing-exit at 9c0b0364)

Adversarial, read-only review of Plan 00135 Task 11.1 against F44.

## Verdict: FIX-FIRST

One blocking finding. Everything else holds: the default-to-fault is real in code, the log
lines are clear, the SPEC 4.7 table matches the code, the deploy queue entry is right, no
install-specific names, and both suites pass (`go test ./internal/sessions/` and `go vet`
clean; `scripts/test-ccy-sessions-reboot.bash` 284 passed, 0 failed).

## Blocking

### B1. `busy-before-set-going` is called "already working" on one look, with no evidence

`src-go/internal/sessions/setgoing.go:511-512` (`notReady`: `lookBusy` is terminal, not a
wait) and `:68-69` (`expectedWhy`: `lookBusy` -> `whyBusy`, "it was already working, so there
is nothing to set going").

What the code can see: `screen.ClaudeState` returns `Busy` when "esc to interrupt" is in the
four lines above a drawn box (`internal/screen/screen.go:101-123`). That is a spinner, not
input. A spinner on a session nobody has typed into is one of:

- a startup-time spinner on a `--continue` launch (hooks running, the conversation loading)
  that ends a few seconds later at an empty prompt;
- a turn that hung, which looks the same for ever.

In both the session is left alone on the first look, is never looked at again, is recorded
`untouched`, and the unit exits 0 with "that is not a fault". The session then sits at an
empty prompt (case 1) or hangs (case 2) until a person notices. Before this branch the unit
exited 1 for it, which was noise but was loud; now the same state is silent at the unit
level. This is exactly the masking the owner asked to be checked for, and it is the one
expected reason the code asserts rather than observes: attached is a tmux fact, typed is
text in the box, waiting is a known prompt string, but busy is inferred.

The tool already has the evidence it needs. `transcript.Cache.TookInputSince(path, since)`
(`internal/transcript/cache.go:184`) is what `verify-restore` uses to tell input taken from
none (`internal/sessions/verify.go:232`), and `setOne` resolves the transcript file anyway
(`transcriptOf`, `:462`).

Fix, one path, no fallback:

1. In `setOne`, a `lookBusy` is not terminal. Resolve the transcript first (the
   `no-transcript` fault stays as is), then ask `TookInputSince(file, since)` with `since`
   the restore's start for this boot (the manifest's write time is the earliest sound bound;
   the set-going run's own start is too late when a supervisor types before this unit runs).
2. Input taken -> `leaveAsExpected(name, pending, "busy-before-set-going", whyBusy)` —
   now a fact, not a guess. Reword `whyBusy` to say what was seen: "it has already taken
   input since the restore, so there is nothing to set going".
3. No input taken -> `g.waited[name] = waitedOn{reason: "busy-without-input"}` and return
   (a wait, like `prompt-not-drawn`). A spinner that ends at an empty prompt is then set
   going on a later pass, which also repairs the lost-session case above. One still
   spinning at the deadline is `busy-without-input-after-<N>s` with an empty why, so
   `giveUp` makes it a fault.
4. Tests: `TestSetGoingLeavesABusyOrAttachedOrTypedOrUndrawnSessionAlone` "busy" case splits
   in two (input taken -> expected, exit 0; none -> waits, then set going when the screen
   turns ready; none at deadline -> fault, exit 1). `scripts/test-ccy-sessions-reboot.bash:1122-1125`
   pins the same. SPEC 4.7 step 04 and the table, and `docs/ccy.md` "or it is already busy",
   change with it.

The after-compaction `lookBusy` branch (`:557-560`) is unchanged by this branch and is a
different case: the session was typed `/compact` by this run, so input is known. Leave it.

## Not blocking, worth a line

- `lookWaiting` after a compaction is now expected (`:564-565`, test "waiting" ->
  `token-select-after-compaction`, exit 0). A launch prompt after this run typed `/compact`
  means the container left the conversation (a restart request, or Claude exited and the
  launcher re-ran). Leaving it alone is right because only a person can answer it, so
  expected is defensible, but the journal line's why ("it waits at a launch prompt only a
  person can answer") does not say the compaction was cut short. Consider a why for the
  post-compaction form that says the session restarted during its compaction. Judgement call;
  not blocking.
- Default-to-fault is real: `expectedWhy` lists every kind and returns `""` otherwise;
  `leave` routes an empty why to `leaveAlone`. No test pins it directly. A one-line unit test
  `look{kind: lookKind(99)}.expectedWhy() == ""` would make the promise in the journal
  ("an unclassified state is loud") a proof instead of a reading.
- `shares-conversation-with-<owner>` is sound: `conversationOwner` only returns an entry this
  run or an earlier one set going (`compacting|compact|continue`) on the same transcript
  file. An owner that later dies is `verify-restore`'s to report, as before.
- Log lines: `left <name> alone, as expected: <reason>; <why>` and
  `left <name> untouched: <reason>` are distinct at a glance, the count line precedes the
  `ERROR:` line, and every fault is named in the failure line. Good.
- SPEC 4.7: the table, step 04, step 06, `continue_after_compaction`, `leave_alone` and
  `leave_expected` match the code line for line. The departure note carries the date and
  plan.
- meta-deploy: the queued `play-claude-yolo.yml` rebuilds `ccy-sessions` through
  `tasks/build-go-command.yml`; extending its comment rather than adding a row is right. No
  CCY version bump is due (no `files/var/local/claude-yolo/` change).
- Names: no hostnames, users, paths or container names in the diff. The journal says "the
  desktop health notice", nothing more specific.
- `files/home/.config/systemd/user/ccy-sessions-set-going.service.j2` comment still reads
  true (it describes only the no-manifest exit 1).
