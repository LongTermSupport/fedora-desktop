# Plan 00144 — Research: the two panel extensions as they stand

Facts gathered at planning stage, each with its source. Line numbers are against the
`F44` branch at the commit this plan was created on; re-check them before editing, since
Plan 00134 is still touching the panel.

## 1. `fedora-desktop@fedora-desktop` — the host drift/health panel

Files: `extension.js`, `statusDocument.js`, `labels.js`, `terminal.js`, `stylesheet.css`,
`metadata.json`, `sections/health.js`, `sections/plays.js`.

### How sections plug in

- The registry is one array: `const SECTIONS = [healthSection, playsSection];`
  (`extension.js:41`). Each section module exports `section = {id, title, documentSections, build(menu, document, nowMillis, runningKernel)}`
  (`sections/health.js:246-286`, `sections/plays.js:341-373`).
- `_render(document)` rebuilds the whole menu on every read: `menu.removeAll()`, then each
  section's `build` in registry order with a separator between them
  (`extension.js:151-167`).
- **Every section reads the same single input** — the host status document at
  `$XDG_STATE_HOME/fedora-desktop/host-status.json` (`statusDocument.js:52-57`, `:80-82`),
  read asynchronously by `StatusDocument.read` (`statusDocument.js:127-157`). There is no
  per-section data source today, no DBus subscription, and one poll timer of 300 s
  (`extension.js:46`, `:85-92`).

### How the icon's three states are computed

- States: `ok`, `findings`, `unavailable` (`statusDocument.js:29-31`), mapped to
  `emblem-ok-symbolic`, `dialog-warning-symbolic` (amber `#ffaa00`),
  `dialog-question-symbolic` (blue `#8ab4f8`) (`extension.js:48-52`, `:139-149`).
- Before the first read lands the icon is `unavailable` deliberately (`extension.js:80-83`).
- `overallState(document, ids, running)` is worst-of: any section in `findings` wins;
  otherwise any non-`ok` section or any document-structure reason gives `unavailable`;
  else `ok` (`statusDocument.js:282-294`).
- The ids it folds come from `SECTIONS.flatMap(entry => entry.documentSections)`
  (`extension.js:134`). `health` contributes its five check ids (`sections/health.js:253`);
  `plays` contributes none, so it "cannot move the icon" (`sections/plays.js:345-347`).
  **The only way a section influences the icon today is by naming status-document
  section ids.** A section fed from a different file has no route to the icon.

### Contract the panel holds

- It runs no check and applies no fix; the one thing it launches is a terminal a person
  is looking at (`extension.js:1-24`, `terminal.js:1-47`). Command offers are **copied**,
  not run (`sections/health.js:78-121`, whose point 2 cites container-watch's copy idiom
  as the precedent).
- Absence is ignorance: an absent/unparseable/unknown-schema document reads as
  `unavailable`, never as healthy (`statusDocument.js:1-24`, `:84-157`).

## 2. `container-watch@fedora-desktop` — the container watchdog's panel

One file of logic, `extension.js` (385 lines), plus `metadata.json`.

- **Data source**: `$XDG_RUNTIME_DIR/container-watch/report.json` (`extension.js:117-123`),
  written atomically by the backend `helpers/containerwatch/cli.py` (`build_report`
  `cli.py:474-517`, `write_report_atomic` `cli.py:520`). The report carries `schema`,
  `generated_at`, `host_cores`, `thresholds`, `findings`, and optionally `restart_counts`,
  `crashloop_coverage`, `restart_history`, `containment`, `advisories` (`cli.py:488-516`).
- **Refresh triggers**: DBus signal `org.fedoradesktop.ContainerWatch.FindingsChanged` on
  path `/org/fedoradesktop/ContainerWatch`, treated only as "re-read now"
  (`extension.js:30-35`, `:68-78`), plus a 60 s fallback poll (`extension.js:39`,
  `:80-87`). The backend itself runs from a `systemd --user` timer
  (`files/home/.config/systemd/user/container-watch.timer`).
- **Alert states**: two. Findings > 0 → `dialog-warning-symbolic` in amber `#ffaa00`;
  otherwise `system-run-symbolic`, neutral (`extension.js:187-198`). **Advisories never
  move the icon or notify** — they are standing configuration, and counting them would
  make an alarm nobody can clear (`extension.js:176-182`; the same rule on the producing
  side at `cli.py:506-516`).
