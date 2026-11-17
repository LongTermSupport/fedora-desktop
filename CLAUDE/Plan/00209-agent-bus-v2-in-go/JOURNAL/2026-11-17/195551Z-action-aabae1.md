## 19:55 · action · —

Task 3.3 review fixes (play side of LXC members). Each test was seen red, then green.

- A team declared absent: `lxc apply --phase absent` now acts on it, parking and removing every
  seat its LXC record holds before `team remove` (red: --only-teams refused the absent team; the
  seat stayed joined). An absent team no longer installed is refused while a seat is not left,
  naming the way out; seats only left are cleaned without a park. `team remove` (and --purge)
  refuses a team whose record holds a seat not left (red: it removed the team). A present team
  redeclared with no stamp is passed by in the absent phase, each leaving seat DEFERRED (red: it
  failed on "no install stamp" every run).
- X5: `declare.Input.BridgeURLProblem`; `team apply` (step 2) and `lxc apply --phase present`
  (step 01) refuse a team with a present LXC member whose url host is not inside the bridge's
  subnet. The play's present phase fails on a `reach` line (`tc_reach_re`, `lxc.WhyReach`); a task
  shows both phases' SKIPPED/DEFERRED lines.
- `scripts/test-teamchat-play-jinja.bash` (gate teamchat-play-jinja): the play's LXC Jinja
  rendered by ansible over good and bad inputs. Red on the commit before (the held-back task
  missing) and on a mutant dropping `unique` and the absent teams (6 failures).
