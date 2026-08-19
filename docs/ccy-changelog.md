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

## Library 1.12.0 — token-management.bash

**The usage block now says which limit is actually binding.**

The response carries `anthropic-ratelimit-unified-representative-claim`, naming the
bucket the API itself considers representative (`five_hour`, `seven_day`,
`seven_day_opus`, …). It was recorded when the header set was first mapped and then
never captured, which left one question unanswerable from the display: the probe uses
Haiku because the weekly buckets are **per-model**, so "is this weekly figure the
allowance I actually care about?" could only be guessed at. A `seven_day_opus` claim
now states outright that it is not.

```
       5-hour limit   ███░░░░░░░░░░░░░░░░░   14%   resets in 4 hours
       weekly limit   ████████░░░░░░░░░░░░   41%   resets in 5 days
       binding limit: 5-hour limit
```

Unknown bucket names pass through verbatim rather than being dropped — a future
`seven_day_haiku` is far more useful visible than silently absent.

The cache record gains a **fifth** field, appended last, so a 4-field record written by
an older library still renders exactly as before.

## Library 1.11.0 — token-management.bash

**Every account displayed `<1%` usage, whatever it had actually used.**

The `anthropic-ratelimit-unified-*-utilization` headers express utilisation as a
fraction (`0`–`1`), and the API documents that nowhere. Plan 00074 shipped on the other
reading, so a real 41% arrived as `0.41`, rendered as 0.41%, and displayed as `<1%` —
the guard that exists to avoid claiming an account is untouched was doing all the work,
on every account, for a week.

Settled by measurement rather than by argument. The cache already holds the values
exactly as the API sent them — a decision made a day earlier so that flipping the scale
would not cost a refetch — so the plan's `triage.bash` read them straight off disk and
spent nothing: **eight samples across four accounts, every one between 0.04 and 0.41.**

`CCY_USAGE_SCALE` now defaults to `fraction`. Override with `CCY_USAGE_SCALE=percent`.

**The inference is now self-refuting**, which matters more than the flip. No sample
above `1` was ever seen, and one would have settled it outright — so this is strong
evidence, not proof. Previously a wrong scale was invisible by construction: the bar
clamps to 100%, so a value read 100× too large would draw a full green bar and look
correct. A raw value the assumed scale cannot produce — above `1` under `fraction`,
above `100` under `percent` — now prints `SCALE MISMATCH`, naming the value and the
override, instead of a bar. If the header ever changes to `0`–`100`, ccy says so on the
first fetch rather than reporting 100% forever.

Also: a bucket the API did not report says so on its own row instead of vanishing
(1.10.1). The renderer's own comment said a silently missing bucket *"reads as this
account has no weekly limit, which would be a lie"*, and the code three lines below it
did exactly that. Reachable whenever only one of the two buckets comes back.

## 3.38.0 (container 2.26)

**The daily update check could report "✓ Claude Code is up to date ()" on a machine
where nothing had been read at all.**

Both version probes ended `… | grep -oP … | head -1 || echo "unknown"`. This file sets
`set -e` but never `pipefail`, so a pipeline's status is the *last* command's — and
`head -1` succeeds on empty input. The `|| echo "unknown"` therefore **never fired**.
A container that would not start, or a registry that could not be reached, produced an
**empty** version string rather than `unknown`, which walked straight past the offline
guard immediately below it:

- both empty → the two compare equal → *"✓ Claude Code is up to date ()"*, and the
  daily stamp is refreshed, so the check does not run again that day;
- one empty → *"📦 Updating Claude Code: → 2.x.y"* and an in-place `npm install -g`
  driven by a comparison against a version nothing had measured.

Both probes now capture the command's own status first and require a non-empty result
before trusting it, so an unreachable container or registry reaches the offline guard
it was always meant to reach.

Five smaller instances of the same shape, all of which answered a question the code
had failed to ask:

