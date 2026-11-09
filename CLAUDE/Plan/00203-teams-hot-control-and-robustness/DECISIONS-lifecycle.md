# Plan 00203: the whole-team lifecycle, rules and decisions (Tasks 3.15 to 3.19)

PLAN.md lists each decision in a line or two; this is the full text. Decision 13 is the
owner's. Decisions 14 to 24 are decided: their defaults stand (the owner, 2026-10-28, "all
recommended" to the unblocking list; PLAN.md "Decisions"), and the owner may still reverse any. The
expectations themselves are in the SPEC each task names:

- 3.15 and 3.16: [SPEC-team-wind-down.md](SPEC-team-wind-down.md) sections 5 and 6;
- 3.17: [SPEC-team-add-and-restart.md](SPEC-team-add-and-restart.md) section 6;
- 3.18: [SPEC-lxc-connect-and-resume.md](SPEC-lxc-connect-and-resume.md);
- 3.19: [SPEC-hot-control.md](SPEC-hot-control.md) section 9.

## 1. Rules common to all four tasks

### Offline members

The owner's rule, CLAUDE/AgentNotes.md "An offline team member never blocks anything".

- CR1. A member whose container is stopped, or that the last apply held back, is shown as
  `offline (container <name> stopped)` and blocks nothing. The rest of the operation goes on,
  and its exit code ignores it.
- CR2. Only an operation that explicitly targets that one member (Attach on its row,
  `ccy-teams resume <team> --member <handle>`) reports it, as exit 69 (`EX_UNAVAILABLE`, a new
  `term` exit code).
- CR3. Offline is not "failed". A member that is online and does not start, or cannot be
  typed to, is still a failure (exit 1).

### One lifecycle operation per team at a time

- CR4. `wind-down`, `shutdown`, `restart` and `resume` each take a per-team lock, held by the
  process doing the work for its whole run, so a dead process frees it. A second one for the
  same team is refused, exit 1, naming what holds it and since when. While a timed wind-down
  (3.16) is pending it also names `ccy-teams wind-down --cancel <team>`. So a pending wind-down
  can never kill sessions that a restart or resume started after it began.
- CR5. The lock is `src-go/internal/teams/hot/lock.go` `takeLock`, moved to a package both use:
  an `flock` taken without waiting, opened with `O_NOFOLLOW`, the holder's line written into
  the file. It lives in the owner's runtime directory, `$XDG_RUNTIME_DIR/ccy/lifecycle/<team>.lock`,
  beside H20's locks and never where a container can reach (an `flock` needs no write
  permission, so a lock in a mounted directory could be held for good from inside). A transient
  user unit runs under the same user manager, so it sees the same directory.
- CR6. The holder's line records its pid, the pid's start time (`/proc/<pid>/stat` field 22),
  its kind (unit or foreground), the unit's name as started (for a unit), the operation, and
  since when.
- CR7. `ccy-teams wind-down --cancel <team>` is exempt from the lock and acts on the holder.
  It first tries a non-blocking `flock` on the lock file. If that succeeds, nothing holds it:
  it releases it at once, says no timed wind-down is pending (exit 1) and touches nothing. Only
  when the `flock` fails (the lock is held) does it read the holder's line. A holder that is not
  a timed wind-down is named, exit 1. For a unit it stops the unit named in the line, never one
  derived from the team's name. For the foreground countdown it compares the recorded start
  time with the pid's own; only when they match does it send SIGTERM, which the countdown takes
  as a cancel. A mismatch is refused, naming both, so a reused pid is never signalled.
- CR8. A timed wind-down run in a unit does all its work in the unit: the typing (W6's wait for
  idle included) as well as the wait and the force. The invoker only checks the lock (a
  non-blocking try, released at once, so a held lock is refused before anything starts),
  starts the unit, and shows its progress. The unit's process takes the lock as its first step
  and fails, typing nothing, if it cannot. So there is no handoff in which a `restart` could
  slip in: until the unit holds the lock nothing has been typed.

## 2. Decisions

### Settled by the owner

13. **The whole-team resume resumes each host session by its recorded conversation id, never
    `--continue`; an LXC member resumes with `--continue`.** Settled by the owner on
    2026-10-26. The owner's request said `--continue` (today each member is relaunched by hand
    with `ccy --continue --teams`); asked, the owner ruled that resuming by recorded id is
    better. Decision 10 and SPEC-team-add-and-restart.md F4 and F5 give the reasons: sessions
    in one checkout share one transcript folder, so `--continue` takes the newest conversation
    of any of them, and one stale background record from another PID namespace makes it refuse
    for good. **LXC members are the exception**, by the same ruling: inside a container
    `ccy --continue` resumes a session cleanly and is what the owner uses today, so an LXC
    member is resumed by 3.18's connect command, a `cd` into its recorded working directory and
    its resume command, default `ccy --continue`. No conversation id is passed to claude in the
    container. How a `service: true` member fits this ruling is Decision 24 (its default stands). The
    limits of `--continue` are Decision 23's.

