# The sign-in gap after a failed apply: fixed

Follows `261015-room-access-diagnosis-opus.md` ("Gap noted, not fixed"). Placeholders only:
team `team-a`, person `<name>`.

## Defect

`ccy-teams create` offered the sign-in (a password) and Open in Element only after its apply
passed. The play creates each person's account with a random password nobody is shown, and
it runs before the apply's read-back, so an apply that failed left a person with an account
they could not sign in to. The recovery the failure message gives, `ccy-teams apply <team>`,
never offered a sign-in either.

## Change

- `login.SignInGap(ctx, teams, ask)`: collects the board (the picker's own read), and for each
  person of the teams whose Element sessions read `never signed in`, offers the sign-in when
  `ask` (a gum question, then the password shown once) or says it and the exact way when not:
  `ccy-teams`, Enter on the team, choose `Open <team> in Element`.
- `ops.Apply` (`ccy-teams apply <team>|--all`) calls it after every apply, passed or failed.
  `ask` is true only on a terminal. The apply's own result stands (a failed apply still
  exits 1); only Ctrl-C at the question turns it into the cancelled exit.
- `create` calls it when its apply fails (`ask` is `!--yes`); the success path keeps its own
  offer, so a passed create does not ask twice.

## Can "no sessions" be read when the apply failed?

Sometimes. It is read the same way the picker reads it: `agent-bus list` and
`agent-bus human devices` as root. After a play that failed before the homeserver was up, or
with sudo not ready, those fail. The code does not skip that case: it says which read failed
("Who is signed in to team-a could not be read (...)" or "Whether <name> has signed in to
team-a could not be read (...)") and prints the same exact way, so the person is never left
without the command.

## Evidence

- Tests first: `login/signin_gap_test.go` (offer, declined, no terminal, unreadable session,
  unreadable roster), `ops_test.go` (called after a passed and a failed apply, `ask` follows
  the terminal, Ctrl-C is the cancelled exit), `create/wizard_test.go` (a failed install,
  with and without a terminal, still exits 1 and looks at the sign-in; a passed install does
  not look twice). They were red (undefined symbols) before the code.
- `scripts/test-ccy-teams-command.bash` pins the new text for a failed create and a failed
  apply (two lines added); the suite passes (338 checks).
- The apply's failure message is unchanged; the sign-in text is printed before it.
