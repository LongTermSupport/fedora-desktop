VERDICT: MERGE

All seven round-1 dispositions hold, and the fixes add no blocking defect. I ran the following in the worktree and changed nothing:
- `go test -race ./internal/ccylaunch/...` passed (54.8 s).
- `scripts/test-ccy-gpu-device.bash` passed 22 of 22.
- `./scripts/qa-changed.bash --base 1b931868` exited 0 (shellcheck, semgrep, ansible-syntax and the dependent suites).
- `./scripts/qa-bash-limits.bash` reported BASH-LIMITS-OK.

**Dispositions checked**

1. **Env recording.** `readsEnv`/`passedByName` (`src-go/internal/ccylaunch/difftest/fake_test.go:262-323`) is applied in `recordCall`. The goldens lost the leaked names on getenforce, curl and timedatectl. PLAN.md Task 4.4 now lists token export to every child as bash behaviour the port must NOT keep, with a follow-up named. Holds.
2. **host-paths.bash.**
   - It is sourced first after `common.bash` (`claude-yolo:53-54`), so `print_error` (`common-pure.bash:25`) is defined.
   - It runs before every use of `DOCKERFILE_DIR`, `GPU_DEVICE_DIR` and `ZONEINFO_DIR` (`claude-yolo:2004+`, `:3484`, `:3582`, `:3725`), including the lazy one in `dockerfile-custom.bash:16`.
   - `set -e` comes later (`claude-yolo:85`), but each call ends in `|| exit 1`, so it fails fast anyway.
   - `cc` sources `token-management.bash` but not host-paths, and needs none of the three variables, so nothing breaks there.
   - Unset or empty overrides give the defaults with no check, so `--help`/`--version` behave as before. The test case "an empty override is the default" covers this.
   - `realpath -m` defeats symlinks and `..`. My probes: a symlink to `/dev`, the same with a trailing slash, and `rootlink/dev/dri/..` were all refused.
3. **Play and image.** `play-claude-yolo.yml:483` adds the file to the lib copy loop, mode 0755 like its siblings. The image does not copy `lib/`, so the container version correctly stays 2.78.11.
4. **meta-deploy.** The existing `play-claude-yolo.yml` entry was rewritten in place, not added again; no non-comment line appears twice. The text names CCY 3.99.13.14 and says no rebuild.
5. **TMPDIR.** Every changed site is `${TMPDIR:-/tmp}`, so a host with TMPDIR unset behaves exactly as before. A TMPDIR naming no directory now makes `mktemp` fail under `set -e`, which fails fast; that is acceptable.
6. **Pid, `-update`, mutations.** The pid is marked only in `ccy-claude-update-`, `launch-N-` and a whole argument or line equal to the pid (`sandbox_test.go:515-539`). `-update` with `LAUNCHER` set fails (`parity_test.go:70`), and `TestOracleCanFail` skips under `-update`. `lostLine` requires a golden line holding the mutated text to be gone.
7. **Allowlist, version, changelog, leaks.** `bash-limits.allowlist` only shrank (`claude-yolo` 2810→2809, 2741→2740), and the new lib file is within the limits. The changelog has a 3.99.13.14 entry. No install-specific names appear in the lines the branch adds.

**Non-blocking**

1. **Relative overrides are accepted** (`lib/host-paths.bash:23-36`). The check works on `realpath`, but the raw value is what gets stored.
   - `CCY_GPU_DEVICE_DIR=dri` returns `ok dri`. The value is relative, its parent `.` exists, and if no `./dri` exists the result is silently no GPU, which is the failure this library was meant to stop. If `./dri` does exist, podman gets `--device dri:dri` and rejects it.
   - `CCY_DOCKERFILE_DIR=.` is accepted as `.`.
   - The scenario `override-dockerfile-dir-missing` itself uses a relative value.
   - Suggest requiring an absolute path, or storing `$resolved`.
2. **The refusal is a two-entry denylist.** `/proc` and `/dev/shm` are accepted for the GPU directory and become `--device /proc:/proc` and so on (`common-pure.bash:263-268`). It does what round 1 asked for. An allowlist would be tighter: the GPU directory under `/dev/dri` or the test sandbox, and the other two required to contain an expected file such as `Dockerfile` or `UTC`.
3. **`CCY_ZONEINFO_DIR` as a third production seam is justified but thin.** Without it, `scenarios.json` depends on the host's tzdata. Its only check is that the directory exists, and a wrong value fails loudly later in `ccy_host_time_zone`. Acceptable; the alternative would be a harness-only shim.
4. **A PLAN.md and package-doc wording slip.** They say `/etc/localtime` is "read only when timedatectl names no zone". In fact `claude-yolo:3721-3724` always runs `readlink /etc/localtime`; its value is only used in that case. The goldens are unaffected, but the sentence is inaccurate.
5. **The guide's recovery command is stale.** `files/opt/claude-yolo/docs/CCY-GUIDE.txt:178` still tells users to `cat /tmp/ccy-fix-dockerfile-*.txt`. That file is deleted after use (`claude-yolo` about line 2245), so the line was already wrong, and with TMPDIR set it now points at the wrong directory too. It should be a doc fix in a follow-up.
6. **A possible SELinux difference, inferred and not tested.** `CONFIG_TEMP` is mounted with `:ro` plus `Z` only when `CCY_MOUNT_RELABEL` is set (`claude-yolo:2520`). A TMPDIR under `$HOME`, without relabelling, gives a host label other than `/tmp`'s. Hosts with TMPDIR unset, the normal Fedora case, are unaffected.
7. **One PLAN.md line breaks the wrapping.** "CCY 3.99.13.13 and 3.99.13.14 (the commit hook requires a bump per launcher commit)" runs past the column the rest of the paragraph wraps at. Cosmetic.

Nothing found blocks the merge.