- **Two finding shapes**: process findings (`cpu_pct`, `age_s`, `cmd`) and crash-loop
  findings (`kind: 'crashloop'`, `restart_count`, `restarts_per_min`, `reasons`)
  (`extension.js:224-233`, `:293-321`).
- **Menu actions**: activating a finding copies its engine-correct `exec_hint` to the
  clipboard and notifies (`extension.js:243-246`, `:323-332`); activating an advisory
  copies its restart-policy `advice` (`extension.js:256-291`). It never spawns anything.
- **Notifications**: one desktop notification per newly appeared finding, deduped on
  `kind:host_pid:container_id` (`extension.js:334-365`). This is the only unsolicited
  notification either extension raises.
- **Error handling differs from the panel on purpose**: every failure path ends in an
  empty findings list (`extension.js:135-167`). `statusDocument.js:16-21` records why that
  was judged right for container-watch — its subject is live, so "the scanner has not run
  yet" means nothing is flagged now. A **parse** failure also collapses to "no findings"
  with only a `log()` (`extension.js:160-163`), which is the one path that does silently
  read "unknown" as "clear".
- **Ignored report fields**: `schema`, `generated_at` and `containment` (containers the
  watchdog has stopped) are not rendered or checked.

### Shared / duplicated between the two

| Concern                           | fedora-desktop@                           | container-watch@                           |
| --------------------------------- | ----------------------------------------- | ------------------------------------------ |
| `PanelMenu.Button` in status area | `'fedora-desktop'` (`extension.js:72-78`) | `'container-watch'` (`extension.js:56-64`) |
| Async `load_contents_async` read  | `statusDocument.js:127-157`               | `extension.js:127-168`                     |
| Cancellable re-read + poll timer  | 300 s (`extension.js:46`)                 | 60 s + DBus (`extension.js:39`, `:68`)     |
| Amber attention colour `#ffaa00`  | `extension.js:144`                        | `extension.js:193`                         |
| Warning icon name                 | `dialog-warning-symbolic`                 | `dialog-warning-symbolic`                  |
| Copy-to-clipboard + `Main.notify` | `sections/health.js:70-75`, `:116-119`    | `extension.js:284-332`                     |
| Detail-line styling               | `.fedora-desktop-detail` (stylesheet)     | inline `style:` strings                    |

So **both icons use the same amber warning glyph** for their alert state: with two
icons, the user cannot tell which subsystem is shouting without opening a menu anyway.

## 3. IaC that deploys and enables them

- `playbooks/imports/optional/common/play-fedora-desktop-panel.yml` — `scope: gnome`
  (`:41`), whole-directory copy of the extension (`:76-86`), declares it enabled via
  `helpers.gnome.apply_enabled_extensions --uuid=fedora-desktop@fedora-desktop` (`:98-113`),
  asserts no `GNOME-EXT-FAIL` (`:119-132`), ends with the Wayland logout message
  (`:138-144`). Opt-in, not imported by `playbook-main.yml` (`:33-35`).
- `playbooks/imports/optional/common/play-container-watch.yml` — `scope: general` (`:23`)
  because the backend matters on a server; the extension vars are `:24-26`; the extension
  directory and copy tasks are desktop-gated (`:107-124`); the enable task is
  `:207-223`, also desktop-gated. Opt-in, not imported by `playbook-main.yml` (`:15-17`).
- Neither uuid is in `vars/gnome-shell-extensions.yml`; custom extensions with a backend
  are deployed by their own play (00109 `DESIGN-panel.md` §7).

### Declared enabled-extensions state (Completed Plan 00112)

- `helpers/gnome/apply_enabled_extensions.py` confirms each `--uuid` is on disk, merges it
  into `org.gnome.shell enabled-extensions`, writes, and re-reads to prove the write
  (`:1-38`, `:97-166`). Marker lines: `GNOME-EXT-DEPLOYED`, `GNOME-EXT-ENABLED-CHANGED`,
  `GNOME-EXT-ENABLED-UNCHANGED`, `GNOME-EXT-FAIL`.
- **It is additive only, by design**: `enabled_extensions.merge` "removes nothing"
  (`helpers/gnome/enabled_extensions.py:16-18`, `:106-128`). There is **no declarative
  route today to take a uuid out of the list.** Retirement therefore needs a new, narrow,
  TDD'd capability — it cannot be expressed with the current helper.
