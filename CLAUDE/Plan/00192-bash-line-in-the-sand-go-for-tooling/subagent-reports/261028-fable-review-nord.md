# Fable review: the nord port (branch `go-port/nord` against `origin/F44`)

Reviewed: the deleted bash (`origin/F44:files/home/.local/bin/nord`), `src-go/cmd/nord`,
`src-go/internal/nord` (sources and tests), `SPEC-nord.md`, `PORTS.md`, `PLAN.md`, the three
journal entries, `play-nordvpn-openvpn.yml`, `tasks/build-go-command.yml`, the docs, the
allowlist/tsv/test-suite edits, `qa/rules/table.go`, `qa/deployeddrift/review_test.go`, and the
`execx` runner the port depends on (`Run`, `Feed`, `Secret`, `runSession`). Ran `go vet`,
`golangci-lint` (0 issues) and `go test -race` over `internal/nord`, `cmd/nord`,
`qa/deployeddrift`, `qa/rules`: all green. Nothing in the branch was changed.

Verdict: no blocker. Two things to fix before merge, both small; the rest are follow-ups or
confirmations.

## BLOCKER

None.

## FIX-BEFORE-MERGE

### F1. A password with a `,` (or edge whitespace) is stored wrong and nmcli still says "successfully updated"

- `src-go/internal/nord/connect.go:217-233` (`storePassword`),
  `src-go/internal/nord/checks.go:78-108` (`readCredentials`).
- Scenario: the feed is `set vpn.secrets password=<value>`. `vpn.secrets` is an nmcli
  option-list property: nmcli splits the value at `,` into `key=value` pairs and trims the
  whitespace around each. A password holding a comma is stored as its first half plus a junk
  key; one with a leading or trailing space is stored trimmed. nmcli exits 0 and prints
  `successfully updated`, so the `savedMark` check (the only fail-safe for the store) passes,
  the import is kept, and `nmcli connection up` fails later with an auth error pointing at the
  wrong cause. The bash had the same defect through `+vpn.secrets "password=$password"`, so
  this is kept, not introduced; but the spec (E10 "nothing else is trimmed", E13 "the session
  succeeds only when ... nmcli says it saved") promises a correctness the store cannot give.
  NordVPN service credentials are generated alphanumerics, so the likelihood is low; the cost
  of the check is one line.
- Fix: in `readCredentials`, refuse a password (and a username, which goes through
  `+vpn.data` and is split the same way) containing `,` or with leading/trailing whitespace,
  with the E10 message style (`the password (line 2) contains a comma, which nmcli cannot store`), and add the two cases to `TestBadCredentials`. Note it in E10.

### F2. E24/B16 claim a wait that does not happen at a real terminal; a Ctrl-C during the import can leave the profile B9 promises to remove

- `src-go/internal/nord/app.go:116-121` (`changing` = `context.WithoutCancel`),
  `src-go/internal/execx/execx.go:108-133` (the non-session `Run`: no `Setsid`, the child is
  in nord's foreground process group), `SPEC-nord.md` E24 and B16,
  `src-go/internal/nord/cancel_test.go` (`TestCtrlCDuringImport`).
- Scenario: `WithoutCancel` stops nord signalling nmcli, but the terminal driver delivers
  SIGINT to the whole foreground group, so `nmcli connection import|modify|edit|up|down|delete`
  receives the SIGINT directly (execx.go:110-112 says exactly this). nmcli exits on it. So at a
  terminal the real sequence is: nmcli dies mid-call, `nmChange` returns an error, nord prints
  `could not import <name>: nmcli killed by SIGINT` (or similar), then `Cancelled.`, 130. For
  the import, NetworkManager's `AddConnection` may already have landed when nmcli died: the
  profile exists, nord never saw its UUID, and `removeHalfImport` is never reached, so a
  profile named after the file is left (the very thing B9 says cannot happen; the next
  `connect` imports a second one, which E12 then refuses until `cleanup`). The fake cannot
  see this: `w.interrupt()` cancels a context, it does not deliver a terminal SIGINT, so
  `TestCtrlCDuringImport`'s "commands abandoned mid-run: none" proves only the context
  arithmetic.
- Fix (either): (a) run the changing nmcli commands with `NewSession: true` in `nmChange`
  (`runSession` starts the child as a session leader, outside the terminal's foreground group,
  and its stdin is a pipe the run writes and closes, so the `Feed` still works; the group kill
  on cancel never fires because the context is `WithoutCancel`), and add a sentence to E24
  saying why; or (b) if the owner prefers nmcli to die on Ctrl-C as the bash's did, rewrite
  E24/B16 and the `changing` comment to say what happens, and in `importConfig` treat a
  signalled import as "unknown outcome" (say so, and point at `nord cleanup`). (a) is what the
  spec already promises and is about three lines.

## FOLLOW-UP

### U1. `connect`/`switch` now need the credentials file even when the profile is already imported

- `src-go/internal/nord/checks.go:38-60` (`check` with `needConnect` always reads the file),
  `SPEC-nord.md` E8.
- The bash read the credentials only inside `import_config`. Now a person whose profile is
  imported and whose `.credentials` was removed (or a consumer whose play never wrote it)
  cannot bring that profile up, and every connect reads the password into memory for nothing.
  Deliberate per E8, and cheap to keep; but the password should be read when it is about to be
  used (`importConfig`), not in a precondition sweep. Low priority.

### U2. The chooser opens the log before it knows it has a terminal

- `src-go/internal/nord/app.go:140-145` then `chooser.go:28-31`.
- `nord </dev/null` (a script, a cron) creates `~/.local/share/nordvpn/nord.log` and then
  refuses. Harmless; swap the two checks.

### U3. `nord cleanup` deletes every profile without a confirmation

- `src-go/internal/nord/commands.go:143-180`; InteractiveScripts.md rule 6.
- Unchanged from the bash, and `cleanup` is a non-interactive command, so this is not a
  regression; but it is a destructive command on a tool that otherwise follows the interactive
  rules, and the chooser offers no route to it, so a `[y/N]` (with `-y`) on a terminal would
  be consistent with `reclaim`.

### U4. The `nmcli` owner is a string literal where every other owner is a named constant

- `src-go/qa/rules/table.go:83` (`"internal/nord"`), against `lxcfreezePkg` etc. at lines
  35-48. Add `nordPkg = "internal/nord"`.

### U5. A late Ctrl-C turns a finished success into `Cancelled.` 130

- `src-go/internal/nord/app.go:87-95`: `Run` replaces the code when `ctx.Err() != nil`, even
  when `dispatch` had already returned `ExitOK` (the SIGINT arrived between `nmcli up`
  returning and the return). The VPN is up, nord says Cancelled. Edge case; could check the
  context only when the code is not already a result.

### U6. The host check H4 carries real unknowns, and the spec should name them

- `SPEC-nord.md` H4; `connect.go:217-233`.
- Two things only the host proves: that nmcli's editor accepts commands on a pipe (readline in
  non-tty mode; it does in every version I know, and EOF is `quit`, so no hang), and that
  `save persistent` asks nothing on an existing profile (the autoconnect confirmation is for
  new ones). Both failure modes are safe here: `savedMark` is not printed, the import is
  deleted, nmcli's words are shown with the password hidden (readline may echo the fed line
  to stdout in non-tty mode; `creds.hide` covers it). Worth listing the two in H4 so the
  owner knows what a failure there means.

