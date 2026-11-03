# Decision review: A10, A11, A12, B4 (2026-10-31)

Independent review by a Fable agent. Read: CLAUDE.md, the owner sheet, Plan 00192 Task 3.12,
Plan 00191 `dbf-report-command-failure-dropped.md`, `src-go/internal/teams` (picker, login,
create wizard, `internal/ui/picker.go`), `files/var/local/claude-yolo/entrypoint.sh` and
`Dockerfile`.

## A10: upstream guard blocks a word, not a file

**DECISION: file.**

Reason: the task is already fully specified (body from `hooks-daemon issue-report`, record the
number in PLAN.md and PORTS.md) and the workaround stays either way, so filing costs one
generated issue and closes nothing prematurely. A path-based guard is a genuine upstream
improvement, and the public tracker is the route the repo's own tooling is built for.

Risk if wrong: a public issue that upstream declines; nothing in this repo changes. Residual
exposure is a leak in the issue body, which the generator exists to prevent; the filer must
still read the body before posting.

## A11: the two wider checks not built

**DECISION: accept.**

Reason: all eight bash instances are fixed or moved, the launcher bash is on the ratchet
allowlist and under A7a is being retired to Go, where the rule already blocks the class on
arrival, so a bash detector would guard code that is leaving. The any-error rule cannot tell a
dropped cause from a deliberate sentinel branch (`errors.Is(err, fs.ErrNotExist)`) without
per-sentinel meaning, which is the report's honest finding, not an excuse.

Risk if wrong: a new bash instance lands in the launcher before its port and is caught only by
review; a Go `errors.Is`-then-fixed-message drop goes unflagged. Both are bounded by the
existing command-failure rule and by the shrink-only allowlist.

## A12: ccy-teams yes/no defaults

**DECISION: no (both default to No).**

Reason: the codebase's own convention, written in `src-go/internal/ui/picker.go` on
`Confirm`, is that the cursor starts on Exit unless the step is "harmless"; starting a session
and launching Element are not, and the sibling prompt in `internal/sessions/picker.go`
("Start ccy in ...?") already defaults to No. Changing the two named sites is consistent with
the code, and Yes costs one key.

Risk if wrong: one extra arrow key per intended start. Note for the implementer: the same two
questions also appear in `internal/teams/create/wizard.go` (lines 1084 and 1138, default
`true`) and the sign-in offer at `login.go:318`; change them together or the convention is
only half applied.

## B4: entrypoint GitHub host-key fetch, bash or Go

**DECISION: leave as bash (no "B4 Go").**

Reason: the block is about 35 lines in a 769-line entrypoint that is on the ratchet allowlist,
the fix shrank it and it already reports the real cause (curl's own text, or "malformed
answer"), so the defect is closed; a Go program whose only job is one curl plus jq is YAGNI
and would add a build step and a binary to the image for no behaviour the bash lacks. A7a's
direction is served by porting the entrypoint as a whole when that is planned, not by one
block.

Risk if wrong: the entrypoint keeps bash the Go rule cannot guard, so a regression of this
instance is caught by review only. One correction to the sheet: the premise "the image has no
Go program of ours" is not exact, since `pingbus-status-line` is a static Go binary built into
`/opt/claude-yolo/optional/agent-bus/` by the same play; that lowers the cost of a later port
but does not make this one worth doing now.
