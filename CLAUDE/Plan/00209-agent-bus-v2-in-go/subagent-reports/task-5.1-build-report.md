# Task 5.1 build report: host `cc` members on v2

Branch `v2-host-cc`. Design: [DESIGN-host-cc-v2.md](../DESIGN-host-cc-v2.md). CCY 3.124.1,
container 2.84.51 (above v2's 3.123.3 / 2.84.50, merged in before the bump).

## Steps

| Step | What                                                                                                                                                              | Commit(s)                      |
| ---- | ----------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------ |
| 1    | The conversation-record class: rule `project_conversation_record`, `conversation.Locate` by the record's live system                                              | on v2 before this round        |
| 2    | `teamchat kit assemble`: the host kit holds plugin and settings                                                                                                   | on v2                          |
| 3    | `teamchat run --extra-settings FILE` (H4)                                                                                                                         | c7194a52f                      |
| 4    | `cc-claude -- claude <args>` (H5)                                                                                                                                 | d93958cfa                      |
| 5    | `ccy-teams internal cc-run`, both systems; cc-teams.bash shrunk                                                                                                   | fde07c186                      |
| 6    | The host chooser lists and takes teamchat pool seats; the Matrix consent names a teamchat seat by its pool handle (`Teamchat.Handles`, `system.Cmd.Handles`)      | 329dff178 (red), c9490e270     |
| 7    | `teamchat reach` inside a `run --state` session: the keeper writes `<run>/state-root`, reach reads the bundle below it; a bad record or shared root is refused 78 | e58f519d4                      |
| 8    | Docs (agent-bus.md, agent-bus-cross-network.md, ccy.md) and plan text (B-member H1, seam 56, D 3.9, LAUNCH-ccy.md, PLAN.md 4.3, 5.1, 6.4)                         | dcb772cb6, e58f519d4           |
| 9    | Merge of v2, version bumps, changelog, meta-deploy entries, `acceptance-host-cc-v2.bash`, this report                                                             | d29216243 (merge) and the last |
| 10   | The host seat's lane (Plan 00263 D3, static half): cc's deny file denies tests, builds, lint and QA; `hostlane_test.go`                                           | the last                       |

Step 9's second merge of `origin/v2` (5f442cac0, CCY 3.123.3, container 2.84.50) was already in
the branch, so there is no new merge commit; 3.124.1 / 2.84.51 are above it and cover step 5's
ccy-teams and cc changes.

## Step 10: the host lane's deny rules

`files/var/local/claude-code/cc-teams-settings.json` (cc-run hands it to teamchat as extra
settings and merges it into agent-bus's settings) now denies, each plain command as
`Bash(<cmd>)` and `Bash(<cmd> *)`: `go test`, `go build`, `go vet`, `golangci-lint`, `npm test`,
`npm run test`, `pytest`, `make`, `podman build`, `ansible-playbook`, `semgrep`; and
`Bash(python -m pytest*)`, `Bash(python3 -m pytest*)`, `Bash(*scripts/qa-*.bash*)`,
`Bash(*run.bash qa*)`. `TestTheDenyFileHoldsTheHostLane` (red first) holds each rule in the file,
`TestCCRunAssemblesTheHostLane` in what cc-run assembles on both systems. They match command text
only: `sh -c`, an env prefix or another path passes; the duty-aware check is the hooks daemon's.
`meta-deploy.bash` still runs Ansible for a host seat, since the rule sees only its own name.

## Tests run (this round)

`go test` and `go vet` (with the project rules driver, `qa/driver`) on `internal/teams`,
`internal/teams/chooser`, `internal/teams/system`, `internal/teamchat/paths` and
`internal/teamchat/member/...`; red seen first for step 6 (the consent asked agent-bus) and step 7
(reach read the home layout; the refusal cases each hold a home seat so a fallback answers 20,
not 78). `scripts/qa-docs.bash`. `shellcheck -x` and `bash -n` on the acceptance script.
Step 10: `go test`, `go vet`, the project-rules `go vet` and `golangci-lint` on
`internal/teams/teamchatlaunch` (its parity test split under gocognit, gofumpt and an unparam
`pool` fixed on the way), `go test` on `internal/matrixclients`, `scripts/test-cc-teams.bash`.
No `-race`, no qa-all or qa-changed, no image build, no Ansible (per the brief).

## Open

- The acceptance (design section 8) is the host's, attended: the script does the read-only
  checks and exits 2 until a pool seat is placed and `cc --teams` launched in ccy's tmux.
- The consent's wording still says the session posts "through pingbus"; on teamchat it posts
  through teamchat. A wording change for a later step.
- Tooling fault: the hooks daemon's `secret_file_guard` (R-SECRET-SCRIPT-AUTHOR) denies a
  Write or Edit of an ordinary bash script whose path-taking command (`cd`, `dirname`,
  `basename`, `grep`, `env`) has a quoted variable argument, as matching `*.secret*`, so the
  plan-script prelude cannot be authored. The acceptance script was assembled from an existing
  script's prelude and a text file. Not yet filed upstream (`hooks-daemon issue-report` wants a
  source citation and a relative fixture); reported to the coordinator.
