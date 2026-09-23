# Plan 00134 Phase 2 — IaC fixes (fork report, opus-5, 2026-09-23)

Each task is one commit on the fork's branch. Every task below also needs a HOST check,
recorded under that task in PLAN.md and in the journal (18:20–18:55). Deploy legs 5–8 of
`deploy.bash`.

## Task 2.1 — WirePlumber 0.5 (`play-hd-audio.yml`)

- **Output:** `~/.config/wireplumber/wireplumber.conf.d/51-hd-audio.conf`
  (`monitor.alsa.rules`) and `51-bluetooth-quality.conf` (`monitor.bluez.properties` plus
  `monitor.bluez.rules`).
- **Retired Lua:** the play removes its two Lua files. It then asserts that nothing else
  is left in `main.lua.d/` or `bluetooth.lua.d/`: any file there was not written by the
  repo, so the play names it and stops rather than deleting it. Then it removes those
  directories.
- **Second defect, found while porting:** the 0.4 rules matched `device.name` only, so the
  rate, period, headroom and suspend settings were applied to device objects and never to
  nodes. Node properties now match `node.name` (`alsa_input.*`, `alsa_output.*`,
  `bluez_input.*`, `bluez_output.*`). `bluez5.auto-connect` and `bluez5.a2dp.ldac.quality`
  are device rules. `bluez5.headset-roles` is renamed `bluez5.roles`.
- **Validation:** a scratch structural SPA-JSON parser, not committed, rejected three
  malformed controls and parsed both files. A committed gate was not added: it would need a
  real SPA-JSON grammar, and WirePlumber itself reports a parse error at start, which is
  the HOST check.
- **HOST check:** the journal shows no Lua warning, and `wpctl inspect` shows the
  properties on the nodes.

## Task 2.2 — Vivaldi repo (`play-browsers.yml`)

- **Evidence:** the `%post` of `vivaldi-stable-8.2.4133.68-1`, read from the RPM header. It
  is Chromium's installer script:
  - `/etc/default/vivaldi` is created with `repo_add_once="true"` when absent;
  - while that reads `true`, `%post` writes `/etc/yum.repos.d/vivaldi.repo` with the same
    `[vivaldi]` id, on install and on upgrade;
  - its `update_repo_file` rewrites `vivaldi.repo`'s baseurl whenever that file exists.
- **Decision:** keep `vivaldi-fedora.repo`, which the package never touches. Write
  `repo_add_once="false"` (the Chrome block's pattern in the same play) and remove
  `vivaldi.repo` on every run.
- **Not read:** the daily cron job's own body is in the payload.

## Task 2.3 — `vmtest-bridge@.service`

- `RuntimeMaxSec=120` is now `TimeoutStartSec=120`.
- **Audit:** 13 oneshot units under `files/`. None of the others carries `RuntimeMaxSec`,
  and no play writes one inline.
- `systemd-analyze verify` shows the warning for the old unit and not for the new one.

## Task 2.4 — `play-toolbox-install.yml`

The play stats the autostart entry, then sets mode 0644 when it exists. It runs on every
run, with no `creates:` guard, and does nothing when the file is absent.

## Task 2.6 — Thunar

- **Confirmed:** nothing in `playbooks/`, `tasks/`, `vars/` or `files/` installs it.
  `docs/fast-file-manager.md` records PCManFM as chosen over it.
- **Left open as an owner decision** (🚫 in PLAN.md). The options:
  - A: remove it via `play-fast-file-manager.yml`. Recommended unless it is in use.
  - B: declare it wanted and accept the D-Bus conflict.
  - C: leave it.

## QA

- `ansible-playbook --syntax-check` passes on all four changed plays, run with a dummy
  vault file because the worktree has none.
- `qa-all.bash` passed toolchain, bash, python, patterns, ansible and ansible-syntax (82
  playbooks). It then stopped at `js`: `extensions/node_modules` is not set up in this
  worktree, and no JS changed.
- Run individually and passing: docs, plan-script-logging and vmtest-manifest.
