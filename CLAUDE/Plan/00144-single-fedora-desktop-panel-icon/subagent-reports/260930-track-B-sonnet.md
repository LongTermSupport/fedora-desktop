# Plan 00144 Track B report (JavaScript, tests, QA gate, docs)

## Design as built

- `containerReport.js`: report path, command path (backend installed = `~/.local/bin/container-watch`
  is executable), DBus constants (moved unchanged), async read with cancellation, four
  outcomes (`not-installed`, `no-report`, `unreadable`, `read`), `stateOf`, `findingKey`.
  Schema must equal 1 (mirrors `core.SCHEMA_VERSION`; not covered by `check_panel_contract`).
  Missing/non-list `findings`, non-list `advisories`, bad JSON, unknown schema -> unreadable.
- `sections/containers.js`: owns `source` (DBus subscribe + 60 s poll + cancellable + dedupe
  set, all cleared in `stop()`), `state()`, menu build with `labels.wrap` and stylesheet classes
  (new `.fedora-desktop-heading`). Notification title "Container Watch", dedupe key unchanged
  (`kind:host_pid:container_id`). An unreadable report leaves the dedupe set alone.
- Registry additions beyond D5's `source`/`state()`: two small optional hooks, `hidden()`
  (feature absent or nothing read yet: no header, no stray separator) and `leads()` (findings:
  listed first). `extension.js` calls them generically. SECTIONS = health, containers, plays.
- D1 rule: `StatusDocument.worstOf([documentState, ...section.state()])`, folded even before the
  drift document has been read (a live container finding does not wait). No new state/colour.
- `extension.js` keeps `_document` so a source `onChange` re-renders without re-reading.
- Comments updated in `statusDocument.js`, `sections/health.js`, `extension.js` header;
  `metadata.json` description mentions containers. `extensions/container-watch@fedora-desktop/`
  removed with `git rm -r` (staged deletions).

## Playbook copy list (for Track A / coordinator)

No change needed. `play-fedora-desktop-panel.yml` deploys the extension with a whole-directory
`ansible.builtin.copy` (`src: {{ extension_src }}/`), so `containerReport.js`,
`sections/containers.js` and the stylesheet change deploy automatically. Note: `copy` does
not delete stale files, irrelevant here (nothing renamed inside the panel dir).

## Verification

- `./scripts/test-panel-sections.bash`: `passed: 124` (new suite registered in `TEST_FILES`).
- ESLint `cd extensions && node_modules/.bin/eslint .`: clean, no suppressions, no blocking calls.
- `qa-nokill-containerwatch.bash`: passes, "6 container-watch file(s) clean" (4 py + 2 JS);
  `--self-test` passes with two new fixtures (h: missing declared JS target fails collection,
  i: JS `force_exit(` detected). `scripts/test-qa-helper-summary.bash` passes (wording kept).
- `check_extension_compat` and `check_panel_contract .` both pass.
- qa-all result: see the final message.

## Files changed by Track B

- extensions/fedora-desktop@fedora-desktop/containerReport.js (new)
- extensions/fedora-desktop@fedora-desktop/sections/containers.js (new)
- extensions/fedora-desktop@fedora-desktop/extension.js
- extensions/fedora-desktop@fedora-desktop/statusDocument.js (comments, `worstOf`)
- extensions/fedora-desktop@fedora-desktop/sections/health.js (comment)
- extensions/fedora-desktop@fedora-desktop/stylesheet.css
- extensions/fedora-desktop@fedora-desktop/metadata.json (description)
- extensions/container-watch@fedora-desktop/ (deleted, staged)
- tests/extensions/gi-stubs.mjs (Gio.DBus, DBusSignalFlags, GLib.get_user_runtime_dir)
- tests/extensions/test-panel-containers.mjs (new)
- tests/extensions/test-panel-indicator.mjs (matrix, disable tests, stage reset)
- scripts/test-panel-sections.bash
- scripts/qa-nokill-containerwatch.bash
- docs/playbooks.md
- CLAUDE/QA.md
- CLAUDE/Plan/00109-desktop-drift-detection-and-fedora-desktop-panel/DESIGN-panel.md (pointer notes)
- CLAUDE/Plan/00055-container-process-watchdog/testing-checklist.md (pointer note)

Scratch (gitignored): untracked/scratch/.
