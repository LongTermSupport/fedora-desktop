# Defence Before Fix report: the resolver bind (teamchat-server@ 226/NAMESPACE)

## Specification followed

Method specification 1.1.0 (cached copy, as the refresh script printed it), with the detector
specification 1.0.0 and the toolchain specification 0.2.0 (both cached). The project prompt
was read in full.

## Toolchain

The repository has no DBF manifest declaration (no `defenceBeforeFix` or
`extra.defence-before-fix` key, and no known-gap record). The project's own defences are
`src-go/qa/rules` (Go analyzers), semgrep rules under `.semgrep/`, and Go tests run by the Go
gate (`scripts/qa-go-qa.bash`). The task allowed the detector to be "a Go test or project
rule". A Go analyzer reads Go syntax, and the unit text here is built by string
concatenation, so it cannot judge it. The detector is therefore a rule function in Go,
`internal/unitsandbox.Check`, that reads unit text statically. It runs through Go tests, the
same form `unit-name-with-suffix-read-as-email` uses. No calibration was recorded, so the
calibration assumed is: fail closed on any bind source whose kind is unknown.

## Defect and class

- **Originating instance:** `src-go/internal/teamchat/host/sandbox.go:47-49` (as it was):
  `TemporaryFileSystem=/run/systemd/resolve:ro` plus two FILE binds of `paths.ResolverStub`
  onto `stub-resolv.conf` and `resolv.conf` inside it. This was rendered into
  `teamchat-server@.service` and into every team's transient sandbox (`host.TeamSandbox`).
- **Class:** a systemd sandbox that binds a file, or a source nobody can show to be a
  directory, onto a path strictly inside a `TemporaryFileSystem=` path.
- **Hazard:** systemd must create the file's mount point inside the tmpfs it has made. When
  that tmpfs is read only, it cannot (EACCES on systemd 259), and the unit exits 226/NAMESPACE.

## Rule

- **Identifier:** `tmpfs-file-bind`. Documented in `CLAUDE/QA.md` under that heading.
- **Code:** `src-go/internal/unitsandbox/unitsandbox.go`.
- **Message:** `tmpfs-file-bind: line N binds a file (SRC) onto DEST inside TemporaryFileSystem=T; bind a directory over T instead`.
  For an unknown source, "binds a source it cannot tell is a directory".
- **What it parses:** `Key=value` lines, indented or not, so YAML blocks are read too. It
  understands systemd's list syntax: several entries on a line, the `-` and `+` prefixes,
  `src` alone or `src:dest:opts`, quoted entries, and an empty assignment resetting the list.
  After the review it also reads a property written as a string literal in code (Python
  `f"Key=value",`, a quoted shell argument) and a `systemd-run --property=` argument; a code
  placeholder source (`{binary}`) is unknown and fails closed.
- **What it excludes, because these carry no hazard:**
  - a bind whose source is a known directory;
  - a bind outside every tmpfs path;
  - a bind onto the tmpfs path itself.
- **Next wider rule considered:** any bind inside a tmpfs at all, directories included. Not
  built for two reasons. The operator reported that a directory bind works. v1's
  `TemporaryFileSystem=/var/lib/agent-bus:ro` with `BindPaths=/var/lib/agent-bus/%i` is a
  directory bind that has been running. Refusing directories would match code that does not
  carry the hazard.

## Proof

- **Fixtures kept in `unitsandbox_test.go`:**
  - the originating lines, giving two findings;
  - an unknown source, which fails closed;
  - six cases that must not be findings;
  - the list syntax.
- **Red commit `2a6732b98`**, which also holds the sweep and teamchat tests. In that commit:
  - `go test ./internal/teamchat/unitfiles -run TestNoUnitBindsAFileInsideATmpfs` failed with
    4 findings: 2 in `teamchat-server@.service` and 2 in the ops team's transient sandbox;
  - `go test ./internal/unitsandbox` failed with 3 findings: 2 in `agent-bus-hs@.service` and
    1 in the assertion in `scripts/test-agent-bus-install.bash`;
  - `TestEveryGoRendererIsJudged` failed on `unitfiles.go`.
- **Second red commit `a2e0057d3`** (the rule widened after the conformance review, below):
  `go test ./internal/unitsandbox` failed with 10 findings, 5 in Plan 00161's
  `triage_probe.py` and 5 in its `test_triage_probe.py`. A fixture,
  `TestQuotedAndPropertyLines`, keeps the f-string and `--property=` forms.
- **Commit order:** red `2a6732b98`, then fix `6cca8982f` (teamchat and v1 together), then red
  `a2e0057d3`, then fix `487be476c` (the probe). The original teamchat fix shares its commit
  with the v1 sweep fix rather than coming strictly last (review finding, accepted as is; each
  fix commit is green on its own).

