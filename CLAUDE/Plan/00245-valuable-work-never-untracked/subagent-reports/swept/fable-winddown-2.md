# Fable re-review: wind-down branch 575f3905 (diff from d18a08ce)

Verdict: MERGE.

## Probes

- Quoting/injection: `Sudo.Capture` passes an argv slice to `execx.Runner` (no shell anywhere).
  Container and user are single argv items after `-n` / `-u`; typed text reaches tmux as
  `send-keys -t =<pane> -l <text>` (panes.go:67), literal. The tmux args after `env -i PATH=… tmux` are all code-owned; `containerTmux.Run` refuses any program but tmux. A hostile record
  value (lxcrecord has no shape validation of container/user) can at worst aim at a different
  container/user of the same host, which is the owner's own data (AppliedDir is written by the
  play from localhost.yml). No execution path.
- sudo -n: `Sudo.Ready` is asked once, only when a reachable LXC member exists; failure marks all
  reachable rows "not reached" (exit 1), nothing typed. Capture adds `-n`; a password demand is
  an error. Fail-fast holds; test TestAnLxcMemberWithoutSudoIsNotReached covers it.
- Socket: render_member_unit sets only HOME and PATH (no TMUX_TMPDIR, no PrivateTmp), User=<user>,
  so the server socket is /tmp/tmux-<uid>/agent-bus-claude. `runuser -u <user>` + `env -i PATH=…`
  also has no TMUX_TMPDIR, same uid, so `tmux -L agent-bus-claude` resolves to the same socket.
  Client commands (capture-pane, send-keys, list-sessions) need neither HOME nor TERM.
- Tests: winddown tests build the real `lxcfreeze.Lister` over `execx.Fake` keyed on
  `sudo -n lxc-attach …`, assert the full prefix argv including `-P /var/lib/lxc -n box --clear-env -- runuser -u agent -- env -i`, and parse `-L agent-bus-claude` + the tmux verb
  from the real argv. attach_test asserts the exact argv. go test -race green for winddown and
  lxcfreeze; vet clean.
- Grace: reenter() returns nil on the first still-typed poll, then one Enter; typeLine resets
  graced/reentered. TestALineSlowToClearIsNotEnteredTwice.

## QA claim

`./scripts/qa-changed.bash --base F44` reproduces: only `go` fails, at go-qa `build`:
`internal/freeze/golden: no non-test Go files`. Cause is in the pinned go-qa-ci
(v0.0.0-20261015133126): diff mode selects changed packages plus importers (golden imports
lxcfreeze) and names them to `go build -o /dev/null <pkgs>`; a named test-only package is a
go build error, while the full run's `./...` pattern silently skips it. Proof on F44 main
checkout: `go build ./internal/freeze/golden` exit 1, `go build ./internal/freeze/...` exit 0.
golden/ predates this branch on F44. So any F44 change to lxcfreeze or podfreeze trips it; not
this branch's defect. Fix belongs upstream (filter packages with no GoFiles before build) or in
go-qa.toml if it has an exclude.