- GNOME Shell watches `changed::enabled-extensions` and disables any loaded uuid that
  leaves the list, live (`untracked/gnome-shell/48.7/js-extracted/org/gnome/shell/ui/extensionSystem.js:564-598`,
  `:688-691`). That extract is GNOME 48.7 while the branch targets GNOME 50, so re-extract
  (`./extensions/scripts/gnome-shell-extract-js.bash`) and re-confirm before relying on it.
  If it holds, removing the uuid takes the old icon off the bar at once; loading the new
  panel code still needs a logout.

### Retirement precedents in the repo

- Files: `ansible.builtin.file: state: absent` over a list of retired paths
  (`play-photography.yml:137-145`, `play-claude-yolo.yml:159-166`).
- Retired **plays** go in `helpers/play_ledger/retired-plays.json` (`playbooks/CLAUDE.md:33-34`).
  **Not applicable here**: `play-container-watch.yml` survives (it still deploys the
  backend); only the extension is retired.

## 4. Tests and QA gates that touch these

- `scripts/test-panel-sections.bash` runs every `tests/extensions/test-*.mjs` by name and
  cross-checks the list against the directory (`:31-60`):
  `test-panel-sections.mjs` (imports `statusDocument.js`, `sections/health.js`,
  `sections/plays.js`, `:42-44`), `test-panel-indicator.mjs` (drives `enable()`/`disable()`
  and the three icons, `:1-50`), `test-dock-recovery-on-unlock.mjs`.
  **No test exercises container-watch's extension today.**
- `tests/extensions/gi-stubs.mjs` has no `Gio.DBus` stub (`export`s at `:21-434` include
  `Gio`, `GLib`, `St`, timers and deferred reads, but no signal subscription) — the new
  section's DBus wiring needs one.
- `helpers/gnome/check_panel_contract.py` compares the panel's constants with the Python
  producer, scanning `PANEL_SOURCES` (`:49-62`). It is a vocabulary check against the host
  status document only; container-watch's report is outside its contract.
- `helpers/gnome/check_extension_compat.py` checks every `extensions/*/metadata.json`
  against the branch's GNOME major; removing a directory just shrinks its set.
- `scripts/qa-nokill-containerwatch.bash` scans `helpers/containerwatch/*.py` and
  **`extensions/container-watch@fedora-desktop/*.js` under `nullglob`** (`:90-106`).
  Delete that directory and the gate silently stops scanning any JavaScript while still
  printing "N container-watch file(s) clean". It must be re-pointed at the new section
  file, and should fail when a declared target is missing rather than glob it away.
  `scripts/lib/qa-helper-summary.bash` and `scripts/test-qa-helper-summary.bash:473-536`
  parse its summary wording.
- ESLint: `cd extensions && node_modules/.bin/eslint .` (`extensions/CLAUDE.md:87-99`);
  `scripts/qa-js.bash` covers `.js` and `.mjs`.

## 5. Docs that mention either extension

- `docs/playbooks.md:794-829` (`play-container-watch.yml`, incl. "Installs a GNOME Shell
  panel extension that surfaces the findings", `:823`) and `:912-944`
  (`play-fedora-desktop-panel.yml`).
- `CLAUDE/QA.md:55`, `:304` (nokill gate scope and summary wording).
- Code comments that name container-watch as a sibling extension:
  `statusDocument.js:16-21`, `:77-79`; `sections/health.js:89-91`;
  `play-fedora-desktop-panel.yml:17-19`, `:72-75`.
- Plan 00109 `DESIGN-panel.md` §1, §7, §9a, §12 describe container-watch as a separate
  extension; 00109 is In Progress, so its design doc should gain a pointer to this plan
  rather than be rewritten.

## 6. Related open plans

- **00055 container-process-watchdog** (Dormant): its only open item is the L3 visual
  pass of the container-watch panel per `testing-checklist.md` (Task 4.2). This plan
  replaces that surface, so the checklist describes a UI that will no longer exist.
  00055's backend work is untouched.
- **00134 startup-log-triage-and-status-panel-unavailable** (In Progress): Task 1.3 added
  the panel's copy row and label wrapping, borrowing container-watch's clipboard idiom.
  Its remaining tasks are host-side (WirePlumber, dnf repo, ABRT, SELinux) and do not
  touch the extension. No conflict, but both edit `sections/health.js`'s neighbourhood.
- **00109 desktop-drift-detection-and-fedora-desktop-panel** (In Progress): owns the panel
  design (`DESIGN-panel.md` §3 three states, §5 registry). This plan extends §5 (a section
  with its own source) and needs a note there.
