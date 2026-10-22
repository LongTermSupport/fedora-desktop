# SPEC: N2 slice n2e, health, the status records, the seat sweep and Element (Plan 00209 Task 2.4, part)

Built from area C's 3.11 (health and the status record, DC4, DC7), 3.7.5's owner seat sweep
(DC21, INT7), 3.10 (Element profiles, DC18), DC16's stated cost (the pool-password watch) and
the n2c re-check's low 2 (the announcer and a member-written run id). Each expectation is
numbered and has a Go test written first and seen to fail.

**Inert code.** Nothing here is a play, a unit, a timer file, a sudoers rule or an install step,
and no verb of `teamchat-admin` is wired to it (they still exit 1 "not built yet", n2a-D1).
systemd unit state, the LXC guest probe and the backups sit behind seams, so godbus (DC17) is
not needed. The only passwords are random test keys.

**Out of this slice:** backups and restore (C 3.12; the tick reads their outcome through a seam),
the LXC guest root fd and its seat lock probe (C 3.9.2; a seam), starting the `teamchat-ctl`
child (n2a-D6; the view is a package function the child will call), the verbs `health`, `status`
and `element`, `desired.json` (C 3.7.5; the sweep asks it through a seam), and ccy-teams' board.

## 1. The status record (`teamchat/status`, C 3.11.3, DC4)

- **SR1. Schema.** `status.json` is C 3.11.3's document, `v` 1. Decoding is strict: every key
  of the example and no other, no repeated key, nothing after the value, at most 1 MiB (n2e-D1).
  `team` is a team id and must be the file's team. `origin` is `v2` or `migration`. `homeserver.unit`
  is a systemd `ActiveState` word; `homeserver.probe` is `ok`, `missed` or `not-run` (n2e-D3).
  A room membership is `join`, `invite`, `leave`, `ban` or `knock` (a room the account is not in
  is absent from `rooms`). `backup.last_run` is `ok`, `failed` or `none`. `online` is on LXC
  member entries only. Every `what` and `fix` is one printable line. No field can hold a token:
  the record has no free-form field but `what`, `fix`, `name` and the events' `what`, which the
  writer builds.
- **SR2. Write.** `Write(path, rec, gid)` writes 0640 with the owner group through
  `state.WriteFile` (temporary file, rename, directory sync), so a reader sees the old record or
  the new. It refuses a record that does not encode under SR1, and one whose `expires_ms` is not
  `written_ms` + 300 s.
- **SR3. The seq rule.** `Next(prev)` is the record a writer writes after `prev` (nil for missing
  or unreadable, seq 0): `seq` raised by one, `written_ms` and `expires_ms` from the clock, events
  older than 24 h dropped and at most the newest 32 kept (n2e-D2).
- **SR4. The bounded read.** `Read(path)` is the record, refusing a link, a file that is not
  regular and one over 1 MiB (`state.ReadFile`), then SR1. Missing is found false.
- **SR5. The board's view.** `Shown(rec, found, err, now)` is what a reader shows: the record's
  own problems, plus "health has not run since <time>" (fix: `systemctl status teamchat-health.timer`) when `now` is past `expires_ms`, or "the status record cannot be read"
  when it is unreadable, or "no status record yet" when it is missing. It never shows an expired
  record as current: `Current` is false past `expires_ms`.

## 2. The team lock (`teamchat/teamlock`, C 3.4)

- **TL1.** `Take(ctx, dir, team, verb, wait, clock)` is the one team lock: every root verb
  (`host` apply and remove, the `admin` member and role verbs, the `health` tick) takes it here,
  and each caller owns its wait (`host.PlayWait` 10 minutes, `admin.SeatWait` 60 s, the tick 0).
  The directory `dir` is made 0700 when missing and opened `O_DIRECTORY|O_NOFOLLOW`; one that is
  a link, not a directory, not mode 0700 or not owned by the euid is refused. The file
  `team-<team>.lock` is opened relative to that directory's descriptor,
  `O_RDWR|O_CREAT|O_NOFOLLOW|O_CLOEXEC` 0600, and must be a regular file. `flock(LOCK_EX|LOCK_NB)`
  is tried every 250 ms by the clock until `wait` is spent (0 is one try), and ctx ends it.
  The holder writes `<verb> <team> pid=<n> since=<ms>`, the verb matching
  `^[a-z][a-z.-]{0,31}$`. A busy lock is a `*BusyError{Path, Holder}` that is `ErrBusy`
  (`errors.Is`), naming the holder's line as read: one printable line of at most 256 bytes,
  else "an unreadable holder". `Release` clears the line and frees the lock; the file is never
  unlinked.

