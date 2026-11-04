# Adversarial review: go-port/agent-bus-install (Plan 00192, Task 3.2)

Reviewed at d490689f against F44. Read-only; worktree at `untracked/scratch/fable-abi/wt`.
Evidence paths are relative to the repo root on the branch.

What was verified by running things (not by reading the report):

- `go build` without the tag: `strings` finds 0 occurrences of `AGENT_BUS_INSTALL_TEST_PREFIX`;
  `go tool nm` shows `businstall.Test` is not linked. With `-tags agentbustest`: 2 occurrences.
- `go test -cover`: cmd 46.2% (53.3% tagged), businstall 90.2%, tarstate 93.6%, fsx 91.8%,
  firewalld 95.5%. All green.
- `ansible-playbook --syntax-check playbooks/imports/play-agent-bus.yml`: passes.
- D7 list (B2, B3, B5, B6, B7, B9, B10, B11) and the extra fixes (B4, B8 message, B17, B21):
  each is implemented and pinned by a suite case or a Go test. B12/B13 are NOT "kept as they
  were" (see finding 2).

Verdict: **do not merge yet.** No hole in the root privilege boundary was found; the
`os.Root` writer, the single-parser tar, the CIDR/zone/interface validation, the pin, the
argv regexes and the test-tag isolation hold up. What blocks is a cluster of should-fixes
that are cheap and that the branch's own claims depend on: an unnumbered behaviour change
(process umask) that stubs cannot observe and that reaches `dnf`/`useradd`, an acceptance
document that tells the owner the opposite of what the code does for B12/B13, a lost drift
defence for a root binary run daily by a timer, and two QA-gate relaxations that are wider
than their stated reason. Fix those, then merge.

---

## Findings (ranked)

### 1. should-fix — `umask 077` is set process-wide and inherited by every child (dnf, rpm scriptlets, useradd, zstd, python bundler)

- `src-go/cmd/agent-bus-install/main.go:22-27` sets `syscall.Umask(0o077)` before anything.
- `src-go/internal/hostsys/hostsys.go:298-321` then runs `dnf install -y ...` and `useradd`
  with that umask. The bash installer set no umask (sudo/Ansible give 022). This is a
  behaviour change not in the D7 list and not observable under the suite's stubs (the stub
  `dnf` only touches files). Whether rpm resets the umask for `%post` scriptlets is not
  something this repo should depend on; a first `software` run that installs firewalld or
  NetworkManager on a fresh host is the exposure.
- Fix: do not set a global umask. Every write the installer makes already carries an explicit
  mode on an open descriptor (`fsx.fill`, `fsx.settleDir`, `fsx.writeTemp` with 0600 + chmod),
  so PR5's goal is met without it. If the umask is wanted for the installer's own files, set
  it only around those (or pass `Umask` through `execx.Cmd` as 022 for `hostsys`). Add a
  harness assertion that the umask seen by a child of `dnf`/`useradd` is 022 (the `sim` can
  record `syscall.Umask` through a seam) so the decision stays pinned.

### 2. should-fix — ACCEPTANCE-agent-bus-install.md misdescribes B12 and B13; the owner would misread the probes

- `CLAUDE/Plan/00192-.../ACCEPTANCE-agent-bus-install.md` section 3: "The port kept the bash
  installer's current behaviour for both" and, for B13, "the installer's comparison with the
  plain address text would never match and every run would modify".
