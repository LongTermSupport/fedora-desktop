## 13:21 · action · —   — Task 4.9 red: the store-clean gate's tests fail

Task 4.9, red. Five new tests in src-go/internal/containerimages fail before the code exists:

- TestYesRemovesNothingWhileTheStoreIsNotConfirmedClean: prune --yes with no
  container-images.store-clean marker must exit 1, remove nothing, log nothing, write no
  success stamp, and say the one line on stderr. It removed the backlog and exited 0.
- TestADryRunNotesAStoreNotConfirmedClean: the dry run lists as before and notes the missing
  marker once on stderr. No note was printed.
- TestAMarkerThatCannotBeReadStopsTheDryRun: a marker path that cannot be stat'ed (ENOTDIR)
  stops the run before any podman call. The dry run went on.
- TestNothingIsReplacedWhileTheStoreIsNotConfirmedClean: replaced with no marker removes
  nothing, exits 0, says the one line. It removed the old :latest.
- TestAMarkerThatCannotBeReadStopsTheSelfPruneBeforeARemoval: as above for replaced.

Every world and shelf now starts with the marker written, so the existing tests keep their
meaning; the two tests that made ~/.local or ~/.local/state a file were adjusted (the state
dir test clears the tree first; the replaced log test makes the log path a directory, as the
prune one does).
