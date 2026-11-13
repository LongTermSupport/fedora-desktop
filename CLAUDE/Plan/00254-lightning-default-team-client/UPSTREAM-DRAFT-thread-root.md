# Draft: upstream issue and pull request for the clipped thread root

**The issue is posted** as [Mizerd/lightning#75](https://github.com/Mizerd/lightning/issues/75),
on the owner's go, with the QML change as an untested sketch. **The pull request is not**: it
waits for Task 2.2's totals (Task 2.4 of [PLAN.md](PLAN.md)). The canonical repository is the maintainer's GitLab; GitHub
(`Mizerd/lightning`) is a mirror where the maintainer applies a pull request by hand onto `main`,
keeps the contributor as author, and closes the pull request (PR 62 and PR 40 were handled this
way).

## Existing reports: none found

Searched on GitHub, against the 74 issues and 16 pull requests there (Lightning HEAD `e6ea127a`,
v0.10.5 plus 75 commits):

- `gh issue list -R Mizerd/lightning --search thread --state all`: 9 hits, none about the root card
  (#69 spoilers, #51 keybinds, #8 Ubuntu Touch, #55 room-return defaults, #11, #3, #12, #56, #10).
- `gh pr list -R Mizerd/lightning --search thread --state all`: PR 50 (message keybinds) and PR 7
  (Lithuanian), neither related.
- The full issue and pull request lists were read by title: nothing on a clipped, truncated or
  unscrollable thread root.
- Lightning's own history: the 190 px cap and the six-line elide have been there since the thread
  panel was first written (`aca9954c`, then `63c9da78` "Rebuild threads as the 340px right-side
  panel"). No later commit touches either.

The maintainer's GitLab tracker was not searched. Section 15 of Lightning's own `CLAUDE.md` says
public registration and merge requests may not be enabled there, so GitHub is where outsiders
report; the issues above show the maintainer answers there.

## Draft issue

**Title:** Thread panel: a long root message is cut off and cannot be read

**Version:** Lightning 0.10.5 (Flathub, `org.lightning_matrix.Lightning`), Fedora 44, GNOME on
Wayland. The code is unchanged on `main` at `e6ea127a`.

**Steps**

1. In a room, find or send a long message: several paragraphs, or a numbered list of ten items.
2. Open its thread, or reply to it in a thread.

**Expected:** the whole root message can be read in the thread panel, by scrolling if it is
taller than the space it is given.

**Actual:** the root card shows the first six lines, ends the sixth with an ellipsis, and has no
scroll bar. The rest of the message cannot be read from the thread panel. When the message ends
with a list of choices to answer, those choices are hidden while the reply is written; the only
way to read them is "Open in room", which leaves the thread.

**Cause** (`qml/ThreadPanel.qml`, the `threadRootHeader` card): two caps, either one enough to
lose text.

- The body `Label` (`threadRootBody`) has `maximumLineCount: 6` and `elide: Text.ElideRight`.
- The card's `implicitHeight` is `Math.min(rootColumn.implicitHeight + spacing12 * 2, 190)` with
  `clip: true`, so at a larger text scale even six lines can be clipped mid-line.

**Suggested behaviour:** the body grows with its text up to a cap (for example 40% of the panel's
height), and scrolls beyond it. Nothing is elided. The collapse toggle stays as it is.

A patch is attached below (or offered as a pull request).

## Draft pull request

**Title:** Let a long thread root grow to a cap and then scroll instead of eliding it

**Body:**

The thread panel's root card was capped at 190 px with `clip`, and its body at six lines with an
ellipsis, so a long root message could not be read from the thread (issue above).

The body `Label` now sits in a `Flickable` (`threadRootBodyScroll`):

- its height follows the text up to `rootBodyMaxHeight`: 40% of the panel's height, and never less
  than 120 scaled px;
- beyond that it scrolls, with the usual `AppScrollBar`, and `interactive` only when it overflows,
  so a short root does not swallow wheel events meant for the reply list;
- it scrolls back to the top when a different root is shown;
- `maximumLineCount` and `elide` are gone, and the card's 190 px cap is gone.

The existing cases still hold: the body is never wider than its column, and collapsing the root
still shrinks the card. One new data-driven case in `ThreadPanelPaginationAnchorQmlTest`,
`theThreadRootGrowsToItsCapThenScrollsAndIsNeverElided`: a two-line root fits without scrolling,
a sixty-line root is held at the cap, is interactive, is not `truncated`, and can be scrolled to
its last line.

Validation: <!-- FILL IN after the host build of Task 2.2; until then: -->
Not built and not run here: no Qt toolchain was available to the author. CTest and the QML test:
**NOT TESTED**. Live GUI: **NOT TESTED**. <!-- Replace with exact totals once built. -->

Disclosure: written with an AI coding agent and reviewed by a human before submission.

<!-- Issue #74 on Lightning asks whether the project has an AI policy; none was found. -->

The patch: [lightning-thread-root.patch](lightning-thread-root.patch) (one commit on `e6ea127a`).
