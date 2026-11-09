# Plan 00235 research: where images are made, and where nothing frees them

Branch `plan/podman-cleanup` (from F44 `77c86895b`). Read-only research in the checkout;
nothing was run against any container engine. The host figures are the host agent's
read-only measurements (2026-10-28), anonymised; `triage.bash` re-measures them.

## 1. Every image-building path

Ten call sites, in four homes. All tags are fixed, so each rebuild or commit re-points the tag
and leaves the image it pointed at untagged (`<none>:<none>`). Nothing afterwards removes it.

| #   | Where                                                                    | What it makes                                                                                                           | Through `ccy_image_build_locked`?                                                                                                    |
| --- | ------------------------------------------------------------------------ | ----------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------ |
| B1  | `files/var/local/claude-yolo/lib/common.bash:1033`                       | `claude-yolo:base` (`--target base`), in `build_container_with_hash`                                                    | yes, via `lib/image-build.bash:25-30`                                                                                                |
| B2  | `files/var/local/claude-yolo/lib/common.bash:1042`                       | `claude-yolo:latest`, same function                                                                                     | yes                                                                                                                                  |
| B3  | `files/var/local/claude-yolo/claude-yolo:2028`, `:2050`, `:2056`         | the base build above, for `--rebuild=claude` with no image, `--rebuild=full`, and a normal launch                       | yes                                                                                                                                  |
| B4  | `files/var/local/claude-yolo/claude-yolo:2172` (`run_project_build`)     | `claude-yolo:<project>` from the project's `.claude/ccy/Dockerfile` (`:2066-2067`)                                      | yes, `:2184`, and its retry `:2196`                                                                                                  |
| B5  | `files/var/local/claude-yolo/claude-yolo:2005` (`update_claude_inplace`) | `container_cmd commit` onto the same tag (Claude Code updated in place)                                                 | yes, callers `:1957` (daily auto-update), `:2030`, `:2032` (`:base`), `:2041`, `:2043`, `:2140` (project), `:3794` (restart request) |
| B6  | `tasks/claude-yolo-image.yml:417-436`                                    | `claude-yolo:latest`, on every play run (imported by `play-claude-yolo.yml:139` and `play-claude-code-consumer.yml:50`) | yes (argv runs the lock)                                                                                                             |
| B7  | `playbooks/imports/optional/common/play-claude-devtools.yml:70-73`       | `claude-devtools:latest`                                                                                                | **no**                                                                                                                               |
| B8  | `playbooks/imports/optional/common/play-unifi-controller.yml:224`        | `podman compose pull`: a pull re-points the tags, the old images go `<none>`                                            | no (a pull, not a build)                                                                                                             |

Not image-making: the Go front (`src-go/internal/ccylaunch/front.go`) only execs
`claude-yolo.bash`, so its goldens (`difftest/testdata/launch-image-older.txt:86-87`,
`launch-image-missing.txt:73-74`, `launch-rebuild-claude.txt:78`) record the bash's calls.
`ccy-teams`, `agentbus`, `podfreeze`, `vmtest`, `ccc` and `reclaim` build, commit and tag
nothing. `lib/dockerfile-custom.bash` (`--custom-docker`) edits a Dockerfile, never builds.
Plan-folder scripts that build throwaway images (`Completed/00151…/prototype-ccy-wl-paste.bash`)
remove them in their own trap.

**B5 is the likeliest source of the steady rate.** Every daily Claude Code auto-update commits a
new layer onto each image a launch uses (`:latest`, `:base`, every `:<project>`), and each
commit leaves one `<none>`. A handful of projects launched most days gives 100-250 a month,
which is the measured rate since 2026-07. Unverified: triage's `labels` leg counts the
dangling images by their `claude-yolo-version` label (the title label the host agent counted
is not one ccy sets, so "314 untitled" does not say whose they are).

**The one choke point.** B1-B6 all run inside `ccy_image_build_locked`
(`lib/image-build-lock.bash:28`). Only B7 and B8 are outside it. That function is 57 code lines
against the bash-limits ceiling of 60, so the cleanup cannot be written there in bash: it is a
Go command the lock calls (two short lines), with the bash launcher (2768 lines, on the
ratchet allowlist at `scripts/bash-limits.allowlist:50`) not growing at all. Any change under
`files/var/local/claude-yolo/` needs the CCY version bump (`CLAUDE/ContainerRules.md`), and
the `lib/` change rebuilds every ccy user's image.