| Where                    | Was                                                                                                  | Now                                                  |
| ------------------------ | ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------- |
| `--networks` listing     | engine unreachable printed as "no networks found"                                                    | names what the engine said                           |
| `claude --help` in image | "help unavailable", reason discarded                                                                 | prints the engine's own message                      |
| network prune            | inspect failure read as "no containers attached", so nothing was stopped and the removal then failed | reports that containers were not stopped             |
| token discovery          | `ls` failure and "no tokens" were one empty string                                                   | a glob, whose no-match is a fact about the directory |
| update-check stamp       | unreadable stamp became mtime 0                                                                      | says so, then re-checks                              |

All of it found by Plan 00076, which made this file visible to the linters for the
first time — it is extensionless and was mode `0644`, so semgrep never read its
shebang and never assigned it a language.

---

## 3.37.2 (container 2.26)

**A Docker context that cannot be queried is no longer reported as a context that is
not rootless.**

The rootless-Docker check read `container_cmd context inspect` with its exit status
discarded. If the query failed — daemon down, socket unreachable, `docker` missing —
the result was an empty string, which compares unequal to `rootless`, so ccy printed
*"Claude Code YOLO mode requires rootless Docker / Current Docker context: default"*
and sent the user off to reconfigure a context that may have been perfectly correct.

This is the same shape as the GitHub-outage misreport fixed in 3.36.0: a failed query
laundered into a confident statement about configuration. The check now distinguishes
the two, naming what the engine actually said and pointing at the daemon.

Found by Plan 00076, in the pass that made this file visible to the linters for the
first time.

---

## 3.37.1 (container 2.26)

**Housekeeping, from the first time this file was ever linted.**

No behaviour change. The launcher had never been examined by any of the repo's bash
gates — it is extensionless and was committed mode `0644`, and every gate identifies
bash by filename extension or execute bit. `qa-bash.bash` looks for `*.sh`/`*.bash`
**or** an executable file with a shebang, so it matched neither branch; semgrep is
stricter still, refusing to read the shebang of a non-executable file at all
(`target_manager.py:1024`), so it never even classified the file as bash. See
Plan 00076.

What linting it found:

| Change                                                              | Why                                                                                                                                                                                            |
| ------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Removed `CONFIG_LOADED`                                             | Assigned in three places, read in none. Quick-launch takes effect by populating `SPECIFIED_TOKEN`/`SSH_KEYS`/`SPECIFIED_NETWORK`; the flag tracked that a second time and nothing consulted it |
| Removed `HOST_GID`                                                  | Captured under a comment about "proper file ownership" and never used. `HOST_UID` is still read, for the never-run-as-root check                                                               |
| Removed `PODMAN_DEFAULT_DETECTED`                                   | Assigned twice, read never — the inner `if` in the network scan existed only to set it                                                                                                         |
| Library loading unrolled from a `for` loop into five `source` lines | With a computed `$lib.bash` filename shellcheck can resolve none of them. Each line now carries a `# shellcheck source=` directive, so the five libraries are actually analysed                |

Load order is unchanged, and a missing library still fails naming the file — now
*before* anything is sourced rather than part-way through, so a partial load is no
longer possible. Verified both paths against a stub library tree.

---

## 3.37.0 (container 2.26)

**`ccy --top` no longer dies mid-table when a container exits while it is being listed.**

Four captures in the container libraries read a command's output while discarding its
exit status. Under the launcher's `set -e`, a bare `var=$(cmd)` does not leave an empty
string when `cmd` fails — it **aborts the enclosing function**. Every one of these had a
comment claiming the empty-string behaviour it did not have:

| Site                              | What actually happened                                                                        |
| --------------------------------- | --------------------------------------------------------------------------------------------- |
| `docker-health.bash` status table | a container exiting between the listing and the `inspect` killed the whole `ccy --top` render |
| `docker-health.bash` zombie scan  | the same race silently **truncated** the zombie list — every container after it was lost      |
| `docker-health.bash` engine query | an engine that could not be asked aborted the scan rather than reporting nothing found        |
| `network-management.bash`         | the `--connect` failure diagnostic vanished exactly when the container had gone away          |

