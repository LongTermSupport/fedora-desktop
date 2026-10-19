# Plan 00209: agent bus v2 design (opus)

Branch `feat/agent-bus-v2-design` from F44 `442143f5`. Design only; no production code changed.

## Scope change

The task started as Plan 00192 Task 3.3's read-only port spec (`SPEC-agent-bus-read.md`). The
owner's direction changed it before anything was written: a v2 rebuild in Go, designed from the
requirements. No read-port spec was written; what was learnt of the read path is section 4 of the
design.

## What was produced

- Plan 00209 scaffolded by `mkplan.bash`, indexed in `CLAUDE/Plan/README.md`.
- `RESEARCH-pingbus.md`, `RESEARCH-host.md`, `RESEARCH-lxc-robustness.md`,
  `RESEARCH-go-callers.md`: four read-only digests of v1 (by four research agents), with
  `file:line` citations, placeholders only.
- `DESIGN.md`: why v2 (with the measured numbers), requirements R1-R39, shape (four Go binaries:
  `agent-busd`, `agent-bus`, `agent-bus-install`, `pingbus`; packages under `internal/bus/`), the
  read model and its latency budget, state and versioning (all v1 files kept, three new ones, a
  shared fixture corpus), privilege (sudoers rule removed, writes still need the sudo password via
  a root peer of the admin socket), concurrency (single writer, install lock), pingbus v2 (keeper
  plus worker, Go hooks and status line, no Python in members), homeserver interaction,
  migration in eight stages with rollback, test strategy, what v1 does that v2 does not carry,
  observability, owner decisions D1-D13, risks, host checks H1-H10, departures B1-B6.
- `PLAN.md`: phases (design review by Fable, owner decisions, then one phase per stage).

## Key findings

- An open starts Python 7 + C + T + H times, serially: about 22 at six checkouts, three teams and
  six people. The "six seat lists" are six checkouts, not six passes. `declare overview` runs three
  times per open and rebuilds the agent-bus zipapp in each to hash it.
- The import cost (0.36-0.42 s per start) is what is paid, not the interpreter (33 ms).
- `human devices` feeds only a count; the read needs root only because the admin token is the
  `agent-bus` user's.
- The status line runs a Python `pingbus` every second in every team session.

## Not done

- No `qa-all` (as instructed). No push, no merge.
- Fable's review (Task 1.3) and the owner's decisions (Task 1.4) are next.

## Rework round after Fable's review (Task 1.3)

Branch `feat/agent-bus-v2-design-2` from `07625931` (the first branch is checked out in another
worktree). Design only; no code. Fable's review is copied beside this report
(`261019-v2-design-review-fable.md`). Dispositions, finding by finding:

