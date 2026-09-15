# DESIGN — restarting an AI agent session unattended, and what goes wrong

This feature restarts agent sessions **with no human watching**. Every decision below is
recorded with the failure it prevents, the alternatives rejected, and how it is verified.
The facts it builds on are in [FACTS-ccy-mechanics.md](FACTS-ccy-mechanics.md) (F1…F10).

The governing principle, and the one this repository has been bitten by most often: **"could
not tell" and "nothing to do" must be different answers.** Every mechanism here is shaped to
keep those two apart.

---

## The state tree

```
${XDG_STATE_HOME:-$HOME/.local/state}/ccy/
  sessions/                      live records, one file per tmux session
    ccy-<project>.record           named after the TMUX SESSION, which is unique on the
    .ccy-<project>.record.tmp.<pid>   server by construction; the temp form never matches *.record
  restore/
    attempted/<name>.record      consumed at boot, before the start (D2)
    retired/<name>.record        not restorable; reason inside (D4)
    malformed/<name>.record      failed validation; quarantined, not deleted (D3)
    last-run                     the boot id and verdict of the last restore run
```

`sessions/` is mode `0700`. A record names a project directory and a token *name* — never a
token value — but a directory path is still information about the machine, so the tree is not
world-readable.

---

## D1 — A record carries its boot id; a record from the current boot is live, not restorable

**Failure prevented.** The restore service runs and finds records for sessions that are
*running right now* — because someone ran it by hand, because systemd restarted it, or
because a future change gave it a timer. Restoring those duplicates live sessions, and
"restore" would then mean "start a second claude on the same conversation state", which is
precisely the one-terminal-per-session invariant Plan 00111 went to some trouble to make
unconditional.

**Decision.** Every record stores `boot_id` from `/proc/sys/kernel/random/boot_id`. The
restore reads the current boot id once and treats any record matching it as `LIVE` — reported,
untouched, not an error. Only a record written under a *different* boot can be a survivor of a
machine that went down.

**Rejected.** Probing whether the tmux session named in the record exists. That answers a
different question: a session can exist because the restore *just created* it, so the probe
would be true for exactly the records it must not act on twice. The boot id is a fact about
when the record was written, which is what is actually being asked.

## D2 — The restore consumes a record before starting it, so a retry loop cannot exist

**Failure prevented.** A restored session that crashes immediately on start, restored again,
crashing again — for ever. This is the failure mode the task brief asks about directly.

**Decision.** The restore **moves** the record into `restore/attempted/` and only then starts
the session. After the move there is nothing in `sessions/` to retry. If the new `ccy` gets as
far as its container, it writes its own fresh record under the current boot id (so D1 protects
it); if it dies before that, nothing is left behind and nothing accumulates.

The consequence is deliberate: **a restore is attempted exactly once, and a failed restore
loses the session.** That is the right trade for an unattended mechanism, and it is not silent
— the record survives in `attempted/` as evidence, the journal holds the reason, and
`restore-status` names it with the manual way back (`cd` to the directory and run
`ccy --continue`).

**Rejected.** A per-record attempt counter with a cap. It was designed, then dropped as dead
weight: consuming the record already makes a loop structurally impossible, so a counter would
have counted to one and never fired. YAGNI, and a mechanism that can never trigger is worse
than no mechanism because a reader believes it is doing something.

**Second brake, independent of the first.** `Type=oneshot` with no `Restart=`, so systemd
never re-runs it within a boot. D1 and D2 fail independently; either alone is sufficient.

## D3 — Atomic `rename` plus a required terminator; a `.tmp` file is never a record

**Failure prevented.** The brief asks what reads a half-written record at boot. Two ways a
record can be partial: the writer is killed mid-write, or the machine loses power mid-write.

**Decision, in three layers.**

1. The writer writes `sessions/.<name>.record.tmp.$$` and then `mv -f`s it onto
   `sessions/<name>.record`. Same directory, so this is `rename(2)`, which is atomic: a reader
   sees either the old record or the new one, never a mixture.
2. The reader globs `*.record`. The temp name begins with a dot **and** carries a `.tmp.<pid>`
   suffix, so an abandoned in-flight write is invisible to every reader, for two independent
   reasons.
3. The record's last line is a literal `end=1` terminator, and the reader **requires** it.
   This is belt-and-braces against a write that did not go through `rename` at all — a
   hand-edit, a restore from backup, a filesystem that reordered — where layers 1 and 2 have
   nothing to say.