### Decided: the defaults stand (2026-10-28)

14. **The inner deadline.** Default: the outer window less 3 minutes (15 gives 12, 30 gives
    27), and an outer window under 5 minutes is refused. The 3 minutes cover the forced
    shutdown's own waits, all sessions together: 30 s for a prompt, 60 s for an exit, and
    SD14's 30 s seat wait, 2 minutes in all, with the rest for the LXC route and its
    `systemctl stop`. The wind-down's own bounds are capped at the inner deadline: W6's 300 s
    wait for idle ends there (with a 5-minute window the inner deadline is 2 minutes away), and
    a member still waiting then has had nothing typed and is forced with the rest. The
    alternative: a separate `--coordinator-deadline`.

15. **Who is told.** Default: the host types the wind-down into every member, the coordinator
    first and the others once the coordinator's line has gone in; the workers' text says the
    coordinator leads it. Only a typed line can clear a `/goal` (SPEC-team-wind-down.md F1),
    and the coordinator cannot type into a worker. The alternative: only the coordinator is
    typed to, and it tells the workers over the bus.

16. **Shutting down before the deadline.** Default: the forced shutdown starts early once every
    session has been idle with an empty box for 2 minutes after its wind-down turn. The
    alternative: it always waits for the inner deadline.

17. **Where the wait runs.** Default: a transient systemd user unit, so closing the terminal or
    the picker does not cancel it; `ccy-teams wind-down --cancel <team>` stops it and says what
    was already typed.

    - **Except for a team with an online LXC member.** The LXC route is `sudo -n lxc-attach`,
      which needs a sudo timestamp, and sudo's timestamp belongs to the owner's terminal, which
      a unit does not have (and 12 minutes later it would have expired). Such a team's
      wind-down stays in the foreground with a countdown. It keeps the timestamp fresh each
      minute with the same two-step probe as `sudo.Ready` (`sudo -n -v`, else `sudo -n true`,
      since `-v` fails on a host with some NOPASSWD entries while root commands still run;
      `internal/sudo` `Ready`), asking nothing.
    - A refresh that fails is said, and the countdown goes on. An LXC member is a failure
      (named with the text to give it by hand, exit 1) only when its own `lxc-attach` fails,
      and the desktop sessions are still forced.
    - When the countdown ends, by finishing, a cancel or a signal, it calls `sudo.Forget`, as
      other ccy-teams flows do, so the terminal's next session cannot use the owner's sudo.
    - Closing that terminal ends the countdown and frees the lock. What was typed stays,
      nothing is forced, and the draft record is deleted (TW9), so the owner runs
      `ccy-teams shutdown <team>`. A hold the countdown could not remove on its way out is
      released by the team's next resume (TW10).

    The alternatives: a root timer scheduled in each container in the foreground
    (`systemd-run --on-active` through `lxc-attach`), which needs a guest-side stop program; or
    the foreground countdown for every team.

18. **A coordinator that does not start on resume.** Default: a coordinator that is online and
    fails to start is ✗ with why, the rest are still started, and the run exits 1. An offline
    coordinator (its container stopped) is not a failure: it is shown `-` offline, the rest are
    started without waiting for its seats, and the run exits 0; only `resume --member` naming
    it exits 69. The alternative: nothing more is started after a coordinator that failed.

19. **One verb or two.** Default: two. `ccy-teams resume <team>` (and its menu line) reads and
    consumes the last-shutdown record, checking each entry against the current roster;
    `ccy-teams restart <team>` stays 3.11's, restarting what is running and never reading the
    record, so a restart weeks later cannot bring back members the owner has since ended or
    removed. The alternative: `restart` also reads the record, but only when the team has no
    running member.

20. **The form of the connect and resume settings.** Default: a command text with
    placeholders (`{container}`, `{user}`, `{checkout}`, the last being the member's recorded
    working directory), run in a login interactive shell (`bash -lic`) inside the container as
    the member's user, so a shell alias resolves as typed. There is no conversation
    placeholder, since an LXC member is resumed by none (Decision 13). Inside the owner's
    containers `ccy` is the owner's own alias for a claude wrapper, and `ccy --continue` run
    there resumes cleanly, so the default resume command is the text `ccy --continue`; a team
    or seat may configure another. The alternative: a word list run with no shell, where a
    team that uses an alias names a script or the alias's expansion.

