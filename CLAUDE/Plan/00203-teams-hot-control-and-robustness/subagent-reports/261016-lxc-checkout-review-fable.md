# Fable review: `fix/lxc-btrfs-checkouts` (a7ba567b) against F44 1d3c65bb

Reviewed: `git diff F44...fix/lxc-btrfs-checkouts` (c77b9645, the btrfs reader, is already in F44).
Developer's report: `261016-lxc-checkout-opus.md`.

## Verdict: FIX-FIRST

The code is sound and the tests exercise the real paths. What blocks the merge is mechanical
but real: the branch sits on an F44 that no longer exists (it was cut before the coordinator
merge), so it carries version numbers F44 already holds, a `meta-deploy.list` that lost an F44
entry, and three code conflicts in `adder.go` whose resolution has to carry this branch's
context headers onto F44's new Role question. Plus one docs/code contradiction.

## Blockers

1. **Versions collide with F44.** F44 holds CCY 3.99.12.4 / container 2.77.3 (coordinator merge).
   This branch must become 3.99.12.5 / 2.77.4. Every place:

   - `files/var/local/claude-yolo/claude-yolo:17` (`CCY_VERSION`, and its comment)
   - `files/var/local/claude-yolo/claude-yolo:81` (`REQUIRED_CONTAINER_VERSION`)
   - `files/var/local/claude-yolo/Dockerfile:36` (`LABEL claude-yolo-version`)
   - `docs/ccy-changelog.md:20` (the heading; the entry must sit above F44's 3.99.12.4 entry,
     not replace it)
   - `CLAUDE/Plan/meta-deploy.list:26` (the comment on the `play-claude-yolo.yml` entry)
   - `CLAUDE/Plan/00203-teams-hot-control-and-robustness/subagent-reports/261016-lxc-checkout-opus.md:103`
   - `CLAUDE/Plan/00203-teams-hot-control-and-robustness/JOURNAL/00203-Journal-26-10-16.md:470`
     (append-only: a new journal entry saying the renumber, not an edit)

