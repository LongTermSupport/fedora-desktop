# Watch-list run baseline-20261120-1050: team-client

One repository. Lightning is not studied for ideas to build into ccy: it is the Matrix client
the team humans use to read an agent bus, so this baseline studies it as a dependency of
[Plan 00254](../../Plan/00254-lightning-default-team-client/PLAN.md). The study used the clone
at the run's commit, read only, and the public GitHub issues, releases and Flathub repository.
No earlier scan notes on Lightning were found under `untracked/watchlist-reports/` or
`CLAUDE/Plan/00200-repo-watch-list/subagent-reports/`, so Plan 00254 and its research files
are the only earlier record.

## lightning-matrix/lightning

Studied: baseline at 02d40ce5f1595b88064a5048f717f3ed742fcedc

### What it is, and who it is for

A native desktop Matrix client: Qt 6 and QML for the interface, a C++20 application layer,
and the official Rust Matrix SDK (matrix-sdk 0.18, `rust/Cargo.toml`) for everything Matrix:
sync and Sliding Sync, timelines, threads, E2EE, media, receipts. Linux and NixOS first, also
Windows and macOS. It is for people who want a fast, non-Electron Matrix client; it ships
Flathub, AppImage, snap, `.deb`, `.rpm`, a COPR, Nix and Windows packages
(`packaging-ci/`, `.copr/`, `flake.nix`, `org.lightning_matrix.Lightning.yaml`).

The GitHub repository moved from `Mizerd/lightning` to `lightning-matrix/lightning` in 0.10.6
(the old address redirects). The development remote named in the repository's own agent guide
is a self-hosted GitLab, with GitHub as the public face for issues, releases and the updater
manifest (`CLAUDE.md` section 1; release notes of v0.10.6).

### Licence and reuse class

`LICENSE` is the GNU GPL version 3; the row's `GPL-3.0` is right. Reuse class `clean-room`:
nothing here may be copied, and nothing needs to be, because we consume the Flathub build
rather than its code. Plan 00254's own patches were contributed upstream under its licence,
which is the direction the licence allows.

### Architecture, as it bears on reading a team bus

- **Layering** (`CLAUDE.md` section 5, `docs/architecture.md`): QML owns presentation only;
  C++ owns application state, models, notification policy and routing; the Rust SDK owns all
  protocol behaviour. A display defect (all of ours so far) is almost always in one QML file,
  which is why our reports have been fixed so quickly. A protocol defect would sit in the SDK
  or the FFI bridge and be slower and harder for us to diagnose.
- **Threads** (`CLAUDE.md` section 8; `docs/feature-contracts.md`, "Threads";
  `src/threads/`, `qml/ThreadPanel.qml`): the room timeline hides threaded replies; each
  thread is its own SDK thread-focused timeline; the root stays in the main timeline with a
  summary card. The project holds a hard rule that a thread reply never falls back to an
  ordinary room send: a send that cannot reach the thread fails instead. That matches how
  pingbus expects human replies to arrive. The thread list is the SDK's thread list service.
  Thread subscriptions (MSC4306) are followed only after the human replies in a thread, and
  only on a server that advertises it; being mentioned in a thread does not subscribe, and
  subscription does not change push verdicts. Unread state per thread is a conservative dot,
  never an invented count.
- **The thread panel** is where every one of our reports has landed: the root card (cap,
  scroll, selection, Markdown), the reply list's follow-latest behaviour, the side panel's
  width and the thread list's height. Upstream rewrites this area constantly (Plan 00254
  counted 79 commits to the panel file in three months; 103 changed lines in
  `qml/ThreadPanel.qml` between v0.10.6 and the studied commit alone).
- **Edits** (`docs/matrix-feature-status.md`): `m.replace` is rendered through the SDK
  timeline. Our open #101 (an edited thread root keeps its first text in the thread list)
  is the thread list service not following an edit, a separate path from the timeline's.
- **Notifications** (`src/notifications/NotificationManager.*`,
  `src/app/NotificationTroubleshooter.cpp`): a C++ policy over the SDK's push-rule
  evaluation, with a desktop-notification path, a Do Not Disturb rule and an in-app Activity
  Center with a Threads area. `docs/matrix-feature-status.md` still says notifications are
  "log only", which is stale: the feature table there trails the code, so read
  `docs/feature-contracts.md` and the release notes instead.
