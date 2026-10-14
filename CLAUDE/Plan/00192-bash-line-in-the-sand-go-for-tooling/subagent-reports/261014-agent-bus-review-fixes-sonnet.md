# Agent-bus review fixes (qa-reviewer items 3 to 6), branch go-port/agent-bus-install

Merged F44 first (`git merge --no-ff`): QA.md kept both rows' intent (the `qa-go` row gains F44's
"and over the nested `src-go/qa` module", the `qa-bash-limits` row keeps the ported-file clause);
`meta-deploy.bash` keeps this branch's 00192 entry comment plus F44's Plan 00177 and sudo-fix text
and drops the `play-python.yml` entry F44 had already retired; the allowlist was taken from F44
and rewritten only with `scripts/qa-bash-limits.bash --update`. One merge interaction fixed:
this branch's `tagliatelle` snake-case setting flagged F44's `qa/changed/golden_test.go`, so that
file joins `checks.go` in the existing exclusion.

## Findings and fixes

| Item | Finding                                                                                                                    | Fix                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                        |
| ---- | -------------------------------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 3    | New standalone Python CLIs `helpers/go_drift/check.py` and `installer_digest` (run on every build)                         | Ported to Go: package `src-go/qa/gobuild` and command `src-go/qa/cmd/go-source` (`digest`, `drift`), process start through `qa/internal/proc` (the QA module's `execx`), typed, tested, imports parsed with `go/parser` instead of regexes. `tasks/build-go-command.yml` runs `go run ./cmd/go-source digest` (chdir `src-go/qa`, login user, same Go env as the build); `qa-deployed-drift.bash` runs `go-source drift` via `lib/go-tool.bash`. Python deleted: `installer_digest.py`, `go_drift/check.py` and their tests. `declare.py` (a standalone CLI, not Ansible-loaded) keeps no copy of the definition: `installer_source_digest` runs the Go tool and raises `DeclareError` when it cannot. The digests are byte-identical to the Python ones for all five commands (compared before deletion). |
| 4    | Agreement test covered only agent-bus-install                                                                              | `qa/gobuild/agree_test.go` takes the population from `src-go/cmd` (and asserts `scripts/go-binary-suites.tsv` and `scripts/go-binary-installs.tsv` name exactly it, each row's play building that command): for every command the digest closure equals `go list -deps`, and a binary built the play's way prints the digest for `--version`. Replaces `internal/businstall/digest_agree_test.go`.                                                                                                                                                                                                                                                                                                                                                                                                         |
| 5    | `installer_digest` lived in `agent_bus` though every command uses it                                                       | The definition now lives in the neutral package `gobuild`; no non-agent-bus command or play depends on anything agent-bus.                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                                 |
| 6    | `qa-deployed-drift.bash` reported a checker that could not run as drift, and mixed binaries with script pairs in one count | Exit 2 from the Go check is its own message ("could not run ... not drift") and the gate exits 2; exit 1 is drift. The failing summary counts script pairs only and is printed only when pairs drifted; Go commands are counted and reported by `go-source drift` itself. New suite `scripts/test-qa-deployed-drift.bash` (5 cases against fixture checkouts, since the gate skips in the container and in worktrees), wired into `qa-all.bash` and QA.md.                                                                                                                                                                                                                                                                                                                                                 |

Red first: the Go tests failed to compile before `gobuild` existed; the bash suite failed on the
old script (it printed "2 of 1 deployed script(s) differ" and a Python import error).

## Checks run

- `scripts/qa-go.bash`: GO-OK (51 packages, coverage 94.0%, qa module 94.4%, project rules clean).
- `shellcheck` on the touched scripts: only the existing SC1091 info about sourced libraries.
- `scripts/qa-bash-limits.bash`: OK; `qa-deployed-drift.bash` and `qa-all.bash` shrank (records
  lowered with `--update`; `qa-all.bash` needed neighbouring stanzas tightened to make room for the
  new suite's stanza).
- `scripts/qa-ansible-syntax.bash`: 83 playbooks OK (`build-go-command.yml` is covered through the
  plays that import it).
- `scripts/qa-helper-tests.bash`: 4036 tests OK. `scripts/qa-changed.bash`: passed, including
  `test-agent-bus-install`, `test-qa-helper-summary`, `test-qa-changed` and the new suite.
- `qa-all.bash` not run in full, as instructed.

## For the host run

The next `meta-deploy.bash` run rebuilds every Go command (their source digests changed with the
neutral package and the `srcdigest` comment); `play-agent-bus.yml` and the others now run
`go run` for the digest, so the login user needs the Go toolchain on PATH, which `play-golang.yml`
already provides for the build itself.
