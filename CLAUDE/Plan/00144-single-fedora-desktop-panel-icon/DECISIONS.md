# Plan 00144 — Decisions

Open decisions carry a **recommendation** and are marked **DECISION FOR THE OWNER**. The
facts they rest on are in [RESEARCH-current-state.md](RESEARCH-current-state.md).

---

## D1. How the container-watch alert combines with the drift state on the one icon

**DECISION FOR THE OWNER.**

**Context.** Today there are two icons and both use the same amber `dialog-warning-symbolic`
for their alert. The panel's icon has three states (`ok` / `findings` / `unavailable`),
worst-of across the status-document sections it reads. Container-watch has two (neutral /
amber), driven by findings only; advisories never move it. Container findings are **live**
(a crash loop is happening now, and can take GNOME Shell down); drift findings are
**standing** (true since the last login report, until a play runs).

### Option A — Fold into the existing three states, worst-of (**recommended**)

The container section contributes a state like any other section: findings > 0 →
`findings`; advisories → no effect; a report that cannot be interpreted → `unavailable`
(see D2). Overall = worst-of, unchanged. The source is named in words, not colour: the
container section renders **first** in the menu when it has findings (live outranks
standing), and the existing per-finding desktop notification ("Container Watch: Flagged:
<name>") is kept, so the user learns *which* subsystem raised the alert without opening
the menu.

- Pro: no new icon vocabulary; `overallState` and the indicator test keep one rule; the
  panel's "colour is not a statement" stance (`stylesheet.css:1-3`) is held.
- Pro: exactly what the user sees today in the common case — the same amber glyph.
- Con: while drift findings are standing, a new crash loop does not change the icon (it
  is already amber). The notification is the only immediate signal in that case.

### Option B — A fourth, higher-urgency state for live container findings

`findings` split into `findings` (standing, amber) and `urgent` (live container finding,
e.g. red `dialog-error-symbolic`). Precedence: urgent > findings > unavailable > ok.

- Pro: a crash loop is visible on the bar even over standing drift.
- Con: a fourth icon state, a new constant in `statusDocument.js` that the Python
  producer never emits (so `check_panel_contract.py` must learn that it is panel-local),
  and a rule that ranks one subsystem's findings above another's by kind.

### Option C — Composite icon: drift glyph plus a small container badge

The indicator becomes a box holding the drift icon and, only when container findings
exist, a second small glyph.

- Pro: both facts visible at once.
- Con: re-spends the top-bar width this plan exists to save whenever it matters; more
  St layout inside the compositor process; hardest to test in the stub harness.

**Recommendation: A.** It saves the space, keeps one state machine, and the
notification already carries the "which one" answer. Option B is the fallback if the
owner judges "new crash loop hidden behind standing drift" unacceptable.

---

## D2. What an absent or unreadable container-watch report means

**DECISION FOR THE OWNER** (sub-decision of D1; affects the icon only under Option A/B).

Three cases, which today's container-watch collapses into "no flagged containers":

| Case                                                                   | Recommended rendering                                           | Icon contribution |
| ---------------------------------------------------------------------- | --------------------------------------------------------------- | ----------------- |
| Backend not installed on this host (no `~/.local/bin/container-watch`) | Section hidden entirely — the host does not have this feature   | none              |
| Installed, `report.json` absent (no scan yet this boot)                | "no scan recorded since this boot" — stated in words, not blank | `ok`              |
| Installed, report present but unparseable / unknown schema             | "the container report could not be read: <reason>" as a caveat  | `unavailable`     |

The middle row keeps container-watch's recorded reasoning (`statusDocument.js:16-21`: the
runtime directory is cleared at boot and the subject is live, so "not yet scanned" is not
a fault), but says so in words so the section never shows a bare "clean". The last row
closes the one path where container-watch currently reads "unknown" as "clear"
(`extension.js:160-163`), which is the panel's own founding rule.

**Alternative:** show a not-installed host a single non-clickable "container watch is not
installed here (play-container-watch.yml)" line instead of hiding. Rejected as the
default: every desktop without the opt-in play would carry a permanent line about a
feature it chose not to have — the same argument `sections/health.js:29-32` uses for
`quietWhenOk`.

**Out of scope, noted:** a stale report (old `generated_at` because the timer died) and
the `containment` list (containers the watchdog stopped) are not rendered today. Both are
worth doing, but they are new behaviour; see PLAN.md Non-Goals.

---

## D3. Which play retires `container-watch@fedora-desktop`

**DECISION FOR THE OWNER.**

- **Option A — `play-fedora-desktop-panel.yml` retires it (recommended).** The panel play
  deploys the replacement surface, so the old icon leaves in the same run that the new
  section arrives. The two facts cannot come apart: no host ever has both icons, and no
  host loses the container surface without gaining the section.
- **Option B — `play-container-watch.yml` retires it** (the play that deployed it). Cleaner
  ownership, but a host that re-runs only the container-watch play loses its GUI surface
  if the panel play has not run, and a host that runs only the panel play keeps both
  icons until the other play is re-run.

Under either option `play-container-watch.yml` stops deploying and enabling the extension
(its `extension_*` vars and the three desktop-gated extension tasks go), and its header
and `docs/playbooks.md` say the panel play is the surface. The play itself is not retired,
so `helpers/play_ledger/retired-plays.json` is not touched.

**Recommendation: A**, with the retirement tasks commented as transitional and a
follow-up note to drop them once no managed host could still carry the old uuid.

---

## D4. How the retirement is declared (settled by this plan's research, not a preference)

`apply_enabled_extensions` is additive by design and cannot remove a uuid
(`helpers/gnome/enabled_extensions.py:16-18`). Choices:

- **`gnome-extensions disable`** — rejected: it asks the running shell, the very route
  Plan 00112 retired because the request can be lost.
- **Delete the files only** — rejected: the uuid stays in `enabled-extensions` for ever,
  and the loaded code keeps its icon until logout.
- **A new, explicit `--retire-uuid` on `apply_enabled_extensions`** — chosen. Removal of
  a **named** uuid only (declared, never discovered — the same rule the module applies to
  additions), merged in the same read-modify-write, with a read-back that proves the uuid
  is gone, and new markers (e.g. `GNOME-EXT-RETIRED removed=<uuid>`). The user's own
  extensions are still never touched. Then `ansible.builtin.file: state: absent` on the
  deployed directory. Order: remove from the key first (GNOME disables the loaded
  extension live — to be re-confirmed on GNOME 50), then delete the files.

Pure logic in `enabled_extensions.py` and executor changes are test-first under
`tests/helpers/gnome/` per `helpers/CLAUDE.md`.

---

## D5. How a section with its own data source plugs into the registry (settled)

Today a section can only affect the icon by naming status-document ids
(`extension.js:134`). The container section reads a different file, on a different
cadence, with a DBus trigger. The registry gains an optional **source** per section rather
than a second hardcoded path in `extension.js`:

- A section may export `source = {start(onChange), stop()}` — the panel calls `start` in
  `enable()` and `stop` in `disable()`; the source owns its subscription, poll timer and
  cancellable, and calls `onChange()` when its data moved, which triggers a re-render.
- A section may export `state(document, runningKernel)` returning one of the three state
  constants. `overallState` folds the status-document ids as now, then the `state()` of
  every section that has one. `health` and `plays` need no change.
- The container report reader lives in its own module (`containerReport.js`), mirroring
  `statusDocument.js`, so parsing and the D2 rules are unit-testable without widgets.

This keeps DESIGN-panel.md §5's rule ("registration is one array entry and one module")
and adds nothing to the panel class that is specific to containers.
