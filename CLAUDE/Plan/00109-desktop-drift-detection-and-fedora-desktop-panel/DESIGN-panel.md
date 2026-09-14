# The `fedora-desktop` panel — design

Phase 4's design, settled before the scaffold exists. Task 4.1 says to follow the pattern the
four existing extensions establish, and that pattern is real and worth following — but it
settles the *mechanics* and not one of the decisions this panel actually turns on. Those are
below, so they are decided in writing rather than inherited from whichever behaviour the first
draft happens to have.

## 1. What the existing pattern already settles

`container-watch@fedora-desktop` is the closest model and the answers it gives are taken as
read here:

- **The extension is a thin, read-only front-end.** ESM imports, `export default class … extends Extension`, a `PanelMenu.Button` in `Main.panel.addToStatusArea`, full teardown in
  `disable()`. All logic lives in a Python CLI helper under `helpers/`.
- **A JSON document is the source of truth**, written atomically (`tempfile.mkstemp` + `os.replace`)
  so a reader never sees a half-written file, and carrying a `schema` version.
- **The DBus signal is a hint, never the data.** `container-watch` treats `FindingsChanged`
  purely as "re-read the file now", and a missing session bus warns on stderr rather than
  aborting the producer. Same here.
- **Reads are async.** `load_contents_async`; ESLint forbids the synchronous and `wait`/
  `communicate` forms because a blocking call freezes the shell and costs the user a reboot.

`extensions/CLAUDE.md` adds the constraint that shapes everything else: **extension.js cannot be
reloaded without a logout**, on Wayland. So every line put in the extension is a line that costs
a logout to change, and every line put in the helper is free. That is not a style preference
here, it is the reason the split exists.

## 2. One aggregate status document, not one file per section

The panel needs host-health findings (Phase 3) and play-ledger state (Phases 1–2). Those are two
independent producers, so the obvious shape is one file each and two readers in the extension.

**Rejected.** The cost lands in exactly the wrong place: two async reads, two staleness stories
and two failure paths, all of it in the file that costs a logout to fix. Instead one CLI
subcommand aggregates and writes **one** document, and the extension does one read of one schema.
The aggregation is then in Python, where it is unit-testable and where a fix is live immediately.

The obvious objection to aggregating is that it couples the producers — a raising ledger read
could blank the health section. That is answered by the rule Phase 3 already had to adopt one
level down (`login_report.collect`): **merged, not chained.** The aggregator guards each producer
separately, and a producer that raises becomes that section's `unavailable` state carrying its
own error. It never becomes a missing key, and it never becomes an empty findings list.

## 3. Three states, because "unknown" must not render as "healthy"

This is the decision the whole panel turns on, and it is this plan's own subject appearing in a
new place. A panel with a neutral icon and an empty menu is what a healthy host looks like. It is
also what a missing status file, an unparseable one, and a crashed producer look like. Collapsing
those is the incident, rebuilt in the UI layer.

So every section in the document is in exactly one of three states, and the schema makes them
distinct rather than inferable:

| State         | Means                                 | Panel must show                            |
| ------------- | ------------------------------------- | ------------------------------------------ |
| `ok`          | Checked, nothing to report            | Neutral icon, "nothing to report"          |
| `findings`    | Checked, and here is what is wrong    | Attention icon, one entry per finding      |
| `unavailable` | **Not checked** — with the reason why | A *distinct* icon, and the reason in words |

Three consequences that are easy to get wrong and are therefore requirements:

- **`unavailable` comes from the data, never from the wording.** Phase 3's findings each carry
  `probe_results.Finding.checked`, the producing check's own answer to "did I manage to look",
  and the aggregator reads that field. It must not re-derive the distinction from the text. The
  handoff file tried: two substrings covered seven of the messages the three checks emit and
  missed six, and all six then read as established faults. A panel that classifies by phrase
  would inherit that, and a reworded message would silently change its colour.
- **`unavailable` is not a quiet state.** The panel icon must not be neutral while any section is
  `unavailable`. "I could not look" is closer to "something is wrong" than to "nothing is wrong",
  which is the ordering Phase 3 already uses.
- **A missing or unparseable status document is itself `unavailable` for every section**, rendered
  with the read error. The extension must not fall back to an empty findings array. `container-watch`
  does exactly that fall-back — correctly, because for it an absent report means the scanner has
  not run yet and there are genuinely no flagged *live processes* to report. The facts here are not
  live: a dead DKMS module for the running kernel stays true, so absence of a report is ignorance,
  not health, and must read as such.

## 4. Staleness is shown, not assumed away

The document carries `generated_at`. The health producer runs **once per graphical login** rather
than on a timer, so by mid-afternoon a panel presenting login-time findings as current is stating
something it did not measure — the same defect one more time.

So: the panel displays the collection time, and offers **re-check now**, which spawns the
producer, which rewrites the document and emits the signal. A timer is deliberately not added;
the login run plus an explicit re-check covers it, and a periodic `git fetch` from a panel is a
cost with no reader.

## 5. Sections are registered, not hardcoded

Task 4.4's requirement, and the shape that keeps 4.1 honest: a registry with no registered
section cannot be exercised, so the scaffold lands with the health section as its first real
consumer rather than with a placeholder.

A section is a small object — an id matching the document, a title, a builder that renders its
state into the menu, and an optional action list. Registration is one array entry. Adding
quick-launch later touches the registry and a new module, not the panel class.

