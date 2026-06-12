# Bash Quality Audit

## Scope & Method

This audit covers all Bash/sh scripts in:

- `scripts/` (QA scripts, `git-hooks/`, `vault/`, `lint`, setup and diagnostic scripts)
- `files/var/local/claude-yolo/` (`claude-yolo` 2,633 lines, `lib/*.bash` ~3,900 lines, `entrypoint.sh`)
- `files/home/.local/bin/*` (all bash scripts; Python scripts excluded)
- `files/usr/bin/`, `files/usr/local/bin/`
- `fedora-install/*.bash`

Method: enumerated every script by shebang (57 bash scripts), ran `shellcheck 0.9.0` at `-S warning` across the full set (161 diagnostics captured and triaged), then read the highest-risk files in full: all `qa-*.bash`, both git hooks, `scripts/lint`, `scripts/setup.bash`, `scripts/test-ccy-ssh-probe.bash`, `scripts/desktop-symlinks`, the entire `claude-yolo` wrapper, `lib/common.bash`, `lib/common-pure.bash`, `lib/token-management.bash`, `lib/ssh-handling.bash`, `lib/docker-health.bash`, `entrypoint.sh`, `files/usr/local/bin/qp`, `files/usr/local/bin/shutdown-with-update`, `scripts/check-displaylink-status.sh`, plus targeted reads/greps of `lib/network-management.bash`, `lib/dockerfile-custom.bash`, `files/home/.local/bin/*` and `fedora-install/*`. Key behavioural claims (the pre-commit pipeline bug) were verified empirically in a shell.

Focus areas per the brief: quoting/word-splitting bugs, TOCTOU/temp-file races, broken error propagation in pipelines (missing `pipefail`), traps/cleanup, arg parsing, and structure of the largest scripts. Annotated `# FAIL-FAST-OK:` patterns and legitimate idioms (`((x++)) || true`, rollback-path `|| true` in `fedora-install/setup-netinstall-boot.bash`) were not reported as violations.

## Summary

