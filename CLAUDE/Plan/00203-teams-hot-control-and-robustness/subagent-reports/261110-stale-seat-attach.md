# Add a running session to a stale seat (Task 3.9 follow-up, SPEC A11-A15)

The owner asked for Enter on a stale seat to offer attaching a running session to that seat
directly, without going through the team's own add and choosing the seat again.

## What was built

- `src-go/internal/teams/picker/addrunning.go`: a seat row (ccy or cc, known checkout) whose
  session is not running gets **Add a running session to this seat** (`actAddToSeat`), placed
  where A7's line sits. The team add and the seat add share one session chooser
  (`chooseRunning`: team refusal, listing, the no-session and none-eligible notes, the ask);
  each passes its own refusal function. The seat add refuses, besides A4's reasons, a session
  of another checkout or another kind.
- `src-go/internal/teams/picker/sessions.go`: `addSeatAndRole` split; its role step is now
  `addInRole`, which both the seat add and the existing flows use.
- The seat is not asked again. The role is: `session add` passes the role to an existing seat
  (`--role=worker`), so skipping the question would silently make a coordinator's seat a worker;
  the question starts on the role the seat holds and keeps the takeover path.
- A seat `session add` would refuse (H22's claims) says why on the menu line and asks nothing:
  a seat a live process holds, and a seat a stopped, restored record names. The second is the
  common shape of a stale seat after a crash or reboot; the line points to the menu's own Stop
  starting its session again after a reboot, which frees it.

## Tests (red first, then green)

`addrunning_test.go`: the menu line and its place; the flow (refusals for another checkout and
another kind, headings 1/2 and 2/2, `session add <s> alpha/dev --role=worker`); none eligible;
held and restored-record seats refused before listing; a non-restored record leaves the seat
free; the help text names the line.

## Docs

SPEC A11-A15, `docs/agent-bus.md`, the picker help, and a JOURNAL entry. PLAN.md's Task 3.9
line is not updated: PLAN.md is already past the plan-doc-size hard limit (35,000 bytes), so
the plan QA hook refuses any edit to it until it is trimmed (extract or split); that is the
coordinator's call.

## Open for the owner

- Seats claimed by a restored stopped record are refused, not taken over. Taking over would
  mean changing `session add`'s claim rule (hot, H22) or turning that record's restore off as
  part of the add; that is a decision, not done here.
- Suspected defect, not changed: `askSeat` (the existing team and session adds) asks
  `agent-bus seat choose` with `--type=podman` even for a cc session, where `session add`
  itself uses the cc member type. Unverified against agent-bus's real output.

## Deploy

No CCY version bump: nothing under `files/var/local/claude-yolo/` or in the image changed.
`ccy-teams` is built by `play-claude-yolo.yml`; `meta-deploy.list` already holds
`changed-plays`, which covers it.
