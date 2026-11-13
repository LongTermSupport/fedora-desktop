# Lightning: threads side panel uses only the top half

Owner's report: the threads side panel shows content only in the top 50%; that content scrolls, the bottom 50% is empty. Ultrawide monitor, Flathub build.

## Version the owner runs

Flathub `flathub/org.lightning_matrix.Lightning` manifest (head cc6034cce0, "Update to 0.10.5") builds `Mizerd/lightning` at tag `v0.10.5`, commit `2b63840bf06db13010726628d5d6a8e2c165aa0a`. Read both that tag and `origin/main` at 020a214c.

## Cause (high confidence, read from code; not run)

`qml/ThreadPanel.qml`, the top-level `ColumnLayout` (line 308 on main) holds two children that both have `Layout.fillHeight: true`:

| Item                     | main line | v0.10.5 line | visible                                     |
| ------------------------ | --------- | ------------ | ------------------------------------------- |
| thread list area         | 431       | 428          | `!app.thread.active && app.thread.listOpen` |
| reply area (`replyList`) | 920       | 904          | none, so always laid out                    |

`ColumnLayout` splits the spare height evenly between visible fill-height items. When the room's thread list is open and no thread is active, both are visible: `threadListView` (anchors.fill of its item) gets half and scrolls inside it, and the reply area, whose children are all gated on `Opening`/`Failed`/`Ready`, draws nothing in the other half. That is exactly the report. When a thread is opened, the list area hides and the reply area gets full height, so the open-thread view is fine.

Introduced by 0ce26cdf "Add thread unread and navigation" (2026-07-17, first in v0.6.0), which added the list area beside the existing reply area. The ultrawide monitor only makes it more visible (taller and wider panel); it is not a width or SettingsManager-size issue. `TimelinePane.qml` sideSurface gives the panel its full height; 020a214c only changes its width cap.

## Fix

`ThreadController::active()` is `m_state != Closed` (`src/threads/ThreadController.h:97`), so:

```diff
@@ -920,6 +920,7 @@ (qml/ThreadPanel.qml, origin/main)
         Item {
             Layout.fillWidth: true
             Layout.fillHeight: true
+            visible: app.thread.active
```

Nothing visible is lost: every child is already gated on a non-Closed state. With the list closed and no thread active the panel shows only its header, as before (the empty item is just hidden). A QML layout test asserting `threadListView.height` is about panel height minus header, with the list open and no thread, would pin it.

## PR #77

Already merged to main as 76f334f8 (the branch `thread-root-grow-then-scroll` is that commit). It changes the root card's height cap (`rootBodyMaxHeight`, the root `Flickable`), which is hidden when no thread is active. It neither causes nor fixes this.

## Duplicate check

`gh issue list -R Mizerd/lightning --state all` searches ("thread panel half", "threads panel empty", "thread list height", "side panel half", "threads list") and a scan of the last 40 issues: no duplicate. Related but different: #75 (root cut off, closed), #76 (panel width cap, closed).

## Draft issue

`untracked/scratch/lightning-issue-halfpanel.md` (not posted). Working copies of the two ThreadPanel.qml versions are in `untracked/scratch/halfpanel/`.
