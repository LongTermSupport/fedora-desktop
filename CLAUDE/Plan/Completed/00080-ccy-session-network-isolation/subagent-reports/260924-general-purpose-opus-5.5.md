# Plan 00080: run 4 triage findings, threat model, Task 2.2 prep (opus-5.5)

Tasks 1.3 and 2.1 are done. Task 2.2 is set out for the owner and left open, marked
"awaiting the owner". Commit `3bd2a074` on F44 is pushed.

## What run 4 settled (findings F29–F38)

- **H1 is confirmed on the host.** P6 got `REACHED`: one container on the shared bridge can
  open a TCP port on another, by IP.
- **H2 is confirmed again.** The shared network has DNS off, so reach is by IP only.
- **H3: nothing a neighbour can reach is listening,** for the third snapshot in a row (5 of 5
  sessions). The one listener is a database bound to loopback only.
- **H5 is only partly confirmed.** A fresh network has outbound access and resolves the host
  alias, but only a network with default options was tested, not one set up the way a
  per-session network would be.
- **H4 is not settled,** so success criterion 1 stays unticked.
- **New from the code (F31):** a saved network preference is written only by `--connect`, and
  on later launches it is applied at startup. So the 4 projects that have one join their
  network at launch without needing the shared bridge. What the bridge buys is the first
  attach of a session that is already running. That narrows F28's "Option 4 would break real
  use".

## Probes recorded as broken, not as results

- **P11 measured nothing.** Its container was started detached and without `--rm`. The client
  had already exited, so the kill matched no process (rc=1). "Running" just means `sleep 300`
  was still running. The only valid fact is that removing a network is refused (rc=2) while a
  container is attached. That is documented behaviour and says nothing about leaks.
- **P7 can't be read as isolation.** Its listener accepts one connection and P6 used it up.
  The silence also contradicts what upstream reports for netavark 1.17.
- **P12** looked in the wrong directories, so "no such directory" is not absence of configs.
  **P10** left out the DNS servers field.

`triage.bash` is **not** fixed. The fixes and a re-run are a Task 3.1 precondition, needed
only if Task 2.2 picks an option that creates networks.

## Threat model

Little risk in practice today, but one bind flag away from a real exposure. A dev server or
database bound to `0.0.0.0` in any session would be silently reachable by every other
session: 5 different projects across two GitHub identities. Tokens and SSH keys have no
network surface.

## Task 2.2 options (`DECISIONS.md`)

1. **Keep the shared default and document it.** Nothing to build. Reverse if a P4 run (ideally
   while a dev server is up) finds a `0.0.0.0` listener, or a non-CCY container joins the
   bridge.
2. **Option 4: remove the override, default to pasta.** Isolated by design, nothing created so
   nothing leaks, saved preferences still join at launch. Loses `--connect` on a session
   launched with no network; the launch preflight needs moving. Reverse if attaching running
   sessions is common.
3. **Option 4b (new):** Option 4, but `--connect` on a pasta session fails fast and saves the
   preference first, so a relaunch joins. The lost capability becomes a restart.
4. **Option 2: a network per session.** Keeps `--connect`. Needs explicit `--opt isolate=` with
   a sound isolation test, needs `--disable-dns`, and is gated on a fixed P11 (leak rate
   unknown). Splits `podfreeze`'s menu into one row per session.
5. **Option 5: a network per project.** Same machinery and caveats as 2, harder "last session
   out" cleanup.

Deciding question: how often does the owner attach a running session to a project network,
rather than launching into it? Options 3, 6, 7 and 8 are ruled out on evidence.

## Other notes

- Anonymisation scan clean (only match: the plan's pre-existing `Owner:` line). Pre-commit
  scanner passed.
- The journal's auto-generated "plan scaffolded" seed entry was replaced with a preamble note,
  as in the 09-23 day-file, because the plan dates from 08-20. The plan-QA hook flagged this
  as an edit to journal history.

## Files

In `<repo>/CLAUDE/Plan/00080-ccy-session-network-isolation/`:

- `PLAN.md`
- `THREAT-MODEL.md`
- `DECISIONS.md`
- `subagent-reports/260924-triage-findings-opus-5.5.md`
- `JOURNAL/00080-Journal-26-09-24.md`
