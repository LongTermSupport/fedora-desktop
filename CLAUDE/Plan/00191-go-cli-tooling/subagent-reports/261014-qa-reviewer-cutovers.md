I could not write the report to `CLAUDE/Plan/00191-go-cli-tooling/subagent-reports/261014-qa-reviewer-cutovers.md`. My role has no Write or Edit, and I may not create files through Bash, so you will need to save this reply there yourself. I did create one file I should not have: `untracked/scratch/qa-reviewer-qa-all-00191.log`, the output of `qa-all.bash` (untracked; delete it if unwanted).

## QA Review — Plan 00191, `origin/F44...F44` (35 commits, not yet pushed)

**Verdict**: FIX-BEFORE-MERGE

### Should fix

1. **The builds are not reproducible, so the play reports "changed" after every commit.** `playbooks/imports/play-claude-yolo.yml:852-867` and `:889-904`. `go build` writes the git commit into the binary by default (`-trimpath` does not stop this). `go version -m untracked/scratch/ccy-teams` shows `vcs.revision=5f8edc36…` and `vcs.modified=true`. So any new commit, or any uncommitted edit, changes the binary and `copy` reports changed. The comments' claims ("byte-for-byte reproducible", "an unchanged source reports ok") are false. Fix: add `-buildvcs=false` to both builds, and to the suites that claim to build "as the play builds it" (`scripts/test-ccy-*.bash`; `scripts/test-ccy-tmux-targets.bash:237` also lacks `-trimpath`).
2. **Bash library code that only the deleted commands called now has no caller, yet suites still test it as if `ccy-sessions` used it.**
   - These functions have zero non-comment callers in `files/`, `helpers/` or `playbooks/` (I checked by grep): `ccy_tmux_confirm`, `ccy_tmux_detail_rows`, `ccy_tmux_row_heading`, `ccy_tmux_other_terminals`, `ccy_claude_screen_state`, `ccy_transcript_for` (in `files/var/local/claude-yolo/lib/tmux-session.bash` and `session-registry.bash`).
   - `scripts/test-ccy-session-network.bash:292` says it drives "ccy_tmux_detail_rows — the thing ccy-sessions actually calls". The Go binary calls no `ccy_*` function; its production path is `src-go/internal/sessprobe`. `CLAUDE/QA.md:98` makes the same false claim, so its 62 passing cases vouch for code nothing runs.
   - The changelog's "ccy and cc still use them" (`docs/ccy-changelog.md:22-29`) is only partly true.
   - Fix: delete the dead functions and their bash cases (lib change, so bump CCY). Keep the functions the Go tests deliberately use as parity references (`ccy_restore_verdict`, `ccy_restore_manifest_update`, `ccy_transcript_*`) and document why they stay. Correct QA.md:98.
3. **The runtime deploy play, which self-update runs unattended, now imports a developer-tooling play.** `play-claude-yolo.yml:5` is in `helpers/self_update/unattended-plays.json:2`. The build needs only the `golang` package. The import also brings `gcc` and a root-owned golangci-lint download from GitHub (`playbooks/imports/optional/common/play-golang.yml:33-62`) into every unattended cycle. `playbook-main.yml` also runs `play-golang` twice (it is imported by both `play-python.yml` and `play-claude-yolo.yml`). Fix: either the build path installs only `golang`, or confirm the owner accepts the wider unattended surface.
4. **`ccy-teams` does not fail fast when built without `-X main.RootDir`.**
   - Where: `src-go/cmd/ccy-teams/main.go:12` and `src-go/internal/teams/config.go:21-35`.
   - Problem: if `RootDir` is empty, the host_vars and play paths become relative to the working directory. A plain `go build` or `go install`, run inside another checkout, would then act on that checkout's `localhost.yml`.
   - Fix: refuse with a clear error when `RootDir == ""`.
5. **`qa-all.bash` fails in this container, and the 35 commits are unpushed.** It failed on `toolchain`, `go` and `helper-counts-reader`: golangci-lint is not on PATH, because the running image predates the Dockerfile pin. With `untracked/tools/bin` on PATH, `qa-go.bash` printed `GO-OK 19 package(s)` and `qa-toolchain.bash` printed `TOOLCHAIN-OK 5 tool(s)`. The push gate cannot pass here until the container is rebuilt with `ccy --rebuild` (the owner's step).

### Nits

6. `scripts/qa-all.bash:914-915` still says "the template renders with no Jinja left and its rendering is lint-clean". There is no template any more.
7. The comment on `CCY_VERSION` (`files/var/local/claude-yolo/claude-yolo:17`) was not updated for 3.95.6/3.95.7.
8. `GO_BINARY_SUITES` in `scripts/qa-changed.bash:330-333` is a hand-kept list. A new `cmd/<x>` would map to no suites without any warning; assert that every `src-go/cmd/*` has an entry.

### Checked and clean

- **Callers of the binaries:** both systemd units, `shutdown-with-update:111,235` and `helpers/self_update/cycle.py:821-829` use the same path and subcommands. `notify`, `verify-restore`, `restore` and `set-going` are routed in `internal/sessions/app.go:172,279-289`, and SIGINT/SIGTERM are handled (`app.go:356`).
- **No dangling references:** the deleted files are named only in historical plan folders and the changelog.
- **Self-update freshness:** `affected_plays` treats `src-go/` (and `go.mod`) as an input of `play-claude-yolo` (both `True`), so a Go change re-runs the play.
- **Play placement:** the build lives in the play that deploys the binaries. It runs as the user (the play has `become: false`, and HOME and GOCACHE are the user's). `go.mod` has no dependencies, so the build needs no network.
- **`.claude/ccy/Dockerfile`:** the change is a comment only, so no image version bump is needed.
- **Docs:** `ccy.md`, `agent-bus.md` and `meta-deploy.bash` are updated.
- **Bash and Go agree on the restore manifest format:** the header `ccy-restore-manifest 2` is the same in both, and the Go tests check it against the bash library.
- **Public-repo safety:** placeholders only (a placeholder home directory, 192.0.2.x, `example-org`).

### Mechanical gates

- `qa-all.bash`: FAIL, for the toolchain reason in item 5 only. Every other gate passed, including `ccy-sessions-reboot` 284, `take-over` 75, `tmux-targets` 20 and `ccy-teams-command` 323.
- `plan-qa --sweep`: 0 block, 15 advise, none for 00191.
- `--syntax-check play-claude-yolo.yml`: passes.
- `qa-helper-tests.bash`: was required (`tests/helpers` changed). All tests passed apart from 2 skips. It also printed one diagnostic about a truncated capture, which I did not dig into.