The registry carries one rule of its own: **a section whose id is absent from the status document
renders `unavailable`**, saying that the document has no section for it. A registered section that
silently renders nothing is a check that cannot fail, wearing a different hat again.

## 6. Actions: a visible terminal, an argv, and no claim about the outcome

Task 4.3's constraint is that a play never runs silently in the background. The precedent is
`speech-to-text`'s model manager: `foot --window-size-chars=WxH -- <script>`, falling back to
`xdg-terminal-exec` for whatever terminal the user has configured. That fallback chain is taken.

Two corrections to the precedent, both deliberate:

- **An argv, not an interpolated command string.** `GLib.spawn_command_line_async` word-splits
  the string it is given, so building one by interpolation is the injection shape. With a play
  path chosen from a list rather than a fixed literal, that matters. `Gio.Subprocess` with an
  argv array instead.
- **A successful spawn is not a play that ran.** The spawn call reports whether the *terminal*
  started; it says nothing about what happened inside it, and `foot` can spawn and exit at once.
  So the panel never reports that a play ran. It reports what it launched, and the answer to
  "did it run" comes from the ledger on the next refresh — which is what the ledger is for. This
  is the design's cheapest honest answer: no verification code, and no claim that needs it.

The extension never invokes `ansible-playbook` itself. It launches the repo's own script in a
terminal the user is looking at, so the strict-IaC boundary is where it already is: a human runs
plays, and watches them.

## 7. Deployment

**Its own play**, `play-fedora-desktop-panel.yml`, not an addition to
`play-host-health-login-report.yml`. The two have different dependencies and one is useful without
the other: the login report needs only `notify-send` and is meaningful on a server profile, while
the panel needs GNOME Shell. Folding the panel into the reporting play would make a GNOME
extension a dependency of a text report.

**The declaration.** `vars/gnome-shell-extensions.yml` is the single source of truth for
extensions deployed by `play-gnome-shell-extensions.yml`, and its `custom:` list holds exactly the
ones that play owns. The three custom extensions with their own backends are deployed by their own
optional plays and are correctly absent from it; this one has a backend too, so it follows them.

**The UUID needs no scanner allowlist, and should not be given one.** `fedora-desktop@fedora-desktop`
is shaped like an email address, which is why `vars/gnome-shell-extensions.yml` exists as the
scanner's allowlist source. But measured against the hook's own pattern
(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`, byte-identical in `pre-commit` and in the test
suite): this UUID never matches, because the half after the `@` contains no dot. A dotted UUID
such as `Vitals@CoreCoding.com` does match and genuinely needs the exemption. Adding an entry for
this one would be dead weight implying a protection that is not the one operating.

**The Wayland cost is stated to the user, every time.** A deploy of this extension is followed by
"log out and log back in" — nothing else reloads extension JavaScript, and `extensions/CLAUDE.md`
is explicit that the alternatives do not work.

## 8. Non-goals, inherited from the plan and restated because a panel invites them

- **Nothing is auto-applied and nothing is auto-fixed.** The panel offers; a human decides. This
  is the same boundary `handoff.py` holds, and a clickable surface is precisely where it erodes.
- **No play runs in the background.** See §6.
- **The panel is not a second implementation of any check.** It renders what the producers say. A
  check reimplemented in JavaScript would be a check that drifts from the one under test.

## 9. Left open, deliberately, for the tasks that must decide them

- **Task 4.2**: whether the health section's per-finding entries do anything when activated. The
  handoff file is the obvious target (`handoff.offer()` already returns the command as a string),
  but "one-click" is the part Task 3.3 could not finish, and what a click should *do* — copy the
  command, as `container-watch` does with its inspect hint, or open a terminal running it — is a
  3.3 decision, not a 4.2 one.
- **Task 4.3**: which plays the runner lists. Every play is a long list with no ordering; the
  ledger knows which have ever run here, which is a different and probably better answer. Needs
  the ledger's real contents from a HOST run (Task 1.2) before it can be settled on evidence.

## 10. As built — the deployment, and the one assertion it deliberately does not make

`play-fedora-desktop-panel.yml`, its own play for the reasons in §7. Two decisions the
task tree is too small to hold:

**It does not assert the producer play is installed.** The status document comes from
`play-host-health-login-report.yml`, and a tempting `assert` would check for it. That
would be backwards. The panel reads an absent document as `unavailable` — "nothing is
known about this host" — which is the honest state and precisely the state this whole
design exists to render rather than hide. A reporting surface that refuses to deploy over
the thing it reports on has confused its own installation with its subject.

**The enable goes through Plan 00112's declared-state route, never
`gnome-extensions enable`.** That command asks the *running* shell, and on a fresh deploy
the shell has not scanned the new directory, so the request is silently lost — 00110's
desktop acceptance run found eight extensions installed, compiled, loaded and none
enabled. `apply_enabled_extensions` writes the gsettings key the shell reads at session
start, merges without removing so the user's own enabled extensions survive, and re-reads
the key to prove the write took. That self-check is why the task carries no `failed_when`:
the operation is its own probe.

The play ends by telling the operator to log out, on every run. On Wayland nothing else
loads new extension JavaScript — `Alt+F2 r` is X11-only and toggling the extension
restarts code already in memory — so an operator who skips it is testing the previous
version and will report its behaviour as this one's.