01. **D1 not neutral — changed.** D1 now lists (a) daemon, (b) live file, (c) Go reader under the
    old `(root)` rule shape, and (c') `agent-bus view` run as `agent-bus` by a NOPASSWD Runas
    line, no root, no daemon. Section 3.3 is now "Why no daemon in this plan": the first draft's
    three reasons and why each fails, and the daemon's operational cost listed. The evidence
    (RESEARCH-go-callers 5 step 3, about 0.1 s) supports (c'), so it is the stage-4
    recommendation; the daemon is deferred to the broker plan (D11). Sections 4.2, 6 and 7 are
    written to (c'), and say they are revised from 6.4 if the owner picks (a). Checked:
    the sudoers file today grants `(root)` for two argv shapes
    (`files/etc/sudoers.d/agent-bus-read-only.j2:9-10`), and root is there only for the wrapper's
    `runuser` (`files/usr/local/bin/agent-bus:55-58`).
02. **Locks dropped — changed (option b).** Section 7 now keeps every v1 member lock at its v1
    path, holder kind and order (new R47), in a table naming each holder: the delivery lock
    `relay/lock` shared by the worker and `recv`/`wait` (`helpers/pingbus/cli.py:1233-1238`), the
    outbox `flock` on the `state/` directory (`helpers/pingbus/inbox.py:560-571`), the told lock
    (`membership.py:63`), the watch lock (held by the worker), sync, seat and keeper locks. The
    shared `PINGBUS_HOME` for host and LXC members is stated; `recv`/`send`/`say`/hooks stay
    separate processes that take the locks; the "seven locks to two" claim is gone. Added: a
    goroutine-level note (one file description per acquisition).
03. **Security — changed.** Under (c') there are no sockets; the reader takes no argument, stdin
    or environment, refuses the wrong uid, and its binary is root-owned (4.2). The admin-token
    exposure is said plainly (one process holds every team's token for its run; 6.3). The
    socket modes (admin 0600, view 0660 group or 0750 + ACL, peer check as defence in depth) and
    sandbox parity with the homeservers are recorded in 6.4(a) as what any future daemon must
    have. `m.room.member` content is grammar-checked before it is forwarded (R39 extended, 4.2),
    and seat rows are cross-checked against the roster (B8).
04. **Missing requirements — changed.** Added R40 (admin bootstrap), R41 (description via
    `m.room.topic`, a protocol invariant), R42 (outbox/ack/TIMEOUT), R43 (member-side token),
    R44 (forge credential in a long-running worker), R45 (hooks: signals, 4 s budget,
    `stop_hook_active`, `inject_frame`), R46 (`conversation.json`, `relaunch.json`, restart by
    resume), R48 (LXC `service: true`, pre-checks 4-5, absent before remove, `only_teams`,
    `since`), R50 (the Element play and its replacement `agent-bus element`, D19); R26 now
    carries the status line's process-group kill and Popen race, `INNER_VAR`, `COLUMNS`,
    `(role?)` and the 300 s down mark; R7 names the health check's sandbox. Each cites v1 code,
    checked this round (`hooks.py:35,213,701,967,1011`, `statusline.py:59,88,187-224,340-342`,
    `forge.py:64,112-136`, `syncer.py:163-172`, `members.py:21,353,360,390,489-493,730,743,819-821,1001-1003`,
    `admin.py:457-506,552,597-614`, `element.py:1-23`, `desired.go:20,123-131`,
    `restart.go:93`). `relaunch.json` turned out to be written by Go ccy-teams and read by the ccy
    supervisor, not by pingbus; R46 says so.
05. **Image bump and kit — changed.** Section 8.4 states the zipapp is baked
    (`Dockerfile:398-407`, `applied_record.py:4-7`), that stages 2, 6 and 8 are container version
    bumps, that the container-version gate must cover pingbus's Go sources, and what "a running
    session keeps v1" means for ccy. 8.5 covers the LXC kit sync and the host kit. Found while
    checking: the host and LXC kits are replaced in place under running sessions, so a new R49
    and B9 (versioned kits resolved at launch, D18, H13).
06. **Stage order — changed.** New order: 1 in-process reads with one `declare overview` kept
    (no installer dependency, about 6 s saved), 2 Go hooks and status line (6a), 3 installer and
    digest, 4 reader, 5 admin and Element, 6 keeper and worker, 7 LXC, 8 retire. The first draft's
    stage 1 (foundations) is gone: each stage brings the decoders and corpus cases it needs. D20
    asks the owner.
07. **Latency headline — changed.** Section 1 and H1 record the coordinator's host measurement
    (`--list` 14.5/23.0/14.1/10.6/9.3 s at load 30-39 on 22 cores, six seat lists, eight sudo
    calls, about 0.45 s each); H1 repeats it at an ordinary load before stage 1 (Task 2.1). 4.4
    gives the per-stage open and redraw figures.
08. **Status line — changed.** The worker writes `status-row.json`; the status line reads one file
    and never opens the inbox (8.2, 5.3). The per-refresh budget and the inner command's cost are
    stated; `refreshInterval` is D16. Stage 2's status line derives from v1 files (no worker yet)
    and stage 6 replaces that in one commit, one implementation per kit version (B10).
09. **Coexistence tests — changed.** Section 11 adds the registry test, `seat disconnected` both
    ways (H11), every member lock against a real v1 process, and the R46 files; H4 is now the
    concurrent admin-token check; H12 checks the registry version after stage 5; D9's migration
    runs before the decoders drop legacy forms.
10. **Redesign or port — changed.** Section 1 says plainly: new process and privilege model, same
    data model, and why the optimisation stops there until stage 8.
11. **Silent decisions — changed.** D14 (reader failure on the board), D15 (keeper in every
    member), D16 (refresh interval), D17 (image bumps), D18 (versioned kits), D19 (Element), D20
    (stage order); the device-ID reduction is in D1's row; D6 points at the fixtures. The daemon's
    sockets fold into D1.
12. **Smaller corrections — changed.** `pingbus` on the host is a deployed tool (3.1, Task 3.3);
    H 7.24 kept for the one systemd call (4.1); the health check's sandbox and the unit-file
    CHANGED and `daemon-reload` (R7, 10.2); the Unicode corpus pins both directions (8.3); goodbye
    records: `said-goodbye.json` stays, `goodbye.json` goes (B7); PLAN.md has tasks for Element
    and the image bumps and puts `qa-reviewer` in each stage's acceptance; D7 says it departs from
    RESEARCH-host 8.12 and why.

Rejected: none. One deviation from a suggestion: finding 3's socket modes are recorded for a
future daemon rather than built, because finding 1's recommendation removes the sockets.

Open for the owner: D1-D20. Not done: no `qa-all` (as instructed), no push, no merge.
