# Fork and fix, or upstream only: Lightning's thread root

Read against `Mizerd/lightning` `main` at `e6ea127a` (v0.10.5 plus 75 commits; a fresh fetch on
2026-11-13 found nothing newer), its GitHub issues and pull requests, and its Flatpak manifest
`org.lightning_matrix.Lightning.yaml`. Plan 00253's
[CASE-STUDY-fork.md](../00253-element-fork-thread-switch-case-study/CASE-STUDY-fork.md) weighed the
same question for Element; where its reasoning carries over it is cited, not repeated.

## The bug and the fix

`qml/ThreadPanel.qml` draws the thread root in a card whose height is
`Math.min(content + margins, 190)` with `clip: true`, and whose body `Label` has
`maximumLineCount: 6` and `elide: Text.ElideRight`. Both caps date from the panel's first version
(`aca9954c`, then `63c9da78`, both July 2026) and no later commit touches them. The prototype
([lightning-thread-root.patch](lightning-thread-root.patch)) puts the body in a `Flickable` that
grows with its text to 40% of the panel's height (at least 120 scaled px) and scrolls beyond it,
drops both caps, and adds a data-driven QML test. One QML file plus one test; no C++ or Rust.

## Building it here: not possible, and what a build needs

The ccy container has no `cmake`, `ninja`, C++ compiler, Qt 6, `cargo`, `nix`, `flatpak` or
`flatpak-builder`, so the patch is **not built and not tested**. Installing them by hand would
break the IaC rule; a build is an IaC gap to close, not a manual install. What it needs:

- **The QML test alone** (enough for this patch): the non-Rust tree. CMake 3.21 or newer, Ninja, a
  C++20 compiler, Qt 6.5 or newer with Quick, QuickControls2, Svg, Test and LinguistTools, and
  `-DLIGHTNING_ENABLE_WEBRTC=OFF` to skip GStreamer. Then build the
  `ThreadPanelPaginationAnchorQmlTest` target and run it under
  `QT_QPA_PLATFORM=offscreen dbus-run-session -- ctest -R <its name>`. Upstream builds in
  `nix develop`; on Fedora the same packages exist as RPMs (`qt6-qtdeclarative-devel` and
  friends), so a toolbox or podman image declared in IaC would do.
- **A Flatpak to run** (to see the fix in the app): `flatpak-builder` with `org.kde.Sdk//6.11`
  and `org.freedesktop.Sdk.Extension.rust-stable`, building the root manifest from a local source
  directory (offline once `cargo-sources.json` is fetched). That runs on the host, never in the
  container.

Either is a play, or a Plan script run through `meta-deploy.bash`, if we choose to carry the
patch: Task 2.2 in [PLAN.md](PLAN.md).

## Upstream health

| Question                 | Finding                                                                                                                                                                                                                                                                                                                                                                                                   |
| ------------------------ | --------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Who maintains it         | One maintainer writes nearly every commit (1107 of 1136 between 2026-10-13 and 2026-11-13); five outside contributors had commits land in that span. The project is heavily agent-developed, by the maintainer's own guide.                                                                                                                                                                               |
| Release cadence          | 42 tags from 2026-07-13 (v0.5.14) to 2026-11-08 (v0.10.5). Often several in one week through September; since v0.10.0 the tags are 2026-10-01, 10-11, 10-21, 11-01, 11-02 and 11-08.                                                                                                                                                                                                                      |
| Review responsiveness    | Fast. PR 62 (avatar decode) was opened and applied on 2026-11-03, contributor kept as author. PR 40 (touch taps) opened 2026-10-13 and was applied on 2026-10-14, with a follow-up on the contributor's failing suites. PR 52 (Nix packaging, opened 2026-10-23) had detailed reviews on 2026-10-30 and 2026-11-04. Issues are answered and closed in batches (13 closed on 2026-10-11, 9 on 2026-10-21). |
| How a change lands       | The canonical repository is the maintainer's GitLab. GitHub is a mirror: a pull request there is applied by hand onto `main`, authorship kept, and the pull request closed rather than merged. GitHub issues are the public tracker.                                                                                                                                                                      |
| Open questions upstream  | Issue #74 ("AI policy?") is open and unanswered. Our patch is agent-written; the draft pull request says so.                                                                                                                                                                                                                                                                                              |
| History stability        | History was rewritten once (2026-08-11, message-only, maintainer-authorised); the project says it will not happen again. A fork would have to re-clone, not pull, if it did.                                                                                                                                                                                                                              |
| Churn where our patch is | High. `qml/ThreadPanel.qml` had 79 commits between 2026-08-13 and 2026-11-13, and the root card itself changed on 2026-11-07 (collapse to one line, `4e528c66`) and 2026-09-18 (width, `79a58fa6`). A carried patch to this file will conflict often.                                                                                                                                                     |

