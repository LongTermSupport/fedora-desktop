# agent-bus-install cut-over wave (Plan 00192, Task 3.2)

Branch `go-port/agent-bus-install`. F44 merged first (conflicts resolved, below), then the
cut-over. Nothing pushed, nothing merged into F44, no Ansible run (syntax-check only).

## The F44 merge

- **term.ExitCode.** The run functions return it; `ExitNotRoot` (77) and `ExitConfig` (78) are
  `term.ExitCode` constants. `teams/flow.InPicker` lists them in its exhaustive switch.
- **fsx is one package.** The installer's `os.Root` writer stays in `fsx.go` (`FS`, `Open`,
  `EnsureDir`, `PutFile` ...); F44's `WriteAtomic` and `RemoveIfPresent` are `atomic.go`; one
  package comment. F44's test `TestRemoveIfPresent` was renamed (the method of that name has one).
- **Project rules.** F44's hardened table (lists of owners, programRunners, wrappers seen through,
  constants for names) was taken whole and the installer's programs added to it: `journalctl` to
  systemd, `firewall-cmd` to firewalld, `ip`/`ss`/`curl` to hostnet, `rpm`/`dnf`/`getent`/
  `useradd`/`uname`/`zstd` to hostsys, `nmcli` to lxcfreeze and hostnet. The installer code was
  refactored to name its programs as constants (hostnet and hostsys take an `execx.Cmd`, not a
  name). Two rows in `dynamicNames` for busadmin (the admin tool at its configured path).
- **Rule extended (Defence Before Fix, red first).** `systemd.RunTransient` runs a program its
  caller names under `systemd-run`; a wrapper whose arguments are not a literal is a finding
  unless the function has a `dynamicNames` row. Fixtures: `dynamic.Transient` clean,
  `dynamic.Unexcused` flagged. Catalogue updated in `CLAUDE/GoProjectRules.md`.
- **Bash-limits gate.** `scripts/go-binary-suites.tsv` replaces `GO_BINARY_SUITES`; the installer
  has its row there (the entry on the ccy-teams line is gone). The allowlist row of the deleted
  installer needed a gate fix: a file gone beside an exempt suite of its name counted as moved
  out of sight. It is now ported when `src-go/cmd/<name>/main.go` exists (test first).
- Journal and docs conflicts: both sides kept.

## The cut-over

1. **D1 digest.** Defined in `helpers/agent_bus/installer_digest.py` (docstring is the
   definition): `cmd/agent-bus-install` plus every package of the module it imports, read from
   the `.go` files' imports; the files are `go.mod` and every regular file directly in a closure
   directory except `*_test.go` (so the embedded `usage.txt` counts); sorted by path; sha256 of
   `path NUL filehash LF` per file. `declare.declared_software` uses it (a `DigestError` becomes
   a `DeclareError`); `INSTALLER_FILE` is gone. Python tests: toy tree (closure, file list, exact
   digest, what changes it and what does not, errors, the command line) and the real checkout.
   Go test `internal/businstall/digest_agree_test.go` computes the digest from the closure `go list -deps` reports and compares with the Python module's answer, and builds the binary with
   that value in `-ldflags` and checks `--version` prints it.
