# Wave 3c: `ccy-teams create` wizard (Sonnet)

Files: `src-go/internal/teams/wizard.go` (+ `wizard_test.go`), `src-go/internal/ui/gumx.go` (+ `gumx_test.go`).
Entry point: `func (c *creator) run(ctx, args []string) int`. `need_checkout` is not inside it:
the coordinator calls `App.needCheckout()` before `run`.

## creator fields (all set explicitly by the coordinator)

declare `*declare.Client` (its `Err` receives the backup note), gum `*ui.Gum` (Runner, Stdin, Stderr),
git `*gitx.Git`, term `*term.Term` (fresh per run: Steps/step counters live there), runner
`execx.Runner` (gh, id), stdin `io.Reader` (terminal test), stdout `io.Writer` (the CHANGED line),
cwd, hostVars, home, launcher, `lookPath func(string)(string,error)`,
`applyTeams func(ctx, ...string) error`, `newPassword func(ctx, team, human) error`,
`launchElement func(ctx, team) error`, `execLauncher func(dir string, argv []string) error`
(chdir + syscall.Exec; returns only on failure). No `sudo` field: create never calls sudo itself
(apply does, behind `applyTeams`).

## ui additions (new file, ui.go untouched)

`Gum.Spin(ctx, title, argv...)`, `Gum.Box(ctx, bold, lines...)` (gum style --border rounded --padding "0 1"),
`Gum.Explain(ctx, text, indent)` (tput cols, \<44 or unreadable = 80, width = cols-4).

## Deliberate behaviour changes (spec section 8)

- 14: `--org` given as a flag is looked up on GitHub (declare check, then `gh api users/<o> --silent`) in
  both modes; interactive with `--org` now also needs gh logged in (it did not before). `--yes` uses plain
  gh (no gum needed), interactive uses `gum spin`. Failure: the reason, then
  `ERROR: create stopped: fix the organisation above (or drop it); nothing was written.` exit 1. The suite
  only uses `--org example-org --yes` and its gh fake answers `api users/example-org`, so no suite change.
- 15: interactive create prints the next steps without the `Sign in` and `Element` lines (it has just
  offered both); `--yes` prints all. The `Agents ... ccy --teams <team>` lines stay, so [871] and [975] hold.
  Filtering is by line prefix on declare.py's text (` Sign in`, ` Element`).
- 16: `split_commas` no longer deletes inner spaces. Parts are trimmed at the edges only; `a b` goes to
  declare check and is refused (verified: declare.py refuses inner spaces for human, repo, allow-from).
- Offers after create (`newPassword`, `launchElement`): a failure prints
  `That did not finish (see above). The team is made either way.` (bash was silent via `|| true`); a
  `ui.ErrCancelled` from them cancels the run (130), as bash `cancelled` did via `exit`.
- A failed `teams-for` in offer_join prints declare's message and skips the offer (bash was silent).
- Exit codes of declare.py (64/78) pass through as in bash (suite [1029] asserts 78); its `declare: ...` line is printed.
- Not changed: interactive `--repo` flags are still not checked on GitHub (only `--yes` checks them).

## Section 5 "create --yes" / "no teams / wizard" assertions and their unit tests

All in `src-go/internal/teams/wizard_test.go` (names abbreviated, `Test` prefix omitted).

create --yes:

- stdout `CHANGED\tteam beta, port 6169` \[869\]: YesWithEveryValueCreatesInstallsAndPrintsTheNextSteps
- play argv `["beta"]` \[870\]: same (applyTeams called with `beta`; argv shape belongs to apply)
- `ccy --teams beta` \[871\]: same, plus NextStepsKeepEveryLineWhenNothingWasOffered
- no `localhost.yml` on stderr \[873\]: AFailedInstallSaysHowToRetry (uses `~/...` form), YesWithNoBusAddressPassesDeclaresRefusalThrough (mustLack)
- order play then element-profiles \[876\]: YesWith... (create < element < next-steps) and wizard order test
  (element declared before install); the Element *play* order is apply's
