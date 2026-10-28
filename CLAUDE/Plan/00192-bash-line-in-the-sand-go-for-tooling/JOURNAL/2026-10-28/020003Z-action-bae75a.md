## 02:00 · action · —   — nord: review findings fixed

nord: the Fable review (subagent-reports/261028-fable-review-nord.md, no BLOCKER) fixed, tests
first. Red: 7 tests failed before the code (TestBadCredentials's new nmcli-unsafe cases,
TestChangingCommandsRunInTheirOwnSession, TestLateSignalKeepsTheSuccess,
TestChooserNeedsATerminal's no-log check, TestCleanup, TestCleanupFailure,
TestCleanupConfirmation). Green: all pass under -race, golangci-lint 0 issues, coverage 91.2%.

F1: a username or password that starts or ends with a space, or holds ',', '=' or '\', is
refused at E10 (B17): nmcli's option lists split, trim and unescape them. A re-prompt is not
possible there: the value is the play's, from the vault, so the message names the play. F2:
nmChange sets NewSession, so the changing nmcli commands are outside the terminal's
foreground group and a Ctrl-C cannot kill a half import before its rollback (E24, B16).
U2, U3 (cleanup asks [y/N], --yes skips, no terminal without --yes is refused; E20, B18), U4
(nordPkg), U5 (a late signal keeps a success) and U6 (H4 names its unknowns) fixed; U1 left
by design (E8), as the reviewer allowed.
