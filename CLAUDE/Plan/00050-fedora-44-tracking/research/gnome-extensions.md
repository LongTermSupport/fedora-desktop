# GNOME Shell & Extensions — Fedora 44 Migration Research (Plan 00050)

Fedora 44 (released 2026-04-28) ships **GNOME 50** "Tokyo" (released 2026-03-18), not GNOME 49 — so the extension `shell-version` major for Fedora 44 is **"50"** ([Fedora Magazine — What's New in Fedora Workstation 44](https://fedoramagazine.org/whats-new-fedora-workstation-44/), [GNOME 50 release notes](https://release.gnome.org/50/), [TechPowerUp — Fedora 44 launches with GNOME 50](https://www.techpowerup.com/348619/fedora-linux-44-launches-with-gnome-50-kde-plasma-6-6)). The Fedora 43 baseline this repo currently targets is GNOME 49 (comment at `playbooks/imports/play-gnome-shell-extensions.yml:27`). The [GNOME Shell 50 porting guide](https://gjs.guide/extensions/upgrading/gnome-shell-50.html) confirms **no changes to the core `extension.js`/`prefs.js`/`metadata.json` structure**, but GNOME Shell 50 **removed X11 session support** and dropped `RunDialog._restart()`, `holdKeyboard()`/`releaseKeyboard()` and the `show-restart-message`/`restart` signals on `global.display`. Critically, GNOME Shell's extension version validation is **enforced by default** (`disable-extension-version-validation` defaults to `false` in [org.gnome.shell.gschema.xml.in](https://gitlab.gnome.org/GNOME/gnome-shell/-/raw/main/data/org.gnome.shell.gschema.xml.in)), so any extension whose `metadata.json` does not list `"50"` is disabled as OUT_OF_DATE on Fedora 44.

## EXT-01: Custom extension metadata lacks shell-version 50 so all three are disabled on Fedora 44

**Severity**: high
**Area**: extensions

**Files**:

- `extensions/speech-to-text@fedora-desktop/metadata.json:5-11` — `"shell-version": ["45" … "49"]`
- `extensions/workspace-names-overview@fedora-desktop/metadata.json:5` — `"shell-version": ["45", "46", "47", "48", "49"]`
- `extensions/remote-desktop-toggle@fedora-desktop/metadata.json:5` — `"shell-version": ["47", "48", "49"]`

**Concern**: GNOME Shell validates `shell-version` against its own major by default (`disable-extension-version-validation` defaults to `false` — confirmed in the [upstream gschema](https://gitlab.gnome.org/GNOME/gnome-shell/-/raw/main/data/org.gnome.shell.gschema.xml.in)). On Fedora 44's GNOME Shell 50, none of the three custom extensions lists `"50"`, so all three will be flagged OUT_OF_DATE and refused at load. That kills the speech-to-text workflow (Insert-key dictation), workspace names in Overview, and the remote-desktop quick-settings toggle in one stroke — a core daily-driver regression.

**Recommendation**: Add `"50"` to the `shell-version` array of all three `metadata.json` files (the porting guide confirms no structural metadata changes for 50), bump each extension `version`, and runtime-test each on a Fedora 44 host (logout/login required on Wayland) before marking the migration complete.

## EXT-02: Extensions playbook verified against GNOME Shell 49 and installer silently falls back to wrong-shell builds

**Severity**: medium
**Area**: install

**Files**:

- `playbooks/imports/play-gnome-shell-extensions.yml:25-48` — installs seven e.g.o extensions via `gnome-shell-extension-installer --yes`; comment at line 27 states "Verified compatibility status checked against GNOME Shell 49 (Fedora 43)"
- `files/usr/bin/gnome-shell-extension-installer:106-141` — `check_version_availability()` only has a fallback branch for legacy GNOME `3.x` versions; `select_version()` under `--yes` (`SKIP_PROMPTS`, lines 123-141) auto-selects `EXTENSION_VERSIONS[0]` (newest published build, sorted `-rV` at lines 166-167) even when it is not built for the running shell
- `files/usr/bin/gnome-shell-extension-installer:455-458` — derives `GNOME_VERSION` as the major from `gnome-shell --version` (50 handled correctly)

**Concern**: The compatibility audit baked into the playbook comment is for shell 49, not 50. Live checks of the extensions.gnome.org API on 2026-06-12 show all seven IDs (Blur my Shell 3193, Vitals 1460, AppIndicator 615, Clipboard Indicator 779, Just Perfection 3843, Tiling Shell 7065, Space Bar 5090) **do publish a GNOME Shell 50 build**, so the happy path works today. The residual risk is the installer's behaviour when an exact shell-50 build is missing (e.g. a future addition, or an extension temporarily lagging a shell release): with `--yes` it silently installs the newest listed build for a *different* shell major, which version validation then disables — a silent, fail-fast-violating install path.

**Recommendation**: As part of the F44 bump, re-verify all seven IDs against shell 50 and update the line-27 comment to "GNOME Shell 50 (Fedora 44)". Consider hardening the install task to assert post-install state (e.g. `gnome-extensions info <uuid>` does not report OUT_OF_DATE) so a wrong-shell fallback fails loudly instead of silently.

## EXT-03: workspace-names-overview private API paths verified only against GNOME Shell 48.7

**Severity**: low
**Area**: extensions

**Files**:

- `extensions/workspace-names-overview@fedora-desktop/extension.js:5-7` — header: "API paths verified against GNOME Shell 48.7 source"
- `extensions/workspace-names-overview@fedora-desktop/extension.js:50-72` — relies on private members `Main.overview._overview._controls`, `controls._thumbnailsBox._thumbnails`, `controls._workspacesDisplay._workspacesViews`, `display._primaryIndex`, `view._thumbnails._thumbnails`

**Concern**: Underscore-prefixed Shell internals are unstable across majors. Inspection of the upstream `gnome-50` branch confirms `ControlsManager` still has `_thumbnailsBox` and `_workspacesDisplay` ([overviewControls.js](https://gitlab.gnome.org/GNOME/gnome-shell/-/raw/gnome-50/js/ui/overviewControls.js)), and `WorkspacesDisplay._workspacesViews`/`_primaryIndex` and `SecondaryMonitorDisplay._thumbnails` survive ([workspacesView.js](https://gitlab.gnome.org/GNOME/gnome-shell/-/raw/gnome-50/js/ui/workspacesView.js)), so no code change is expected — but the extension fails soft (try/catch logging at lines 75-77) so a path break would manifest as silently missing labels, not an error dialogue.

**Recommendation**: After the F44 upgrade, re-run `extensions/scripts/gnome-shell-extract-js.bash` against the installed 50.x shell, re-verify the five private paths, and update the header comment from 48.7 to the verified 50.x version. Carried over from Plan 00049 EXT-08.

## EXT-04: Deprecated Gio.Settings schema construct property in workspace-names-overview

**Severity**: low
**Area**: extensions

**Files**:

- `extensions/workspace-names-overview@fedora-desktop/extension.js:19` — `new Gio.Settings({schema: WM_PREFS_SCHEMA})`

**Concern**: The `schema` construct property has been deprecated since GLib 2.32 in favour of `schema_id`. It still functions in the GLib shipped with GNOME 50 (the GNOME Shell 50 porting guide lists no Gio.Settings break), but it is the kind of long-deprecated API that GLib may eventually warn on or remove, and extension reviews flag it. Noted by Plan 00049 EXT-08; re-confirmed still present.

**Recommendation**: When touching the extension for the `shell-version: 50` bump (EXT-01), switch to `new Gio.Settings({schema_id: WM_PREFS_SCHEMA})` in the same change and re-test on GNOME 50.

## EXT-05: Playbook enable-and-verify steps cannot detect OUT_OF_DATE extensions

**Severity**: medium
**Area**: install

**Files**:

- `playbooks/imports/play-gnome-shell-extensions.yml:58-66` — `gnome-extensions enable workspace-names-overview@fedora-desktop` with `failed_when: false`
- `playbooks/imports/optional/common/play-speech-to-text.yml:448-472` — verifies via `gnome-extensions list --enabled | grep -q …` then `disable`/`enable`
- `playbooks/imports/optional/common/play-remote-desktop-toggle.yml:107-131` — same `list --enabled | grep` pattern

**Concern**: `gnome-extensions enable` only flips the UUID into the `enabled-extensions` gsettings list; it succeeds even when the shell has rejected the extension as OUT_OF_DATE, and `gnome-extensions list --enabled` likewise reports the gsettings list rather than runtime state. On Fedora 44, with EXT-01 unfixed, every one of these playbooks would report green while the extensions are dead — a silent failure that contradicts the project's fail-fast rule and will mask the GNOME 50 breakage during the migration itself.

**Recommendation**: For F44, extend the verification tasks to assert real runtime state, e.g. `gnome-extensions info <uuid>` output contains `State: ACTIVE` (or at least does not contain `OUT OF DATE`/`ERROR`), failing the play otherwise (keeping the existing FAIL-FAST-OK escape only for "no active GNOME session" runs).

## EXT-06: Stale GNOME version references in docs and playbook comments

**Severity**: low
**Area**: docs

**Files**:

- `CLAUDE/GnomeShell.md:45` — "GNOME Shell 3.36+ (Fedora 42 has 48.7 ✓)"
- `extensions/CLAUDE.md:146` — example path `./untracked/gnome-shell/48.7/js-extracted/...`
- `playbooks/imports/play-gnome-shell-extensions.yml:27` — "Verified compatibility status checked against GNOME Shell 49 (Fedora 43)"
- `extensions/workspace-names-overview@fedora-desktop/extension.js:5` — "verified against GNOME Shell 48.7"

**Concern**: Version references span three generations (48.7/Fedora 42, 49/Fedora 43) and none mentions GNOME 50/Fedora 44. These comments steer future agents to verify against the wrong shell source tree (the `untracked/gnome-shell/<version>` extraction workflow is version-keyed), causing wasted or wrong API verification during F44 work.

**Recommendation**: In the F44 migration commit, sweep these references to GNOME Shell 50 / Fedora 44 (the extraction example path should use a placeholder like `<version>` so it does not rot again).

## EXT-07: GNOME Shell 50 removes the X11 session and several Shell APIs — no repo extension affected

**Severity**: info
**Area**: extensions

**Files**:

- `extensions/*/extension.js` — grep for `holdKeyboard|releaseKeyboard|RunDialog|show-restart-message` returns no hits
- `playbooks/imports/play-gnome-shell-extensions.yml:19` — installs `xprop` (an X11/XWayland utility)

**Concern**: Forward-looking watch-item. GNOME Shell 50 removed X11 session support and with it `RunDialog._restart()`, `misc/keyboardManager.js` `holdKeyboard()`/`releaseKeyboard()`, and the `show-restart-message`/`restart` signals on `global.display` ([gjs.guide GNOME Shell 50 porting guide](https://gjs.guide/extensions/upgrading/gnome-shell-50.html)). None of the three repo extensions uses any removed API, and they already use ESM imports and Wayland-native helpers (`wtype`, quick-settings `QuickToggle`/`SystemIndicator`), so no code change is required. `xprop` remains useful for XWayland windows, so its installation stays valid but is X11-legacy in nature.

**Recommendation**: No action required for F44 beyond the metadata bump (EXT-01). Keep the porting-guide URL in the migration plan for any future extension work, and reassess `xprop`'s usefulness if XWayland usage disappears from the workflow.

## EXT-08: DNF-installed dash-to-dock must come from the F44 repo build

**Severity**: low
**Area**: repos

**Files**:

- `playbooks/imports/play-gnome-shell-extensions.yml:20` — installs `gnome-shell-extension-dash-to-dock` via DNF

**Concern**: Distro-packaged shell extensions are rebuilt per Fedora release for the bundled GNOME major. Fedora 44 ships `gnome-shell-extension-dash-to-dock 105-1.fc44` ([Fedora packages](https://packages.fedoraproject.org/pkgs/gnome-shell-extension-dash-to-dock/gnome-shell-extension-dash-to-dock/)), so the package exists for F44; the residual question is only whether the 105 build declares `shell-version` 50 and behaves on GNOME 50. If the system-upgrade leaves the fc43 build behind (e.g. partial upgrade), the extension is disabled as OUT_OF_DATE.

**Recommendation**: After the F44 system upgrade, confirm the package is the `.fc44` build and that the dash-to-dock extension reports State `ACTIVE` (run `gnome-extensions info` against its UUID, the `dash-to-dock@micxgx...` identifier from extensions.gnome.org); no playbook change expected since DNF resolves the per-release build automatically.

## Sources

- https://fedoramagazine.org/whats-new-fedora-workstation-44/ — Fedora 44 ships GNOME 50; available 2026-04-28
- https://fedoramagazine.org/announcing-fedora-linux-44/ — Fedora 44 release announcement
- https://release.gnome.org/50/ — GNOME 50 "Tokyo", released 2026-03-18
- https://www.techpowerup.com/348619/fedora-linux-44-launches-with-gnome-50-kde-plasma-6-6 — Fedora 44 with GNOME 50
- https://gjs.guide/extensions/upgrading/gnome-shell-50.html — GNOME Shell 50 porting guide (X11 removal, removed APIs, no metadata/extension.js structural changes)
- https://gitlab.gnome.org/GNOME/gnome-shell/-/raw/main/data/org.gnome.shell.gschema.xml.in — `disable-extension-version-validation` default `false` (validation enforced)
- https://gitlab.gnome.org/GNOME/gnome-shell/-/raw/gnome-50/js/ui/overviewControls.js — `_thumbnailsBox`/`_workspacesDisplay` still present in GNOME Shell 50
- https://gitlab.gnome.org/GNOME/gnome-shell/-/raw/gnome-50/js/ui/workspacesView.js — `_workspacesViews`/`_primaryIndex`/`SecondaryMonitorDisplay._thumbnails` still present
- https://extensions.gnome.org/extension-info/?pk=3193&shell_version=50 — Blur my Shell has a shell 50 build
- https://extensions.gnome.org/extension-info/?pk=1460&shell_version=50 — Vitals has a shell 50 build
- https://extensions.gnome.org/extension-info/?pk=615&shell_version=50 — AppIndicator Support has a shell 50 build
- https://extensions.gnome.org/extension-info/?pk=779&shell_version=50 — Clipboard Indicator has a shell 50 build
- https://extensions.gnome.org/extension-info/?pk=3843&shell_version=50 — Just Perfection has a shell 50 build
- https://extensions.gnome.org/extension-info/?pk=7065&shell_version=50 — Tiling Shell has a shell 50 build
- https://extensions.gnome.org/extension-info/?pk=5090&shell_version=50 — Space Bar has a shell 50 build
- https://packages.fedoraproject.org/pkgs/gnome-shell-extension-dash-to-dock/gnome-shell-extension-dash-to-dock/ — dash-to-dock 105-1.fc44 available in Fedora 44