All four now carry an explicit `|| var=""` fallback, so the documented behaviour and the
real behaviour agree. The last one matters most: it is the error-reporting path, so the
failure suppressed the report of the failure.

Found by the review pass for Plan 00075, which exists for this defect class — a command's
failure silently becoming data, or in this case silently becoming a dead function.

---

## 3.36.0 (container 2.26)

**A GitHub outage no longer blocks launch with a bogus configuration error.**

After picking an SSH key, CCY cross-checks that the `gh-token-<alias>` token belongs to
the same account the key authenticates as — a mismatch means the `github_accounts` mapping
disagrees with the key registrations. The check ran:

```bash
token_user=$(GH_TOKEN="$GH_TOKEN" gh api user --jq .login 2>/dev/null)
```

and compared whatever came back, **discarding the exit status**. When GitHub answers 502,
it returns a JSON error body, `gh` writes that body to stdout, and the whole blob was
treated as an account name. The result was a mangled report — with the error JSON spliced
into the middle of the sentence — telling the user their `github_accounts` mapping was
wrong and to go and edit `localhost.yml`. The mapping was fine. GitHub was down. A check
that misdiagnoses an outage as a config error is worse than no check.

The lookup is now retried (3 attempts, backing off) and its answer **validated as a GitHub
login** before any comparison. That validation is what catches the genuinely nasty variant:
an error body returned with a *zero* exit status, which checking the status alone would
still let through.

When the API cannot be reached, CCY now says so, quotes what GitHub actually replied,
points at githubstatus.com, and offers an explicit override:

```bash
CCY_SKIP_TOKEN_OWNER_CHECK=1 ccy
```

The override is deliberately explicit rather than a silent fallback: it skips a real safety
check, so it should be a decision, not a default.

## 3.35.0 (container 2.26)

**The usage display is now bars, and says what it means.** 3.34.0 rendered a compact line
per account — `5h <1% r4h · wk <1% r6d`. It was unreadable: `r4h` means nothing unless you
already know, and the compression bought columns nobody needed. Replaced with a bar per
limit, coloured green/orange/red by how much is used against a dim track, aligned across
accounts, and the reset time written out — "resets in 4 hours", "resets in 20 minutes".

```
  1) work       expires 2026-11-02
       5-hour limit   ██████████████████░░   91%   resets in 2 hours
       weekly limit   █████████████░░░░░░░   63%   resets in 3 days
```

**Two new switches for the undocumented utilisation scale.** The API sends a float and
does not say whether it is a percentage (`0`–`100`) or a fraction (`0`–`1`) — a 100×
difference. `CCY_USAGE_DEBUG=1` shows the raw value the API sent next to each bar, using
the figures already fetched rather than spending another request; `CCY_USAGE_SCALE=fraction`
switches the interpretation. The scale is read in exactly one function, so this stays a
switch rather than an assumption spread through the renderer.

The cache now holds the values rather than a pre-rendered line, since the bars need the
numbers. Cache format changed, so the deploy discards any existing cache.

## 3.34.0 (container 2.26)