2. **Deploy.** `tasks/build-go-command.yml` gained optional `go_command_owner` and
   `go_command_become` (the build stays the login user's; only the `copy` is elevated).
   `play-agent-bus.yml` imports `play-golang.yml`, computes the digest (`check_mode: false`, read
   only), builds and installs the root-owned 0755 copy at `/usr/local/sbin/agent-bus-install`, and
   `agent_bus_installer` is that path: root runs the installed installer, not a checkout script (corrected after review: it still runs the checkout's Python bundler and wrapper). The sudo argv of ccy-teams and
   the units are unchanged.
3. **Python tests and drift.** `test_member_docs.py` read the bash source three ways: the
   synopsis now comes from `src-go/internal/businstall/usage.txt` (Go tests pin that `--help`
   prints it byte for byte, so no toolchain is needed); the kit file list and the kit directory
   are read from the Go constants (`kitFiles`, `usrLocalDir` + `kitRel`). `test_declare` uses the
   source digest. `qa-deployed-drift.bash`: the installer pair is **retired**, not replaced: a
   binary has no byte pair, and a replacement in that script would exceed its allowlist record
   (never fold statements). The digest comparison lives in the plan's `acceptance.bash`
   (`--version` vs the helper) and in ccy-teams' D65 status. Plan 00161's `deploy.bash` and
   `acceptance.bash` now name the installed path and refuse, naming the play, when it is not
   installed.
4. **Deletions and coverage.** The bash installer; `tarstate.Extract` (the file modes the
   backup writer used moved to `write.go`; the `walk` helper went with its tests); the suite's
   bash mode (`AGENT_BUS_INSTALL_SUITE`), the checkout copy of the installer and the Go-only
   `if`. The suite passes 302 cases against the Go binary. Coverage: `businstall` 90.2%
   (was 86.3), `fsx` 91.8% (84.8), whole module 94.1%. The gains are two sweeps that also pin
   fail-fast behaviour: every directory the installer works in replaced by a regular file, per
   command, with the exact exit status (`blocked_test.go`), and every external call of each
   command failing in turn, which must fail the command with a message except the two probe
   kinds that are answers (`injected_test.go`). `fsx` got direct failure tests. The unreachable
   branches left uncovered are syscall failures of a successful `Chown`/`Chmod`/`Close`.
5. **meta-deploy.** Plan 00192 was already in `PLANS`; its `deploy.bash` now also runs
   `play-agent-bus.yml` (so the play runs through the plan) and `acceptance.bash` checks the
   installed binary (ELF, root:root 755, `--version` is the checkout's digest, the bash file
   gone) and records the raw B12 and B13 probe output in its run directory, unjudged. The owner's
   steps, including how to read the probes, are `ACCEPTANCE-agent-bus-install.md`.
6. **Docs.** `docs/agent-bus.md` ("From another project" now builds the binary and installs it
   root-owned, with the ldflags), `docs/playbooks.md`, `CLAUDE/QA.md` (suite row, gate rule),
   `src-go/CLAUDE.md`, `CLAUDE/GoProjectRules.md`, PLAN.md Task 3.2, a journal entry.

## Gates run

`qa-go.bash` GO-OK (94.1%); `qa-bash-limits.bash` OK (allowlist 110 to 109 entries, the drift
script's record down one); `qa-changed.bash --base F44` green (with shellcheck following sources:
the container has no shellcheck config, and the Plan 00161 scripts that were edited are only
clean with `-x`); `test-qa-changed.bash` PASS; the agent_bus helper tests (399); `ansible-playbook --syntax-check` of play-agent-bus, play-claude-yolo, play-podfreeze and play-lxcfreeze.
`qa-all.bash` not run (not asked).

## For the owner and the reviewer

- **B12 and B13 (corrected after review):** both are fixed in the port, not kept (`ParseZone` treats
  the text `no zone` as authoritative; `AddressesMatch` normalises terse `nmcli` output), and pinned by
  suite cases; the host probes confirm the real tools.
- **A one-time visible effect:** teams stamped by the bash installer show out of date in
  ccy-teams until the play restamps them (the digest's meaning changed).
- **First run on a host that has the bash installer:** the `copy` replaces the script with the
  binary; the play then runs the installed copy. Corrected after review: root no longer runs the installer as a checkout script, but the bundler (checkout Python) and the admin wrapper (copied from the checkout) still run as root.
- **`tarstate.Write` opened the team directory by path** (a minor TOCTOU): fixed after review, it takes the `os.Root` the run already holds.
- **Review points:** the `dynamicNames` excuse for a wrapper (a fixture proves the unexcused
  twin is still flagged); the gate's ported-by-name rule; `cmd/agent-bus-install` coverage is
  46% (the `main` and the syscall wiring); the sweeps characterise present behaviour, so a
  deliberate change to an exit status in a failure path changes `blockedOutcome`.