**The lock key is not the tag set.** A base build re-tags both `:base` and `:latest` under the
key `claude-yolo:latest`, and `:2032`/`:2043` commit `:base` under the key `:latest`. So "remove
what the key's tag used to point at" misses `:base`. The command records the image id of every
`claude-yolo` tag before the build and, after a successful one, removes each recorded id that
no tag now names and no container uses.

## 2. What removes images today

- `play-claude-yolo.yml:952-961`: a one-off `rmi claude-browser:latest` (a retired image).
- `claude-yolo:2848`: `network prune`, networks only.
- `reclaim` (2.0.5, `src-go/internal/reclaim`): interactive only. It runs `podman system df`
  (retrying under `podman unshare`), offers `system prune -f`, and `image prune -a` under
  "everything". On exit 125 with "permission denied" (`containers.go:129-141`) it falls back to
  `podmanPartialPrune` (`podman.go:259-296`): lists as JSON, withholds `system prune`, offers
  only image prunes, and names the unreadable volume with its owner against `/etc/subuid`
  (`podman.go:381-434`). That is the model for coping without `system df`.
- No timer, no service, no play task prunes images. `play-podman.yml` has an opt-in store wipe
  only.

## 3. Where the timer belongs

- `container_engine` defaults to `podman` (`vars/container-defaults.yml:10`).
- `playbook-main.yml`: `play-systemd-user-tweaks.yml` (`:15`, linger and the user manager),
  `play-podman.yml` (`:33`), `play-claude-yolo.yml` (`:36`), `play-claude-code-consumer.yml`
  (`:38`).
- `play-podman.yml` installs the engine for every audience (ccc consumers included) and already
  resolves the user's uid for a `scope: user` unit (`:31-45`). The store the timer cleans is
  that engine's, not ccy's: unifi and devtools images are in it too. So the timer's home is
  `play-podman.yml`; it builds the Go command with `tasks/build-go-command.yml`, which needs
  `play-golang.yml` imported first (the file's header).
- Models: `play-container-watch.yml:94-155` (a podman-related user timer, the uid asserted
  first) and `play-vm-test-lab.yml:537-578` (`vmtest-nightly`, `Persistent=true`). Every user
  unit logs to the journal through stderr; no repo doc sets a user-timer convention.
- The existing host-health probe for failed user units (`helpers/host_health/probe.py:173-175`)
  already reports a failed prune service, for free.

## 4. Host health

- All probes are one function, `collect` (`helpers/host_health/probe.py:155-183`): dkms, failed
  system and user units, the previous boot's shutdown errors. A finding is
  `Finding(text, checked)` (`probe_results.py:81`), broken or could-not-check; no severities, no
  thresholds file.
- `login_report` writes `~/.local/state/fedora-desktop/host-status.json`
  (`status_document.py:79`); the panel reads it (`extensions/fedora-desktop@fedora-desktop/statusDocument.js:52-81`,
  rendered by `sections/health.js`); the login message comes from
  `files/home/bashrc-includes/host-health-report.bash.j2:48`. Installed by
  `play-host-health-login-report.yml` (`host-health.service` at login; `host-health-collect.timer`
  on servers).
- There is no disk or podman probe, which is why the panel stayed quiet to 99%.
- Tests: `tests/helpers/host_health/test_probe.py` and siblings, `probe.collect(runner=...)`
  takes a fake runner. Not being ported (Plan 00192 `INVENTORY.md:135`: BORDERLINE).

## 5. Defence Before Fix

Plan 00192's audit (`subagent-reports/261028-defect-classes-audit.md`, class 17: "command
unbounded, or resource never given back") recorded the general class as not a rule: there is
no mechanical test for "freed". A narrower rule is mechanical here, because the code already
has one choke point:

- **Sites** are found by text: `container_cmd build|commit`, `{{ container_engine }} build`,
  `podman|docker|buildah (build|bud|commit)`, `compose pull`, in tracked non-plan bash, YAML,
  Jinja and Go.
- **Paired** means one of: the site runs inside `ccy_image_build_locked` (whose prune step is
  unit-tested), or the same task/function calls the Go cleanup command after it.
- An unpaired site fails the gate naming the file:line and the two ways to pair it. Today it
  would fire on B7 and B8: red before the fix, as DBF wants.

The timer is the second defence (whatever slips past is swept within a day) and the health
probe the third (a store that grows anyway is reported).

## 6. Dangling volumes

30 of 35 volumes are dangling. A volume holds data (databases, caches) and a dangling one may be
a stopped project's only copy, so nothing here removes one. The orphaned volume owned outside
the current subuid range breaks `podman system df` (exit 125); the timer and probe never call
`system df`, and list with `podman images`/`ps`/`volume ls` instead. Report them (count, and
each one's last-used date where it can be read), and let the owner decide.