21. **The last-shutdown record's life.** Default: consumed. Each entry is removed once its
    member has been started, or named as gone from the roster. An entry whose start failed, or
    whose member was offline, is kept, so a second `resume` retries only those. An entry whose
    member is found already running in the conversation the shutdown recorded is consumed; one
    found started fresh since is kept (RS9). The next shutdown that happens replaces the whole
    record: `ccy-teams shutdown` when it writes it, a timed wind-down only when its forced phase
    begins, its start-time copy being a draft until then (TW8). A cancelled or abandoned
    wind-down deletes its draft and keeps the previous record. The alternative: kept whole
    until the next shutdown, every resume reading all of it.

22. **The coordinator's start text**, default wording: "agent-bus: in team {team} you are the
    coordinator. ... You cannot start, add or provision team members yourself; only the owner
    can, from ccy-teams on the host. When the work needs members who are not present, ask the
    human to provision them, saying how many and for what, then delegate to them." (the
    existing first sentences kept where the dots are). The alternative: a shorter text that
    only names the limit ("only the owner can add members, from ccy-teams") and points at a
    coordinator section of the pingbus skill for the rest.

23. **What an LXC member's `--continue` can take, and where it looks.** `--continue` takes the
    newest conversation in one transcript store, and which store depends on what runs claude:

    - **A member resumed by the default `ccy --continue`**: `ccy` there is the owner's alias
      for a claude wrapper, so which store it reads is a fact to read from a running member,
      not to assume (the build's first step, SPEC-lxc-connect-and-resume.md section 1). The
      store kind comes from the `transcripts:` setting, `ccy` (per checkout,
      `<working directory>/.claude/ccy/projects/-workspace`, `transcript.Dir("ccy", ...)`,
      src-go/internal/transcript/transcript.go) or `claude` (the user's own); its default is
      settled by that step.
    - **A `service: true` member**: its unit runs `agent-bus-claude`, which execs `claude`
      directly at the unit's `WorkingDirectory`. Its store is the user's own,
      `<home>/.claude/projects/<escaped directory>` (`transcript.Dir("cc", dir, <home>/.claude)`).
    - **Any other resume command** names its store kind on the same `transcripts:` setting;
      one that names none, where the default does not apply, is refused by the declare
      parsers.

    The limits: (a) one (container, user) session is in all its user's teams, so one team's
    resume brings it back in the others, and their resumes find it already running (RS7).
    (b) A conversation run in the same store since the shutdown is the newest and is the one
    taken: for a per-checkout store that is any run in that checkout, by any user of the
    container, the owner's own included; for a user's store, any claude that user ran in that
    directory.
    (c) Two LXC members whose stores are one: when the store is per checkout (`transcripts: ccy`), two users in one container whose entries name one checkout share it. This is F4,
    the reason Decision 13 rejects `--continue` for host sessions. The declare parser also
    refuses two entries for one (container, user, team), so two members of a team never
    share a user's store.

    Default for (b): the shutdown records the newest transcript file's name and mtime in that
    store, read-only, and the resume warns, naming the member and the newer file's time, when
    a newer one exists, then resumes with `--continue` as the owner ruled. Default for (c): both
    declare parsers refuse it, naming the two entries and the checkout. The alternatives: for
    (b), such a member is not resumed (named, exit 1, its entry kept) until `resume --member`
    names it; for (c), it is allowed and warned at each resume as in (b). If step 1's finding
    is that the store is not per checkout, the shared-checkout refusal does not apply.

24. **A `service: true` member at shutdown and resume.** A service member was never a ccy
    session: its unit runs `agent-bus-claude`, which execs claude with the kit's plugin and
    settings. A connect, `cd` and `ccy --continue` beside the unit would be a second session in
    one seat, without the kit's settings. Default, set by the coordinator as the one reading of
    Decision 13 that works for such a member:

    - it is ended through its unit, held stopped by a persistent `ccy-teams-hold.conf` drop-in
      that `play-agent-bus.yml`'s apply honours (LS3 to LS6), and resumed through its unit with
      `agent-bus-claude --continue` for that one start, by the one-shot `continue-once` flag
      file (K5 to K7 of SPEC-lxc-connect-and-resume.md);
    - a seat's resume override does not apply to it.

    The alternatives: service members are converted to non-service at shutdown (the unit
    stopped and disabled, then resumed as any other LXC member, by the connect, `cd` and resume
    command); or the hold is a runtime drop-in under `/run` (Restart=no only), which a
    container boot loses, so the boot starts the member fresh and the resume can only say so
    (RS9); or a persistent hold file tested by `ConditionPathExists=!` in `render_member_unit`,
    which changes the unit's text and so restarts every service member once.