**A record that fails validation is quarantined, never skipped.** Missing terminator, unknown
`schema`, absent required key → the file is moved to `restore/malformed/`, the reason is
printed, and the run exits non-zero. Skipping it would be the "skip and warn" pattern this
repository bans; deleting it would destroy the only evidence of why.

**What writes the record if `ccy` is killed mid-start? Nothing does, by design.** The write
happens at `claude-yolo:~2881`, immediately before `container_cmd run` — after every prompt,
every validation and every resolution. A launcher killed before that point never had a session
to restore, so leaving no record is the correct answer rather than a gap. Removal extends the
existing `cleanup` EXIT trap (F8), so it happens however the launcher exits: normal exit,
`claude` crashing, or a signal.

## D4 — Every non-restore is a retirement with a reason, kept as evidence

**Failure prevented.** A record that quietly disappears. The operator comes back to a machine
that restored three of four sessions and has no way to learn which one, or why.

**Decision.** A record the restore will not act on is moved to `restore/retired/` with a
`retired_reason=` line appended, printed to the journal, and counted in the run summary. The
reasons are a closed set:

| Reason               | Meaning                                                     | Checked with                                    |
| -------------------- | ----------------------------------------------------------- | ----------------------------------------------- |
| `no-restore`         | started with `ccy --no-restore`                             | the record's own field                          |
| `directory-gone`     | the project directory no longer exists                      | `[ -d ]`                                        |
| `not-a-git-checkout` | it exists but is not a git work tree, so `ccy` would refuse | `git -C … rev-parse --is-inside-work-tree` (F9) |
| `different-project`  | the directory was reused for another repository             | root-commit fingerprint (D7)                    |
| `stale`              | older than `CCY_RESTORE_MAX_AGE_DAYS` (default 7)           | record mtime                                    |
| `malformed`          | failed validation (D3)                                      | reader; goes to `malformed/`                    |

**Exit-code semantics.** The service uses the *gather* semantics of
`CLAUDE/PlanScriptStandards.md` R7: it processes every record, records every failure by name,
and exits non-zero if any record could not be resolved. A deliberate retirement is a
*resolved* outcome and does not fail the run; a malformed record or a start that errored does.
Aborting on the first problem would leave healthy sessions unrestored, which is the wrong
trade for a boot-time service — but exiting zero on a failure would be the silent skip the
repository bans. Both halves matter.

## D5 — The record stores the resolved configuration by value, not argv

**Failure prevented.** Restoring a session with the wrong configuration — no token, the wrong
SSH key, the wrong network — or hanging on a prompt the missing flag would have avoided.

**Decision.** The record captures the launch configuration **by value**, at the point where
`save_launch_config` already assembles it: token name, SSH key paths, the ssh-agent sentinel,
`no_ssh`, network name, `no_network`, `github_443`, engine, `disable_custom_docker`. The
restore reconstructs flags from those fields. F4 explains why argv is the wrong source: the
quick-launch path supplies all of that configuration with an empty argv, so a record built
from argv would describe nothing about the majority of real sessions.

The original argv is recorded too, base64 of the NUL-joined vector, purely as evidence. Nothing
reads it to build a command. Base64 because a launch argument can contain spaces, quotes and
newlines, and a line-based record cannot carry those faithfully otherwise.

**The supervisor mode is one of the recorded values, and is HONOURED.** Issue 44 asks for
restore with `--supervise`, and for a session that expressed no preference that is right: the
default supervisor is unarmed, and an unattended session needs the arming to be nudged back to
work. But `ccy --no-supervise` is an explicit opt-out of the supervisor *entirely* — ctrl+z
guard included — so restoring such a session armed would hand back auto-compaction and goal
injection the operator deliberately turned off, silently. An unrecognised value falls back to
armed rather than off: losing the ctrl+z guard on a session that never asked to lose it is the
worse of the two errors.

**A caveat that cost three defects.** Several of these values are NUL-delimited on the wire, and
`$(…)` strips NUL bytes. Capturing such a payload into a variable silently glues the elements
together — it did exactly that to the record list and to a multi-key SSH configuration. Where
both the data and the exit status are needed, the read goes through a temp file or a second
pass; never a command substitution.

