# Adversarial review: LXC member handle derived from the checkout (Plan 00177 Phase 9)

Delta reviewed: `7590b1ad..origin/feat/ccy-teams-lxc-member` (89fec89f, bec325aa).
Reviewer: Fable 5.1 (sub-agent), read-only except this report. Branch `review/lxc-derive`.

## Verdict

**Not ready to merge as it stands: one must-fix (F1), two should-fix (F2, F3).** The host-side
reader itself held up against everything thrown at it (symlinks at every component, FIFOs,
devices, sparse files, hostile passwd, control characters, `..`, no git run). The gaps are in
what the play does with the derivation: the play still lets the container's own
`pingbus suggest-handle` choose `<repo>`, so the host-side derivation is not the oracle for
the handle actually minted; and a lost bundle gives a member a new handle with no error.

## What was run

- `python3 -m unittest tests.helpers.agent_bus_lxc.test_derive tests.helpers.agent_bus_lxc.test_members tests.helpers.agent_bus.test_checkout tests.helpers.agent_bus.test_declare` — 305 tests OK.
- `go test ./internal/declare/... ./internal/lxcfreeze/... ./internal/teams/...` — all OK.
- `./scripts/qa-go.bash` — GO-OK, 42 packages, coverage 94.4%, golangci-lint clean, project rules clean.
- `./scripts/test-ccy-teams.bash` — 156 passed; `bash ./scripts/test-ccy-teams-command.bash` — 336 passed (the file is not executable in the checkout, see F8).
- A hostile-input harness against `derive.ROOT_READ_PY` (fake LXC tree, this user) and a probe of what `git remote get-url origin` says for the same configs; a fixture-based probe of the play's present phase. Scratch only, under `untracked/scratch/`, not committed.

## Findings, ranked

### F1 (must fix) — the play mints the handle with the container's `<repo>`, not the derived one

`helpers/agent_bus_lxc/members.py:722-724` (`_join`): `repo = _suggest(guest, member, home)` runs
`pingbus suggest-handle` **inside the container, as the agent user**, and builds the handle from
what it prints. `resolve_members` (`members.py:394-436`) derives `<repo>` host-side but stores
only `host` and `seat` on the Member (`members.py:433`); `identity.repo` is dropped. `_suggest`
compares the suggested repo only to an explicit `repo:` key (`members.py:662`), which a derived
entry does not carry. The host check at `members.py:660` exists; the repo check does not.

Hostile input: a container whose `/usr/local/bin/pingbus` (root-in-container owns it) prints
`--repo=other --host=box --type=lxc` while `.git/config` says `.../proj.git`. Proven with the
test fixture: the play joins `other.1+box.lxc` and reports success; `ccy-teams lxc-add` had
shown `proj.1+box.lxc` in its summary and prints "is a member of … as proj.1+box.lxc"
(`src-go/internal/teams/lxcadd/adder.go:532`) — both false. With `proj.1` active elsewhere the
derivation picks seat 2 and the play mints `other.2+box.lxc` although `other.1` is free: the
seat is numbered against one repo's roster and minted under another.

Non-hostile trigger too: `git`'s `url.<base>.insteadOf` in the agent's `~/.gitconfig`
**does** rewrite the last component (`git remote get-url origin` printed `beta.git` for a
config saying `alpha.git`), contradicting `derive.py:252-254` and
`docs/agent-bus-lxc-members.md:153` ("only the last path component matters"); and git reads
`[Remote "origin"]` case-insensitively while `_ORIGIN_SECTION_RE` (`derive.py:241`) does not,
so derive falls back to the directory name where ccy's `git remote get-url` finds the URL.

Fix: make the derivation the oracle. `resolve_members` should set
`repo=member.repo or identity.repo` on the resolved Member (one word at `members.py:433`), so
`_suggest`'s existing comparison at `members.py:662` refuses a disagreeing container; add the
test `test_suggest_handle_must_agree_with_the_derived_repo` beside the host one
(`tests/helpers/agent_bus_lxc/test_members.py:726`). Make `_ORIGIN_SECTION_RE` case-insensitive
for the `remote` keyword (subsection names stay case-sensitive, as in git). Correct the
insteadOf sentence in `derive.py` and the docs: it is not applied, and a rewrite that changes
the last component is refused by the agreement check, not ignored.

