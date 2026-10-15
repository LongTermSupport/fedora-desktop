# Final release security review: the teams release as it lands together (Fable)

Scope: `git diff a6d26d2c..447be9f8` on F44, read whole, with the code around each change
(`helpers/pingbus/envelope.py`, `cli.py`, `protocol.py`, `helpers/agent_bus/admin.py`,
`checkout.py`, `render.py`, the units, `agent-bus-install`, `play-agent-bus.yml`,
`meta-deploy.list`, the Go `sudo`, `teams`, `login`, `chooser` packages, the SKILL and
`inject_frame`). Read-only; no QA run. The seams between the separately reviewed parts were
the target.

Verdict: **SHIP**. 0 blocker, 1 should-fix, 5 notes.

## What holds

- **Who sent it, by what route, on which team.** Every header line of the envelope is
  pingbus's, from the team record and the re-fetched event; the body is one JSON string on
  one `body:` line and can start no line (no raw newline, U+2028/2029, Cf or Cc is ever
  shown: `envelope._shown`). `from:` is the account, never a display name, so
  `enable_set_displayname = false` plus the documented per-room-name limit changes nothing
  on the agent side. A reply (`m.relates_to`) is accepted only as exactly
  `{"m.in_reply_to": {"event_id": <id>}}` (`protocol._check_text_reply`), is not rendered
  into the envelope, and so carries no authority a body could not already claim.
- **Body cannot escape.** `_render` shows ASCII printable but `"` and `\`, and non-ASCII
  only from the allow-listed categories and blocks, after an NFKC fold that refuses anything
  folding to `"\<>`. The cap is monotone (a longer prefix never renders shorter: the only
  look-ahead cases, ZWJ and keycap FE0F, turn a 6-byte escape into 3 bytes plus a following
  character of at least 3), so the binary search in `cap_body` is sound. `fit` counts wire
  bytes (`wire_bytes`, the ASCII JSON the socket carries), so the 12 KiB delivery cap is
  measured where it matters.
- **`--re` is shell-safe.** The event ID grammar is `\$[A-Za-z0-9_-]{43}`
  (`protocol.EVENT_ID_PATTERN`), validated by `_event_id` before it is single-quoted on the
  `REPLY` line, and again by `cmd_say` before use.
- **Sudo.** `seat take` and `seat remove` end in `sudo -k` in a `finally`
  (`checkout.take/remove`); `ccy-teams` runs `Forget` after every run that called `Ready`,
  under a `signal.NotifyContext` so Ctrl-C still reaches it. Both launchers (`ccy --teams`,
  `cc --teams`) reach root only through `seat take`.
- **Homeserver isolation.** `TemporaryFileSystem=/var/lib/agent-bus:ro` +
  `BindPaths=/var/lib/agent-bus/%i` + `PrivatePIDs=yes`; Tuwunel's backups go to
  `<team>/backups` (`render.toml_values`), inside the bind, and `backup-now` signals through
  `systemctl kill`, so the PID namespace does not break SIGUSR2; it waits for the numbered
  backup and dies when none appears.
- **Deploy order.** `meta-deploy.list` runs `play-claude-yolo.yml`, then `play-agent-bus.yml`,
  whose backup step precedes `software` and `team`, is gated on the pin or a unit differing
  from the installed copy (true in this release: the pin moved to 1.9.4), and fails the run
  on a failed backup (`die` in `cmd_backup_now`). The 3.98.10 launcher needs nothing new from
  the host `agent-bus` CLI, so the window between the two plays is safe.
- **Old-image sessions.** The old `EDIT_KEYS` check refuses any `m.relates_to`, so an old
  session drops a new session's reply as an edit and nothing else: it still renders the new
  pingbus's envelopes (same grammar), read receipts are not room events, and the homeserver
  restart is a reconnect. Matches the documented "drops replies as edits".

## Should-fix

### S1. `wl-copy --paste-once` is consumed by the Clipboard Indicator this repo installs

- `src-go/internal/teams/login/login.go:425-427` (`copyToClipboard`), `:413-419` (the
  consent text), `:426` (`Tell`: "on the clipboard for one paste").
- `vars/gnome-shell-extensions.yml:41-44` installs and enables the Clipboard Indicator
  GNOME Shell extension by default.

