## 14:43 · action · —   — Tasks 3.13-3.15: member-up console tail, settable SSH wait, Ctrl-C keeps the member directory

Tasks 3.13, 3.14 and 3.15 (vmtest H4 follow-ups) done, tests first in `internal/vmtest`.

- 3.13: a member-up that does not finish shows the last 10 non-blank lines of
  `members/<name>/console.log` on stderr, indented, with carriage returns, terminal escapes
  and other control bytes dropped, before the "member not started" line and the `ERROR:` line
  (E57). No log (a failed virt-install) and an empty log are each said in one line. Red:
  `TestE57MemberUpShowsTheConsoleTail` failed (no tail in stderr); green after
  `showConsoleTail` in `member.go`, called from `memberTeardown`.
- 3.14: decided settable, not scaled with load. `VMTEST_SSH_WAIT_TIMEOUT` (default 600,
  1 to 86400 whole seconds, the same refusal as `VMTEST_MEMBER_SSH_TIMEOUT`, now one helper
  `secondsSetting`) is read at the lab load, so a bad value fails before anything boots; it
  bounds every wait for SSH and the desktop's login-prompt wait. The timeout message names the
  setting. Why: two readings (a timeout at load average about 10, then an answer after about
  192 s) give no relation between load and boot time, and the load average lags and counts
  I/O waits, so a load formula would be untestable guesswork; 600 s is three times the slow
  boot measured and keeps a dead guest's failure within ten minutes. Red:
  `TestE24SSHWaitIsSettable` failed (waited 600 s, not 900 s); green after the `cli.sshWait`
  field.
- 3.15: SPEC E57 and H4 say Ctrl-C or SIGTERM during member-up keeps `members/<name>/` with its
  console log until member-down; `TestE57MemberUpSignal` now pins the directory and the line.

Docs: `usage.txt` and `docs/vm-acceptance-testing.md` updated. `qa-changed.bash` passed.