### F2 (should fix) — a lost bundle renames the member silently and leaves a ghost

`derive.derive_seat` (`derive.py:359-372`): no bundle → not joined by name → lowest number no
*active* member holds. Proven: join as `proj.1+box.lxc`, delete the bundle directory in the
container, run again: `proj.2+box.lxc joined team …` is emitted as a change, `proj.1` stays
active with a live token nobody holds. The same with an explicit `seat: 1` is refused at
`members.py:483-488` ("active member … holds no bundle"). The docs claim "nothing is renamed
silently" (`docs/agent-bus-lxc-members.md`, **Entries written before…**) and then, in the last
sentence of the same paragraph, describe exactly this. A hostile container that deletes its own
bundle before each play run collects a fresh active handle and token per run.

Options: (a) when the entry has no bundle and the roster holds an **active numbered** handle of
this identity that no other declared present entry resolved to, refuse and name the two ways
out (`seat: <n>` to reclaim, or `state: absent` then present); hand-joined members are
indistinguishable from this case, so (b) at least say so in the change line
("… joined; proj.1+box.lxc is still active and nobody holds its bundle") and emit it as a
distinct marker the play surfaces. (a) is what the explicit-seat path already does.

### F3 (should fix) — `ccy-teams lxc-add` reports the preview handle as the result

`adder.go:532` prints `ans.derived.Handle` after `ApplyTeams`, but that handle was computed
before the play ran, with `bundle=None` and without entries still to be derived
(`declare.py:1171` `kept` filter drops present entries lacking host or seat). Two `lxc-add`
runs for two containers of one repo, before either is applied, both show `proj.1`; the play
gives `1` and `2`. A container already holding a bundle for the team shows a fresh number but
keeps its bundle's seat. The honest line is "declared; the play reported …", reading the play's
marker, or saying it is the expected handle.

### F4 (low) — a missing bind-mount source is a traceback, not a refusal

`derive.py` `root_fd` (`ROOT_READ_PY`, `os.open(base, O_PATH|O_DIRECTORY)`) is not guarded.
`lxc.mount.entry = /nope home/agent/work none bind,optional 0 0` (an `optional` mount whose
source is absent is a normal LXC config) with a checkout under that destination gives
`FileNotFoundError` with a Python traceback; `members.read_checkout` relays the traceback text
into the Ansible failure. Catch `OSError` there and name the mount.

### F5 (low) — a quoted URL is refused where git accepts it

`url = "https://github.com/o/quoted.git"` is valid to git (`get-url` prints it unquoted); derive
refuses it (`derive.py:264-266`). `git remote set-url` never writes quotes, so this is rare, but
a hand-edited checkout cannot join and the message points at nothing the person did wrong.
Either read a simple fully-quoted value without escapes, or keep the refusal and say
"remove the quotes around the url".

### F6 (low) — error text from the container's tree reaches the Ansible output unsanitised

`ROOT_READ_PY` exits with `shown + "/" + name` from `os.scandir` in `checkouts` mode; a
directory named `\x1b[31m…` in the agent's home lands in stderr, which `members._failed`
(`members.py:894-895`) puts into the MemberError verbatim. The Go side sanitises
(`lxcfreeze/lister.go:plain`); the Python side does not. `errors='replace'` is not
`isprintable()`. Note `derive.parse_checkouts` correctly refuses the whole listing for one
such name, so the suggestion is lost rather than a terminal steered; the Go flow then falls
back to typing the path, which is fine.

### F7 (informational) — things a hostile container *can* still choose, and why that is accepted