2. **`meta-deploy.list` drops an F44 entry.** The branch's list has four entries; F44's has five.
   Missing: `00170-unifi-network-tooling/clear-ledger-broken.bash`. The dry-run merge conflicts
   on this file (`CLAUDE/Plan/meta-deploy.list:26-53` in the merged tree), so the resolution
   must take F44's entries and comments wholesale and only reword the `play-claude-yolo.yml`
   comment (3.99.12.5 / 2.77.4, both the coordinator text and this branch's).

3. **Code conflicts against F44 must keep the context headings on F44's Role question.**
   `git merge-tree --write-tree HEAD F44` conflicts in:

   - `src-go/internal/teams/lxcadd/adder.go` (three regions): `New()` must have both this
     branch's `Picker: h.Picker, ReadTimeout: readTimeout` and F44's `SetRole: h.Bus.SetRole`;
     `askRole` must call `a.step(ctx, ans, "Role", duties)` and
     `a.Gum.Choose(ctx, a.header(ans, "Role", "Role"), roleWorker, []string{roleWorker, coord})`
     (F44's `duties`/`coord` with this branch's header, otherwise the takeover offer is the one
     question with no context line); the summary must show
     `ans.derived.Handle+" "+ans.where2()` and `roleLine(ans)`.
   - `src-go/internal/teams/lxcadd/adder_test.go:481-492` and `src-go/internal/teams/usage.txt:100-110`:
     both sides' text, merged by hand.
   - Resolve, re-run `go test ./internal/teams/...` and `scripts/test-ccy-teams-command.bash`
     on the merged tree before pushing: the test pins on the Role question's header will not
     exist for the takeover row until written.

4. **Docs contradict the code on the check command.** `docs/agent-bus-lxc-members.md:116` says
   lxc-add prints `... pingbus status`; `src-go/internal/teams/lxcadd/adder.go` (the Check
   line in `write`) and `docs/ccy-changelog.md` print
   `pingbus config check && pingbus history --limit 1`. One of them is wrong; make the doc say
   what the code prints.

## Attacks, with findings

### 1. Reading through the running container's root

- **Route:** `/proc/<init pid>/root`, pid from `lxc-info -P <lxcpath> -n <c> -p -H`
  (`helpers/agent_bus_lxc/derive.py:152-201`). Not `lxc-attach`; nothing runs in the
  container. Correct choice and consistent with the Go rule that `ps` is `sessprobe`'s.
- **Rootful:** the reader is run by `Lister.ReadCheckout` through `Sudo.Capture` as before; the
  config parse refuses `lxc.idmap` (`derive.py:208`), so only privileged containers are read and
  the container's uids are the host's. Fine.
- **Symlink escapes:** every component is opened `O_PATH|O_NOFOLLOW` and a link anywhere is
  refused (`child`, `walk`, `real_dir`, `derive.py:95-134`). `/proc/<pid>/root` and
  `/proc/self/fd/N` are opened without `O_NOFOLLOW` deliberately (they are magic links to the
  held directory), which is right. A member home that is a symlink makes the whole search die
  naming the link; a `..` in a typed path is refused by `split` (`derive.py:258`) and never
  reaches it anyway because Go `path.Clean`s typed text (`checkout.go resolve`). The
  `test_a_path_through_a_link_is_refused` and the bind-not-reapplied test cover this.
- **Restart race:** `running_dir` re-asks the pid after holding the fd (`derive.py:175`). Good.
- **Stopped container:** refused with the store, the config line and "it is not running: start
  it, then try again", naming the container (`derive.py:197-200`); tested
  (`test_a_stopped_container_is_refused_saying_why_it_must_run`). `sessions` mode on a stopped
  container prints `[]`, which Go reports as "No Claude session ... is running": semantically
  right.
- **Real path exercised:** `RunningContainer` (`tests/helpers/agent_bus_lxc/test_derive.py:303`)
  spawns a real child process, fakes only `lxc-info`, and reads through the real
  `/proc/<pid>/root`; the sessions test starts a real process named `claude`. Not a mock of the
  mechanism.

### 2. The find

- Depth 4, 30 checkouts, hidden trees and `node_modules`/`vendor`/`venv`/`__pycache__`/
  `site-packages` skipped (`derive.py:308-329`). Deadline 5 s checked on entry to each
  directory; `cut` stops the sibling loop; `timed_out` is printed and `declare.py` turns it into
  `TIMEDOUT`; Go says "stopped at its time limit, so it may have missed some; those it found are
  offered" (`checkout.go findCheckouts`). Partial results are said as partial. Tested with
  `seconds=0`.
- Second bound: Go's `ReadTimeout` 20 s on the whole read, said as "took too long and was
  stopped". The `dirs` listing has no Python-side deadline; it relies on the 20 s Go bound and
  `DIRS_MAX` 500 (`truncated` is printed and shown). Acceptable.
- Cannot follow links out: `child` refuses, `subdirs` uses `is_dir(follow_symlinks=False)`.

### 3. Folder walker and completion

- Correct in the tested cases: arrows/Enter open or choose, `../` up, `Use <dir>` first when it
  is a checkout, Tab on a highlighted row opens it, Tab on text completes the one match or the
  shared prefix (hidden names and absolute paths included), "Nothing in X starts with Y" when
  none, Enter on text takes it as the path (trailing `/` opens it). A typed path then goes
  through `checkoutOK` (the existing probe + derive) and is re-asked up to `maxTries`, the
  list reopening at its parent (`checkout.go askCheckout`). A listing that fails is said and the
  list stays; a first listing that fails ends the run naming the container. Names with
  non-printable runes are dropped and counted.
- fzf `--print-query` + `--expect` parsing (`ui/picker.go picked/typedOnly`) matches fzf's
  real output order (query, key, row); exit 1 on a Typed picker hands back the text instead of
  leaving. `q` is no longer bound to abort in Typed mode. Esc and the visible exit row still
  leave.
- Visible controls: the header reads "Enter open or choose Tab complete Esc back" and every
  Tab action has an Enter equivalent on a visible row except prefix completion of typed text,
  which has no row form; the exit is a visible row. Note: `CLAUDE/InteractiveScripts.md` has 13
  rules, there is no rule 14; the intent (keys never the only control) is met for everything
  but prefix completion, which is itself a convenience over typing.

### 4. Fallback chains

None silent. The three "say and go on" spots all name the failure and change what is asked,
not what is read: a home search that fails or times out says so and offers the typed path; a
directory that cannot be listed says so and stays; the post-add session look that fails says
"could not be checked" with what to do either way. The first listing failing ends the run.

### 5. Install-specific names

None found in the diff. Fixtures use `container-a`, `box-a`, `agent`, `/srv/agent/one`,
`alpha`; the reports and journal name no host, user, container or path of any install.

### 6. Versions: see blocker 1.

### 7. meta-deploy.list: see blocker 2.

### 8. Tests

- Go: `lxcadd`, `declare`, `ui`, `teamtest`, `lxcfreeze` pass; `go vet` clean on `lxcadd` and
  `ui`.
- unittest: `test_derive`, `test_declare`, `test_config`, `test_members`: 426 tests, OK.
- `bash scripts/test-ccy-teams-command.bash` with `PINGBUS_*` and `CCY_SESSION_NAME` unset:
  passed 369, failed 0.
- Red first cannot be verified from history: the branch is one commit. The tests are specific
  enough (reader argv pinned, `/proc` route asserted, no-attach asserted) that they would have
  been red against F44's code.

## Non-blockers

- `checkout.go move`: Tab with a highlighted row opens the row before `complete` runs, so with
  fzf's fuzzy filter the shared-prefix branch fires only when no row matches (hidden names,
  paths elsewhere). Documented ("Tab on a row opens it too"); say so in the header if it
  surprises anyone. Related: Enter on a typed absolute path that fzf fuzzy-matches to some row
  picks the row. `--scheme=path` or an exact-match mode for the Typed picker would remove the
  ambiguity; not required today.