- repo errors `example-org/readonly: read-only`, `other/missing: not found`, `bad: not OWNER/REPO`, not
  `example-org/proj:` \[895-898\]: YesChecksEveryRepositoryAndNamesTheBadOnes
- `ccy-teams apply beta` on failures \[906, 915\]: AFailedInstallSaysHowToRetry; Element declare failure:
  AnElementEntryThatCannotBeDeclaredFailsBeforeTheInstall
- exit 78 with no bus address, `ccy-teams create`, never `localhost.yml` \[1029-1031\]: YesWithNoBusAddressPassesDeclaresRefusalThrough
- usage: no terminal and no --yes \[64\]: WithoutATerminalAndWithoutYes...; flag errors: FlagsNeedValues...

no teams / wizard (create part; the "No agent teams" / `Create one now?` lines belong to the picker):

- `gum choose --header Trust --selected Repositories ... starting with example-org/proj` \[961\]: WizardInACheckoutAcceptsEveryDefault
- `--value example-org/proj`, `--header Team name` `--value proj`, `--header People` `--value owner` \[963-967\]: same
- `gum style --width` with `installer creates the accounts` \[968\]: same
- no gum input header of 70+ chars \[970\]: same (loop over input and choose lines)
- `gum confirm --default=true Create team proj?` \[972\]: same
- summary `only example-org/proj, on any branch`, `a profile for the team, set up on this desktop` \[973-974\]: same (gum style box body)
- `ccy --teams proj` \[975\]: same
- no `localhost.yml` except `saved to`/`kept at` \[977\]: same (the confirm names `~/fd/localhost.yml` in the `saved to` clause; next steps have none)
- `Sign owner in to Element now?`, `Open proj in Element now?` \[978-979\]: same
- `launcher --teams proj (in proj)` \[981\]: same (execLauncher dir `/home/u/proj`, argv `--teams proj`)
- GUM env \[983-988\]: ui_test.go TestGumEnvironmentUsesTheTerminalsColoursAndOneAccent (existing); every gum call here goes through ui.Gum
- `1/5  What the team works on`, `5/5  Other machines` \[989-990\]: WizardInACheckoutAcceptsEveryDefault
- `✓` line with the repo \[991\]: same
- never `forge_api`, `path_prefixes` \[992-993\]: same
- `private room` exactly once \[994\]: same
- owner scope `everything example-org owns: ...` \[1012\]: WizardOutsideACheckoutOffersTheOwnerFirst...
- bus address `not an IP address` \[1020\]: AMachineWithNoBusAddressAsksForOneAndDeclaresItWithTheTeam (asked, bad refused, retried, `--address` passed to create)
- re-prompt `--value Example-Org/Alpha, example-org/readonly, other/missing, bad` \[1042\]: ARefusedAnswerComesBackAsTypedNotLowerCased
- `only these repositories, on any branch: example-org/alpha, other/tool` \[1047\]: same
- `Try again`, `no user or organisation called 'nope'` \[1242-1243\]: AnOwnerGitHubDoesNotKnowIsRefusedWithAReason
- `Cancelled; nothing was changed.` with 130, no Traceback \[1250-1256\]: EscOnAnyQuestionCancelsWithNothingChanged
- `Nothing was added.` \[1261\]: NoAtTheSummaryAddsNothing
- `gh auth login` \[1267\]: AGhThatIsNotLoggedInIsSaidWithItsOwnWords
- `play-claude-yolo.yml` when gum missing \[1271\]: WithoutGumTheWizardNamesThePlayThatInstallsIt

Also covered: retry limit (ThreeRefusedTriesEndTheRunWithNothingChanged), empty/whitespace answers,
people/subnet splitting, inner space (AnInnerSpaceIsRefusedNotDeleted), login with no default,
offer_join (not in a work tree, team not covering the checkout, declined, teams-for failure, launcher
exec failure, top-level failure), after_create (accept, fail, cancel), dry-run refusal, UNCHANGED output.

Not exercisable here: that the real gum fake of the suite accepts the argv (the parity suite is not
dispatched to create yet); the argv orders were taken from the bash and are asserted as strings.