## Licence: GPL-3.0-or-later

A reading of the text, not legal advice (the same caveat as Plan 00253).

- Building and running a patched Lightning on the owner's own machines, with no copy handed to
  anyone else, creates no source obligation.
- Handing a built copy to anyone else is distribution: they are entitled to the complete
  corresponding source of that build under GPL-3.0-or-later, with notices kept. Publishing the
  upstream commit, our patch and the build manifest satisfies that; the manifest is part of "the
  scripts used to control compilation".
- Unlike Element (Plan 00253) there is no AGPL clause and no CLA: a patch we send stays under
  GPL-3.0-or-later with our authorship, and the maintainer keeps contributor authorship.
- fedora-desktop is MIT. The patch file kept here as a plan artefact is under its own licence
  (GPL-3.0-or-later, as a change to Lightning), not MIT. Once the upstream pull request exists, a
  link can replace the file.
- The name and icon: no trademark policy was found. A fork Flatpak should use its own app id (it
  must anyway, below) and must not present itself as the official Lightning.

## Shipping a fixed build to the desktop through IaC

| Route                                    | What it takes                                                                                                                                                                                                                                                                                                                                                                                                  | Verdict                              |
| ---------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------ |
| Wait for Flathub                         | Nothing. Since v0.10.0 Lightning's release pipeline opens the Flathub pull request itself, so a fix in a release reaches `flatpak update` shortly after its tag.                                                                                                                                                                                                                                               | The default                          |
| A local Flatpak build by a play          | `flatpak-builder`, `org.kde.Sdk//6.11` and the rust-stable extension on the host; a play clones upstream at a pinned commit, applies the patch, builds into a local repository and installs. It must use a different app id (two installs cannot both own `org.lightning_matrix.Lightning` in one installation), so every guard and launcher keyed on the id needs the second id as well (#251's client list). | Fallback only, and only while needed |
| A public fork plus a hosted Flatpak repo | The above, plus a repository and a signing key we host and keep alive.                                                                                                                                                                                                                                                                                                                                         | Not justified for one patch          |
| The official Flatpak with QML overlaid   | Not possible: `/app` is read-only and Lightning compiles its QML into the binary.                                                                                                                                                                                                                                                                                                                              | Ruled out                            |

## Drift cost

A carried patch on `ThreadPanel.qml` meets close to one upstream commit to that file for every
calendar day. Every release we take would mean a rebase likely to conflict in the very block we
change (the root card gained its collapse row on 2026-11-07), a rebuild of a Qt and Rust
application, and a re-run of its QML suite. Plan 00253 found Element's thread file almost static
and still ranked a carried patch second; here the file is one of the busiest in the project, so
the case for carrying one is weaker still.

## Recommendation

1. **Upstream only (recommended).** Post the issue with the patch offered (drafts in
   [UPSTREAM-DRAFT-thread-root.md](UPSTREAM-DRAFT-thread-root.md)) once the owner gives the go,
   ideally after the host build of Task 2.2 so the pull request can report real test totals. The
   maintainer's record says a small, clear fix is applied promptly and ships in the next release,
   which reaches Flathub without us.
2. **Fallback: a short-lived local Flatpak build**, only if upstream refuses the fix or two
   releases pass without it and the owner wants it sooner. Pin the upstream commit, apply the one
   patch, build with `flatpak-builder` on the host under a separate app id, and delete the whole
   route when a release carries the fix. Its cost is the IaC above, a rebase per release taken,
   and a second app id every guard must know.
3. **Not recommended: a standing fork.** A single maintainer moving this fast makes a fork a
   second full-time project, as Plan 00253 concluded for Element's full-fork route.

Until the fix ships, the card's "Open in room" button reaches the full message, and the collapse
toggle gets the root out of the way. Neither is a fix.