**The staleness guard, which is the real point of this decision.** A hand-written list of "the
flags that matter" is a list that goes stale — `CLAUDE/ContainerRules.md` records two separate
incidents in this very program where an enumeration of its own parts was a file short of the
truth, one of them for two months. So `scripts/test-ccy-session-registry.bash` **derives** the
flag set from the launcher's own parser and asserts that every flag is classified as either
durable or one-shot. A flag added to `ccy` without a classification fails `qa-all.bash` at
commit time, with a message telling the author to decide. The list can still be wrong, but it
can no longer be silently incomplete.

## D6 — `CCY_UNATTENDED=1` plus a `read()` shadow keyed on `-p`

**Failure prevented.** F6 in full: a restored session parked for ever on
`Use same configuration? [Y/n]`, present in the picker, looking restored, with no `claude`
running behind it. This is the largest risk in the feature and the issue does not mention it.

**Decision.** The restore exports `CCY_UNATTENDED=1`. The launcher defines a function named
`read` that shadows the builtin:

- called **with `-p`** (a prompt addressed to a human) while `CCY_UNATTENDED=1` → print the
  prompt text and a fatal explanation, and exit non-zero;
- **any other** call — `read -ra` splitting a string, `while read` over a pipe, `read` in a
  library loop — falls through to `builtin read "$@"` unchanged;
- with `CCY_UNATTENDED` unset, every call falls through. An attended `ccy` is bit-for-bit
  unaffected.

**Why one seam rather than seventeen guards.** F6 lists seventeen prompt sites reachable on a
restore. Guarding each individually leaves the eighteenth — added next year by someone who has
never read this plan — unguarded, and its symptom is a silent hang. Keying on `-p` is a
property of *what a prompt is*, so it covers sites this plan never enumerated. It is the same
"derive it, do not enumerate it" lesson as D5 and as the `CCY_HASH` history in
`CLAUDE/ContainerRules.md`.

**Two unattended defaults, because a default is honest there.**

- **Quick launch is accepted.** The saved configuration *is* the recorded one — the record and
  `.last-launch.conf` are written by the same code path at the same moment — so accepting it is
  not a guess. Printed, so the journal says it happened.
- **Compose services are declined**, both the start prompt and the stop prompt. Starting a
  project's service stack unattended at boot is a side effect well outside "resume my
  conversation", and failing the whole restore because a project has a compose file would make
  the feature useless for most real projects. Printed, and documented in `docs/ccy.md`.

Everything else is fatal. A token that needs renewing, an ambiguous network, a container-engine
health problem — those need a human, and saying so in the journal is the correct outcome.

## D7 — The project's root commit is the fingerprint

**Failure prevented.** The brief asks what happens if `--continue` resumes a conversation
belonging to a different project because the directory was reused. The state lives in
`<dir>/.claude/ccy` (F2), so a directory that was deleted and re-used for another repository
would hand the new project the old project's conversation.

**Decision.** The record stores `root_commit`, the output of `git rev-list --max-parents=0 HEAD`
(first line). At restore, it is recomputed; a mismatch retires the record as
`different-project`. The root commit is stable across rebases, branch switches, remote renames
and re-clones, and differs between unrelated repositories.

**The no-commits case, handled explicitly.** A repository with no commits has no root commit,
so the record stores the sentinel `no-commits`. Comparison then runs only when **both** sides
have a real hash: a record with the sentinel is accepted, and the report says the fingerprint
could not be compared. The alternative — treating `no-commits` → `abc123` as a mismatch —
would retire a session merely because the project made its first commit, which is not a
different project.

**Not solvable, and stated rather than hidden.** If the conversation state itself is corrupt,
nothing here can tell: the format belongs to Claude Code. The bound is that `claude` exits
non-zero, the tmux trampoline holds the window open with the status
(`lib/tmux-session.bash:315-316`), the session is visible in `ccy-sessions` as a failed one,
and D2 guarantees it is not retried. A bounded, visible failure rather than a prevented one.

## D8 — Enablement and record count are reported independently

**Failure prevented.** The defect shape the brief names explicitly, and the one this
repository has hit repeatedly: collapsing "cannot tell" into "nothing to do". On a machine
that never opted in, "no sessions will be restored" must not be the same sentence as "there
were no sessions to restore".

**Decision.** `ccy-sessions restore-status` reports two axes separately and never multiplies
them into one verdict:

*Is restore set up?* — derived, in this order, so each answer is its own:

