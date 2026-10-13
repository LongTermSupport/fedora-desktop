# ccy-teams Go port: integration and parity (Sonnet)

## Wired

- `create.go`: `App.create` (needCheckout, then the wizard) and `App.newCreator` (the one creator constructor);
  `newPassword` is the picker's `issuePassword` (the wizard has already asked, so no second question), `launchElement`
  is the picker's, `applyTeams` goes through `App.Ops`.
- `apply.go`: `App.newTeamOps` (the one teamOps constructor); `remove.go` and `backup.go` delegate to it.
- `ops.go`: `liveOps` is the default `App.Ops`; the stubs, `errOpNotBuilt` and `App.notBuilt` are gone.
- `App.freshTerm` is the one way a Term is made (wire, creator and teamOps each get their own for the step counters).
- goconst: `flagTeams` and `flagAll` constants.

## Defect fixed

Human rows now carry the sign-in count in SESSION, APPLIED is `-` (spec 2.5). Unit tests updated.

## Parity

Go mode 323/323, bash mode 324/324 (the extra case is the template render).

## Mode-conditional suite changes

Only one, spec section 8 bug 11 (sudo_ready, then plain sudo): a `SUDO_INSTALL` variable is `sudo -n /usr/local/sbin/agent-bus-install` in Go mode and `sudo /usr/local/sbin/agent-bus-install` in bash mode, used by the four
assertions: backup-now alpha, restore alpha 2, backup-now beta (remove), remove zeta.

No other assertion was changed.
