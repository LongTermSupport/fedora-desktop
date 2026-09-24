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
`play-host-health-login-report.yml`.

Not on a dependency argument, because that one does not survive measurement: the login report is
itself `scope: gnome`, `meta: end_play`s on a server profile, and installs `python3-pyyaml`, so the
two plays are identical on `hosts`, `become`, `scope` and main-import — every axis such an argument
would rest on. "The report is meaningful on a server" describes a state this plan records as still
open (§ *A server profile gets no drift detection*), in the present tense.

The reason that does hold: **the panel is a generic multi-section surface, not this report's UI.**
Task 4.3's play runner and Task 4.4's registry add sections with nothing to do with host health, so
its sections arrive and leave independently of the health document and its lifecycle is its own.
`play-container-watch.yml` bundles a backend with its extension, and is right to — there the
extension is that backend's only surface. This one fronts several, so it belongs to none of them.

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

## 9. Settled by Task 4.3: the play runner

The question left open here was which plays the runner lists. Task 1.2's HOST run supplied the
ledger's real contents (18 plays, run counts from 1 to 6), and the decisions below rest on that.

- **It lists the plays the ledger has seen here, each with its freshness state.** Every play in
  the repository is a long list with no ordering and dozens never run on this host; the ledger's
  set is the population a person here has actually chosen, and it is short. Plays outside
  `playbooks/` are left out, because the runner only launches from there.
- **The state is `check_freshness`'s, carried in the document.** `check_freshness.run` hands
  every verdict of a complete judgement — fresh ones too — to a `judged` sink;
  `helpers/host_health/play_runner.runnable` turns them into `{play, state}` rows; the status
  document carries them under `plays`, always present, `[]` when there is nothing to offer. The
  panel renders them and never re-judges (§8). A judgement that answers untrustworthy — the
  BROKEN sentinel, an unresolvable ledgered commit, an unappliable retired-plays map — leaves
  the sink empty, so a partial list is never offered as the whole one.
- **A GONE play is not listed.** There is nothing to run, and the health section's freshness
  block already reports it, naming the successor where the retired-plays map knows one.
- **A click launches exactly that play, in a terminal, through the report's command.**
  `fedora-desktop-health --run-play <play> --hold`, by argv through `xdg-terminal-exec` — the
  route §9a's Plan 00136 note set up for the report. One launcher (`terminal.js`) serves both
  rows. The command sends the name through `play_runner.validate` before anything runs: a
  canonical repo-relative path under `playbooks/`, listed in this host's ledger, present,
  executable, and not a symlink out of the tree. The name comes from a state file, so it is
  judged as untrusted input. The play then runs through its own shebang, and so through
  `run.bash`, exactly as running it by path by hand does.
- **It says what it launched, never that the play ran** (§6). The next login's report reads the
  outcome from the ledger.
- **It does not move the icon.** The runner reads no check section; a stale play is already a
  finding in the freshness block, and counting it twice would be two voices on one fact.

§8 holds as restated: nothing runs unless a person clicks one named play, and nothing runs in
the background.

### 9a. Decided: the handoff offer copies, and it is not per-finding

The question was whether the health section's per-finding entries do anything when activated, and
what a click should do. Both halves are answered, and the first answer is **no**.

**The offer is section-level.** There is ONE handoff file and it describes EVERY finding —
`handoff.write` takes the whole flattened list. A clickable row per finding would hand out the
same command N times while implying each row had its own, which is a claim about granularity the
data does not support. The per-finding rows stay `reactive: false`, and that is now a decision
rather than an absence of one. One row sits at the bottom of the section, after everything it
refers to.

**It copies the command; it does not launch it.** Three reasons, and the first would break a
launch outright:

1. **`claude` reads the repository it starts in**, and this diagnosis is about playbooks. The
   panel does not know where the checkout is, and the status document does not carry it — a
   deliberate omission, since a host state file naming a checkout path is a host state file that
   goes stale on a re-clone. Launching from here would start Claude Code in the compositor's
   working directory, where it cannot see the thing it is being asked about.