| Answer                    | Condition                                                      | What it tells the operator                                                                                                                                                                                                                                  |
| ------------------------- | -------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| `not-installed`           | the unit file is absent                                        | run the play with `-e ccy_restore_sessions=true`                                                                                                                                                                                                            |
| `installed-not-enabled`   | unit present, `is-enabled` says `disabled`/`static`/`indirect` | the play did not finish, or it was disabled by hand                                                                                                                                                                                                         |
| `installed-state-unknown` | unit present, `is-enabled` failed for any other reason         | **could not tell** — a masked unit, an unreachable user bus, or the dangling `.wants` symlink a delete-without-disable leaves. None of those is fixed by re-running the play, which is why folding it into the row above sent an operator somewhere useless |
| `enabled-no-linger`       | enabled, but `loginctl` Linger is `no`                         | **it will not run at boot** — the silent-failure shape                                                                                                                                                                                                      |
| `enabled-linger-unknown`  | enabled, but the linger probe itself failed                    | **could not tell** whether it runs at boot                                                                                                                                                                                                                  |
| `enabled`                 | enabled and lingering                                          | it will run                                                                                                                                                                                                                                                 |

**Six answers, not four**, and two of them exist purely to keep "could not tell" out of the
other four — which is this decision's whole point, applied to itself. The first draft had four,
and the two missing were both the unknowns.

Every one is driven by `scripts/test-ccy-sessions-status.bash` against a stub
`systemctl`/`loginctl`: the broken states cannot be reached any other way, and a state that can
only occur in production is a state verified by reading.

*What is in the registry?* — the record count, and separately the contents of `attempted/`,
`retired/` (with each reason) and `malformed/`, plus the last run's boot id and verdict.

So a non-opted-in machine with three live sessions says "3 sessions are recorded, but restore
is `not-installed`, so nothing will be restored" — two facts, not one shrug. `ccy` writes
records regardless of whether restore is enabled, which is what makes that sentence possible.

`enabled-no-linger` earns its own row because it is the quiet one: everything looks correct,
`is-enabled` says `enabled`, and the unit simply never runs because the user manager is not
started at boot. `play-systemd-user-tweaks.yml` already enables linger and blocks on
`user@UID.service` signalling ready, so on a host provisioned by `playbook-main.yml` this
holds — but a hand-built machine can have the unit enabled and the linger absent, and the
status command is where that is caught.

**Records from before an opt-in.** Because `ccy` always records, enabling restore on a machine
with an old surviving record would otherwise resurrect a session from an arbitrary past boot.
`CCY_RESTORE_MAX_AGE_DAYS` (default 7) retires anything older as `stale`, reported like every
other retirement.

**And that age is measured BOOT-TO-BOOT, which the first implementation got backwards.** It
used the record's own mtime — the moment the *session started* — so a session running
permanently for a fortnight was retired as stale at the very reboot this feature exists to
survive, while one started an hour before a reboot six months ago sailed through. The question
being asked is *how long ago was the boot this record belonged to*, so each record carries its
boot's start time (`/proc/stat`'s `btime`) and the comparison is against the current boot's. A
record with no `boot_time` cannot have its age established at all: it is restored with the age
reported as unknown rather than retired, because silently dropping a session whose age is
unknowable is the same collapse pointing the other way, and the safe direction here is the one
that does not lose work.

## D9 — The restore starts tmux under `systemd-run --user --scope`

**Failure prevented.** F10: a `Type=oneshot` service's leftover processes are killed when the
service completes, under the default `KillMode=control-group`. A `tmux new-session` forked
directly from the restore service would therefore have its server killed moments after
starting it — a restore that destroys exactly what it restored, and the symptom is "restore
does not work" with nothing in the journal to say why.

**Decision.** Each session is started with

```
systemd-run --user --scope --collect --unit ccy-tmux-restore-<slug> -- \
  tmux -L ccy new-session -d -s <name> -c <dir> -- <launcher> <flags> --supervise --continue
```

which is the same mechanism `ccy_tmux_insulate` already uses, for the same reason. `--collect`
lets systemd forget the scope once it is empty, whatever the exit status.

**Rejected.** `KillMode=process` on the service. It would work, but it makes the service's
correctness depend on a unit-file setting whose connection to "the tmux server must outlive
this" is invisible at the point where the server is started. The `--scope` form carries its
reason at the call site and matches the established pattern in this codebase.

**Note on the second and subsequent sessions.** The first `new-session` starts the server
inside its own scope; later ones connect to that existing server, so their sessions live in
the first scope's cgroup. That is identical to what happens today when a second `ccy` joins a
server the first one started, so it needs no special handling.
</content>