- `<repo>`: the agent user owns `.git/config` and may name any origin
  (`url = https://evil.example/x/fedora-desktop.git` derives `fedora-desktop`). This is the
  podman ccy trust model too (the desktop user owns the checkout), and the owner declares the
  entry and sees the handle; an impersonated prefix cannot *take* another container's active
  numbered seat (`members.py:483-488`), only squat beside it. Acceptable, as long as F1 makes
  the host-side reading the only one.
- `<host>`: `ccy.env.local` is the agent's; the owner-uid check against the container's own
  `/etc/passwd` is meaningless against a hostile container (it writes both) but is the ccy rule.
- `home` in `/etc/passwd`: the scan follows it anywhere inside the rootfs or a bind mount;
  `..` and `/` are refused; it can only ever print `.git/config` texts from the container's own
  tree, bounded to 30 entries of 64 KiB.
- Hardlinks: `.git/config` hardlinked to a file outside the checkout is read — it is a file the
  container could already read, and nothing the host side can tell apart without `st_nlink`.
- `..` in a bind-mount *destination* is not refused; the config is root's, so this is not a
  boundary.
- A worktree or submodule checkout (`.git` is a `gitdir:` file) is refused; ccy's podman path
  follows it through git. Documented in the message, not in the docs.

### F8 (housekeeping) — `scripts/test-ccy-teams-command.bash` is not executable

`./scripts/test-ccy-teams-command.bash` → Permission denied; it runs under `bash`. Not part of
this delta, but it is one of the suites this review was asked to run.

## What held up

- **Path resolution.** Every component is opened `O_PATH|O_NOFOLLOW` from a directory fd and
  fstat'd; a symlink at any component (dir, `.git`, `config`, `ccy.env.local`, a bind-mounted
  subtree) is refused by name; `..`/`.`/relative paths are refused before any open; FIFOs and
  devices are never opened for reading (the regular-file check is on the `O_PATH` fd and the
  read reopens through `/proc/self/fd`, so there is no check-then-open race on a different
  inode); `st_size` is checked before the read and the read is capped at `CAP+1` (a 1 TiB sparse
  file is refused at once). A rootfs or bind source that is itself a symlink *is* followed
  (`root_fd` uses plain `os.open`) — those are root's own config values, so that is right.
- **Nothing runs.** No git, no shell; `core.fsmonitor` and a `touch` in `ccy.env.local` were
  verified not executed; `$(…)` in the role value is refused as "only running the file could
  read"; weird role spellings are normalised or refused by `registry.resolve_host`.
- **Printed JSON is judged, not trusted.** Keys exact, sizes bounded, printable-only paths and
  URLs, owner uid cross-checked; the Go side treats the probe as opaque and lets `declare.py`
  judge it, so there is one judge.
- **One rule.** `declare.py lxc-derive` calls `derive` + `members.resolve_members`, the same
  code the play runs; Go only carries strings. The Go `ReadCheckout` passes the script as an
  argv element to `sudo python3 -I -c` with no shell; `lxc-config lxc.lxcpath` output is checked
  for an absolute, printable path on both sides.
- **Seat rule.** Bundle's seat first, then legacy name, then lowest free; `claimed` keeps two
  derived entries in one file apart and `_check_between` is re-run on the resolved set;
  parked seats return; another repo/host/type never counts. A second run with the bundle in
  place changes nothing (test and probe agree). An origin change after joining is refused
  (fail-fast) — though the hint "put back the seat or host" should also mention the repo.
- **Idempotent and fail-fast.** The checkout is read for every derived entry on every run and
  any refusal stops the phase before kit sync or any change; unprivileged containers
  (`lxc.idmap`) and non-directory rootfs are refused.
- **Tests are real.** `test_derive.ProbeScriptTest` runs the actual `ROOT_READ_PY` through
  `python3 -I -c` against a fake LXC tree and asserts refusal text and that a planted `git` and
  `core.fsmonitor` did not run; `test_members` drives `run_phase` through the fake host/guest
  and asserts the minted roster. The gaps are coverage, not fakery: no test for the repo
  agreement (F1), none for the lost-bundle path (F2), none for a case-insensitive section
  header or an `insteadOf` rewrite.