## 3. The health tick (`teamchat/health`, C 3.11.1-3.11.2)

- **HT1. Teams.** `RunAll` ticks every team that has a stamp, at most 4 at once (n2e-D5), and a
  team's failure is that team's alone: it never stops another's tick. The answer is each team's
  outcome (`written`, `dropped` with the reason, or `failed` with the error).
- **HT2. Unit state and the probe (steps 1-2).** Through the `Units` seam: `ActiveState`,
  `SubState`, `InvocationID`, `NRestarts` and when the unit entered `active`. A unit not active
  is `probe: not-run`. Within 60 s of `active`, misses are not counted. After the grace, three
  misses 10 s apart, with the same `InvocationID` still active, restart the unit, only under the
  team lock taken without waiting; a busy lock does no restart. The restart writes its event
  ("restarted after three missed probes") into the record at once, seq raised. A rise in
  `NRestarts` over the record's value is one event, kept 24 h; a fall is the event "restarted by
  hand" (n2e-D4).
- **HT3. The view (step 3)** comes from the `View` seam (the child's read, section 4). A view
  that fails is the problem "the team's rooms could not be read: <class>" (fix:
  `teamchat-admin health` again, or the homeserver), and the rest of the record is still written.
- **HT4. Backups and disk (step 4).** The `Backups` seam answers the newest usable backup (id,
  time) and the last run (`ok`, `failed`, `none`). A newest backup older than 36 h, a `failed`
  last run, or no backup 36 h after the stamp's `applied_ms`, is a problem. The free space of
  `/var/lib`'s filesystem (`statfs`, f_bavail) below the larger of 10% and 2 GiB is a problem on
  every team's record.
- **HT5. The write (step 5).** Under the team lock taken without waiting: the stamp is checked
  again (gone: nothing is written), and the record is written only if its `seq` is still the one
  step 1 read; otherwise the view is dropped. A busy lock drops the view, step 6 included.
- **HT6. LXC disconnected (step 6)**, under step 5's lock, kept in `lxc-online.json` (0600 root,
  strict, per LXC handle: `held`, `run`, `since_ms`, `posted`, n2e-D6). For each LXC handle, the
  `Guests` seam probes its seat lock (held, free with the lock file's record, or stopped). A
  held lock with a valid run id records that run and, when the run changed, `since_ms` now and
  `posted` false. When a handle held at the last tick is now free or stopped, is `active` in the
  roster and its free lock file does not record `launch_failed_ms` for that run, the tick asks
  the `Announcer` seam (`member.disconnected` in the child) with the handle, the run id this
  record held from the last tick, and its `since_ms`; never a run id read now. Posted, or not
  possible for good, marks the entry posted; a transient failure leaves it unposted and asks
  again at the next tick, and the entry carries the problem "the disconnected notice for
  <handle> is not posted yet". A launch failure is the problem `launch-failed`, fix
  `ccy-teams restart <handle>`. `online` is the probe: held is true.

## 4. The view (`teamchat/health/view`, C 3.11.2 step 3, DC16)

Read-only, as the admin, from the roster file and `rooms.json`:

- **VW1. Memberships.** Each member's and each human's membership of each room, read from the
  room's state.
- **VW2. Humans.** Each human's device count and newest `last_seen_ts`.
- **VW3. The pool-password watch (DC16).** For each member whose state is `active` or `parked`:
  a device other than `TCSEAT` is the problem "<handle> has a device that is not its seat's"
  (fix: `ccy-teams reissue <handle>`); a deactivated account is the problem "<handle> is
  deactivated" (fix: `teamchat-admin member retire` and a new member; first name another
  coordinator if it is the coordinator). A missing account is the problem "<handle> has no
  account" (fix: `ccy-teams reissue <handle>`).
- **VW4. Drift.** The rooms engine run against a client that writes nothing: every write it
  would make is one problem (fix `ccy-teams apply <team>`), and drift it refuses is one problem
  naming its room and step.
- **VW5. Questions.** No worker joined or invited, the open coordinator joined, and the form
  (open or closed) the roster gives: each its own problem.
- **VW6. Pending.** A `pending` operation is the problem "<op> did not finish (step <step>)",
  fix: the verb that rolls it forward. The view never completes it.

## 5. The disconnected notice (`teamchat/notice`, C 3.7.5, B 2.11, the n2c re-check's low 2)

- **NT1.** `Disconnected(ctx, c, n)` posts A's `disconnected` as the subject, with transaction id
  `tc-disc-<run id>`, unless the subject's own latest lifecycle notice in main since `since_ms`
  is `left` or `disconnected` (then "a goodbye found", posted). It reads at most 10 pages of 100
  events backwards.