**Usage limits in the token menu, on request.** Press `u` at token selection and each
account's 5-hour and weekly utilisation appears next to it, with reset countdowns and
green/orange/red colouring. See
[Seeing usage limits before you pick](ccy.md#seeing-usage-limits-before-you-pick).

**Why it is a keypress and not automatic.** The figures are only available as
`anthropic-ratelimit-unified-*` headers on a real API response, so reading them costs a
genuine billed request per account — one that consumes a sliver of the allowance it
reports. Fetching at launch would spend quota nobody asked to spend. The probe uses Haiku
with a one-character prompt and `max_tokens: 1`; the weekly buckets are per-model, so the
Opus and Sonnet allowances are untouched. Results cache for 15 minutes, so a second press
in the same sitting is free. `CCY_TOKEN_USAGE=0` removes the option.

An account that cannot be read degrades to a dim note and the other rows still render —
the fetch runs in workers that record their outcome rather than raising, so a dead network
can never take the menu (and the launch) down with it.

Utilisation arrives as a float whose scale is undocumented. A non-zero value that rounds
to zero renders `<1%` rather than `0%`, so the display never claims an account is
untouched when it is not.

## 3.33.0 (container 2.26)

**Reverts 3.32.0.** Superseded within the day by 3.34.0 above; neither 3.32.0 nor this
release was ever deployed.

The 3.32.0 feature read usage from `GET /api/oauth/usage`, which is free to call. It does
not work with the credential CCY stores: every `sk-ant-oat01` setup-token gets
`403 — "OAuth token does not meet scope requirement user:profile"`, from that route and
from `/api/oauth/profile` alike. The scope is fixed when `claude setup-token` mints the
token, so no client-side change can obtain it. Do not re-add a call to those routes with a
setup-token.

## 3.32.0 (container 2.26)

**Per-token usage limits in `--list-tokens`, fetched automatically at launch.** Removed in
3.33.0 — see above.

## 3.31.1 (container 2.26)

Scope fix on the state repair introduced in 3.31.0, found by the new `qa-reviewer` agent.

**There is a third transcript store, and it is world-writable.** The hooks daemon's
`pre_compact` transcript archiver writes verbatim conversation archives to
`.claude/hooks-daemon/untracked/transcripts/` — 8.3 MB of them in the repo this was found
in, at mode `0666` inside a `0777` directory. The daemon calls `os.umask(0)` when it
daemonizes, so **no umask in `entrypoint.sh` can ever reach it**: it does not fail to
inherit the launcher's umask, it deliberately overwrites its own.

The 3.31.0 preflight covered only `$PWD/.claude/ccy`, so the repair reported success while
that material stayed open one directory away. The preflight now covers the whole
`$PWD/.claude` tree. This is safe for git, which records only the execute bit — clearing
group/other read bits produces no diff on tracked files.

**The repair is permanent, not a migration.** Because the daemon re-creates world-writable
files continuously (observed reappearing within the same minute as a repair), the
launch-time pass must keep running even after the umask ships. Do not remove it.

## 3.31.0 (container 2.26)

Claude Code's session state is plaintext, and Anthropic documents that OS file permissions
are its only protection ([Plaintext storage](https://code.claude.com/docs/en/claude-directory)).
Neither launcher set a umask, so the default `022` left all of it group/other-readable —
887 of 990 files and 331 of 348 directories in the store this was measured in, including
verbatim pre-edit file bodies under `file-history/`. Note that `projects/`, the directory
holding the transcripts, was already `700`: the exposure was everywhere *except* the
obvious place.

Two halves, both required:

- **`umask 077`** in `entrypoint.sh` (container) and `/var/local/claude-code/cc` (desktop),
  so new state is owner-only by construction. The desktop store matters most — it has no
  container boundary at all.
- **A launch-time repair pass**, because the umask does nothing for state already on disk.
  It clears group/other bits while leaving owner bits untouched, so execute permissions
  survive; a blanket `600` would have broken 129 plugin and skill scripts. Symlinks are
  excluded, since `chmod` through one applies to its target. It is advisory: it reports and
  repairs but never blocks a launch.

The desktop store's equivalent repair lives in `play-claude-code.yml`, which already
deploys the `cc` wrapper that creates it.

Container 2.26 is required because `entrypoint.sh` is image content — until the image is
rebuilt, the umask is not in effect and the repair decays between launches.

## 3.30.2 (container 2.25)

Two defects found by the new acceptance gate
(`CLAUDE/Plan/Completed/00070-lightweight-agent-browser-engine/acceptance.bash`), which
asserts what a live container *delivers* rather than what the repo *contains*. Both were
invisible to every earlier check because both fail silently.

**The browsing skill never reached the agent.** The Dockerfile baked it into
`/root/.claude/skills/browsing/`, and `entrypoint.sh` then `rm -rf`s `/root/.claude` and
replaces it with a symlink to `/workspace/.claude/ccy` so sessions persist per project.
The skill was therefore deleted on every single container start — it shipped in the image
and no agent could ever load it. This predates the second engine, but it made Plan
00070's central deliverable (the rule telling the agent which engine to pick) inert.
Skills are now staged at `/opt/claude-yolo/skills/` and installed by the entrypoint
*after* the symlink exists, the same way the PHPantom plugin already worked. The copy is
unconditional, so a rebuilt image always delivers current guidance.

**The two engines fought over one daemon.** `agent-browser` keeps a per-namespace daemon
that is bound to the engine it started with, and both engines were using the default
namespace. Reading a page with `agent-browser-lite` and then reaching for Chromium failed
with `Custom Chrome arguments (--args) are not supported with Lightpanda` — reproduced
3/3 — and `close --all` did not cure it, because the close returns before the daemon has
gone. That directly contradicted the skill's "fall back freely, the syntax is identical".
`agent-browser-lite` now passes `--namespace lightpanda`, giving each engine its own
daemon socket; the two interleave in any order with no teardown at all (verified 6/6
alternating calls). The engines keep separate sessions as a result, so re-`open` the URL
after switching — the skill says so.

## 3.30.1 (container 2.24)

Reworked the browsing skill's engine-choice rule around **whether the user needs to see
what is happening**.

3.30.0 framed the choice as content vs pixels, which quietly treated headed Chromium as a
cost to be avoided. That is backwards. The Chromium window appears on the user's desktop
through Wayland forwarding, and when someone is sitting there working on a web page, being
able to watch is often the most valuable thing the browser does — they catch the wrong
page, the missed cookie banner or the broken layout before the agent does.

The rule is now two questions in order:

1. **Does the user want or need to see this?** If they are present and the work is about
   the page itself (design, UI testing, debugging a layout) → `agent-browser`, headed,
   and do not pass `--headed false`.
2. **Otherwise, does it need pixels or geometry at all?** No → `agent-browser-lite`.
   Yes but unattended (batch runs, nobody watching) → `agent-browser --headed false`.

Since both engines run JavaScript with equal fidelity, the choice is only ever about
visibility and pixels — never about whether the page will actually work. The skill also
now tells the agent to state which engine it picked and why when the answer is unclear, so
the user can redirect it before the work is done rather than after.

Skill and guide text are baked into the image, so this ships as container 2.24.

## 3.30.0 (container 2.23)

Added a **second browser engine**: Lightpanda 0.3.6, reachable as `agent-browser-lite`.

`agent-browser` already had a native `--engine lightpanda` flag — only the binary was
missing from the image, so this is a pinned download plus a config file, not a parallel
browser stack. Measured in a CCY container on the same page through the same CLI:

| Engine     | Wall time | Processes | Peak RSS |
| ---------- | --------- | --------- | -------- |
| Chromium   | 1177 ms   | 15        | ~1345 MB |
| Lightpanda | 379 ms    | 1         | ~25 MB   |

JavaScript fidelity is equal, not reduced: Lightpanda matched Chromium on all eight
capability fixtures tested — `fetch()`, ES modules with private fields, custom elements
with shadow DOM, and a React 18 client-side render — and returned equal or more text on
real pages.

**Chromium remains the default and is unchanged.** Lightpanda has no layout or paint
pipeline and, crucially, fails *silently* outside its scope: `screenshot` exits 0 while
writing a placeholder image, and `get box` exits 0 while returning fabricated geometry
(`height: 100000000`). Because an agent checking the exit code would be misled, the cheap
engine is opt-in per invocation rather than a default, and the browsing skill teaches the
boundary: content → `agent-browser-lite`, pixels or geometry → `agent-browser`.

Also replaced the `browsing` skill's command reference. Its 730 lines of
`COMMANDLINE-USAGE.md` / `EXAMPLES.md` documented an `agent-browser run "navigate …"`
syntax that the CLI no longer has — all 97 examples would have failed with
`Unknown command: run`. agent-browser now ships its own version-matched skills, so the
skill points at `agent-browser skills get core --full` instead of keeping a copy that
drifts. The play removes the two obsolete files from the build context, since the copy
task only ever added files and the Dockerfile copies the whole directory.

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
