# Final release review fixes: adversarial pre-merge review (Fable)

Scope: `git diff F44...origin/fix/teams-final-review` (tip dbf0c793, one commit), read whole,
with the code around each change (`inject.DeliveryCheck`, the `watch` loop, the installer's
`team` step and `put_file`, `render.toml_values`, `teamfile.load_team_file`, the Go fake
runner, the bash harness). Read-only; no `qa-all`. The branch's own tests were run on a
detached worktree and removed afterwards: `test_config_restart`, `test_hooks`,
`test_plugin_contract` (146 OK), `test_notify` (45 OK), `go test ./internal/teams/login/`
(ok), `scripts/test-ccy-teams-command.bash` (353 passed, 0 failed).

Verdict: **MERGE**. 0 blocker, 3 notes.

## S1: no clipboard offer while Clipboard Indicator is on

- **Correct.** `login.go` `clipboardHistory`: no `gsettings` on PATH is "no GNOME Shell" and
  the offer stands; `gsettings get org.gnome.shell enabled-extensions` failing is
  `historyUnknown` and nothing is offered (fail safe); the UUID quoted
  (`'clipboard-indicator@tudmotu.com'`) in the output is `historyOn` and nothing is offered.
- **No injection.** The argv is three fixed literals, run through `execx` (no shell); the
  only input read is gsettings' stdout, used in a substring test. A hostile dconf value can
  at most hide the extension's UUID, which is the person's own setting, not an agent's route.
- **Honest text.** "the Clipboard Indicator extension is on" is read from
  `enabled-extensions`; with `disable-user-extensions` true the extension is listed but
  inactive, so the line over-refuses in that corner. Conservative, and the consent's "A
  clipboard manager may keep a copy" remains true. Docs (Limits, the sign-in step, the
  changelog) match the code.
- **Tests fail without the fix.** `execx.Fake` returns `ErrUnexpected` for an unscripted
  command, which is why the two existing Wayland tests had to gain
  `withoutClipboardHistory()`: without the gsettings read nothing would ask for it. The three
  new Go cases assert no `wl-copy`, no copy question, the exact line; the bash case runs the
  built binary against a fake `gsettings` that lists the UUID.

## N1: the read receipt waits for the transcript

- **Correct and race-free as far as this watcher goes.** `watch` is one thread: `_send_delivery`
  calls `check.sent(nonce, ...)` and stores `unread[nonce]` under the spool lock in the same
  pass; `check.poll()` runs at the top of the next `_inject_pass`, so a nonce can never be
  found before its candidates are stored. `pop(nonce, [])` covers deliveries with no HUMAN
  item. A delivery declared missing pops its entry, so `unread` cannot grow: with no
  transcript recorded, `poll` still declares every delivery missing after `quiet_s`, so "with
  no transcript recorded, no receipt is sent" is true and the map empties.
- **stderr timing.** The "counted as sent" line is still written at send time; the missing
  line gains "(no read receipt was sent for it)" only when humans were waiting. Both read
  truthfully.
- **Tests fail without the fix.** `test_a_delivered_human_message_gets_a_read_receipt`
  asserts no receipt before the transcript has the delivery (old code sent at once);
  `test_a_delivery_missing_from_the_transcript_gets_no_read_receipt` asserts none after a
  missing mark (old code had sent one).

## N5: the config-render gate on the pre-swap backup

- **No false positive on a steady state.** The installer writes `team.json` from
  `render check` (`dump_team_file`) and `tuwunel.toml` from `render toml` of that same file,
  so `render_toml(load_team_file(installed team.json)) == installed tuwunel.toml` whenever the
  checkout's `render.py` is the one that installed it: the gate is idempotent after `team`.
- **No false negative for N5's case.** Any change to `toml_values` (the release's
  `max_request_size`, `enable_set_displayname`, `rate_limiting`) renders the installed
  team.json differently and prints `CONFIG-CHANGED`; `agent_bus_swap` ORs it in with the
  same filter the installer-side markers use.
- **Fail-fast.** A non-team name or an unreadable/invalid team.json is one stderr line and
  exit 1, which fails the play before `software`. `check_mode: false` + `changed_when: false`
  is right for a read-only probe (the play runs `become: true`, so the 0640 root:agent-bus
  files are readable). The `python3 -m helpers...` with `chdir: root_dir` pattern has
  precedent in this play (`helpers.agent_bus_lxc.members`) and others.
- **Tests.** Six cases, including the path-traversal name and the installed-without-config
  case; `test_a_team_whose_config_this_checkout_renders_differently_is_named` is red without
  the module.

## Versions

Consistent everywhere they are named: `CCY_VERSION="3.98.11"`,
`REQUIRED_CONTAINER_VERSION="2.66"`, Dockerfile `LABEL claude-yolo-version="2.66"`,
`plugin.json` 1.2.3, `docs/ccy-changelog.md` "3.98.11 — container 2.66", PLAN.md Task 2.12,
ACCEPTANCE-owner.md (3.98.11 / 2.66), meta-deploy.list. No stray 3.98.10 / 2.65 / 1.2.2
outside history (journal, earlier tasks, the earlier queue entries).

## meta-deploy.list

The two appended entries name the right plays in the right order (`play-claude-yolo.yml`,
then `play-agent-bus.yml`) and their descriptions match what the branch changes. No error
found; the coordinator's rewrite stands.

## Public-repo hygiene

No install-specific names in the diff: the only identifiers are public extension UUIDs
(`clipboard-indicator@tudmotu.com`, `blur-my-shell@aunetx`), the upstream repository name,
and the fixture team names.

## Notes (not blocking)

1. **N5 covers a render change, not a team-file change.** `config_restart` renders the
   *installed* team.json; the installer's `team` renders the *new* one from host_vars. A
   change to a team's own entry that alters `toml_values` (e.g. `listen`, `port`) still
   restarts that homeserver without a backup. Pre-existing and outside N5's wording; one
   sentence in the play comment or Limits would keep the promise exact.
2. **Only one clipboard manager is checked.** GPaste, Pano and the like would consume
   `--paste-once` in the same way; the consent line covers them generically. Fine for a
   repo that installs exactly this one.
3. **Receipts owed at watcher exit are dropped.** A delivery found in the transcript after
   `watch` has exited (SessionEnd right after a delivery) gets no receipt. The documented
   promise ("none under a message that reached no model") still holds; the converse does
   not in that corner, and nothing says so.