## OK (checked, no finding)

- Password reach: never in an argv (`checkSecrets` refuses a Feed's value in argv, and
  `wantNoPassword` checks every recorded argv, both streams and the log in the tests that
  matter); `Cmd.Line()` is argv only, so `--debug` and the log never carry the stdin; the log
  is 0600 in a 0750 directory; the troubleshooting no longer says `cat <credentials>`; nmcli's
  stdout/stderr from the store are passed through `creds.hide` before any print. The
  `TestFailedPasswordStoreDeletesTheImport` "not saved" case plants the password in nmcli's
  output and proves it is hidden.
- The joined setting name: `connect.go:21-27` says plainly it is nmcli's setting, not a file,
  and why it is joined; `TestPasswordSettingIsTheVPNSecrets` pins the literal parts (not a
  tautology against the constants: it compares to `"vpn"`, `"secrets"`, `"password"`).
- nmcli on stdin: `Feed` becomes a `strings.Reader` stdin; Go's exec ignores EPIPE on the
  stdin copy when the child exits early, and nmcli's editor quits on EOF, so no hang; the
  `WaitDelay` bounds a stuck child.
- Fail-fast: every nmcli failure is surfaced with nmcli's words; a failed modify or store
  deletes the import and a failed delete is named; a down that fails stops the connect; a
  listing that fails stops the command; cleanup and disconnect try every profile and exit 1 if
  any failed; a log that cannot be opened stops a changing command before it acts; an
  unwritable stdout fails the run.
- Fakes exercise the path: the fake NetworkManager's listings come from its profiles and each
  verb changes them; `edit` stores only when fed exactly nord's three lines; an unscripted
  command fails the test; `wantChanges` asserts the full ordered changing argv for each flow.
- Interactive rules: a numbered menu with every action visible and `q) Cancel`; bad key
  re-asked, three tries then exit 1; Ctrl-D is 130 with a message; Ctrl-C at the menu is 130
  after `Cancelled.`; headings on both screens; colour guarded by `IsTerminal(stderr)` and
  `NO_COLOR`; no terminal is a one-line error pointing at the commands.
- Dropped bash behaviour: all deliberate and listed (B1-B16): the state file, the
  `switch`-disconnects-first order, the `sudo dnf` hints, the per-command dependency sweep,
  `journalctl -f`. Nothing else the bash did is missing; `list`, `list-active`, `status`,
  `cleanup` keep their outputs and exit codes.
- Play wiring: `import_playbook: ../../play-golang.yml` matches the sibling plays;
  `build-go-command.yml` builds offline from `vendor/` and installs with a checksum `copy`
  (idempotent) at the old path, 0755, owned by the login user; the stale
  `.current-connection` removal is `state: absent` (idempotent); the credentials task is
  untouched (0600). `go-binary-suites.tsv`, the allowlist row, `test-qa-changed.bash`'s
  stand-in tool (`lan-scan` is on the allowlist) and `deployeddrift`'s stand-in
  (`clean-paste`, exec) all hold.
- Docs/spec/plan: `docs/nordvpn-installation.md`, `docs/playbooks.md`, `src-go/CLAUDE.md`,
  `PORTS.md` and `PLAN.md` say the same thing as the code; the first journal entry describes
  the superseded `passwd-file` design, and the decision entry records the reversal, which is
  the append-only discipline working.
- Install-specific content: none in the diff (the test IP is TEST-NET `203.0.113.7`, the
  device `wlp1s0`, user `svcuser`).
