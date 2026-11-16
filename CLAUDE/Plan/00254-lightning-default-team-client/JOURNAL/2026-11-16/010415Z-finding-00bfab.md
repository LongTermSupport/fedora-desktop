## 01:04 · finding · — — Gate (a) checked: Flathub has 0.10.5, which lacks the thread-root fix

Task 7.1's gate (a), checked: no Lightning release on Flathub carries the thread-root fix yet.

- Flathub: the published release of `org.lightning_matrix.Lightning` is **0.10.5** (Flathub
  appstream API, `/api/v2/appstream/org.lightning_matrix.Lightning`, releases[0]); the
  manifest repository `flathub/org.lightning_matrix.Lightning` last changed in its PR #6,
  "Update to 0.10.5", merged 2026-11-08, and has no open pull request.
- Upstream: the newest tag and release of Mizerd/lightning is **v0.10.5**, published
  2026-11-08T00:19Z. The fix, pull request Mizerd/lightning#77, was merged 2026-11-13T16:10Z as
  `76f334f8`; GitHub's compare of `v0.10.5...76f334f8` says `ahead 78, behind 0`, so the fix
  is not in v0.10.5. Upstream `main` (`90f279899`, 2026-11-15T23:08Z) contains it and is 82
  commits further on.
- So the release that carries it is the next one cut from `main` after v0.10.5 (v0.10.6 or
  v0.11.0, whichever upstream names it), then its Flathub update pull request. The side-panel
  and thread-list issues (#76, #79) and the selection issue (#78) were closed as fixed after
  v0.10.5 too (2026-11-13 and 2026-11-14), so they wait for the same release.

Task 7.1 stays held on `lightning-default-client`; Task 2.5 still waits on that release.
Re-check: the same two reads (Flathub's releases[0], and a compare of the newest upstream tag
with `76f334f8`).
