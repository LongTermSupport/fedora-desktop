# CCY Container System Audit

## Scope & Method

This audit covers the CCY (claude-yolo) Podman/Docker container system and the recently committed multi-account gh/git work. Files enumerated with `find`/`grep` and then read in full:

- `files/var/local/claude-yolo/claude-yolo` (2,633-line wrapper)
- `files/var/local/claude-yolo/Dockerfile`, `entrypoint.sh`, `ccy-ctrl-z-patch.js`
- `files/var/local/claude-yolo/lib/*.bash` (common-pure, common, token-management, ssh-handling, network-management, dockerfile-custom, docker-health)
- `files/var/local/claude-code/cc` (host wrapper sharing the ccy token lib)
- `scripts/qa-ctrl-z-patch.bash`, `scripts/qa-ccy/`
- `.claude/ccy/Dockerfile`, `playbooks/imports/play-claude-yolo.yml`, `playbooks/imports/play-github-cli-multi.yml`
- `docs/github-multi-account.md`, `docs/ccy-debug-mounts.md`

Behavioural checks were run where cheap and load-bearing: the dockerfile-custom prompt generators were executed to confirm a heredoc defect; greps confirmed the `--user` gap, the version-label match, and the token byte-range message inconsistency.

## Summary

The CCY system is mature and defensively engineered: hash/version coupling between the wrapper and the Dockerfile, a loud "developer error" path when the hash drifts without a version bump, a `check_ccy_gitignore_safety` guard that refuses to start if sensitive `.claude/ccy/` files are tracked, SSH-key→account cross-checks on both host and container, and a fail-fast ethos throughout. The recently committed gh-multi/git-multi work is careful about the global-mutable-state hazard of `gh auth switch` and isolates SSH probes correctly.

The findings below are mostly medium/low. The two most actionable are a **confirmed heredoc defect** that corrupts the AI-guided Dockerfile prompt (CCY-01) and an **over-broad GUI mount** that hands a `--dangerously-skip-permissions` agent the user's entire D-Bus/keyring/Wayland runtime directory, materially weakening the isolation that is CCY's stated security benefit (CCY-02). A QA-coverage gap on the fragile ctrl+z patch (CCY-03) is also worth fixing because that gate is the only automated guard on a known-fragile patch.

No critical secret-leak or data-loss issues were found. The `.gitignore` safety mechanism is working — only `.gitignore` and `Dockerfile` are tracked under `.claude/ccy/`.

## CCY-01: Heredoc defect corrupts the AI-guided custom-Dockerfile prompt

`files/var/local/claude-yolo/lib/dockerfile-custom.bash` builds the `--custom-docker` prompts with a pattern that tries to interleave a quoted heredoc with shell `echo`s on the *same line* as the heredoc terminator. A heredoc delimiter must appear alone on its own line; here it does not, so it never terminates and the literal text `PROMPT_EOF`, `echo "$base_image"`, and `cat << 'PROMPT_EOF'` are emitted verbatim into the prompt, and `$base_image` / `$dockerfile_path` are never interpolated.

Evidence — running the generator:

```
$ get_dockerfile_creation_prompt ".claude/ccy" "ccy"
...
**Base Image: PROMPT_EOF
    echo "$base_image"
    cat << 'PROMPT_EOF'**
Pre-installed tools:
```

The intended output was `**Base Image: claude-yolo:latest**`. The same defect recurs in `get_dockerfile_improvement_prompt` (lines ~566, ~586, ~657, ~670, ~673):

```
1. **Read the current Dockerfile** at PROMPT_EOF
    echo "$dockerfile_path"
    cat << 'PROMPT_EOF'
```

Impact: every `ccy --custom-docker` session feeds Claude a prompt littered with shell fragments and missing the actual base-image / Dockerfile-path values. The variable was introduced specifically to support `claude-yolo:base` vs `:latest`, and that purpose is defeated. Functional degradation of a user-facing feature; not a security issue.

Recommendation: stop trying to interpolate inside the quoted heredoc. Either build the whole prompt with a single unquoted heredoc and reference `$base_image`/`$dockerfile_path` directly, or assemble it in a variable with `printf`. Add a smoke test that greps the generated prompt for a stray `PROMPT_EOF`.

## CCY-02: GUI passthrough mounts the entire XDG_RUNTIME_DIR into the YOLO container

`files/var/local/claude-yolo/claude-yolo:2524-2530`:

```bash
if [ -n "$WAYLAND_DISPLAY" ]; then
    GUI_MOUNTS+=(
        "-v" "$XDG_RUNTIME_DIR:$XDG_RUNTIME_DIR"
        ...
```

This bind-mounts the user's whole `/run/user/<uid>` read-write into a container that then runs `claude --dangerously-skip-permissions`. That directory contains the D-Bus session socket (`bus`), PipeWire/Pulse sockets, gcr/gnome-keyring sockets, and systemd user sockets. A YOLO agent — the exact threat model the container isolation exists to contain — can reach the host session bus (launch arbitrary host apps, talk to the keyring daemon), not just the Wayland display it actually needs.