The codebase is broadly disciplined — almost every script uses `set -e` (most `set -euo pipefail`), QA scripts use `mktemp` + EXIT traps correctly, and array quoting in the CCY wrapper is generally careful. However, the audit found two high-severity defects in enforcement tooling: the **pre-commit CCY version-bump check is dead code** (a classic `grep -q | grep -v` pipeline bug means it can never reject a commit), and **shellcheck never gates QA** (`qa-bash.bash` treats it as advisory and skips it silently when absent, contrary to the project's own fail-fast and missing-dependency rules), which is why 161 shellcheck diagnostics — including genuine runtime bugs — persist in the tree. Below those, a cluster of broken-error-propagation pipeline bugs (missing `pipefail` before `tee`/`grep`), two scripts that crash at runtime on reachable paths (`qp`, `setup.bash`), a CCY token-flow defect, secrets on the process command line, and predictable `/tmp` paths.

______________________________________________________________________

## BSH-01: Pre-commit CCY version-bump enforcement is dead code (pipeline bug)

**Severity: high** — `scripts/git-hooks/pre-commit:72`

```bash
if ! git diff --cached HEAD -- "$CCY_SCRIPT" | grep -q "^[+-]" | grep -v "^[+-]#" | grep -v "^[+-]$"; then
    # Only comments or whitespace changed - allow it
    echo "✓ CCY script: only comments/whitespace changed (version OK)"
else
    ... COMMIT REJECTED: CCY version bump required ... exit 1
fi
```

`grep -q` exits as soon as it matches and **produces no output**, so the two chained `grep -v` filters always receive empty stdin and the final `grep -v` always exits 1. The pipeline's status is the last command's status, so the condition is always false, `if !` always takes the "version OK" branch, and the `COMMIT REJECTED` block is unreachable. Verified empirically:

```
$ printf '+CCY_VERSION change\n+real code change\n' | grep -q "^[+-]" | grep -v "^[+-]#" | grep -v "^[+-]$"; echo $?
1
```

**Impact:** the rule documented in `CLAUDE/ContainerRules.md` ("Pre-commit hook will REJECT the commit") is not enforced at all. A modified `claude-yolo` with an unchanged `CCY_VERSION` commits cleanly; the only remaining protection is the *runtime* hash mismatch warning in `claude-yolo:314-331` (`load_launch_config`), which fires on users' machines after deployment — exactly what the hook was meant to prevent.

**Recommendation:** replace the pipeline with explicit logic, e.g. capture `git diff --cached HEAD -- "$CCY_SCRIPT" | grep "^[+-]" | grep -v "^[+-][+-]" | grep -v "^[+-]\s*#" | grep -v "^[+-]$"` output into a variable and test `[ -n "$changes" ]`, or simply reject whenever the file changed and the version did not (simpler and stricter). Add a shell test that exercises both branches.

______________________________________________________________________

## BSH-02: shellcheck never gates QA; missing shellcheck is silently tolerated

**Severity: high** — `scripts/qa-bash.bash:64-75`

```bash
# Shellcheck (optional, captures JSON if available)
if command -v shellcheck &>/dev/null && [[ $TOTAL -gt 0 ]]; then
    printf '%s\0' "${BASH_FILES[@]}" \
        | xargs -0 shellcheck --format json 2>/dev/null \
        | jq -s 'add // []' > "$TMP_SC" || true
    sc_count=$(jq 'length' "$TMP_SC")
    [[ "$sc_count" -gt 0 ]] && echo "⚠ shellcheck: $sc_count issues (...)"
else
    printf '[]' > "$TMP_SC"
fi
```

Three problems:

1. **Findings never fail the run.** `ERRORS` is only incremented by `bash -n` failures; shellcheck issues are echoed as a `⚠` count and stored in JSON, but `STATUS`/exit code ignore them. `CLAUDE/QA.md` advertises "shellcheck + `bash -n`" as the bash QA — in practice only `bash -n` gates.
2. **Skip-if-absent.** If shellcheck is not installed the block silently writes `[]`. This directly violates the project's "Missing Dependencies — Fail Fast" rule (compare `qa-patterns.bash:26-29` and `qa-python.bash:21-24`, which correctly `exit 2`).
3. **`2>/dev/null` + `|| true`** on the pipeline hides shellcheck/jq crashes entirely.

**Impact (measured):** running shellcheck at `-S warning` across the audited set yields **161 outstanding diagnostics**, including genuine runtime errors this gate would have caught: `files/usr/local/bin/qp:473` SC2168 (see BSH-07), `scripts/check-displaylink-status.sh:83` SC2144, and a dozen SC2068/SC2145/SC2199 quoting errors in `files/usr/bin/gnome-shell-extension-installer`.

**Recommendation:** make shellcheck mandatory (exit 2 when absent, matching its siblings), and gate on at least `error`-severity findings (`shellcheck -S error` pass/fail), with a documented baseline/exclusion list for legacy files (e.g. `scripts/vault/`, vendored `gnome-shell-extension-installer`) rather than a blanket advisory mode.

______________________________________________________________________

## BSH-03: qa-ansible.bash — fixed temp file, grep failure indistinguishable from clean, and scans only playbooks/

**Severity: medium** — `scripts/qa-ansible.bash:13-16`

```bash
if grep -rn --include='*.yml' -E "$patterns" playbooks/ > /tmp/qa-ansible-matches 2>/dev/null; then
  matches=$(cat /tmp/qa-ansible-matches)
fi
rm -f /tmp/qa-ansible-matches
```

1. **Fixed predictable temp path** `/tmp/qa-ansible-matches` — concurrent runs clobber each other and a pre-existing symlink at that path would be followed (`>` truncates through symlinks). Every other QA script uses `mktemp` + trap; this one should too.
2. **Error vs no-match conflation.** `grep` exits 1 for "no matches" but ≥2 for real errors (unreadable dir, bad pattern). Both fall into the "no matches" branch, and `2>/dev/null` discards the evidence, so a broken scan reports `✓ ansible fail-fast: all instances justified`. This is the exact "skip and continue" failure mode the project's #1 rule prohibits.
3. **Coverage gap:** only `playbooks/` is scanned. `tasks/ensure-jq.yml` (and any future YAML under `tasks/`, `roles/`, `environment/`) escapes the fail-fast gate entirely (currently no violations there, but the gate should cover them).

**Recommendation:** use `tmp=$(mktemp)` with a trap; handle `rc=1` as clean and `rc>=2` as a hard failure (`exit 2`); extend the scan to `playbooks/ tasks/ environment/` (excluding `roles/vendor`).

______________________________________________________________________

## BSH-04: create_token — `| tee` masks container exit code; failure diagnostics are unreachable

**Severity: medium** — `files/var/local/claude-yolo/lib/token-management.bash:233-237, 395-428`

```bash
if container_cmd run -it --rm \
    --entrypoint claude \
    -e "GH_TOKEN=$gh_token" \
    "$image_name" \
    setup-token 2>&1 | tee "$tmp_output"; then
    ...
else
    docker_exit_code=$?
    ...125/126/127 diagnosis...
```

`pipefail` is not set in this library (the parent `claude-yolo` sets only `set -e`), so the `if` tests **tee's** exit status, which is effectively always 0. When `claude setup-token` fails (auth cancelled, no subscription, container broken — including the specifically-diagnosed exit codes 125/126/127), the success branch is taken anyway and the carefully written failure block at lines 395-428 is dead code. The user instead falls into the confusing "Could not extract token from output" manual-paste path. Additionally, `docker_exit_code=$?` in the `else` branch would capture the pipeline (tee) status, not the container's, even if the branch were reachable.

**Recommendation:** run with `set -o pipefail` scoped around the pipeline (or capture via `PIPESTATUS[0]`), e.g.:

```bash
container_cmd run ... setup-token 2>&1 | tee "$tmp_output"
docker_exit_code=${PIPESTATUS[0]}
if [ "$docker_exit_code" -eq 0 ]; then ...
```

______________________________________________________________________

## BSH-05: select_token create/renew paths leave SELECTED_TOKEN empty → `set -e` crash; renewal flows end in spurious "Cancelled"

**Severity: medium** — `files/var/local/claude-yolo/lib/token-management.bash:602-625`, `files/var/local/claude-yolo/claude-yolo:886-891, 921-944, 1029-1034`

In `select_token` (container mode), choosing `0) Create new token` or `rN) Renew` invokes `create_token` and then `return 0` **without setting `SELECTED_TOKEN`**. The caller in `claude-yolo`:

```bash
select_token "$TOKEN_DIR" "container" || { ... }   # line 921
...
CLAUDE_OAUTH_TOKEN=$(cat "$SELECTED_TOKEN")        # line 941, SELECTED_TOKEN=""
```

`cat ""` fails ("No such file or directory"), the command substitution propagates the non-zero status to the assignment, and `set -e` (claude-yolo:41) aborts the whole script with a cryptic error immediately after a *successful* token creation.

Relatedly, the expired `--token` path (claude-yolo:886-891) and the no-valid-tokens path (929-935) run `create_token` on a "Y" answer and then unconditionally fall through to `echo "Cancelled. Create a token with: ccy --create-token"; exit 1` — so a successful renewal still reports "Cancelled" and exits non-zero.

**Recommendation:** after `create_token` succeeds, either have `create_token` echo the new token path (and `select_token` set `SELECTED_TOKEN` from it) so the launch continues seamlessly, or exit 0 with an explicit "Token created — re-run ccy to launch" message. Never print "Cancelled" on the success path.

______________________________________________________________________

## BSH-06: Multi-key SSH flow — GITHUB_USERNAME from last key, token alias from first key

**Severity: medium** — `files/var/local/claude-yolo/lib/ssh-handling.bash:297-349, 399-415`

`build_ssh_mounts_and_validate` loops over all `SSH_KEYS`, overwriting `GITHUB_USERNAME` on each iteration (lines 297-335: last key wins), but then derives the gh-token alias from `SSH_KEYS[0]` (line 349: `key_basename=$(basename "${SSH_KEYS[0]}")`). The wrapper explicitly supports multiple keys (`--ssh-key PATH (can be specified multiple times)`, claude-yolo:134). With two keys belonging to different GitHub accounts:

- the cross-check at lines 399-415 (`token_user != GITHUB_USERNAME`) fails spuriously with a misleading "github_accounts mapping is inconsistent" error, or
- if the check is skipped (`token_user` empty), the container receives `GITHUB_USERNAME` from key N and `GH_TOKEN` from key 0, and `entrypoint.sh:39-49` then hard-fails inside the container with "Token authentication mismatch".

**Recommendation:** decide and enforce a single primary-key semantic: derive both `GITHUB_USERNAME` and the token alias from `SSH_KEYS[0]`, and only *verify connectivity* (not identity) for the remaining keys; or reject multi-account key sets up front with a clear message.

______________________________________________________________________

## BSH-07: qp — `local` outside a function aborts the web-launch path at runtime

**Severity: medium** — `files/usr/local/bin/qp:471-474`

```bash
    # Completely detach from terminal using setsid
    setsid qobuz-player open --web </dev/null >/dev/null 2>&1 &
    local pid=$!
    echo "$pid" > "$PID_FILE"
```

This block is at top level (inside `if $open_web; then`, not a function). `local: can only be used in a function` is a runtime error; under `set -euo pipefail` (qp:~5) the script dies right there — after spawning the player but before writing `$PID_FILE` or waiting for startup or opening the browser. The cold-start web path is therefore broken on every run. (shellcheck SC2168 flags this; it survives because of BSH-02.)

**Recommendation:** change to `pid=$!`. Also note `verbose_output` (qp:130) is assigned but unused (SC2034).

______________________________________________________________________

## BSH-08: setup.bash calls undefined `warn` — version-mismatch path crashes instead of prompting

**Severity: medium** — `scripts/setup.bash:19-22, 47-55`

The helper block defines `die`, `ok`, `header`, `check` — but not `warn`. The Fedora version-mismatch branch calls it twice:

```bash
warn "Fedora version mismatch: running Fedora $RUNNING_VERSION but repo targets Fedora $EXPECTED_VERSION."
warn "The correct branch for this host may be: git checkout F${RUNNING_VERSION}"
```

Under `set -euo pipefail`, `warn: command not found` (exit 127) aborts the script before the "Continue anyway? \[y/N\]" prompt is reached. On any host whose Fedora version differs from `vars/fedora-version.yml` (e.g. running the F43 branch's script on an F42 host — the precise situation the branch model creates), the dispatcher is unusable and the failure message is unrelated to the actual problem.

**Recommendation:** add `warn() { echo "  ⚠ $*" >&2; }` next to the other helpers.

______________________________________________________________________

## BSH-09: OAuth and GitHub tokens passed as `-e VAR=value` — visible in /proc cmdline

**Severity: medium** — `files/var/local/claude-yolo/claude-yolo:2567-2568`, `files/var/local/claude-yolo/lib/token-management.bash:104, 235`

```bash
container_cmd run $DOCKER_FLAGS --rm \
    ...
    -e "CLAUDE_CODE_OAUTH_TOKEN=$CLAUDE_OAUTH_TOKEN" \
    -e "GH_TOKEN=$GH_TOKEN" \
```

The full token values appear in the `podman run` process's argv, which is world-readable via `/proc/<pid>/cmdline` for the lifetime of the (long-running, interactive) process. The same pattern exists in `validate_token` and `create_token`. On a single-user desktop the exposure is limited, but the repo's own security rules say "no credentials in logs — sanitise output", and argv is the most leak-prone channel (ps, process monitors, crash reports).

**Recommendation:** use Podman/Docker env pass-through: `export CLAUDE_CODE_OAUTH_TOKEN GH_TOKEN` and pass `-e CLAUDE_CODE_OAUTH_TOKEN -e GH_TOKEN` (name only), or `--env-file` pointing at a `chmod 600` mktemp file removed by the existing cleanup trap.

______________________________________________________________________

## BSH-10: Predictable /tmp paths without mktemp (TOCTOU / collision)

**Severity: medium** — multiple files

- `files/var/local/claude-yolo/claude-yolo:1520-1527` — `CONFIG_TEMP="/tmp/claude-yolo-$$"; mkdir -p "$CONFIG_TEMP"; cp "$HOME/.gitconfig" "$CONFIG_TEMP/gitconfig"; chmod 644`. `mkdir -p` succeeds silently on a pre-existing directory, so a hostile/leftover `/tmp/claude-yolo-<pid>` (PIDs are guessable) becomes the destination for the user's gitconfig (which may contain credential helpers/signing config), world-readable at 644.
- `files/var/local/claude-yolo/lib/ssh-handling.bash:97-98` — `PROBE_LOG_DIR="/tmp/ccy-gh-probe-$$"; mkdir -p`. Probe stderr logs land in a predictable directory.
- `files/var/local/claude-yolo/claude-yolo:1440` — `build_failure_log="/tmp/ccy-dockerfile-build-failure.log"` (fixed path, cross-run collisions; symlink-following `cp` target).
- `scripts/qa-ctrl-z-patch.bash:59` — `> /tmp/qa-ccy-npm.log` fixed path.

The repo demonstrably knows the right pattern (`mktemp` + trap in `qa-bash.bash:16-18`, `qa-ctrl-z-patch.bash:73-74`, `token-management.bash:211`); these are the stragglers.

**Recommendation:** `CONFIG_TEMP=$(mktemp -d /tmp/claude-yolo-XXXXXX)` (and 0700/0600 modes for gitconfig), `PROBE_LOG_DIR=$(mktemp -d /tmp/ccy-gh-probe-XXXXXX)`, mktemp for the two log files.

______________________________________________________________________

## BSH-11: scripts/lint — fix-mode pipeline masks ansible-lint exit codes

**Severity: medium** — `scripts/lint:208-220`

```bash
if ansible-lint --fix="$FIX_MODE" "$TARGET_PATH" 2>&1 | grep -v "WARNING\|DEPRECATION"; then
    ... "✓ Fix completed successfully"
else
    fix_exit_code=$?
    if [ $fix_exit_code -eq 2 ]; then ...
```

No `pipefail`, so the `if` tests `grep -v`'s status: (a) ansible-lint's documented exit code 2 ("violations remain") can never be observed — `fix_exit_code` holds grep's status; (b) `grep -v` exits 1 when *all* output is filtered, so an entirely-clean run whose only output was WARNING/DEPRECATION lines is reported as "✗ Fix encountered errors". Related: in fix mode the JSON re-analysis (lines 198-201, 226-229) falls back to `jq ... || echo "0"`, so an ansible-lint crash counts as zero violations (normal mode validates JSON at line 302; fix mode does not). Also four SC2155 `local x=$(...)` masking instances and two unused variables (`total_violations:131`, `lint_exit_code:298`).

**Recommendation:** capture output to a temp file, test `${PIPESTATUS[0]}` (or run ansible-lint first, filter afterwards), and validate JSON in fix mode the same way normal mode does.

______________________________________________________________________

## BSH-12: desktop-symlinks advertises debug mounts that are never applied

**Severity: medium** — `scripts/desktop-symlinks:49-83`

The script prints "Launching CCY with extra mounts...", builds `EXTRA_MOUNTS`, exports `CCY_EXTRA_MOUNTS="${EXTRA_MOUNTS[*]}"`, and `exec ccy "$@"` — but **nothing in `claude-yolo` or any lib reads `CCY_EXTRA_MOUNTS`** (verified: zero references). The script's own comment admits it: "(This requires CCY script modification to read CCY_EXTRA_MOUNTS)". Users get a container with none of the promised `/host-debug-logs`, `/host-bin`, `/host-config` mounts, while the output claims otherwise. Also `PROJECT_ROOT` (line 10) is unused (SC2034). Under YAGNI this is exactly the speculative/dead code the project bans.

**Recommendation:** either implement `CCY_EXTRA_MOUNTS` consumption in `claude-yolo` (split on a delimiter into the `DOCKER_MOUNTS` array) or delete the script.

______________________________________________________________________

## BSH-13: qa-python.bash — ruff crash is indistinguishable from a clean pass

**Severity: medium** — `scripts/qa-python.bash:75-77`

```bash
ruff check --fix "${PY_FILES[@]}" >/dev/null 2>&1 || true
ruff_raw=$(ruff check --output-format json "${PY_FILES[@]}" 2>/dev/null) || true
RUFF_JSON="${ruff_raw:-[]}"
```

ruff exits 1 for "findings" (which the JSON capture handles) but ≥2 for invocation errors (bad config, internal error). Both are swallowed by `|| true` + `2>/dev/null`; on a crash `ruff_raw` is empty, `RUFF_JSON="[]"`, and the run reports `✓ python: N files OK`. A broken ruff configuration silently disables the lint gate. Additionally, `ruff check --fix` *mutates files during QA* with all output discarded — surprising side effect for a "check" script.

**Recommendation:** capture ruff's exit code; treat `rc>=2` as `exit 2` (missing/broken tool), keep `rc==1` as the findings path. Print a one-line notice when `--fix` modifies files (or move auto-fix behind an explicit flag).

______________________________________________________________________

## BSH-14: `ccy --connect` uses a different project-name derivation than container creation

**Severity: medium** — `files/var/local/claude-yolo/lib/network-management.bash:102-111` vs `lib/common.bash:419-431`

Containers are named via `get_project_name()` which prefixes the parent directory unless it is generic (`projects|repos|work|src|code|dev|home`), e.g. `acme-site_yolo`. But `connect_to_network()` matches containers with `project_name=$(basename "$PWD")` → `grep "^site_yolo"`. For any project whose parent folder is non-generic, `ccy --connect` finds zero containers and reports "No running containers found for project" even while one is running — the advertised "run in a separate terminal while ccy is running" workflow breaks exactly when the collision-avoidance naming kicks in.

**Recommendation:** call `get_project_name` in `connect_to_network` (the library already sources `common.bash` in every consumer).

______________________________________________________________________

## BSH-15: pre-commit secret scan — unquoted file loop and `|| true` on the staged-file list

**Severity: low** — `scripts/git-hooks/pre-commit:45, 109-120`

- Line 45: `STAGED_FILES=$(git diff --cached --name-only --diff-filter=d 2>/dev/null || true)` — if `git diff` itself fails, the hook prints "No files staged" and **passes**; a failure of the listing should fail the hook (this is a security gate).
- Line 109: `for FILE in $STAGED_FILES` — word-splits on whitespace. A staged file with a space in its name splits into nonexistent paths, `git show ":$FILE"` fails, `STAGED_CONTENT` is empty and the file is silently **skipped from the sensitive-pattern scan** (line 118-120 `continue`). The CCY-script match at line 60 is also wrong for such names but that path is anchored to a fixed name.
- Minor: line 62 of `commit-msg` (`grep -q "token|key|json"`) only works because the literal substring happens to appear in the pattern string — fragile but currently correct.

**Impact** is limited because repository filenames are controlled and this is a defence-in-depth gate, hence low.

**Recommendation:** use `git diff --cached --name-only -z` with a `while IFS= read -r -d ''` loop, and let a git failure exit non-zero.

______________________________________________________________________

## BSH-16: entrypoint.sh — GitHub known_hosts fetch silently optional

**Severity: low** — `files/var/local/claude-yolo/entrypoint.sh:94-96`

```bash
if curl -sL --max-time 5 https://api.github.com/meta 2>/dev/null | jq -r '.ssh_keys | .[]' 2>/dev/null | sed -e 's/^/github.com /' >> ~/.ssh/known_hosts 2>/dev/null; then
    chmod 600 ~/.ssh/known_hosts
fi
```

If curl or jq fails (offline at start, API hiccup), no host key is written and there is no message; the first `git push` later fails with an opaque host-verification error inside the container. Given the project's fail-fast rule, this should at least warn loudly (the session genuinely can proceed for non-git work, so a hard exit is arguably too strong — but silence is wrong). Note also without `pipefail` the `if` only reflects the redirection/sed status.

**Recommendation:** capture the fetch into a variable, validate non-empty, and print a prominent warning (or fail) when it is empty.

______________________________________________________________________

## BSH-17: check-displaylink-status.sh — glob test breaks with multiple evdi dirs; `--check` mode is not silent

**Severity: low** — `scripts/check-displaylink-status.sh:83, 36-37, 89-93, 119-122`

- Line 83: `if [ -d /usr/src/evdi-* ]` (SC2144) — works only while exactly one `evdi-*` directory exists; after a driver update leaves two versions, the test errors (`binary operator expected`). Use the `ls -d ... | head -1` value it already computes on line 84.
- The `--check` "silent mode for automation" only guards some output via `print_status`/`CHECK_ONLY`; section headers and several `echo`s (lines 36-37, 89, 92-93, 119, 122 onwards) print unconditionally, so `--check` is not actually silent.
- No `set -e`, but as a pure diagnostic that is acceptable; the summary exit code logic is sound.

**Recommendation:** fix the glob test; route all decorative output through a `CHECK_ONLY`-aware helper.

______________________________________________________________________

## BSH-18: scripts/vault/ — pervasive legacy shellcheck debt (unquoted cd substitutions, unchecked cd, cross-file variables)

**Severity: low** — `scripts/vault/*.bash` (100+ diagnostics)

Every entry script starts with the same fragile prologue, e.g. `scripts/vault/createVaultedPassword.bash:2-3`:

```bash
readonly DIR=$(cd $(dirname $0) && cd ../../; pwd)
cd $DIR;
```

— unquoted `$(dirname $0)` (SC2046: breaks if the repo path contains spaces), `cd` without `|| exit` (SC2164), plus dozens of SC2154 warnings from variables defined in sourced `_*.inc.bash` files (`projectDir`, `defaultEnv`, `vaultSecretsPath`, `standardIFS`, `allEnvNames`) that shellcheck cannot follow, and one `ls | grep` (SC2010, `createVaultedSshKeyPair.bash:106`). These scripts handle vault secrets, so silent `cd` failure followed by relative-path writes is a real (if unlikely) hazard.

**Recommendation:** fix the shared prologue once (quote the substitutions, `cd ... || exit 1`) and add `# shellcheck source=` directives for the include files; alternatively document the directory as legacy/excluded so BSH-02's gating can be enabled without noise.

______________________________________________________________________

## BSH-19: Vendored gnome-shell-extension-installer carries genuine quoting errors

**Severity: info** — `files/usr/bin/gnome-shell-extension-installer:108, 132, 146, 279, 304, 515, 546-553`

This is third-party upstream code (header: "GNOME Shell Extension Installer 1.6.2"). shellcheck reports six *error*-level diagnostics (SC2068 unquoted `$@`/array expansions, SC2145 string/array mix, SC2199 array in `[[ ]]`) plus SC2207. Per the audit brief vendored code is skim-only; flagging because it is deployed to `/usr/bin` and the errors are real word-splitting bugs in argument handling. Recommend tracking it as vendored (exclude from QA gating with a manifest note) and checking upstream for a newer release rather than patching locally.

______________________________________________________________________

## Minor observations (not raised as findings)

- `claude-yolo:1396-1404`: `run_project_build` sets `set -o pipefail` inside the function, which persists globally afterwards — harmless here (pipefail is desirable) but an unintended global mode change.
- `claude-yolo:2560-2562`: `$DOCKER_FLAGS` and `$NETWORK_FLAG` expand unquoted by design (flag words); network names containing spaces would break — acceptable given engine naming rules, but `NETWORK_ARGS=(--network "$name")` would be cleaner.
- `claude-yolo:853`: `matching_tokens=("$TOKEN_DIR/${SPECIFIED_TOKEN}".*.token)` silently picks the first glob match when multiple expiry-dated files exist for one name; `export_token` (token-management.bash:685) deliberately picks `[-1]` — inconsistent tie-breaking.
- `ftp-camera:1207` `GQ_NEW_INSTANCE= setsid -f geeqie ...` — shellcheck SC1007 false positive; intentional env-var clearing with an explanatory comment. Not a bug.
- `fedora-install/setup-netinstall-boot.bash` — heavy `|| true` use is confined to rollback/cleanup paths (umount, losetup -d, fuser) and `(( errors++ )) || true` counters; this is the correct idiom and was not flagged.
- `shutdown-with-update` has no `set -e` but checks every step explicitly with clear messaging — acceptable.

## Positive Observations

- **Consistent strict modes:** 33 of 38 entry scripts set `set -e`, the majority `set -euo pipefail`; the exceptions are pure diagnostics (`nvidia-status.bash`, `check-displaylink-status.sh`, `debug-pipewire.bash`).
- **Temp-file hygiene in QA:** `qa-all.bash`, `qa-bash.bash`, `qa-python.bash`, `qa-patterns.bash`, `qa-ctrl-z-patch.bash` and `test-ccy-ssh-probe.bash` all use `mktemp` with EXIT traps; `qa-bash.bash` correctly handles ARG_MAX with `xargs -0` and `--slurpfile`.
- **Careful array usage in CCY:** mount/argument construction (`DOCKER_MOUNTS`, `SSH_MOUNTS`, `GUI_MOUNTS`, `CLAUDE_CMD_ARGS`) uses properly quoted arrays throughout; the `--` separator and flag validation against `claude --help` (claude-yolo:528-597) is genuinely good arg-parsing design with did-you-mean suggestions.
- **Terminal state management:** the `stty susp undef` save/restore via the cleanup trap (claude-yolo:1534-1552, 2541-2544) is correct and guards the no-TTY case with `[[ -t 0 ]]`.
- **Deliberate, documented suppressions:** `# shellcheck disable=SC2254` with rationale (common.bash:323), `# FAIL-FAST-OK`-style commentary in probe code (ssh-handling.bash:27-33), and the sequential-by-design note on the gh probe show conscious engineering rather than accidental suppression.
- **Defensive identity checks:** the SSH-key → token → API-user cross-validation chain (ssh-handling.bash:393-417, entrypoint.sh:39-51) fails fast on account mismatches with actionable fix instructions.

---

## Adversarial Verification Appendix

### BSH-01 — CONFIRMED (high confidence) (severity adjusted to **medium**)

Confirmed in /workspace/scripts/git-hooks/pre-commit:72. The condition pipes `grep -q "^[+-]"` (which emits no output) into two `grep -v` filters; the downstream greps always see empty input and exit 1, so the pipeline always exits 1 and `if !` always takes the 'only comments/whitespace changed' branch. Verified empirically: a diff line `+real code change` run through the exact pipeline still takes the allow branch. The COMMIT REJECTED path (lines 76-97) is unreachable, so the version-bump rule in CLAUDE/ContainerRules.md is NOT enforced at commit time. However, severity is inflated: a compensating runtime control exists in files/var/local/claude-yolo/claude-yolo lines 50 and 303-331 — CCY_HASH self-hash validation detects 'modified without version bump' at launch, warns DEVELOPER ERROR, and forces reconfiguration. The miss is therefore caught later (at user runtime) rather than silently never; impact is doc/enforcement mismatch and degraded UX, not a security gap. Adjusted high -> medium. Recommendation is correct; also note the filter stages are mis-ordered (the -q should be the final stage after the -v filters, or capture filtered diff and test non-emptiness).

### BSH-02 — CONFIRMED (high confidence)

CONFIRMED by reading scripts/qa-bash.bash and executing it. (1) Lines 67-70: shellcheck output goes through '2>/dev/null ... || true' into JSON diagnostics only; the ERRORS counter that determines exit status (lines 79-105) is driven solely by bash -n. Live run: '⚠ shellcheck: 310 issues' yet exit 0 and '✓ bash: 196 files OK'. (2) Lines 73-74: when shellcheck is absent the script silently writes '[]' — no warning, no exit 2. This contradicts sibling scripts: qa-patterns.bash:26-29 exits 2 on missing semgrep, qa-python.bash:21-24 exits 2 on missing ruff — so the Missing-Dependencies fail-fast rule violation and inconsistency claims are accurate. (3) CLAUDE/QA.md lists qa-bash.bash as 'shellcheck + bash -n' and says 'Fix all errors before committing', so the doc/behaviour contradiction is real. (4) Both cited runtime errors verified in tracked files: files/usr/local/bin/qp:473 'local pid=$!' (SC2168 — local outside a function fails at runtime; under set -e the script aborts) and scripts/check-displaylink-status.sh:83 '[ -d /usr/src/evdi-* ]' (SC2144 — errors with multiple glob matches). Corrections to claim: diagnostic count is environment-dependent — my run shows 310 (3 error / 143 warning / 155 info / 9 style), not 161; one error-level hit is in a gitignored .claude/ccy/shell-snapshots file the find filters fail to exclude, leaving 2 error-level diagnostics in tracked files. Also shellcheck IS installed in this container, so the silent-skip branch is latent rather than active, but the advisory-only gating gap is active regardless. No FAIL-FAST-OK-style annotation or compensating gate exists anywhere in the script. Severity 'high' is fair given fail-fast is the project's #1 hard rule, QA.md misrepresents the gate, and real error-level bugs in deployed files (qp is deployed to /usr/local/bin) pass QA.


