# Research: Element opens the wrong thread

Read against element-web `develop` at commit `7ec1460387a3bcd79125265e7b2e90aef0040bbe`
(shallow clone under `untracked/scratch/element-research/element-web/`, read only; no install
script or code from it was run). Paths below are relative to `apps/web/src/`. element-web is now a
pnpm and nx monorepo that also holds Element Desktop (`apps/desktop`) and the shared UI library
(`packages/shared-components`), so the standalone element-desktop repository is archived (its
README says it was merged back; its last commit is `264c591b9c`, v1.12.13) and the older
matrix-react-sdk repository no longer exists as a dependency.

## The upstream issues as they stand

| Issue                                                            | State  | Labels                | What it says                                                                                                                                                                                                                                                                               |
| ---------------------------------------------------------------- | ------ | --------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| [#24305](https://github.com/element-hq/element-web/issues/24305) | open   | S-Major, A-Threads    | "Wrong thread displayed after opening from a notification". Old thread stays; clicking the new thread flickers and the old one returns; switching room and back cures it. A maintainer reproduced it by rapid switching and saw `TimelinePanel switching to eventId undefined (was $...)`. |
| [#25694](https://github.com/element-hq/element-web/issues/25694) | open   | S-Major, A-Threads    | Open a room by a URL that points at a thread message; clicking any other thread opens it, then it is "quickly replaced again by the thread from the URL". Maintainer: "Can reproduce". Last touched 2024-08-08.                                                                            |
| [#25889](https://github.com/element-hq/element-web/issues/25889) | open   | S-Minor, A-Threads    | "Reply in thread" on message b jumps back to thread a. A maintainer says it is "fundamentally the same as" #25694.                                                                                                                                                                         |
| [#21956](https://github.com/element-hq/element-web/issues/21956) | closed | S-Minor, X-Needs-Info | Same symptom, 2022; closed as fixed by matrix-react-sdk PR 8448.                                                                                                                                                                                                                           |

Two corrections to the owner's summary. #25889 is S-Minor, not Major. And #24305 is a notification
click, with rapid switching as the way a maintainer hit the same state. None of the three has an
assignee or a linked fix. All three are "A-Threads", and none has had a maintainer comment since
2024-08-08.

## The code path

1. **Choosing the thread.** The right panel shows a card; for a thread the card is
   `{ phase: ThreadView, state: { threadHeadEvent, initialEvent, isInitialEventHighlighted, initialEventScrollIntoView } }` (`stores/right-panel/RightPanelStoreIPanelState.ts`).
   `RightPanel.tsx` (~line 225) renders `<ThreadView mxEvent={cardState.threadHeadEvent} initialEvent={cardState.initialEvent} ...>`. The card is written by the `ShowThread` action
   handler in `components/structures/MatrixChat.tsx` (~line 924), which calls
   `RightPanelStore.setCards` or `pushCard`.
2. **A permalink is state, not an event.** A room URL with an event id, a notification click and a
   "jump to message" all dispatch `ViewRoom` with `event_id`. `stores/RoomViewStore.tsx`
   (`viewRoom`, ~line 432) stores it as `initialEventId`. It stays there until another `ViewRoom`
   dispatch replaces it.
3. **RoomView re-reads it on every store update.** `RoomView.onRoomViewStoreUpdate`
   (`components/structures/RoomView.tsx`, ~line 611) runs on every `RoomViewStore` `UPDATE_EVENT`.
   At ~line 675 it reads `roomViewStore.getInitialEventId()`; when that event lies in a thread and
   is not the thread root (~line 694) it dispatches `ShowThread` with `rootEvent: thread.rootEvent`
   and `initialEvent`. That re-opens the permalinked thread in the right panel.
4. **Every thread switch causes a store update.** `ThreadView.postThreadUpdate`
   (`components/structures/ThreadView.tsx`, ~line 262) dispatches `ViewThread`, and
   `RoomViewStore.viewThread` (~line 532) does `setState({ threadId })`, which emits `UPDATE_EVENT`.
5. **The only place the sticky id is cleared is unmount.** `ThreadView.componentWillUnmount`
   (~line 125) dispatches a bare `ViewRoom` (no `event_id`) when `props.initialEvent` was set, and
   `RightPanelStore.handleViewedRoomChange` (~line 474) clears the card's `initialEvent` on a room
   change. `ThreadView.componentDidUpdate` (~line 146) handles a changed `mxEvent` by calling
   `setupThread` and nothing else. Switching from thread A to thread B inside the right panel
   keeps the same `ThreadView` instance (same card phase, same position in the tree), so no unmount
   happens and the permalink survives.
6. **What the log line is.** `TimelinePanel.componentDidUpdate`
   (`components/structures/TimelinePanel.tsx`, ~line 346) logs `TimelinePanel switching to eventId X (was Y)` when the `eventId` prop, which `ThreadView` fills from `initialEvent`, changes. The
   maintainer's `undefined (was $...)` is the panel dropping the permalinked event when thread B
   (no initial event) arrives, followed by the stale `ShowThread` putting thread A back.

### Likely cause

The permalinked `initialEventId` in `RoomViewStore` outlives the user's own choice of another
thread, so the next unrelated `RoomViewStore` update (the `ViewThread` that thread B's own mount
dispatches) makes `RoomView` replay `ShowThread` for the stale permalink. It fits every symptom:
flicker then old thread (#24305, #25694), "reply in thread" on b returns to a (#25889: step 4
selects a by permalink-style jump), cure by switching room (the `ViewRoom` resets it), and the owner's
"close the panel, click again" (closing unmounts `ThreadView`, which clears it). The 2022 fix (PR
8448\) addressed an earlier form; the present code has the unmount-only cleanup and the
`isInitialEventHighlighted` dance on top.

**Confidence.** Medium-high that the mechanism is real, from reading the code; not yet reproduced.
I did not run the app. Two parts are unproven: that a plain click on a thread summary
(no permalink involved) can leave `initialEventId` set, which the owner's description suggests but
the upstream issues only show for permalink and notification entry; and the exact ordering of the
store update against the `ShowThread` handler.

### Reproduction that would confirm it

In a development build (or an instrumented Playwright run), in a room with two threads, A and B:

1. Open the room by URL with the event id of a reply inside thread A.
2. Click thread B's summary in the main timeline.
3. Expect B; the bug shows A returning.
4. With the debugger or a `console.log` at `RoomView.tsx` ~line 675, confirm `getInitialEventId()`
   still returns the thread A reply after step 2.

Then repeat without step 1 (open the room normally, open A from its summary, then B) to learn
whether a permalink is needed at all. If that also fails, the sticky value is set by another path
and the fix site moves with it.

## Proposed fix

Treat the permalink as consumed once the user chooses a thread themselves.

- In `ThreadView.componentDidUpdate`, when `mxEvent` changes and the room view store still holds an
  `initialEventId` that is not the new `initialEvent` prop's id, dispatch the same bare
  `ViewRoom { action, room_id, metricsTrigger: undefined }` that `componentWillUnmount` already
  dispatches. That is the existing cleanup moved to the one transition it missed; it adds no new
  action and no new state.
- Rejected alternative: have `RoomView` remember the last permalink it handled and skip a repeat.
  It cannot tell a repeated click on the same permalink from a stale replay, because the store
  emits the same value either way.

### Tests

Element-web now runs unit tests on vitest, not Jest (`AGENTS.md`, "Running unit tests").

- `components/structures/ThreadView.test.tsx`: next to "clears highlight message in the room view
  store" (~line 207, which spies on `dispatcher.dispatch` and expects the bare `ViewRoom`), add one
  test that renders with thread A, sets the store's initial event id, re-renders with thread B and
  expects the `ViewRoom` dispatch; and one that re-renders with `initialEvent` equal to the store's
  id and expects none.
- `apps/web/playwright/e2e/threads/threads.spec.ts`: a test that makes two threads, navigates to a
  reply in the first by URL, clicks the second thread's summary, and asserts the right panel header
  and root message are the second thread's after a short settle; it must fail on `develop` first.
- Run: `pnpm vitest run apps/web/src/components/structures/ThreadView.test.tsx`; lint with
  `pnpm lint` (oxlint and oxfmt, not eslint or prettier); `pnpm i18n` only if a string changes
  (none does).

## Upstream route

- **Terms.** `CONTRIBUTING.md`: contributions require a Contributor License Agreement (CLA, through
  cla-assistant; the DCO sign-off is not what they use). The CLA lets Element relicense, which is
  how the dual and commercial licence works. Pull request descriptions must be written by a human:
  "Any PR with a generated AI PR description will be closed." Before substantial work, comment on
  or open an issue and talk to the developers in the element-dev Matrix room. Each PR needs a
  step-by-step testing strategy and a type label (`T-Defect` etc.).
- **Outside thread fixes.** The thread issues above have sat unassigned for years, so nobody has
  picked them up; that is a gap, not a refusal. `A-Threads` PRs from outside the company do merge
  (for example PR 32851 by a CONTRIBUTOR, closed 2026-09-25, a threads feature).
- **Turnaround, from recently merged PRs** (checked on the pull request list): maintainer PRs
  merge within one to five days; the community PRs I could date took two to three weeks
  (PR 35553, opened 2026-10-21, merged 2026-11-11; PR 35707, opened 2026-11-05, merged
  2026-11-10; PR 35726, opened 2026-11-06, merged 2026-11-11). Not a sample large enough for a
  promise. Review is `https://github.com/element-hq/element-meta/wiki/Review-process`.
- **Release path.** `develop` reaches a final release on a fortnightly rhythm: v1.12.30
  (2026-09-29), v1.12.31 (2026-10-13), v1.12.33 (2026-10-27), v1.12.34 (2026-11-11), each after an
  rc. A merged fix therefore reaches users in the next final, then the Flathub update PR
  (Flathub's `im.riot.Riot` bumps follow within about a day of the tarball, per its commit log).