2. **`container-watch` already copies its inspect hint and notifies**, on this same surface. A
   second idiom for "here is a command, you run it" would be one to learn for no gain.
3. **§8: the panel offers, a human decides** — and a clickable surface is precisely where that
   erodes. §6's terminal-launching mechanism belongs to Task 4.3, which has to choose it on
   ledger evidence this task does not have; building it here would front-run that.

**The path reaches the panel through the document, and only after the file exists.** The panel's
sole data source is `host-status.json`, so `status_document.build` carries a `handoff` key —
always present, `""` when there is none. `login_report.record_host_state` writes the handoff
FIRST and records the result, because a path written before the file is a button that fails in
the user's hands. `""` is a complete answer with three origins that agree on what they license:
a clean host, a failed write, and an unreadable document. None of them gets a button; all of them
still get their findings rendered, so a host with faults never goes quiet — it just has no
button.

`handoffPath()` refuses anything that is not an absolute string, because the value is
interpolated into a command a human runs and a relative path would resolve against whatever
directory their terminal opened in.

**Since settled for the report itself, by Plan 00136.** The health section launches one
thing: `fedora-desktop-health --hold` in the user's default terminal, through
`xdg-terminal-exec` by argv, from a row under the collection time. That is a reader, so §8
holds, and reason 1 above does not apply to it: the command is installed by the report play
with the checkout path written in, so the panel needs to know only the user's home
directory. Reason 1 still applies to the handoff, which still copies. Task 4.3's play
runner hangs off the same command, as `--run-play` (§9).

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

## 11. The panel's decisions, and where they are now provable

Three gates already touched this extension. `qa-js.bash` parses it, ESLint lints it, and
`check_panel_contract.py` proves it uses the same words as the producer. None of them can
tell a demoted finding from a current one — and on the primary surface for these findings,
that is the whole question. Neither can a screenshot.

The blocker recorded against Task 4.2 was "needs GJS". That is true of **rendering** and
false of everything before it. `statusDocument.js` imports only `Gio` and `GLib`;
`sections/health.js` adds `PopupMenu`. Every decision they make — is this finding current,
is this document readable, what does the icon say — is a plain function over a plain
object. Tying those to a Wayland session meant they were unprovable exactly where a wrong
answer is silent.

`tests/extensions/gjs-loader.mjs` resolves `gi://` and `resource:///org/gnome/shell/`
imports to stubs, so the tests import the **shipped** files. Two properties of that
harness are deliberate:

- **Each stubbed specifier gets its own generated module.** The panel imports defaults
  (`import GLib from 'gi://GLib'`), so one shared default would hand every import the same
  object and a test passing against it would be reporting on the stub.
- **An unknown GNOME specifier is a refusal, not an empty module.** A new import resolving
  to nothing would land silently, and the first anyone would know is a panel that does
  nothing in a live shell — the failure this harness exists to move earlier.

### What the tests found

**`state` was a second mechanism for a fact the lists already carried.** `sectionOf`
passed `section.state` through and both the menu and the icon branched on it, while the
producer DERIVES that field from the lists. A section saying `state: "ok"` over a populated
`findings` list rendered "nothing to report" on the panel while the login report showed the
fault. The fix is to derive it here too: the lists are the fact, and there is one reading
of them.

**A malformed document read as a clean host**, the same way `_texts` did on the Python side
before `unreadable_reasons` — a group that is not a list, entries that are not strings, and
a document naming no checks at all all degraded to "nothing in this group", which on this
surface means healthy.

**The demotion had to live in one place.** Applying it in the menu alone would leave the
icon reporting a fault the menu had already explained away, so `resolvedSection` is where
it happens and `overallState` reads the same function.

**The running kernel is read once, in `enable()`.** It cannot change without a reboot, and
a reboot ends the shell — so re-reading it per render would be repeated synchronous I/O in
the compositor for an answer that cannot have moved. An unreadable `/proc` gives `''`,
which suppresses the claim rather than inventing one, and is logged: a panel that silently
stopped asking would look like a host that never reboots.

### And one in the test helper