Failure scenario: the person presses Ctrl-P, answers "Yes, copy the password". The
extension reads every new clipboard selection as its owner changes; that read is the one
paste `--paste-once` serves, so `wl-copy` exits at once. The password is now in the
extension's history (persisted on disk under the user's cache directory, and shown in the
top-bar menu), and the person's paste into Element finds an empty clipboard, after being
told it would serve one paste. Both halves of L4's intent are inverted on the project's own
desktop: the password is kept longer than before (the old timer at least cleared the
selection; nothing clears the extension's history) and the human has to type it from the
screen. Not a route an agent can use, which is why this is not a blocker.

Fix: on a desktop with the extension enabled, either skip the clipboard offer and say why,
or have the play configure the extension so it does not record the selection (its settings
expose a history toggle), and document the interaction in `docs/agent-bus.md` Limits.
`scripts/test-ccy-teams-command.bash` cannot see this (a fake `wl-copy`), so the owner's
acceptance should include one real Ctrl-P paste.

## Notes

### N1. The read receipt is sent before the delivery is known to have reached the model

`helpers/pingbus/cli.py:1585` sends the `m.read` receipt as soon as the socket write
completed, including when `closed` is false ("counted as sent"), and before the E16
transcript check. A delivery later reported `missing` (`cli.py:1608`) has already put the
agent's avatar under the human's message. The human reads "received", gets no answer, and
the UX fix's promise ("none under a message that reached no agent") does not hold for the
one case it matters. Consider sending the receipt from the point the E16 check finds the
nonce in the transcript, or at least not when `closed` is false.

### N2. The failed-login limit is a sign-in lock anyone on the bus can apply to a human

`helpers/agent_bus/render.py` `LOGIN_LIMITS["failed"]`: three wrong passwords, then one per
five minutes, keyed on the account. Every agent holds the team record, so it knows every
human's localpart, and any process on the bus network reaches `/login`. Three wrong attempts
every five minutes keep a human from signing in on a new device indefinitely (existing
sessions stay). Synapse has the same shape; it is the right trade. The Limits bullet names
only the limit, not that it is a lock a peer can hold; one clause would do.

### N3. The "compare the text" check compares two renderings

`SKILL.md` and `hooks.py` `inject_frame` tell the agent to confirm that the `history`
line's text is the request it was given. `cli.human_line` renders the text
`json.dumps(..., ensure_ascii=True)` (`café`), while the envelope now shows `café`.
An agent comparing by eye sees a difference on every non-ASCII request; one decoding JSON
sees none. Say "JSON-decode the line's text" in the skill, or render the history text
through `envelope._render`.

### N4. Real angle brackets are shown; their look-alikes are escaped

`envelope._render` shows every printable ASCII character but `"` and `\`, so a body may
hold a literal `<<end agent-bus 00000000 1/1>>` on the `body:` line, while
`_STRUCTURAL` escapes non-ASCII look-alikes of `<` and `>`. Containment does not depend on
either: the body cannot leave its line and the marker nonce is drawn after the bodies
(32 bits). No code change wanted; SPEC-inject E2 ("a look-alike bracket arrives escaped")
reads as if brackets were a defence, and could say the nonce is.

### N5. The pre-swap backup is gated on the binary and units, not on a config restart

`playbooks/imports/play-agent-bus.yml` `agent_bus_swap_paths` holds the pin and the two
units. A release that changes only `render.toml_values` (as this one also does:
`max_request_size`, `enable_set_displayname`, `rate_limiting`) restarts every homeserver
through `team` with no backup unless the pin or a unit moved too. Covered this time by the
1.9.4 pin; a future config-only release restarts without a restore point. Adding the
zipapp (or the installer's config digest) to the gate, or backing up whenever `team` will
report a config change, closes it.

## Checked and not a defect

- `_stamped_teams` on a 0750 directory the caller is not in the group of: `Path.glob`
  returns nothing rather than raising, so the only effect is a shorter "teams here" list.
- `agent_bus_swap`: both result lists derive from the same ordered loop and are filtered
  to the installed subset, so a host with nothing installed compares `[] == []` and runs
  no backup.
- `_admin_api`: the register-then-store crash window still recovers through the stored
  password; `M_USER_IN_USE` with no stored password fails naming `restore`, never loops.
- `_empty_stray_status`: `GET /rooms/<id>/state` returns a list; `http_transport` now
  admits lists and `_Api.call` still demands a dict everywhere else.
- `cc-teams-settings.json` deny rules and the consent say plainly they are guidance; the
  Limits bullet matches.
- Cross-team: `with_coordinator_copy` and every `say` are team-scoped; an `--re` naming
  another room's event is a reply Element cannot resolve, nothing more.