- The code does the opposite: `src-go/internal/netprobe/netprobe.go:505-526`
  (`AddressesMatch`) unescapes `\:`, splits on `,`/`|`, drops `--`, compares parsed prefixes;
  `src-go/internal/firewalld/firewalld.go:117-137` with `netprobe.ParseZone` treats the text
  `no zone` as "no zone" whatever the exit status. Both VERIFY items were in fact fixed.
  The subagent report (`subagent-reports/261014-agent-bus-cutover-sonnet.md`, "Behaviour the
  host settles") repeats the wrong statement.
- Fix: rewrite section 3 so the probes confirm the fix rather than diagnose the bash
  behaviour: for B13, an IPv6 host's second run must print no `CHANGED` for `agentbus0`; for
  B12, an interface with status 0 and text `no zone` is handled (text is authoritative). Add
  the two cases to `scripts/test-agent-bus-install.bash` (an IPv6 `--bus-address` second run;
  a `zone-of` stub answering `no zone` with exit 0) so the claim is a gate, not prose.

### 3. should-fix — the play's and the docs' "root never executes a file the user can write" is overstated

- `playbooks/imports/play-agent-bus.yml` header (lines ~249-262): "root never executes a file
  the user can write, and every step below runs that installed copy".
- `src-go/internal/busadmin/busadmin.go:397-408` runs `python3 -B -s -m helpers.pingbus.bundle`
  with `Dir: source` as root: Python code from the user-writable `--source` checkout, plus the
  installer copies `files/usr/local/bin/agent-bus` from that checkout to `/usr/local/bin`
  (`software.go:367`) and later runs it as root. The spec (2.1 item 5) called this the existing
  trust boundary; the port narrows it (the installer binary itself) but does not close it.
  The `copy` in `tasks/build-go-command.yml:74-83` also copies root's installed binary from a
  user-writable scratch path (`~/.cache/ccy-go-build/`): the same boundary, one step earlier.
- Fix: say exactly what changed: "root runs the installed installer, not the checkout script;
  the zipapp bundler and the admin wrapper still come from the checkout (D2)". Same sentence
  in `ACCEPTANCE-agent-bus-install.md` ("Nothing runs a checkout file as root any more") and
  the subagent report.

### 4. should-fix — removing the `qa-deployed-drift` pair drops a recurring defence for a root binary the timer runs daily; the "would exceed the record" reason does not hold for every replacement

- `scripts/qa-deployed-drift.bash` diff: the installer pair is removed; the comment points at
  the plan's `acceptance.bash` (one-off) and ccy-teams' D65 status (only shows when a team is
  declared, and only as "outdated team", not "stale root program"). A host whose
  `/usr/local/sbin/agent-bus-install` is older than the checkout (or is still the bash script)
  is no longer a QA finding anywhere recurring.
- The bash-limits record went 165/1/165 to 164/1/165: one code line of room, no statement of
  room, so a bash `if ...; then fail; fi` would indeed grow it. But the gate also allows
  shrinking: the script can lose one redundant statement to pay for one new one, or (better)
  the comparison can be data, not a statement.
- Fix, in order of preference: (a) teach the existing pair loop a third kind of row,
  `go-binary:<installed path>|<command name>`, whose compare is "installed `--version` equals
  `python3 -m helpers.agent_bus.installer_digest --source .`" and implement that compare in a
  helper the loop already calls (one Python module, no bash growth); this also covers
  `ccy-teams`, `podfreeze`, `lxcfreeze` once they carry a digest, which is where "the system is
  the deliverable" points; (b) if the drift script cannot take that without growth, it is on
  the ratchet precisely so it can be ported: move the gate to Go rather than drop the check.

### 5. should-fix — the bash-limits "ported" rule is wider than the case it was written for

- `src-go/qa/bashlimits/allowlist.go:235-237`: an allowlisted file that is gone is "ported"
  whenever `src-go/cmd/<stem>/main.go` exists, before the exempt-twin check. F44's rule
  (`git show F44:src-go/qa/bashlimits/allowlist.go`, `removalProblem`) flagged the deletion
  because `scripts/test-agent-bus-install.bash` is an exempt twin (`test-` + name). The change
  was needed, but the new test uses the whole name space of `src-go/cmd`.
- Loophole: move an over-limit `files/usr/local/bin/foo` into `scripts/test-foo.bash`
  (exempt), add a stub `src-go/cmd/foo/main.go`; the entry drops and the bash is hidden. A
  stub command is cheap next to a 1000-line script.
- Fix: excuse the twin only when `scripts/go-binary-suites.tsv` names it as the suite of the
  command `<stem>` (that table already exists and already lists
  `agent-bus-install scripts/test-agent-bus-install.bash`). The test
  `TestAFileGoneBesideAnExemptSuiteIsPortedWhenAGoCommandHasItsName` then needs the tsv row
  in its scan, and a sibling case where the twin is exempt but not in the tsv must stay orphan.

### 6. should-fix — the seam rule's `dynamicNames` excuse now silences every wrapper in the excused function, and `RunTransient` hides the program from the rule entirely

- `src-go/qa/rules/seamcommands.go:330-343`: in a function with a `dynamicNames` row, a wrapper
  whose arguments are not a literal is not reported at all. `src-go/qa/rules/table.go:618-622`
  adds `Systemd.RunTransient` with "runs the one-off program its caller names". The caller
  (`src-go/internal/businstall/restore.go:413`) passes
  `[]string{restoredTuwunel, "--restore-backup", id}`; nothing in the rule looks at it, and
  any future caller can run anything under `systemd-run` unseen.
- The row for `Admin.render`/`Admin.Bootstrap` (configured tool path) is sound; the
  `RunTransient` row is not the same shape: the program is known at the call site.
- Fix: give `RunTransient` the program as its own parameter
  (`RunTransient(ctx, unit, properties, program string, args ...string)`), register it in
  `programRunners` so the call site's literal is judged (as `sudo.Run` is), and drop the
  `dynamicNames` row and the `walkProgram` early return. Keep the fixture `dynamic.Unexcused`
  red.

### 7. should-fix — `tarstate.Write` opens the agent-bus-owned team directory by path, not through the `os.Root` the rest of the run uses

- `src-go/internal/businstall/backup.go:117`: `tarstate.Write(&buf, a.path(stateDir+"/"+team), ...)`;
  `src-go/internal/tarstate/write.go:324`: `os.OpenRoot(filepath.Clean(dir))`. `/var/lib/agent-bus`
  is 0700 `agent-bus`, so the homeserver's user can replace `<team>` with a symlink between
  `requireTeamDir` (which did go through `fsAt`/`Sub`, refusing an escape) and `Write`. The
  content requirements (`team.json` + `secrets/`) make exploitation unlikely, and the report
  admits the gap, but it is the one place a root read of that tree bypasses the `os.Root`.
- Fix: `tarstate.Write(w io.Writer, root *os.Root, limits)` and pass `teamFS`'s root (add an
  accessor on `fsx.FS`, or an `fsx.FS` parameter). The write tests already build a root.

