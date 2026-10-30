## 09:59 · decision · T6.5   — the owner's answers: no consumer post-processing; optional plays go on; Decisions 4-9 stand

The owner answered the unblocking list of 2026-10-28 with "all recommended", typed directly in
the coordinator's session.

- Item A5 (Task 6.5): no Claude post-processing on consumer installs, and those menu items are
  hidden there; revisit if a real consumer asks. The post-processing needs ccy's token pool,
  which a consumer install does not have. Task 6.5 is now the build task (unticked): hide the
  items on a consumer box, tests first, and the consumer VM scenario checks plain dictation and
  the items' absence.
- Part C item 5 (the coordinator's decision, not overruled), Task 6.6: on the developer path,
  optional plays go on after one fails, with a summary at the end and a non-zero exit; main
  plays, `--rerun`'s main plays included, still stop at the first failure. Task 6.6 is now the
  build task (unticked).
- Part C item 5: Decisions 4-9 stand on their defaults (no account prompt; re-run `run.bash`
  with an "Update fedora-desktop" launcher; consumer to developer allowed, the reverse refused;
  no backup of a consumer's `localhost.yml`; no passwordless sudo on a consumer box; kickstart
  later).