Impact: the "container isolation prevents unintended host access" guarantee advertised in `--help` (lines 169-173) is substantially weakened whenever a Wayland session is present (the common case on this Fedora desktop).

Recommendation: mount only the Wayland socket, e.g. `-v "$XDG_RUNTIME_DIR/$WAYLAND_DISPLAY:$XDG_RUNTIME_DIR/$WAYLAND_DISPLAY"` (plus the runtime dir created with the right ownership), rather than the whole directory. Document the residual exposure. Consider gating GUI mounts behind an explicit flag given the YOLO threat model.

## CCY-03: ctrl+z patch QA only exercises the legacy cli.js path

`scripts/qa-ctrl-z-patch.bash` installs `@anthropic-ai/claude-code` via npm and hardcodes the target to `cli.js`:

```
CLI_JS="$NODE_MODULES/@anthropic-ai/claude-code/cli.js"
...
if [[ ! -f "$CLI_JS" ]]; then
    _fail "cli.js not found after install" "Try: $0 --update"
fi
```

But `ccy-ctrl-z-patch.js` itself documents that Claude Code 2.1.x+ ships as a native binary (`bin/claude.exe`, Node SEA) and contains a whole `patchNativeBinary()` path with same-length byte replacement. The QA never sets up or validates that path. If the installed package is native-only, the QA hard-fails on "cli.js not found"; if it still ships cli.js, the QA validates an artifact that may no longer match what the Dockerfile's `npm install -g` produces in production. Either way, the gate that is supposed to guard the project's self-described "KNOWN FRAGILE PATCH" cannot exercise the patch's current real-world code path.

Impact: the most fragile, version-sensitive piece of the system (the byte-level binary patch) has no working automated coverage. A future Claude Code packaging change to the native-binary guard variable would slip past QA and only surface as a frozen container at runtime.

Recommendation: detect which artifact npm produced (`cli.js` vs `bin/claude.exe`) and run the matching patch path; assert the guard is applied for whichever exists. Fail only if neither artifact is present. Keep `--update` refreshing to latest so the native path is genuinely tested.

## CCY-04: gh active-account switching has no interrupt-safe restore

`files/var/local/claude-yolo/lib/ssh-handling.bash:81-138` (`probe_gh_keys_for_remote`) captures the originally-active gh account, loops calling `gh-token-<alias>` (each of which runs `gh auth switch`, mutating global state), and restores the original account only on normal completion at line 132. There is no `trap` to restore on `Ctrl+C`/error. `build_ssh_mounts_and_validate` similarly switches via `gh-token-<alias>` with no restore guarantee.

Impact: if the user interrupts during the SSH-key probe (it runs interactively at launch and is explicitly "sequential, ~1-3 seconds"), their global `gh` active account is left switched to whichever alias the loop reached. Subsequent host `gh` commands silently operate as the wrong account. The function's own comment promises "the user's shell state is unchanged," which the interrupt path violates.

Recommendation: set a `trap '...restore...' RETURN`/`EXIT` (or an explicit cleanup on the loop) so the original active account is restored even on interrupt or mid-loop failure.

## CCY-05: Dockerfile claims a runtime `--user` mapping the wrapper never provides

`files/var/local/claude-yolo/Dockerfile:200-201` states: "USER directive is NOT set here - we use --user flag at runtime / This allows dynamic UID/GID mapping to match the host user." The wrapper computes `HOST_UID`/`HOST_GID` (claude-yolo:1076-1077) but uses them only for the UID-0 safety check (line 1080); the final `container_cmd run` (lines 2560-2580) passes **no** `--user` flag. The container runs as root, and correct host ownership comes from rootless userns mapping, not from a `--user` flag.

Impact: no functional bug under rootless Podman/Docker (ownership is correct via userns), but the Dockerfile comment is misleading and `HOST_UID`/`HOST_GID` read as dead code to a maintainer, who may "fix" ownership by adding `--user` and break the rootless mapping.

Recommendation: correct the Dockerfile comment to describe the actual rootless-userns mechanism, and either remove the unused `HOST_GID` or add a comment that these are intentionally only used for the root-safety guard.

## CCY-06: Token byte-length message inconsistent with the accepted range

The token length guard accepts 90–120 bytes (`claude-yolo:959,1041`, `lib/token-management.bash:334`) but three of the user-facing messages say "expected: 100-110 bytes" (`claude-yolo:987,1042`, `lib/token-management.bash:337`), while `files/var/local/claude-code/cc:65` correctly says "90-120 bytes".

Impact: a 95-byte token passes validation but, if it later fails the API check, the user is told to expect 100–110 — confusing and self-contradictory diagnostics.

Recommendation: make the messages quote the actual accepted range (90–120), matching the `cc` wrapper.

## CCY-07: ctrl+z patch soft-fail is silent in the build stream

