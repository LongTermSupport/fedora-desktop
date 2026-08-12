# CCY Changelog

Release notes for the CCY launcher (`files/var/local/claude-yolo/claude-yolo`) and its
container image.

Two version numbers move independently — see
[Keeping CCY Current](ccy.md#keeping-ccy-current):

- **`CCY_VERSION`** — the host-side launcher script. Bumped on every change to it.
- **`REQUIRED_CONTAINER_VERSION`** / the Dockerfile's `claude-yolo-version` label — the
  image. Bumped only when image content changes, which forces a one-time rebuild.

> **Why this file exists.** These notes used to accumulate in the trailing comment on the
> `CCY_VERSION` line, which reached 5,645 characters across six releases and was printed
> verbatim by the rebuild banner. Comments carry current state; git tracks changes; this file
> documents them.

---

## 3.29.0

Dropped the `CLAUDE_CODE_MAX_SUBAGENTS_PER_SESSION` forwarding added in 3.28.0 — it was dead
config, and picking a bigger number was never the fix.

Upstream removed the cumulative session-lifetime sub-agent cap outright in Claude Code
**2.1.224**: *"Removed the 200-subagent-per-session spawn cap; long-running sessions no
longer refuse new agents (concurrency and depth limits still apply)."* The environment
variable is now a documented no-op, and CCY tracks `@latest` (2.1.228 at the time of this
release), so every CCY session is already past the removal.

CCY now sets **no** sub-agent fan-out limits. The two dials that remain are the right kind of
guard and keep their upstream defaults — `CLAUDE_CODE_MAX_CONCURRENT_SUBAGENTS` (20, bounds
instantaneous CPU/memory/rate-limit load) and `CLAUDE_CODE_MAX_SUBAGENT_SPAWN_DEPTH` (3,
bounds recursive fan-out). Neither penalises a session for living a long time; both are
tunable per project via `.claude/ccy/ccy.env`.

Host-side launcher change only — no image content change.

## 3.28.0

*Superseded by 3.29.0.*

Forwarded `CLAUDE_CODE_MAX_SUBAGENTS_PER_SESSION` into the container with a CCY default of
10000, to lift Claude Code's then-current cumulative cap of 200 for long unattended sessions.

Host-side launcher change only — no image content change.

## 3.27.1

3.27.0 was assigned to two different script contents during dogfooding (the
`DISABLE_MOUSE`-only interim on container 2.21, then the both-vars-removed final on 2.22), so
CCY's runtime self-hash guard correctly flagged a version-unchanged content change on the
second deploy. This bump re-keys the hash; no behaviour change versus the intended 3.27.0
final.

## 3.27.0

CCY stops hardcoding both fullscreen/mouse environment variables in `entrypoint.sh` so Claude
Code's own defaults apply — the classic renderer by default, with `/tui fullscreen` opting in
per repo via the symlinked `.claude/ccy/settings.json`, or `CLAUDE_CODE_NO_FLICKER=1` in
`.claude/ccy/ccy.env`.

1. **Dropped `CLAUDE_CODE_DISABLE_MOUSE=1`.** With mouse capture off, fullscreen sessions on
   Wayland terminals (GNOME-Terminal/VTE, even kitty) fall back to DECSET-1007 alternate
   scroll, and the wheel emits arrow keys that clobber the prompt. Letting Claude Code capture
   the mouse makes it handle the wheel natively inside the alt-screen (verified on
   GNOME-Terminal).
2. **Dropped `CLAUDE_CODE_DISABLE_ALTERNATE_SCREEN=1`.** That kill switch (Plan 00047, Path D)
   only existed because fullscreen scroll was broken while mouse tracking stayed off, so
   fixing (1) removes the reason to force the classic renderer — fullscreen is a normal opt-in
   again.

Resolves Plan 00047 via a simpler path than the abandoned Path C+ (per-emulator wheel remap)
and Path D (drop fullscreen). `REQUIRED_CONTAINER_VERSION` 2.20 → 2.22 forces a one-time
rebuild so the new entrypoint takes effect. Ref: `LongTermSupport/fedora-desktop#31`.

## 3.26.2

Whitelisted `.claude/ccy/claude-supervise*` in the tracked-file safety net
(`lib/common.bash`) — the same fix 3.26.1 made for `ccy.env`. The vendored PTY supervisor
that `CCY_CLAUDE_WRAPPER` points at is legitimately git-tracked, but
`check_ccy_gitignore_safety()` flagged it as a false "SENSITIVE FILES TRACKED IN GIT" alert.
Uses a `claude-supervise*` glob (any extension or none) so it is not locked to a Python
implementation.

Host-side launcher change only. Ref: `LongTermSupport/fedora-desktop#31`.

## 3.26.1

Whitelisted `.claude/ccy/ccy.env` in the tracked-file safety net. The per-project config file
added in 3.26.0 was correctly tracked in git, but `check_ccy_gitignore_safety()` only allowed
`.gitignore`/`Dockerfile`/`allowed-hostnames`, so it raised a false "SENSITIVE FILES TRACKED
IN GIT" alert.

Host-side launcher change only. Ref: `LongTermSupport/fedora-desktop#31`.

## 3.26.0

Per-project CCY config: `entrypoint.sh` now sources `/workspace/.claude/ccy/ccy.env` (if
present) inside the container, immediately before the `claude` exec, so a project can declare
`CCY_CLAUDE_WRAPPER` and friends in a tracked file instead of ad-hoc host exports.

Sourced **in-container only**, never on the host, so a project cannot run host code via it. A
host-set or `--supervise` value (non-empty, forwarded) still wins over a project default that
uses the `${CCY_CLAUDE_WRAPPER:-<default>}` idiom. Complements the 3.25.0 supervisor seam.

`REQUIRED_CONTAINER_VERSION` 2.19 → 2.20 forces a rebuild. Ref:
`LongTermSupport/fedora-desktop#31`.

## 3.25.0

Optional, default-off supervisor-wrap seam: the new `--supervise` flag sets a default
`CCY_CLAUDE_WRAPPER` (`claude-supervise --`, operator-overridable) and forwards it into the
container, where `entrypoint.sh` prepends it to the `claude` invocation. Unset means
byte-for-byte unchanged behaviour. The supervisor binary is provided separately, so there is
no image-content change. Ref: `LongTermSupport/fedora-desktop#31`.

## 3.24.0

Container base bumped to `node:lts-slim` (was `node:20-slim`) so the bundled Claude Code
satisfies its rising Node engine floor (2.1.x needs >= 22); the floating LTS tag auto-tracks
future majors at build time.

Also fixed the long-soft-failing ctrl+z patch: Claude Code 2.1.198 removed the
platform-boolean guard and now gates suspend on a shared `ano()` call, so
`ccy-ctrl-z-patch.js` no-ops the single-purpose `handleSuspend()` method itself
(guard-refactor-proof) instead of chasing the condition.

**Critical:** `update_claude_inplace()` now re-applies the patch after the daily in-place npm
update. The fresh install ships an *unpatched* binary, which previously re-enabled the ctrl+z
freeze until the next full rebuild; the Dockerfile keeps the patch script at
`/opt/claude-yolo/` so it is available for that.

`REQUIRED_CONTAINER_VERSION` 2.18 → 2.19 forces a one-time rebuild.

## 3.23.0

Daily per-image Claude Code auto-update: on launch, once per 24h per image, compare the
running image's bundled Claude Code against npm latest and run the fast in-place npm update
when behind — a no-op when current.

Gates on the final resolved `IMAGE_NAME` (the project image when a custom Dockerfile exists,
else `claude-yolo:latest`), so old project images self-update on their next session; a second
session the same day is zero-impact. Soft-fails offline; `CCY_AUTO_UPDATE=0` falls back to
notify-only. Cache moved to the per-image directory `~/.cache/claude-yolo-update-checks`.

## Earlier

- **3.22.0** — GitHub SSH-over-443 launch banner
- **3.21.0** — inherited `GITHUB_SSH_443`
- **3.20.0** — SSH-over-443 auto-fallback
- **3.19.0** — `--github-443`

Full detail for these and everything before them is in git history:

```bash
git log --follow -p -- files/var/local/claude-yolo/claude-yolo
```
