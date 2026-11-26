## 11:53 · finding · —

Runaway evidence from 26 Nov, from the coordinating team's host-seat captures and reports. Names are replaced with placeholders: "team A" is the busier team, "team B" the other.

**The 26 Nov 11:10Z runaway (live capture, read-only):**

- Team A's instance started at 10:40Z at normal size. From about 11:10Z it grew at about 2.7 GB/min of anonymous memory, with about 6200 new QtQml memfd mappings a minute. At 11:15:42Z it had 22.5 GB RSS and 49k QtQml mappings out of 43k-51k total.
- The main thread was in state R at 100% of one core. Every other thread used under 17 s of CPU in total, so the UI could not respond.
- Lightning's own log went silent after 11:10:43Z. Its last four lines were the MessageDragSelection TypeError, a `thread open`, `thread subscription started`, and the TypeError again. The 5-minute search-index sweep stopped logging.
- That morning there were no coredumps, no systemd-oomd entries and no kernel oom-kill: a live runaway, not a crash.
- The process ignored SIGTERM at 27.6 GB; only SIGKILL stopped it. `flatpak kill` would also have stopped team B's instance. Folded into Task 1.8.

**Compared with 25 Nov:** the same burst from about 660 MB, the same silent log and the same recurring TypeError. The trigger is NOT established. On 26 Nov a thread open came just before the stall. On 25 Nov the last thread open was 23 minutes before it, and the stall began within 3 minutes of a read-state line.

**The build range (coordinator's report, section 8):** the runaways start with the Lightning Flatpak build of 22 Nov 08:02Z (app commit 51e71f77662d). There were none on the two earlier builds, whose peak was 2.2 GB on busy days, and the MessageDragSelection TypeError first appears on that build. A third runaway, 24 Nov 15:58Z, was on an instance from before the teams split. A fourth, 26 Nov 11:40Z, came right after a thread in the questions room was opened. So this is an upstream regression with a build range.

**The TypeError (section 7):** `MessageDragSelection.qml:35: TypeError: Property 'applyDragSelection' ... is not a function`. On team A's account it fires 13-20 an hour even with no runaway (25 Nov, 13-16Z; the 24 Nov counts are from instances whose account was not checked). On team B's account it never fires (0 across 30 thread opens), and that instance never runs away. Lead: content specific to team A's rooms (message shape, thread size, markdown), not the error itself.

**Decisions (owner, Q20, 26 Nov):**

- 11:38Z: no pin or mask of an older build ("lightning is under active dev ... lets push forwards"). The upstream side (the 17-22 Nov source diff, a duplicate-issue search, a TDD fix as a PR) is the coordinating team's own investigation. This plan does not draft the upstream issue.
- 11:40Z: a plain cap now, 2 GB, with a report when it trips; a manual restart is acceptable. Built as Task 1.9.
- The coordinating team's one-off stack capture aborts a runaway at 1.5 GB, below the cap, so the two do not collide.