`ccy-ctrl-z-patch.js:softFail()` prints a warning and `process.exit(0)`, so `RUN node /tmp/ccy-ctrl-z-patch.js` in the Dockerfile succeeds even when the patch target is not found. This is documented as intentional in `CLAUDE/ContainerRules.md`, but the only signal is a warning line in a long build log. If missed, ctrl+z sends an unblockable SIGSTOP and freezes the container with no recovery path.

Impact: low likelihood, high annoyance; mitigated by `CCY_DISABLE_SUSPEND` being a no-op only when the patch silently failed. Residual risk is purely visibility.

Recommendation: surface the soft-fail more prominently — e.g. write a sentinel file inside the image (`/opt/claude-yolo/.ctrlz-patch-status`) that the wrapper checks at launch and warns about once, rather than relying on the user catching a build-time stderr line.

## CCY-08: known_hosts population in the container silently continues on failure

`files/var/local/claude-yolo/entrypoint.sh:94-96` fetches GitHub's SSH host keys via `curl … | jq … >> ~/.ssh/known_hosts` inside an `if`, and does nothing on failure. There is no fallback `StrictHostKeyChecking` setting for in-container git operations.

Impact: if the `api.github.com/meta` fetch fails (offline build cache reuse, transient network), `known_hosts` is empty and an in-container `git push` can hang on interactive host-key verification — surprising in a non-interactive/headless run. This is a soft-continue that sits uneasily with the project's fail-fast rule, though it is best-effort by nature.

Recommendation: on fetch failure, either fail with a clear message or set `StrictHostKeyChecking=accept-new` for the container's git/ssh so pushes do not hang; log which path was taken.

## CCY-09: update_claude_inplace leaks the temp container on commit failure

`files/var/local/claude-yolo/claude-yolo:1205-1224`: the in-place update creates a named container (`ccy-claude-update-$$`, not `--rm`), then `container_cmd commit …` and `container_cmd rm …`. Under `set -e`, if `commit` fails the function aborts before the `rm`, leaving a stopped temp container behind (no trap cleanup). The npm-install failure branch does clean up, but the commit branch does not.

Impact: minor — a leftover stopped container that the startup stale-cleanup may later reap only if it matches the `_yolo` naming pattern, which `ccy-claude-update-$$` does not.

Recommendation: wrap the temp-container lifecycle in a `trap`/cleanup that `rm -f`s it regardless of where the function exits.

## CCY-10: OAuth and gh tokens passed via container env vars

`files/var/local/claude-yolo/claude-yolo:2567-2569` passes `CLAUDE_CODE_OAUTH_TOKEN`, `GH_TOKEN`, and `GITHUB_USERNAME` via `-e`. Container env is readable via `podman inspect <ctr>` and `/proc/<pid>/environ` to processes of the same user.

Impact: low and partly inherent (GitHub Actions uses the same OAuth-via-env model, as the comments note). On a single-user workstation this is acceptable; it is noted for completeness since this is a security-focused audit. The entrypoint does correctly `unset GH_TOKEN` after `gh auth login` (entrypoint.sh:30-31), limiting in-process lifetime of that one.

Recommendation: no change required for the workstation threat model; optionally document that container env is not a secret boundary and prefer secret-file mounts if CCY is ever used on shared hosts.

## Positive Observations

- **Hash/version coupling is excellent.** Both the wrapper (`CCY_VERSION`/`CCY_HASH`) and the Dockerfile (`claude-yolo-version` label vs `REQUIRED_CONTAINER_VERSION`) detect "modified without version bump" and rebuild with a loud, specific developer-error message. The two version constants are in sync at `2.18`/`2.18` (no infinite-rebuild-loop risk).
- **`.claude/ccy/` safety guard works.** `check_ccy_gitignore_safety` force-creates the protective `.gitignore`, refuses to start if anything beyond `.gitignore`/`Dockerfile`/`allowed-hostnames` is tracked, and gives a `git filter-repo` remediation. Verified: only `.gitignore` and `Dockerfile` are tracked in this repo.
- **Multi-account global-state hazard is handled with care** — sequential probing with documented rationale, isolation flags (`-F /dev/null -o IdentityAgent=none -o IdentitiesOnly=yes`) to prevent ssh-config/agent false positives, and a host-side token-owner cross-check (`ssh-handling.bash:399-415`) that fails fast before the container build.
- **Token-source parity (`cc`) reuses the ccy lib cleanly** via `common-pure.bash`, which deliberately omits container-engine dependencies so sourcing it on the host cannot trigger the podman-check `exit`.
- **Ansible playbook hygiene is good** — every `failed_when: false` is annotated `# FAIL-FAST-OK:` with a probe-then-fail justification, owner/group/mode set on file tasks, and `force: false` on user-customisable templates.
- **Build-failure recovery is thoughtful** — a failed custom Dockerfile build falls back to the base image and injects a diagnostic prompt so the agent can fix the Dockerfile interactively, with a rootless-buildah-cache auto-recovery retry.