- **Multi-account** exists (one active account per window, Ctrl+\` to switch, a Labs
  "live accounts" mode, off by default). Plan 00254 rightly does not use it: one process per
  team through per-team XDG roots is the hard requirement, and the instance lock and data
  root follow `XDG_DATA_HOME` (`docs/architecture.md`, the `MatrixClient/` data directory).
- **Rooms with many bots**: nothing in the code treats bots specially. Relevant behaviour
  is generic: the room list and timeline cost (0.10.6 bounded the growth of an open room's
  memory, at about 0.55 MB per message before), presence dots hidden on servers that share
  no presence, and mention pills. A busy bus room left open on a team desktop all day is
  exactly the case 0.10.6's memory trim targets.
- **Homeserver proximity**: the maintainer tests against Continuwuity
  (`docs/open-items.md`, the 2026-10-16 and 2026-11-18 entries). Our teams run Tuwunel, a
  sibling of the same conduwuit lineage, so server quirks they hit (Continuwuity not telling
  Sliding Sync a room was left) are likely, though not proven, to apply to us. Guess.
- **Updates in a Flatpak**: the in-app updater never installs inside a Flatpak; it tells the
  user to use `flatpak update` (`docs/updates.md`). That fits our `flatpak_update: true` in
  `play-matrix-clients.yml`: the play is the only update path.

### Maturity and activity

- 3,149 commits from 2026-07 to the studied commit (2026-11-20); about 750 to 800 a month
  since September. 178 commits in the four days since v0.10.6.
- One maintainer in effect: about 98% of commits are one person under two identities; the
  next contributor has 15. The project is developed largely through coding agents: a
  2,325-line agent guide (`CLAUDE.md`) with a multi-agent review protocol, checkpoint and
  completion-report rules, and long `docs/open-items.md` and `docs/round-history.md` records.
- Releases: 30 tags since v0.7.0 (2026-08-15), every 3 to 10 days; recently roughly weekly
  (v0.10.3 11-01, v0.10.4 11-02, v0.10.5 11-08, v0.10.6 11-16). Each release has long notes
  with an explicit "NOT TESTED" list and "how this release was checked" section, which is
  unusually honest and useful to us. Published tags are immutable by policy (`CLAUDE.md`
  section 14).
- **Flathub follows within the hour.** The Flathub repository's last update merged "Update to
  0.10.6" at 11:37 UTC on 2026-11-16, 45 minutes after the GitHub release; every earlier
  release shows the same pattern. The Flathub manifest pins `v0.10.6` at commit `61642b3c`.
- Testing: Rust and C++ suites on GitLab runners, QML tests, manual passes on NixOS and
  Windows before a release. Fedora/GNOME is not one of the named manual passes, so our
  platform is covered by automated tests only.

### Maintainer responsiveness to our reports

`gh issue list --repo lightning-matrix/lightning --author <our account> --state all`, with
comments, on 2026-11-20:

| Issue / PR  | Subject                                                    | State          | Upstream response                                                                                  |
| ----------- | ---------------------------------------------------------- | -------------- | -------------------------------------------------------------------------------------------------- |
| #75, PR #77 | Long thread root cut off                                   | Closed, merged | Our PR merged the same day (2026-11-13); in v0.10.6, credited in its notes                         |
| #76         | Side panel capped at 640 px                                | Closed         | Fixed on `main` the same day; in v0.10.6                                                           |
| #78         | Text selection cannot span messages                        | Closed         | Fixed next day as a keyboard range selection and "Copy messages"; in v0.10.6                       |
| #79         | Thread list uses half its height                           | Closed         | Fixed next day; in v0.10.6                                                                         |
| #88         | Thread root text cannot be selected                        | **Open**       | Fixed on `main` 2026-11-17, plus a touchpad scroll follow-up; "in the next release"                |
| #89         | Scrolling replies jumps back to the newest                 | **Open**       | Our hypothesis confirmed and fixed on `main` 2026-11-17; scroll bar to top now loads older replies |
| #90         | Draggable separator for the root area (feature)            | Open           | "happy to take a PR"; keep the current cap as default and remember the dragged size                |
| #100        | Thread root shown as raw text, no Markdown                 | Open           | Filed 2026-11-20, no response yet                                                                  |
| #101        | Edited thread root keeps its old text in the list          | Open           | Filed 2026-11-20, no response yet                                                                  |
| #102        | "Open in room" does nothing when the thread fills the pane | Open           | Filed 2026-11-20, no response yet                                                                  |

Every report so far was answered and fixed within about a day, usually with the reproduction
and hypothesis credited. #88 and #89 are fixed but left open; the earlier pattern suggests
they close on release (guess). The fixes for #88 and #89 are in commits `a84c0c16`,
`50431e7c`, `bb475b8b` and `2f192407`, all after v0.10.6 and ancestors of the studied commit,
so they will be in the next tag. #90 is the only one waiting on us.

### Findings that change Plan 00254's state

1. **v0.10.6 carries the thread-root fix and is on Flathub.** PR #77's commit `76f334f8` and
   #76's `020a214c` are ancestors of `v0.10.6`, and Flathub shipped v0.10.6 on 2026-11-16.
   Plan 00254 still says "the latest release is still v0.10.5" (status line, Task 2.5, Task
   7.1 gate (a)). Gate (a) of Task 7.1 is now met, Task 2.5's live check can run after the
   play's next `flatpak update`, and `lightning-thread-root.patch` can then be deleted.
2. **The next release will carry #88 and #89**, so
   `lightning-thread-root-selectable.patch` is due for deletion then too.
3. **The repository moved.** Plan 00254 Task 1.1 still names `Mizerd/lightning`; the plan's
   later tasks already use `lightning-matrix/lightning`.
4. **#90 is ours to deliver** if the owner still wants the separator: the maintainer's terms
   are recorded above.

### Known regressions and open defects that touch a bus reader

From `docs/open-items.md` and the v0.10.6 notes; none is ours, each could bite us:

- **A missing plain message** (upstream GitHub #30/#48, open since 2026-10-20): a message
  Element shows can be permanently absent from Lightning's room, consistent with a hole in
  the SDK's event cache. One cause fixed (classic sync not subscribing the cache early); two
  hypotheses open, one being a server answering `limited: false` over a gap. A reset of the
  local cache clears it. For a bus, a silently missing agent message is the worst defect
  class, and Tuwunel's sync behaviour is untested by upstream (guess).
- **An edit synced right after its original may be lost** (open since 2026-10-17): seen only
  in test fixtures, not shown to be impossible in production. Pingbus delivers edits to a
  human as revisions, so this matters if agents edit soon after posting.
- **A reopened jump-to-message view can show stale edits or deletions** until restart
  (matrix-sdk 0.18 limit, v0.10.6 known limits).
- **Changed default shortcuts in 0.10.6** (mark read moved to Ctrl+R, start a DM to Ctrl+N,
  and others): any owner muscle memory or doc we wrote for 0.10.5 shortcuts is stale.
- **First sign-in writes about 80 MB** in its first minutes while background search indexing
  runs (on by default since 0.10.6). Per team instance, so N teams means N times it once.
- The notification round's ring, `m.fully_read` and thread auto-subscribe behaviour is
  marked "Live: NOT TESTED" upstream.

### How it compares with what we build

We build no Matrix client; the comparison is with Element, the other client
`play-agent-bus-element.yml` supports. Lightning is ahead on what a bus reader needs most:
a thread panel and thread list that upstream tunes weekly and fixes on report, native
performance with many rooms, and a maintainer who takes our pull requests. It is behind
Element on breadth of tested platforms (Fedora/GNOME is automated-only), on server
diversity in testing, and on the open event-cache gap above. It has no `--profile`, which
Plan 00254 works around with XDG roots; upstream's own multi-account work goes the other
way (several accounts in one process), so a per-profile flag is unlikely to arrive unasked.

### Ideas worth adopting

Not a source of ideas for ccy by design. Two dependency-handling ideas:

- **Track each release against our open reports**, not only the thread panel: compare each
  new tag's ancestry with the fix commits named in our issues, and the release notes' "Known
  limits" and "NOT TESTED" sections with our usage. Lands in this group's diff runs and Plan
  00254 Task 2.7. Clear win; it is how this baseline found finding 1.
- **Ask the maintainer whether a Tuwunel server is in any test path**, or offer a reproduction
  of the missing-message trace (`LIGHTNING_TIMELINE_TRACE=1`, documented upstream in
  `docs/open-items.md`) from a team desktop if a gap is ever seen. Needs discussion (it would
  involve a team human's log, scrubbed). Plan 00254.

### Code worth lifting

None. GPL-3.0, clean room; and we consume the Flathub build, not the source.

### Risks to depending on it

| Risk                                          | Note                                                                                                                                                |
| --------------------------------------------- | --------------------------------------------------------------------------------------------------------------------------------------------------- |
| Bus factor of one                             | One maintainer writes about 98% of commits. Element stays installed and selectable, which is the mitigation Plan 00254 already keeps.               |
| Churn in exactly the area we use              | The thread panel changes most weeks; an unpinned `flatpak update` (D1) takes every change. Weekly releases make regressions likely and fixes quick. |
| Silent message loss (upstream #30/#48)        | Open; the worst class for a bus. Watch it in diff runs; Element is the fallback for a team that sees it.                                            |
| Untested on our server and desktop            | Upstream tests on Continuwuity, NixOS and Windows; Tuwunel and Fedora GNOME are ours to test (Plan 00254 Task 7.2).                                 |
| Agent-driven development at high velocity     | 750 to 800 commits a month; review discipline is documented but live validation is often "NOT TESTED" by the project's own account.                 |
| Shortcut and default changes between releases | 0.10.6 moved several defaults and turned on background indexing; any docs we write about Lightning's keys or settings will rot.                     |

### Strongest takeaways, ranked

1. Plan 00254 is out of date: v0.10.6 (2026-11-16, on Flathub the same hour) carries the
   thread-root fix (#75/#77), #76, #78 and #79. Task 7.1's gate (a) is met; Task 2.5's live
   check and the deletion of `lightning-thread-root.patch` can proceed.
2. The maintainer has fixed every one of our reports within about a day; #88 and #89 are
   fixed on `main` and will ship in the next tag; #100 to #102 were filed today and have no
   answer yet; #90 waits on a pull request from us.
3. The real dependency risk is not responsiveness but the open SDK event-cache gap that can
   drop a message for good (upstream #30/#48), plus a bus factor of one and weekly churn in
   the thread panel; Element must stay selectable.
4. Plan 00254 Task 1.1 still names the old repository, `Mizerd/lightning`.