## Independent search

The `defence-before-fix:independent-searcher` agent was dispatched before any rule existed and
was kept out of `internal/unitsandbox`. Its saved reply is
`untracked/agent-reports/auto/261111-090910-defence-before-fix_independent-searcher-a2d0b28518773d094.md`.

- **What it found:** the same instances, plus three the rule had not been aimed at:
  - **v1's restore transient unit**: `agent-bus-install` `sandbox_properties` reads
    `agent-bus-hs@.service`, so fixing that unit fixes the transient unit too;
  - **the golden file and `sandbox_test.go`**: pinned copies of the rendered text, updated
    with the fix;
  - **Plan 00161's `triage_probe.py`**: file binds into read-only `/mnt` and
    `/run/systemd/resolve`.
- **Text search versus reading:** text search found every line, including the probe's and the
  test assertion's, but could not tell file from directory. Reading established that each source
  is a file (`put_file`, `paths.go`, the probe's `unit_dir` writes) and found the bash
  (`sandbox_properties`) and Go generators, which no text search for the directive names.
- **Effect on the rule:** the search won. The first rule could not read the probe's f-strings
  and its sweep left plan folders out; the conformance review called that a suppression. The
  rule was widened to read string literals and `--property=` arguments and the sweep to plan
  folders (`a2e0057d3`), and the probe was fixed. The near-miss the searcher flagged (a
  directory bind inside a tmpfs) is outside the class, as above.

## Sweep and count

The repository had no recorded scope, so this report records the scope decision:

- **Swept:** every non-Markdown file that sets `TemporaryFileSystem=`, outside `src-go/` and
  the trees git does not publish (`.git`, `untracked/`, `.claude/`, `node_modules/`). That
  covers `files/`, `playbooks/`, `tasks/`, `scripts/`, `helpers/`, `CLAUDE/` (plan probes and
  their tests, `Completed/` included) and the rest. Markdown is prose that quotes units.
- **src-go:** judged on its rendered output. `TestEveryGoRendererIsJudged` refuses any Go file
  that writes the key without such a test. `vendor/` and `testdata/` are not first-party code.

Instances found, 15 in all, all fixed:

| Where                                                       | Instances | Note             |
| ----------------------------------------------------------- | --------- | ---------------- |
| teamchat `host.BaseSandbox` (server template and transient) | 2 binds   |                  |
| v1 `agent-bus-hs@.service` (and its restore transient)      | 2 binds   |                  |
| v1 installer test assertion                                 | 1 line    | test data for v1 |
| Plan 00161 `triage_probe.py`                                | 5 binds   |                  |
| Plan 00161 `test_triage_probe.py` assertions                | 5 lines   | its test         |

## Fixes

Each instance was examined individually.

**teamchat**

- New paths: `paths.ResolverDir` (`/usr/local/share/teamchat/resolve`), `ResolverStubFile`
  and `ResolverFile`. They replace `ResolverStub`.
- `host.BaseSandbox` now has one line, `BindReadOnlyPaths=<dir>:/run/systemd/resolve`. The
  `TemporaryFileSystem=` line is gone, and a comment says why. `TeamSandbox` inherits the change.
- `unitfiles` installs two files, `stub-resolv.conf` and `resolv.conf`, both with the stub
  text, so `units list` and `units print` show them.
- The terminator's dropped-key list no longer names the removed key.
- `play-teamchat.yml`:
  - new variable `tc_resolver_dir`, held equal to `paths.ResolverDir` by `installplay`;
  - the directory is made 0755 root in "Make the Directories";
  - a task removes the old `{{ tc_share_dir }}/resolv.conf`.
- SELinux: the files keep `/usr/local/share`'s default `usr_t` label, as the old stub did, so
  no context task was added. The design now says so.
- Updated with the fix:
  - goldens: `resolv.conf`, the new `stub-resolv.conf`, `teamchat-server@.service`;
  - tests: `sandbox_test.go`, `unitsverb_test.go`, `paths_test.go`, `separation_test.go`,
    `v1names/units_test.go`;
  - design: `DESIGN-v2-C-host.md` 3.2.3 (the block and a paragraph on why) and step 07.

**v1**

- The stub moved to `files/usr/local/share/agent-bus/resolve/{stub-resolv,resolv}.conf`.
- `agent-bus-hs@.service` binds that directory over `/run/systemd/resolve`.
- `agent-bus-install software` makes `$SHARE_DIR/resolve` and installs both files. It stays
  within its bash-limits ratchet record (the pin and the stub now share one loop).
- `scripts/test-agent-bus-install.bash` was updated. Its result: `passed: 311 failed: 0`.
- Plan 00161 `DESIGN.md` 3.5 and the U16 row were updated.

**Plan 00161's H3 probe**

- `unit_properties` now takes two directories. One is the unit directory: the binary is copied
  in beside `tuwunel.toml` and the secret, and it is bound over `/mnt`. The other is `resolve/`,
  which holds the stub under both names and is bound over `/run/systemd/resolve`. Both
  directories are 0755.
- `TemporaryFileSystem=` is gone. A new test pins that it stays gone.
- The probe's tests pass: `python3 -m unittest test_triage_probe` ran 60 tests, OK.

Each instance was examined by hand: the two resolver pairs, the probe's five binds, and both
test files. None was changed by pattern. No finding was suppressed, and no hazard was left in
place to satisfy the rule.

**Not done (bash ratchet):** v1's old `/usr/local/share/agent-bus/resolv.conf` stays on hosts
that already have it. It is unused and harmless. Removing it would grow `agent-bus-install`
past its ratchet record.

## Decisions referred to the owner

- None remains. Every instance found is fixed, and the rule is at full width.
- The owner may want to know one thing. The H3 probe's fix changes what Plan 00161's recorded
  H3 evidence reproduces: it was taken with the file-bind form, on an earlier systemd.
- The v1 hosts keep a stale `/usr/local/share/agent-bus/resolv.conf`, as said above. It is
  harmless.

## Permanence

- **Blocking:** the rule is three Go tests in the Go gate (`scripts/qa-go-qa.bash`, run by
  `qa-all` and by `qa-changed` for any `src-go/` change).
- **Routing:** a new `scripts/qa-routing.tsv` row runs the Go gate for `files/*.service`,
  `files/*.service.d/*` and the v1 resolver files.
- **Green run:** `go test ./internal/unitsandbox/ ./internal/teamchat/...` passes.
- **`qa-changed`, first round (the diff against HEAD):**
  - every check passed except `go`: toolchain, bash-syntax, shellcheck, semgrep,
    ansible-syntax, ansible-patterns, docs, bash-limits, teamchat-keeps-off-v1, arch-pin,
    argv-leak, version-pins, and every suite it chose, including `test-agent-bus-install.bash`
    (311) and `test-qa-changed.bash` (193);
  - `go` first failed on `gofumpt` in `paths.go`, which was fixed;
  - re-run with `--files` on the Go files, every go-qa stage passed (fmt, golangci, analyzers,
    deadcode, failpoints lint and vet, project-rules-tagged, cross-build) except `runners vuln`.
- **`qa-changed`, second round** (`--files` on the probe, its test, `unitsandbox.go` and
  `QA.md`):
  - passed: ruff, docs, plan-tests, arch-pin, argv-leak, hidden-bindings, and the
    ready-wait, stt-rules, hook-handler-rules and walk-open checks;
  - `go` failed only at `runners vuln`.
- **Known repo-wide failure, not from this change:** `runners vuln` fails on GO-2026-6859 until
  this container runs go1.26.11, which is pending a session restart. That failure stops the
  gate before its own test runner, so the green test result above is a direct `go test` run.

## Toolchain and detector gaps

- The project has no DBF manifest declaration and no listing command that enumerates its
  defences. Defences are found by reading `CLAUDE/QA.md` and `GoProjectRules.md`.

- The detector is a Go test over unit text, not a standalone static tool with a single-rule
  harness. `go test ./internal/unitsandbox -run <Test>` serves as the harness.

- The identifier resolves to `CLAUDE/QA.md#tmpfs-file-bind`, a document the project ships.
  The message carries the identifier but not the path to its documentation, and no test pins
  that the anchor exists. That matches the project's other identifiers.

- No runner check reproduces the 226/NAMESPACE itself. Doing so needs systemd to start a unit
  on a host, which no gate does. The rule's entry says so.

- **Conformance review.** It reproduced both the red run and the green run. Resolved:

  - the high finding (the plan-folder scope was a suppression): the rule was widened and the
    probe fixed;
  - the entry-point evidence: the per-gate results are now listed above.

  Accepted with the reason given:

  - the commit order;
  - the test-based detector (as the task allowed);
  - the message not naming its documentation path.

## The original defect

- **Fix:** `teamchat-server@.service` and a team's transient sandbox now bind the directory
  `/usr/local/share/teamchat/resolve` over `/run/systemd/resolve`, with no
  `TemporaryFileSystem=` line.
- **Tests:** `TestNoUnitBindsAFileInsideATmpfs` reproduces the defect (red in `2a6732b98`) and
  now passes. `TestTheResolverStub` pins the directory bind and both files.
- **Version bump:** CCY 3.118.5 and container 2.84.4, with a `docs/ccy-changelog.md` entry.
- **Host run:** queued under `changed-plays` in `CLAUDE/Plan/meta-deploy.list`. It covers
  play-teamchat, play-agent-bus and play-claude-yolo.
