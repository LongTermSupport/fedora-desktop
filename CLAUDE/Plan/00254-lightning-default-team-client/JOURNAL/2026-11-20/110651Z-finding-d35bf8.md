## 11:06 · finding · — — v0.10.6 on Flathub carries the fixes; baseline study of lightning

Lightning studied as a dependency in the watch-list baseline run (Plan 00258): CLAUDE/WatchlistRuns/baseline-20261120-1050/team-client.md, at upstream 02d40ce5.

- Verified from the clone and Flathub: v0.10.6 (61642b3c) carries #77 (76f334f8) and #76 (020a214c), and Flathub merged it on 2026-11-16. Task 7.1's gate (a) is met; Task 2.5's live check can run after the play's next flatpak update, and the thread-root patch can go after that.
- #88 and #89 are fixed on main after v0.10.6, still open, for the next tag. #90 waits on a pull request from us, on the maintainer's terms. #100-#102 were filed today.
- Risks for a team-bus client, from the report: an open upstream event-cache gap that can drop a message for good (upstream #30, #48); an edit synced just after its original may be lost (seen in tests only); one maintainer writes about 98% of commits; weekly releases with the thread panel changing most weeks; no testing on our homeserver or desktop. 0.10.6 changed default shortcuts and turned on background indexing.
