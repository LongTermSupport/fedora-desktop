# 00228 Task 3.3: the attendant ends a run at an unanswered question

Branch `fix/00228-attend-idle`. Not pushed or merged.

## The defect

The host run at 3781e559f stopped at play-basic-configs' colour `pause`. The attendant had
no answer for it, so the run waited out provisionLimit (5400s) and exited 124. The host
transcript shows the prompt's last line is a bare `:`. Ansible appends `:` after a
multi-line prompt, so the line does not end in `": "`.

## Built

- `src-go/internal/vmtest/attend.go`. `Write` keeps the line still being printed: the
  output since the last newline, or since the last answered prompt. When that line is
  prompt-shaped, `Write` wakes one `watch` goroutine.
  - Prompt-shaped means it ends in `:`, `?`, `]`, `)` or `>`, with trailing blanks allowed.
  - `watch` sleeps `promptIdle` on `Env.Sleep`. If no write came in that time and the line
    is still prompt-shaped, it records the last 3 non-blank lines and stops the run.
  - Output during the wait starts the wait again, so a question ends the run between one
    and two bounds after it appears.
  - A finished line never wakes the watch, so a silent dnf or akmods step is not affected.
  - `attended` waits for the watch to end before it returns.
  - `attended` now returns a `halt` (refused or unanswered) where it returned the refused
    string.
  - It also drops its `limit` parameter. The only value ever passed was provisionLimit, and
    unparam flagged it.
- `steps.go`: `promptIdle = 60s`, with its reason in a comment beside it.
- `consumer.go`: an unanswered question writes this to the transcript:
  `ERROR: attended run N of 2 stopped at a question the harness has no answer for, and printed nothing for 60s; the harness ended it. Its last lines: "..."`.
  The status is 1 (fail). `refusedStatus` is renamed `endedStatus`.
- `docs/vm-acceptance-testing.md` says what happens on an unknown question.

## Why 60s

A real prompt prints nothing until it is answered, so any bound separates it from a run
that is still working. The bound is only there for a step that prints a label ending in `:`
and then works without printing a newline. Ansible prints whole lines, and run.bash has no
`echo -n`. 60s leaves room for such a step and still ends an unknown question 89 minutes
sooner than provisionLimit.

## Tests (red, then green)

Red was seen twice: first as compile errors, then against stubs (`promptShaped` returning
false, `watch` doing nothing). Against the stubs, these failed:

- `TestPromptShaped` and `TestAttendantWakesOnlyAtAnUnansweredPromptShape`
- three tests of `watch`: it ends a run at an unknown question, it waits again when output
  follows, and it leaves a run that was already refused alone
- `TestGuestAttendedUnansweredQuestion`
- `TestConsumerProvisionUnansweredQuestion`

`TestAttendantLetsASilentStepRun` passed against the stubs, as it should: a stub that never
ends a run cannot fail it. It guards against the fix ending a run too early. The existing
tests for known prompts and refused prompts passed throughout.

Green: `go test -race -shuffle=on ./internal/vmtest/` passes, with coverage at 90.8%.

No test reads the wall clock:

- The watch tests use a sleep the test holds and releases.
- The guest and consumer tests use the kit's fake `Env.Sleep`, and a runner that waits on
  its context. That runner has a `time.After` safety net that fails the test if it fires.

## Open

- The host proof is Task 3.2's re-run, which is already queued.
