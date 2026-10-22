VERDICT: FIX-FIRST

I ran the checks against tip 0390d976 in the worktree and changed nothing. All passed:

- `go test` for ccylaunch, difftest, engine and cmd/claude-yolo, with Go 1.26.9 and `GOTOOLCHAIN=local`.
- The difftest with `LAUNCHER=` the built binary, with `claude-yolo.bash` and `lib/` beside it: TestLauncherParity 82 of 82.
- `test-ccy-info-flags.bash`: 90 of 90.
- `test-ccy-container-version-hook.bash`: 105 of 105.
- `qa-changed.bash --base integration/261021-membership`: green.
- `ansible-playbook --syntax-check` on play-claude-yolo.yml: clean.

## Blocking

**1. Session records now name `claude-yolo.bash`, so restores and team restarts skip the Go front, and they keep doing so for as long as the record lasts.**

- At `files/var/local/claude-yolo/claude-yolo:1179` the tmux insulation passes `"$SCRIPT_DIR/$(basename "$0")"` as the launcher. Once the front execs the bash, `$0` is `/var/local/claude-yolo/claude-yolo.bash`.
- `lib/tmux-session.bash:409,415` writes that path into the registry record (`launcher=`).
- The record is replayed as written: `src-go/internal/sessions/restore.go:236` and `src-go/internal/teams/restart/jobs.go:288` both run `rec.Launcher`.
- So every session started from a terminal after this deploy, `ccy --teams` from ccy-teams included, is restored and restarted by the bash directly. Three consequences:
  - Whatever later phases move into Go never reaches restored or restarted sessions.
  - Records written now will break when the strangler ends and `claude-yolo.bash` is removed.
  - After the documented rollback (old play revision), restores run the stale 3.99.14.0 `.bash` against the older `lib/`.
- The play comment at `play-claude-yolo.yml:558` says the front's path is "the path the ccy alias, session records and restores name". That is no longer true.
- The restart relaunch at `claude-yolo:3845` uses `"$SCRIPT_DIR/claude-yolo"` (the front), which is inconsistent with 1179.
- None of the harness, info-flags or registry suites catch this: the difftest has no terminal, so the insulation path never runs.
- Fix: pass `"$SCRIPT_DIR/claude-yolo"` at 1179. The re-exec inside tmux then goes front → bash, which is harmless. Add a test pinning the `launcher=` value the front path records.

**2. Live plan scripts read the deployed launcher as bash text and get an ELF.** All three plans are In Progress:

- `CLAUDE/Plan/00203-…/m2-check.bash:178` reads `REQUIRED_CONTAINER_VERSION` from `/var/local/claude-yolo/claude-yolo` with awk. It now gets an empty value and fails with the misleading "the launcher requires " (blank).
- `CLAUDE/Plan/00161-…/_acceptance-u20.inc.bash:242-244` (and `triage-ssh-agent-probe.bash:72-73`) read `CCY_VERSION` and `CONFIG_VERSION` the same way. The u20 acceptance now reports "the installed ccy names no …", a false installation fault.
- `CLAUDE/Plan/00169-…/acceptance.bash:163,170` runs `cmp -s` of the checkout's `claude-yolo` against the deployed one. That now always fails ("not identical").
- Fix: point these text reads at `claude-yolo.bash` in the same change. Nothing outside plan folders sources or greps the deployed file: the alias, ccy-sessions, ccy-teams, wsitoken and the VM acceptance `-x` checks all only exec it or test it.

## Not blocking

**3. Install order and atomicity are sound.**

- The bash is copied to `.bash` first, then the version is read and asserted, then the build, then a checksum-compared `copy` (temp file plus rename, so atomic). A running exec is safe.
- A first run on a host works because the `.bash` lands before the front.
- A failed build leaves the old `claude-yolo` in place. `GOTOOLCHAIN=local` against `go 1.26.0` / `toolchain go1.26.9` builds on a host with 1.26.9 or 1.26.10.
- Root:root 0755. The build runs as the login user; only the copy becomes root.

**4. The version-mismatch refusal is acceptable.**

- It fires during the build window of every update, and persistently if a build fails: `ccy --version` and `--help` exit 1, naming both versions and the fix. Launches still hand over.
- No automated caller runs `ccy --version`. Quick Launch compares the hash inside the bash.
- If the front itself is the defect (it will not start), `CCY_LEGACY` cannot help, because the front is what reads it. docs/ccy.md should also say "or run `/var/local/claude-yolo/claude-yolo.bash` directly".

**5. The D3 narrowing is sound.**

- `IntegrityHash` matches the bash block byte for byte. A test runs the real block over a tree with links, dotfiles, subdirectories, `*.py`, mixed-case names and a file with no final newline.
- `$0` becoming `.bash` does not change the hash: same text, same `lib/`.
- What it misses, already stated in the PLAN: changes to shared packages (`execx`, `engine`, `term`), to `go.mod` or to vendor change the binary with no bump and no change in the hash or version. A version pin also cannot tell two branches apart that share a CCY_VERSION. Worth a line in ContainerRules.md, not only in the PLAN.

**6. The hook is correct.**

- The exclusion arm comes first. In `case`, `*` matches `/`, so `src-go/*_test.go` excludes test files anywhere in src-go, and `ccylaunch/*` covers subpackages, with `difftest/` excluded. `help.txt` is gated.
- No other src-go path triggers it.
- The test proves red on three sources and green on the test files and on a bumped commit.

**7. Tests and docs.**

- The help byte-identity test is real: it extracts the heredoc from the checkout.
- The difftest under `LAUNCHER=` uses `{ccy_hash}` placeholders, so it cannot catch a wrong hash. The hash test covers that instead. The PLAN's "82 of 82" is honest.
- No install-specific names found.
- Stale text:
  - `files/opt/claude-yolo/docs/CCY-GUIDE.txt:206` still says "CCY wrapper script". If that file is in the image's build context, the edit needs a container bump.
  - The `system_paths` hook still maps `/var/local/claude-yolo/claude-yolo` to the bash source file.
- The meta-deploy entry explains the host change and the `CCY_LEGACY` check correctly. Its rollback text should add that the old play leaves a stale `claude-yolo.bash`, and that records naming it (finding 1) would run it. That is moot once finding 1 is fixed, apart from records written before the fix.

**8. Hand-over is correct.**

- argv[0] is the full `.bash` path and the environment is passed unchanged (`os.Environ()`).
- `os.Executable` follows symlinks and drops the " (deleted)" suffix, so `.bash` is found beside the real file.
- The `dockerfile-custom` re-exec via `$0` goes straight to `.bash`. That bypasses the front, which is harmless today, but it is the same pattern as finding 1.