- `checkout.go sharedPrefix` slices bytes, so a shared prefix can end mid-rune for non-ASCII
  names; use `strings.Builder` over runes or `utf8.RuneStart` trimming.
- `derive.py init_pid`: `subprocess.run(..., timeout=30)` raises `TimeoutExpired` uncaught, a
  traceback rather than a `die` message. Catch it and `die` naming lxc-info.
- `derive.py sessions`: `btime` is read from the container's `/proc/stat`. With lxcfs mounted
  over `/proc/stat`, confirm `btime` is the host's (it should be: lxcfs rewrites cpu lines and
  uptime) or read the host's `/proc/stat` instead, since `starttime` is in host ticks either way.
- `derive.py is_claude`: `"/claude-code/" in argv[1]` also matches any node program under a
  path containing `claude-code`; fine today, but name the intended file (`cli.js`) if it ever
  misfires.
- `checkout.go complete`: the `a.problem` branch after `a.show` is unreachable (`show` returns
  an error only when nothing has been shown yet). Harmless.
- `adder.go write`: the attach hint `sudo lxc-attach -n <c>` omits `-P <lxcpath>`; right for the
  default lxcpath only.

## Merge notes

- `git merge-tree --write-tree HEAD F44`: conflicts in the journal day-file, `meta-deploy.list`,
  `ccy-changelog.md`, `claude-yolo`, `adder.go` (3), `adder_test.go`, `usage.txt`. Resolution
  as in blockers 1 to 3.
- vs `dbf/hidden-bindings`: conflicts in the journal, `meta-deploy.list`, `ccy-changelog.md`,
  `claude-yolo`, `src-go/internal/ui/picker.go` (this branch's `typingArgs` replaces the
  `--prompt`/`--bind=q:abort` pair that hidden-bindings also touches; `completeKey` is meant to
  become `ui.Action{Label: "Tab complete", Key: "tab"}` there) and
  `src-go/qa/all/addedgates_test.go`.
- vs `feat/team-settings`: conflicts in `meta-deploy.list`, `ccy-changelog.md`, `Dockerfile`,
  `claude-yolo`, `scripts/test-ccy-lifecycle.bash` (the `unset PINGBUS_MEMBERSHIP` line in
  `wrapper_case` meets another edit there).
- Whichever lands second takes the next version pair again; the changelog entries stack, they
  do not merge into one.
- The `scripts/test-ccy-lifecycle.bash` fix (`unset PINGBUS_MEMBERSHIP` in `wrapper_case`) is a
  genuine hermeticity fix unrelated to the feature; fine to carry.