- **NT2. Nothing a member wrote is an error.** The answer is `Outcome` for everything the
  member's input or its room can cause: an event that does not decode is skipped; the page
  bound reached without a goodbye posts; a refused token (`M_UNKNOWN_TOKEN`), a forbidden post,
  a room that does not verify, or a subject not active in the roster is `Outcome{Posted:false, Why}`. Only a transient failure (unreachable, 5xx, 429 after the client's retries, a cancelled
  context) is an error. The run id is a parsed `paths.RunID`, so its transaction id is always
  valid.
- **NT3. The seat store bounds a transient failure (n2e-D7).** A release or an announce whose
  notice fails transiently records `held/notice.json` with `posted` false, the error class as
  `why` and `until_ms` 10 minutes on, and stops for the retry. A retry before `until_ms` asks
  again; one at or after it goes on as "not posted in time" and the release retires the token.
  So a member that makes its own notice fail (say, by spending the rate limit with its token)
  delays its token's retirement by 10 minutes at most.

## 6. The owner's seat sweep (`teamchat/sweep`, C 3.7.5, DC21, INT7)

- **SW1.** `seat.Store.Holdings()` lists every pool seat of the checkout with a `held/`, with its
  `held.json` (an incomplete one is listed for release).
- **SW2.** `Sweep` passes over every holding, each on its own (one's failure is reported, the
  others still run): a session that is over (not restorable) is released; a session that no
  longer desires the seat (the `Desired` seam) is released as a leave; a restorable session that
  desires it is announced (`Store.Announce`), which posts only for a free seat lock naming a run
  with no `launch_failed_ms` and not already recorded, and keeps the seat held.
- **SW3. Nothing during a start.** A holding whose session seat directory has no lock file, or an
  empty one, or a held one, is not announced: a container starting is never announced.
- **SW4. Nothing for a launch failure.** A free lock whose record carries `launch_failed_ms` is
  not announced, by the sweep or by a release.
- **SW5.** The sweep takes the allocation lock only where release and announce do, and makes no
  root call.

## 7. Element profiles (`teamchat/element`, C 3.10, DC18)

- **EL1. Input.** Strict JSON on stdin: an array of `{"team": <team id>}`, no other key (a
  `base_url` is refused as unknown), no repeated team, at most 64 entries.
- **EL2. The URL is the approved one.** Each team's homeserver URL is `url` from its
  `approved.json` (`declare.FileApprovals`), and its display name is `name` from its status
  record. A team with no `approved.json` gets no profile: `check` says "<team>: not approved
  yet, no profile" on stderr and `apply` writes nothing for it.
- **EL3. What is written** under v2's names (`paths.ElementConfigDir`, `paths.ElementLauncher`):
  the profile directory 0700; `config.json` 0600 with every outbound key overridden (v1's key
  set, `server_name` `paths.ServerName`); `electron-config.json` 0600 `{"spellCheckerEnabled": false}` only when absent; the launcher 0644 with `Name=<display name> team (teamchat)` and
  `Exec=flatpak run im.riot.Riot --profile teamchat-<team>`. A path that is not a regular file
  (a link) is refused, never followed. Writes are a temporary file and a rename, only on change,
  and each changed path is one `CHANGED<TAB><path>` line.
- **EL4. Consent.** `apply` refuses unless `~/.config/teamchat/element-consent` is a regular file
  owned by the user, no group or other bits, in a 0700 directory the user owns, holding
  `{"v":1,"accepted_ms":N,"text":<the consent text>}` with today's consent text (a changed text
  needs consent again, n2e-D8). `WriteConsent` writes it. `check` writes nothing and reports a
  missing consent as a line on stderr.
- **EL5.** `element` refuses to run as root.

## 8. Decisions

| ID     | Decision                                                                                         |
| ------ | ------------------------------------------------------------------------------------------------ |
| n2e-D1 | The status record is decoded strictly and capped at 1 MiB, by writer and reader alike.           |
| n2e-D2 | Every event is kept 24 h, at most the newest 32.                                                 |
| n2e-D3 | `probe` is `ok`, `missed` or `not-run`; `unit` is systemd's `ActiveState` word.                  |
| n2e-D4 | A fall in `NRestarts` is the event "restarted by hand".                                          |
| n2e-D5 | `health --all` ticks at most 4 teams at once.                                                    |
| n2e-D6 | `lxc-online.json`'s keys are `held`, `run`, `since_ms` and `posted` per handle.                  |
| n2e-D7 | A transient notice failure holds a release for at most 10 minutes (`until_ms` in `notice.json`). |
| n2e-D8 | The consent record names the consent text; a changed text needs consent again.                   |
| n2e-D9 | The Element display name is the status record's `name`.                                          |
