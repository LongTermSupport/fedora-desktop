## 13:49 · finding · — — Batch 3 CI trap, coordinator asks, multi-team and the v1 seat-add regression

Batch 3's CI never started on three `ci/` branches. Each was created at a commit GitHub already
held from `v2-smoke`, so the push carried no new commits and the QA workflow's `paths` filter
skipped it in silence (R40). A fast-forward of an existing `ci/` branch gave the push a real range
and run 45625876657 started at once. Before that, the project rules over all of `./internal`
found U4 breaking two rules already on F44 (`project_selector_parse`, `project_start_decision`),
fixed in 1d9f38cb2.

Coordinator asks answered: A12 (the two design records now say `human_text` gates a human's
words only), F14 (R39, minimum model per seat), F15 and F16 (R15 tab title from tmux's
`set-titles-string`; R35 the spaces-rail key `[shell] spacesRailVisible`, from Lightning's
upstream source, since the laptop's secret guard forbids reading the Flatpak's data).

F18/F19 (the owner: the Ballicom coordinator's session also becomes worker seat 7 on lts-ops;
multi-team is a no-degrade requirement). A delegated code reading, spot-checked here: multi-team
works at the F44 tip from the code path; `say --to-coordinator` counts the session itself as one
team's coordinator (R42); and no host verb adds a team to a running v2 session, though v1's
`ccy-teams session add` did (R41, a regression). Both go to U10, next after batch 3. The interim
keeps the conversation: read the id with `ccy-teams seats conversation 1@ballicom`, `/exit`, then
`ccy --resume <id> --teams 1@ballicom,7@lts-ops`.