`groupIn` was first written as a flat scan for the not-checked heading across the whole
menu. The menu renders a heading **per check**, so one demoted section made every later
section's findings read as demoted — and several assertions passed only because the
ordering happened to suit them. A predicate whose answer depends on a distinction it does
not make, in the helper written to catch exactly that. It is scoped per check now.

### What this still does not claim

Whether St renders the demoted lines legibly, whether the caveat heading reads as a caveat,
and whether the icon colour is the right thing to look at. Those need a Wayland session and
a person, and the gate says so rather than implying its green covers them.

## 12. Decided: a separate extension reacts to unlock (Task 5.4a)

Task 5.4's recovery action `REFRESH_BACKGROUND` fires from the dock udev rule and from the
suspend service. The suspend path is **effectively inert**: the screen is already locked when
that service runs, so the run correctly refuses. Something in the *user* session has to react
to **unlock**.

**The owner chose C.** `extensions/dock-recovery-on-unlock@fedora-desktop/`, deployed by
`play-displaylink.yml` beside the units it starts, works like this:

- **Signal.** It connects to `Main.screenShield`'s `locked-changed` and acts only when
  `locked` turns false. Its `session-modes` include `unlock-dialog`. Otherwise the shell
  disables it at the lock and enables it after, and it would never see the unlock.
- **Action.** It spawns the argv `systemctl start --no-block displaylink-dock-recovery.service`,
  with no shell. That is the unit the udev rule starts, so the recovery reads the heads, the
  lock state and the journal and picks its own action. The extension decides nothing about
  the display.
- **Permission.** `files/etc/polkit-1/rules.d/50-displaylink-dock-recovery.rules.j2` lets
  the desktop user START that one unit, from an active local session only. It grants no
  stop, no restart and no other unit, and nothing to an SSH session. A sudoers entry, the
  precedent in Plan 00137, would also cover SSH sessions, which never unlock anything.
- **Timing.** It waits 5 s after the unlock. The shell sets logind's `LockedHint`
  asynchronously, and the recovery skips a session whose hint still says locked. A repeat
  signal inside that window adds no second start, and a lock inside it cancels the start.
- **Failure.** A start that cannot be spawned, or exits non-zero, is logged to the
  gnome-shell journal with the exit status and systemctl's stderr. It does not notify: a
  broken rule would otherwise raise a popup at every unlock.

The options as they were weighed:

**A. The panel.** GNOME Shell already dispatches lock state to extensions (`Main.screenShield`,
or `org.gnome.ScreenSaver`'s `ActiveChanged`), so the panel gets the signal for **no extra
process at all** — and Task 4.1's harness stubs GNOME imports, so the wiring would be
unit-testable to the same standard as everything else in Phase 4.

The cost is the contract. `extension.js` opens by saying the panel runs no check and applies no
fix, and that the one thing it launches is a terminal a person is looking at — the report, or the
single play they clicked (§9). §8 restates it, and Technical Decision 1 in [`DECISIONS.md`](DECISIONS.md) — *detection and
handoff, never unattended repair* — is the plan's own framing. Spawning a recovery helper on
unlock, with nobody clicking anything, ends that, and "it is only a display refresh, not a play" is exactly the kind of
narrowing that erodes a boundary one reasonable exception at a time.

**B. A user systemd unit.** `files/home/.config/systemd/user/` already holds seven units, so the
deployment path exists and the panel's contract stays intact. The cost is that **systemd has no
unlock trigger**: the unit would be a long-running D-Bus monitor whose entire job is to watch one
signal, which is a permanently resident process bought to avoid an architectural concession.

**C. A separate small extension.** Keeps both — the panel's contract and no extra daemon — at the
cost of a third custom extension with its own play, its own ESLint surface and its own Wayland
logout to load. `play-container-watch.yml` is the precedent for an extension that owns one
backend.

**Recommendation: C, then A.** C is the honest answer to "the panel is the natural owner" — the
*session* is the natural owner, and the panel is merely the session component that already
exists. B buys a resident daemon for a purity the other two get for free.

**The HOST item under Task 5.4 still gates it.** The unlock signal only
exists in a live Wayland session, and whether the refresh actually clears a black background has
only ever been exercised on a healthy desktop.