### 8. should-fix — `team` checks `SourceDigest` only at the very end, after units were started and bootstrap ran

- `src-go/internal/businstall/team.go:506-509`: `stampTeam` fails with "built without a source
  digest" after `convergeUnits`, `waitReady`, `assertListening`, `Bootstrap`. On the "from
  another project" route (`docs/agent-bus.md:192-207`) a build without `-ldflags` does all the
  work, then exits 1 with no stamp; the docs say it "refuses `team`", which reads as up front.
- Fix: check `a.cfg.SourceDigest != ""` in `team()` before `requireSoftware`, same message.

### 9. nit — `fsx.makeDir` opens the fresh directory without `O_NOFOLLOW` and with `before == nil`

- `src-go/internal/fsx/fsx.go:142-150, 173-199`: `Mkdir` then `OpenFile(name, O_RDONLY|O_DIRECTORY)`;
  `settleDir` skips the `SameFile` check when `before` is nil. In a directory the service user
  owns (`/var/lib/agent-bus/<team>`) the name can be swapped for a symlink to another directory
  inside the root between the two calls; `os.Root` follows in-root symlinks, so the chown/chmod
  lands elsewhere in the team tree. Impact is nil today (everything there is already
  svc 0700), but the writer's own contract ("never through the name") is broken for one call.
- Fix: add `syscall.O_NOFOLLOW` to the `settleDir` open, or lstat after `Mkdir` and pass it.

### 10. nit — one test proves the production build ignores the variable only through a test-only side effect

- `src-go/cmd/agent-bus-install/testmode_stub_test.go:12-25` runs `check --team alpha` and
  asserts the prefix directory stays empty. It stays non-empty under a prefix only because
  `host.go:62-66` does `MkdirAll` for `/var/lib` when `Prefix != ""`: a test-mode branch.
  `TestTheProductionBuildReadsNoTestVariable` (same file, 27-43) is the real proof (getenv is
  never called). Keep both, but have the first assert the stderr names the real path
  (`/var/lib/agent-bus/alpha`) rather than rely on the MkdirAll.

### 11. nit — the Go harness is a second simulator of the same host as the bash stubs

- `src-go/internal/businstall/harness_test.go` (789 lines) mirrors the stubs of
  `scripts/test-agent-bus-install.bash` (systemctl, firewall-cmd, nmcli, curl, ip, ss, rpm,
  dnf, getent, useradd, zstd, uname, systemd-run, journalctl, agent-bus), and carries its own
  copy of the homeserver unit (`hsUnitText`, line 618) instead of reading
  `files/etc/systemd/system/agent-bus-hs@.service`. Two oracles drift; the restore sandbox
  test (`restore_test.go`) pins properties of the fixture, not of the real unit (the bash suite
  does pin the real one, so this is DRY, not correctness).
- Fix: read the real unit file in the harness (walk up from the test dir as
  `digest_agree_test.go` already does), and note in `src-go/CLAUDE.md` which of the two is the
  oracle for what, or shrink the harness to what the Go tests need beyond the suite.

### 12. nit — the injected-failure sweep never reaches the firewall path

- `src-go/internal/businstall/injected_test.go:28-39`: every `team` case uses a team file with
  no `allow_from`, so `firewall-cmd` never appears in `callsOf` and the sweep's "a failing call
  fails the command, except the probes" is not stated for `--query-rich-rule` (whose failure is
  an answer, per `firewalld.Query`) or `--add-rich-rule`. `team_test.go:416-430` covers the
  add failure; the query-as-answer semantics are only in `firewalld_test.go`.
- Fix: add an `allow_from` to the sweep's team file and `firewall-cmd .*--query-rich-rule=`
  to `resultProbes`.

### 13. nit — unnumbered parity deviations worth a line in the spec's section 11

- `hostsys.UserExists` (`hostsys.go:304-313`): `getent` exiting with anything but 2 is now an
  error; bash treated any non-zero as "absent" and ran `useradd`. Fail-fast, fine, but
  unlisted.
