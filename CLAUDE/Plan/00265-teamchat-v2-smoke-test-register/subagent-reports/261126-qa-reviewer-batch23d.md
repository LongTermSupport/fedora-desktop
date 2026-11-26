# QA Review: batch 23, fifth build (34fe00406..e582d7dce)

**Verdict**: FIX-BEFORE-MERGE (two doc lines only; no code defect found)

The delta is one commit, e582d7dce. `git log 34fe00406..e582d7dce` shows only that commit, so
the 6c2cafcc2 / 0f4676546 / eaaa11f89 / 2476e0ad6 split was reviewed as its combined diff.
e582d7dce is not an ancestor of the current HEAD (b7906d3f1). `git diff e582d7dce HEAD -- src-go scripts playbooks` is empty, though, so the tests below ran on the working tree and
exercised the reviewed code.

## Should fix

1. **The owner-facing R79 entry gives the wrong order for the stopgap.**
   `CLAUDE/Plan/meta-deploy.list:138` says the hand-written file is deleted once the list is
   checked, "It then runs play-git-hooks-security.yml". `REGISTER.md:1522` says the same
   ("Only then does it delete the hand-written file. It then runs the play").
   The script (`r79-private-names.bash:104-120`) does something else. It moves the file aside
   before the play and puts it back if the play fails, and deletes it only after `pinned-state`
   reads `generated`. The script's own header (lines 14-15) and REGISTER's later bullet ("now
   moved aside and put back") describe this correctly. The owner reads the list entry before
   running the script, so it should match the script: "moved aside, put back if the play
   fails, deleted once pin has written the new list".

## Nits

2. **Two W13 subtests do not tell the fixed code from the old code.**
   `watch_test.go` "a task entry that is a file" and "a /proc entry that is a file" pass
   without the `IsDir()` skips too. Before the fix, `7/comm` and `task/notes/stat` read as
   `fs.ErrNotExist` on MapFS, and `gone()` skipped them (`watch.go:133-135`). Also, "a /proc
   entry that is a file" asserts nothing beyond `tick` not failing. The named-pipe subtests do
   discriminate, because a MapFS pipe reads fine without the guard. The report says
   `TestW13...` "was red before the fix", which is true only of the pipe cases.
3. **`checkout-by-home-path` can give a false positive on a non-comment user message.**
   A `debug` msg line that names `~/Projects/fedora-desktop` would be flagged
   (`checkouthome.go:22`). The clean fixture covers only `fedora-desktop-fork`. No such line
   exists in the tree today, because a repo sweep of `*.yml` found zero hits outside the
   fixtures. Accept this, or document it in QA.md#checkout-by-home-path.
4. **The R84 interim records a hand-set `core.hooksPath`** (`REGISTER.md:1707`). This is the
   coordinator's recorded decision, and it is reported here only so that the "no manual fix"
   rule's exception stays visible. The play run that is queued through r79 supersedes it only
   for the host checkout.

## Checked and clean

- **Analyzer fixes are root fixes.** `After func(Duration)` became `Wait func()` bound to the
  constant `watch.Interval` (`lightning.go:150-155`). It was not hidden behind an allow row, and
  `TestL17IntervalWaitIsOneInterval` holds the real wait in synctest. `chatclientinfo` no
  longer calls `time.Until` and uses a synctest bubble. Every `/proc` read in `watch.go` goes
  through `readProc` (comm, cmdline, status, stat, task stat, wchan), and the walk loops skip
  entries that are not directories.
- **The /proc guard is correct for /proc.** procfs files stat as regular (`S_IFREG`). A
  `fs.Stat` error on a vanished pid is ENOENT or ESRCH, which `gone()` still treats as a skip.
  `/proc/<pid>` and `task/<tid>` report as directories from `ReadDir`. The stat-then-open gap
  only matters for a pipe planted after the stat, which a real /proc cannot hold.
- **R84 rule.** It runs inside the blocking `qa-ansible` scan over the Ansible trees. Its
  fixtures have 5 flagged forms (`~`, `$HOME`, `/home/{{ user_login }}`, the env lookup and the
  facts env) and the clean fixture is silent. A repo sweep found no other YAML instance. The
  non-YAML hits are user messages, bootstrap code and VM guests (`docker-in-lxc:277`,
  `setup.bash:33`, `CUSTOM-DOCKERFILES.txt`), outside the class. `repo_path: "{{ root_dir }}"`
  matches the pin task. There are a routing row and QA.md docs (row plus section).
- **R79 owner-script safety.** Names are read with echo off and reach `vault.bash` and
  `ansible-vault` on stdin only (`vaultlist.go:264-270, 366-370`). `vault.bash` pipes them with
  the builtin printf. The script's argv holds only a file path (`--import "${pinned}"`). Step 1
  runs before `plan_start_log`, and the only output is counts (line 121 uses awk to count). The
  script fails fast: `set -euo pipefail`, the `pinnedState` failure aborts the capture,
  vault-list failure aborts with nothing moved, a non-tty stdin aborts, and a play that passes
  without leaving a generated list aborts. The stopgap is restored by `plan_on_cleanup` and by
  the explicit call at line 112 whenever the pinned file is absent. A play that fails after pin
  has written leaves the generated list in place. The aside copy is removed on the next run
  (line 118).
- **Public repo.** `qa-private-names.bash tree e582d7dce` found 3 findings, all in the held
  `JOURNAL/2026-11-25/134943Z-finding-cf58c8.md` (lines 15, 20, 21). There are no others. The
  test placeholders changed to `/home/<user>`.
- **Version bumps.** Nothing under `files/var/local/claude-yolo/` changed, so no CCY bump is
  needed. `vault-list` is in `go-binary-suites.tsv` as `qa-go` (not deployed) and in the
  src-go/CLAUDE.md not-deployed list.
- **Docs.** The W1-W13 ranges are updated in src-go/CLAUDE.md and the 00268 PLAN.md.
  SecurityRules, localhost.yml.dist and the play's fail_msg name vault-list.

## Mechanical gates

- Targeted `go test` passed for lightning/..., chatclientinfo, cmd/agent-bus-lightning,
  cmd/vault-list, vaultlist, hostvars, setuprclone, qa/ansible and qa/privatenames.
- `plan-qa --sweep`: no findings for 00265 or 00268 (the 3 block findings are in other plans).
- qa-all and syntax-check were not run, by instruction (no full QA). CI covers them.
