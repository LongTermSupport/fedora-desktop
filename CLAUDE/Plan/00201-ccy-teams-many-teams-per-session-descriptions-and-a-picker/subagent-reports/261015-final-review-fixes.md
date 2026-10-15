# Final release security review: the fixes (Opus)

Branch `fix/teams-final-review` from F44 (cfb93c38). Review:
`261015-final-release-security-fable.md`. Each fix test first; red seen before green.

## S1: the clipboard password copy

Facts, from the upstream source (`Tudmotu/gnome-shell-extension-clipboard-indicator`,
`extension.js` on master): `_refreshIndicator` runs on every owner change of the CLIPBOARD
selection and reads it through `#getClipboardContent`, which asks for a fixed list of text
and image MIME types. Nothing reads the offered MIME types for a hint: no
`x-kde-passwordManagerHint`, no other. The only ways it skips a selection are its Private
mode (a menu toggle, module state, not a setting) and `excluded-apps`, matched on the focused
window's WM class. Both are its global settings, which the brief rules out. `wl-copy` offers
one MIME type per invocation (`--type`), so it could not add a hint beside `text/plain`
anyway. So the hint route is not available.

Fix (`src-go/internal/teams/login/login.go`): before the copy question, `ccy-teams` reads
`gsettings get org.gnome.shell enabled-extensions`.

- **Clipboard Indicator listed:** no question. One line: the password is not offered for the
  clipboard, because the extension would use up the one paste and keep the password in its
  history; type it from above.
- **The read fails:** one error line, and nothing is offered.
- **No gsettings (no GNOME Shell):** offered as before.

The consent text ("A clipboard manager may keep a copy") and the `Tell` lines are true in
every branch. Tests: three new Go cases in `password_test.go`, plus one case in
`scripts/test-ccy-teams-command.bash` with a fake `gsettings`. Documented in `docs/agent-bus.md`
Limits (and in the step that describes signing in). The owner's check 11 in
ACCEPTANCE-owner.md does one real Ctrl-P with the extension on (no offer), and one with it
switched off in the Extensions app (a real paste into Element, and a second paste is empty).

## N1: the read receipt

`helpers/pingbus/cli.py`: a delivery's HUMAN candidates wait in `_Injector.unread` keyed by
nonce. The pass that finds the nonce in the transcript (`DeliveryCheck.poll` found) sends
their receipts. A delivery declared missing drops them, and its stderr line says no read
receipt was sent. With no transcript recorded, no receipt is sent. Tests in
`test_notify.py`:

- no receipt before the transcript holds the delivery;
- a receipt after it does;
- none for a missing delivery;
- the refused-receipt case, which now writes the deliveries into a transcript.

SPEC-inject E16 is amended, and so are `docs/agent-bus-protocol.md` and `docs/agent-bus.md`.

## N2

The Limits bullet on the login limit says any bus member can lock a human out of signing in
on a new device this way, and that existing sessions stay.

## N3

The skill and `inject_frame` now say to JSON-decode the history line's text before comparing
it, and to decode the envelope's `body:` the same way. The contract tests in
`test_plugin_contract.py` and `test_hooks.py` were changed first. Plugin 1.2.3. The image
carries the skill and pingbus, so CCY 3.98.11 and container 2.66 (Dockerfile label and
`REQUIRED_CONTAINER_VERSION`), with an entry in `docs/ccy-changelog.md`.

## N4

SPEC-inject E2 now says containment is the body's single line plus the nonce (E4). Real `<`
and `>` are shown, and escaping the look-alikes is tidiness, not the defence.

## N5

The new `helpers/agent_bus/config_restart.py` (tested in `tests/.../test_config_restart.py`)
renders each installed team's `team.json` with this checkout's `render_toml` and compares the
result with the installed `tuwunel.toml`. It prints `CONFIG-CHANGED <team>` for each team
that differs.

- **Skipped:** a team that is not installed.
- **Named:** an installed team with no config.
- **Exit 1:** a bad name or a bad team file.

`play-agent-bus.yml` runs it before `software` (read-only, `check_mode: false`, `changed_when: false`) and ORs its result into `agent_bus_swap`, so a config-only release backs up every
running team first. The backup stays fail-fast, and the gate is idempotent: once `team` has
written the new config, the next run finds no difference.

## Host run

Two entries were appended to `meta-deploy.list` after the four already queued:
`play-claude-yolo.yml` (ccy 3.98.11, ccy-teams) and `play-agent-bus.yml` (the new gate, and
the kit's pingbus).