- `software.go:342-353`: the pin and resolver stub are installed after Tuwunel, not before
  (bash steps 11 then 12); only the `CHANGED` order on stdout differs.
- `team.go:184-203`: another team's hostile `team` field is printed into a stderr line
  (`port %d is team %s's`) unescaped; the installer quotes tar member names
  (`tarstate.quote`) but not this. Pass it through `teamPattern` or `strconv.Quote`.
- `hostnet.Download` (`hostnet.go:219-228`) honours root's `~/.curlrc` and proxy variables;
  the hash check makes this safe, but `-q` (ignore curlrc) costs nothing and removes a
  variable from the trust story.

### 14. nit — `play-golang.yml` is imported by `play-agent-bus.yml` as well as by `play-claude-yolo.yml`

- A top-level playbook importing both runs the toolchain play twice (idempotent, slower). Not
  wrong; worth a comment or a single import at the top level.

---

## Checklist against the brief

**(1) Privilege boundary.** argv: `--team` and in-file team name by anchored regex
(`app.go:124`, `team.go:114-127`); `--backup` by `^[1-9][0-9]*$` (`restore.go:274`);
`--source` absolute plus 14 required files; `--bus-address` by `busaddr.Parse` (zone, mapped,
canonical, unusable classes); ports 1-65535 in `firewalld.RichRule`; CIDR must be canonical and
not 4-in-6; zone names by `zonePattern`; interface names by `validInterface`. Environment: PATH
reset in the production config (`main.go:36-40`, `config.go:19`), test variable compiled out
(verified), `PYTHON*` unset for the bundler. TMPDIR/HOME unused: the work directory is under
`/var/lib/agent-bus-install/.work-*` (`host.go:182-200`). Files as root: every write through
`os.Root`, O_EXCL|O_NOFOLLOW temp, fchown/fchmod on the descriptor, rename; symlink at the
destination refused; one `os.Root` gap (finding 7), one open without NOFOLLOW (finding 9).
State tar: one parser, limits, typeflag whitelist, control characters, depth, duplicates,
file-vs-parent conflicts, the validated bytes are what restore writes. Firewall: rules are
argv elements, never a shell. Units: the installer renders none itself; the drop-in comes from
the admin tool; the transient unit's properties are the installed unit's `[Service]` with
`ExecStart` dropped and any other `%` refused. User creation: fixed argv. Secrets: 64 random
bytes as hex, 0600 svc, never printed; the stamp holds the digest only. Download: sha256 of
the asset against the pin before decompression; link made only after. Exit codes: B3 closed;
77/78 still pass through from the admin tool by contract (its 77 means "wrapper not root",
indistinguishable from the installer's, but the installer is root by then so it cannot occur).
Umask: finding 1.

**(2) Parity.** The suite drives the tagged binary; the only difference from the production
binary is `loadConfig` (`testmode.go` vs `testmode_stub.go`), i.e. prefix, admin tool by name,
PATH kept, owners = current user. Every flow, parser and writer is the production code. What
the suite cannot reach: a chown to a different uid, `LookupOwner("agent-bus")`, the real
`Euid`, the umask effect on children (finding 1). Deviations outside D7: finding 13.

**(3) Deployment.** Build as the login user, `copy` as root to `/usr/local/sbin`, owner
`root`, mode 0755; `agent_bus_installer` is the installed path; the digest task is
`become: false`, `changed_when: false`, runs under `--check`. Idempotent: `-trimpath -buildvcs=false` with fixed ldflags reproduces; `copy` compares checksums. A host with the bash
script: the `copy` replaces it before `software` runs. ccy-teams' `sudo -n /usr/local/sbin/agent-bus-install ...` and the sudoers rule are untouched; the rule never named
the installer (B1 comment fixed in `sudo.go`). Trust claim: finding 3.

**(4) Digest.** `digest_agree_test.go` proves Python and `go list -deps` agree and that the
built binary prints the baked value. The definition includes `testmode.go` (a tag-only file)
and excludes the toolchain version beyond `go.mod`'s `go` line: acceptable for option A, say so
in the docstring. Bash-era stamps showing "out of date" until the play restamps: acceptable and
documented.

**(5a/b/c).** Findings 4, 5, 6.

**(6) Code quality.** Typed exit codes, no discarded errors, `errors.AsType` throughout,
explicit modes as constants, one `fail`/`failf` shape. Repetition: `failf("%v", err)` appears
dozens of times where `wrapFS` exists (`software.go:262-267`); pick one. cmd coverage 46% is
`main` and the `Setenv` failure branch; the loadConfig pair is tested under both tags. The
`blockedOutcome` table (`blocked_test.go:138-214`) characterises exit statuses per path and
will need editing on any deliberate status change; that is acknowledged.
