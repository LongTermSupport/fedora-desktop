# Research: Claude Code tooling and misc (Plan 00258, Task 2.1)

Group: Claude Code hooks and tooling, secret scanning, DisplayLink packaging, dotenv.
Clones studied read-only under `untracked/starred-repos/`; nothing was built, installed or run.
Paths below are relative to each clone unless they start with a repository path of ours.

What we compared against (ours): the hooks daemon v3.75.0 as configured in
`.claude/hooks-daemon.yaml` plus three project handlers in `.claude/hooks/handlers/pre_tool_use/`
(`system_paths.py`, `ansible_enforcement.py`, `hidden_git_mutation.py`); the rule table in
`CLAUDE.md`; the secret scanning in `scripts/git-hooks/` (`lib/secret-scan.bash`,
`qa-private-names.bash`), `scripts/qa-gitleaks.bash`, `.gitleaks.toml` and the CI job in
`.github/workflows/qa.yml`; `playbooks/imports/optional/hardware-specific/play-displaylink.yml`;
`docs/ccy.md`, `src-go/cmd/` (ccy-sessions, pingbus/teamchat, stt).

---

## karanb192/claude-code-hooks

Studied: baseline at 4b03f99be3df83773b1c4ae699c30195b6f716bf

**What / who.** A catalogue of 22 single-purpose Claude Code hooks, each packaged as a Claude Code
plugin (and, since November 2026, as a Codex CLI plugin), aimed at individual developers who want
"install one guard with one command". Node.js, no dependencies; one script per plugin
(`plugins/<name>/<name>.js`) with its own tests and README. `plugins/guard-pack/guard-pack.js` runs
seven guards in one Node process to pay one startup instead of seven.

**Architecture and notable features.**

- Regex rule tables with three tiers (`critical` / `high` / `strict`) chosen by `HOOK_SAFETY_LEVEL`,
  and an "ask instead of deny" mode per tier (`HOOK_ASK_*`). Rules carry an id and a reason
  (e.g. `plugins/protect-secrets/protect-secrets.js:71-235`).
- Uses hook events we receive but do not handle: `InstructionsLoaded`
  (`plugins/instructions-audit`), `ConfigChange` (`plugins/config-watch`).
- Measurement tooling: `bench/run.mjs` (per-hook latency), `tests/regex-backtracking.test.js`
  (every exported guard regex must reject a quote-heavy non-match quickly; added after a ReDoS
  wedged PreToolUse, their issue 67).
- `docs/owasp-llm-top-10-2026.md` maps each guard to an OWASP LLM risk.

**Maturity / activity.** 71 commits, first 2026-01-25, last 2026-11-18, single main author,
very active in October/November 2026 (Codex ports, bypass fixes: "Close guard bypasses and
fail-open crashes in six plugins (#74)"). Claims 2393 tests (README badge). Young but disciplined.

**Licence.** MIT (`LICENSE`, Copyright 2026 Karan Bansal). No per-directory exceptions found.
Reuse class: **lift** (keep notice).

### Hook-by-hook against our daemon

| Their hook                             | What it does                                                                                                                                                                                                                                                                                                                                                        | Ours                                                                                                                                                                                                                                                                                                                                                   | Verdict                                                                                                                                                                         |
| -------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| block-dangerous-commands               | `rm -rf ~`/`$HOME`/`/`/system dirs/`.`/`*`/`..`, `dd of=/dev/sd*`, `mkfs`, fork bomb, curl\|sh, force-push main, reset --hard, clean -f, chmod 777, docker volume rm/prune, sudo rm, crontab -r                                                                                                                                                                     | `destructive_git`, `curl_pipe_shell`, `dangerous_permissions`, `host_command_guard` (crontab -r, docker root mount). `rm -rf` and `truncate` are **deliberately allowed** upstream (owner ruling A6, `host_command_guard.py:17`; corpus rows `filesystem-rm-rf: UNCOVERED-accepted`)                                                                   | Git side: we do it better (parsed commands, recovery advice, corpus-tested). Filesystem side: **missing by decision** — `rm -rf .`/`*`/`/workspace`, `dd`, `mkfs` are unguarded |
| protect-secrets                        | Deny read/edit/exfil of ~40 credential path classes (`.aws/credentials`, `.kube/config`, `.netrc`, `.git-credentials`, `.npmrc`, `.pypirc`, `*.pem`, `*.key`, `.docker/config.json`, gcloud/azure), env dumps (`printenv`, bare `env`), `/proc/*/environ`, echo of `$*TOKEN*` vars, curl/scp/rsync/nc upload of secret files, and secrets passed to model CLIs/APIs | `secret_file_guard`: deny-by-default, index-backed, vault-aware, no escape hatch, but **default globs are only** `*.secret*`, `.vault-pass*`, `*.vault-password`, `*vault_pass*`, `id_rsa`, `id_ed25519` (from `hooks-daemon explain-handler secret_file_guard`). `printenv` is `UNCOVERED-accepted` in the upstream corpus                            | Engine: we do it better. Coverage: **behind** — their path list is far broader                                                                                                  |
| git-safety                             | Branch-aware: deny commit/merge/rebase/reset/push while on main/master; deny push to main; deny `gh pr merge/close`, `gh issue close`, `gh release delete`, `gh repo delete`                                                                                                                                                                                        | Protected-branch policy is in git hooks (`scripts/git-hooks/pre-push`, CI-gated); daemon blocks squash/rebase merges, `--delete` of unmerged branches, auto-close keywords                                                                                                                                                                             | Branch model: different (we gate pushes by CI verdict, which is stronger). `gh release delete` / `gh repo delete` / `gh issue close`: **missing**                               |
| protect-tests                          | Stops "fake green": deny deleting/renaming-away test files and adding skip/xfail/ignore markers (pytest, jest, Go, JUnit, RSpec, Rust); counts markers so a second `it.skip` still counts                                                                                                                                                                           | `qa_suppression` covers lint suppressions only (noqa, eslint-disable, type: ignore); `tdd_enforcement` requires a test before source. No skip-marker or test-deletion guard (searched `strategies/qa_suppression/*` upstream: no skip/xfail patterns)                                                                                                  | **Missing**                                                                                                                                                                     |
| case-insensitive-guard                 | `rm -rf content` vs `Content` on APFS/NTFS                                                                                                                                                                                                                                                                                                                          | n/a on Fedora ext4/btrfs                                                                                                                                                                                                                                                                                                                               | Not relevant                                                                                                                                                                    |
| config-guard                           | Deny writes to `.claude/settings*.json`, `.claude/hooks/`, `hooks.json`, `.mcp.json`, plugin manifests, `claude config/mcp/plugin` write commands; strict tier adds CLAUDE.md, `.claude/rules/`, agents, commands                                                                                                                                                   | Upstream ships `write_protected_paths` (Plan 00499) **off by default; we do not enable it**. Nothing stops an agent editing `.claude/hooks/*` forwarders, `.claude/hooks-daemon/**` or `CLAUDE/core/**`, all declared "never edit"                                                                                                                     | **Missing in our deployment** (engine exists upstream)                                                                                                                          |
| subagent-spawn-cap                     | Per-session Agent spawn ledger; ask at 20, deny at 60                                                                                                                                                                                                                                                                                                               | `spawn_rate_advisor`, `workflow_fanout_advisor`, `subagent_tier_advisor`, `resume_guard` (R-RESUME-COLD-LARGE), branch WIP caps                                                                                                                                                                                                                        | Have (advisory, rate-based rather than a hard total)                                                                                                                            |
| instructions-audit                     | On `InstructionsLoaded`, scan CLAUDE.md / rules for invisible Unicode (zero-width, tag chars, variation-selector runs), bidi overrides, decode-and-execute, secret-exfil directives, curl\|sh, hook-tampering directives; on a hit, lock the session (deny every later prompt and tool call) because `InstructionsLoaded` itself cannot block                       | Our `.claude/hooks/instructions-loaded` forwarder exists but **upstream has no handler for the event** (`src/claude_code_hooks_daemon/handlers/` has no instructions_loaded dir). `validate_instruction_content` checks only ephemeral content on writes. No invisible-Unicode check anywhere in our QA (searched `scripts/`, `src-go/qa`, `.semgrep`) | **Missing**, and topical: this plan has agents reading 7+ untrusted clones that carry their own `CLAUDE.md` files                                                               |
| config-watch                           | `ConfigChange` event: make every mid-session settings change visible, or block it                                                                                                                                                                                                                                                                                   | Forwarder `.claude/hooks/config-change` exists; no handler upstream                                                                                                                                                                                                                                                                                    | **Missing**                                                                                                                                                                     |
| auto-stage                             | `git add` every file Claude edits                                                                                                                                                                                                                                                                                                                                   | none                                                                                                                                                                                                                                                                                                                                                   | Do not adopt: conflicts with deliberate staging and the Plan Commit Rule                                                                                                        |
| format-code                            | ruff/prettier after every edit                                                                                                                                                                                                                                                                                                                                      | `lint_on_edit` (lint, not format), `markdown_table_formatter`                                                                                                                                                                                                                                                                                          | Different by design (we lint and deny rather than rewrite)                                                                                                                      |
| session-logger                         | Markdown log of each session (files touched, commands, best-effort secret redaction)                                                                                                                                                                                                                                                                                | Daemon verdict log (`docs/guides/VERDICT_LOG.md` upstream), `background_process_tracker`, transcripts                                                                                                                                                                                                                                                  | Roughly have; ours is machine-oriented                                                                                                                                          |
| context-hogs                           | Per-file context-cost leaderboard (which files cost the most tokens to load)                                                                                                                                                                                                                                                                                        | `lsp_enforcement`, `pipe_blocker` capture advice, status-line context tiers                                                                                                                                                                                                                                                                            | **Missing** (measurement), modest value                                                                                                                                         |
| cache-tax                              | Warns before a message re-writes a lapsed 1-hour prompt cache; prices a stale `--resume`                                                                                                                                                                                                                                                                            | `prompt_cache_indicator`, `resume_guard`                                                                                                                                                                                                                                                                                                               | Have                                                                                                                                                                            |
| nerf-receipts                          | Records own failure rate / churn / tokens-per-task by model version, flags shifts on model change                                                                                                                                                                                                                                                                   | `model_fallback_detector`, `model_downgrade_recorder`, `downgrade_indicator`                                                                                                                                                                                                                                                                           | Partly have (we detect downgrades; we do not trend quality)                                                                                                                     |
| dead-rules-audit                       | Parses CLAUDE.md into atomic rules, scores each edit against them, flags chronically ignored rules to "promote into a hook"                                                                                                                                                                                                                                         | Nothing measures CLAUDE.md compliance                                                                                                                                                                                                                                                                                                                  | **Missing**; fits Defence Before Fix thinking                                                                                                                                   |
| dead-end-registry                      | Remembers reverted approaches and warns before retrying them                                                                                                                                                                                                                                                                                                        | Plan `JOURNAL/` (human-curated), `review_round_advisor`                                                                                                                                                                                                                                                                                                | Different (ours is written, not automatic)                                                                                                                                      |
| pr-provenance-stamp                    | Stamps prompts, spend, tests run and agent-authored lines into the PR body on `gh pr create`                                                                                                                                                                                                                                                                        | none                                                                                                                                                                                                                                                                                                                                                   | Missing; low priority (we push to `ci/` branches more than PRs)                                                                                                                 |
| standup-autopilot                      | Daily standup from transcripts                                                                                                                                                                                                                                                                                                                                      | none                                                                                                                                                                                                                                                                                                                                                   | Not relevant                                                                                                                                                                    |
| bounty-board                           | TODO/FIXME debt as "XP bounties"                                                                                                                                                                                                                                                                                                                                    | `routine_qa_sweep`, plan QA sweeps                                                                                                                                                                                                                                                                                                                     | Not relevant                                                                                                                                                                    |
| notify-permission                      | Slack alert on permission/idle prompts                                                                                                                                                                                                                                                                                                                              | pingbus / teamchat, ccy supervisor                                                                                                                                                                                                                                                                                                                     | Have (different channel)                                                                                                                                                        |
| event-logger (`utils/event-logger.py`) | Dump raw hook payloads                                                                                                                                                                                                                                                                                                                                              | upstream `contracts/claude-code-hooks/*.json` (versioned payload schemas)                                                                                                                                                                                                                                                                              | We do it better                                                                                                                                                                 |

**Where we are ahead overall.** One long-running daemon (socket + static relay) instead of a Node
start per hook; parsed shell (heredoc bodies, quoted data, wrappers) instead of regex over the
command string; a versioned hook contract; a dangerous-invocation corpus with explicit
COVERED / UNCOVERED-accepted verdicts; no self-service escape hatches on secret rules (their
`CONFIG_GUARD_ALLOW=true` and ask modes let the agent's environment lift a guard).

**Where they are ahead.** Breadth of credential path coverage; test-disabling guard; config
tamper guard on by default; use of `InstructionsLoaded` and `ConfigChange`; a ReDoS test across
every rule regex; a latency benchmark per guard.

### Ideas worth adopting

1. **Broaden our protected secret paths** (clear win, issue). Add an additive `protected_paths`
   list to `handlers.pre_tool_use.secret_file_guard.options` in `.claude/hooks-daemon.yaml`
   modelled on their critical/high tiers: `.aws/credentials`, `.kube/config`, `.netrc`,
   `.git-credentials`, `.docker/config.json`, `.npmrc`, `.pypirc`, `*.pem`, `*.key`,
   `*.p12`/`*.pfx`, `.config/gh/hosts.yml`, the Claude Code credentials file, `.envrc`, `.env`.
   Check each against the tree first (the `*.secret*` lesson: measure false positives before
   blocking). Lands in `.claude/hooks-daemon.yaml`.
2. **Enable `write_protected_paths` for daemon-owned and never-edit trees** (clear win, issue):
   `.claude/hooks/*` (forwarders, but not `.claude/hooks/handlers/**`), `.claude/hooks-daemon/**`,
   `CLAUDE/core/**`. Check first that the daemon's own upgrade path does not name these paths in a
   Bash command an agent runs (the guard denies Bash routes too). `.claude/settings.json` needs
   discussion (agents legitimately add permissions), so leave it out of the issue.
3. **An `InstructionsLoaded` audit handler and an invisible-Unicode QA gate** (needs discussion,
   plan). Two halves: (a) upstream daemon handler for `InstructionsLoaded` that flags invisible
   Unicode / bidi controls / hostile directives in any loaded instruction file, with the
   lock-the-session pattern they found necessary because the event cannot block (upstream issue,
   generated with `hooks-daemon issue-report`); (b) a repo QA gate (Go, `src-go/qa/`) that fails on
   zero-width, tag, bidi-override characters in tracked text files. (b) alone is a clear win.
4. **A "fake green" guard** (needs discussion, upstream issue + plan). Deny adding skip markers
   or deleting tests. Related finding in our own tree: `src-go` has 42 `t.Skip` sites; most are
   legitimate root/fixture skips, but several skip when a tool is absent
   (`src-go/internal/ui/drain_pty_test.go:46` gum, `src-go/internal/tmuxccy/start_test.go:173` and
   `panes_test.go:193` tmux, `src-go/internal/transcript/transcript_test.go:309` jq,
   `src-go/internal/teamchat/units/bus_test.go:57` dbus-daemon). That is the skip-if-absent shape
   CLAUDE.md "Missing Dependencies — Fail Fast" prohibits. A Defence Before Fix rule in
   `src-go/qa/rules` ("a test skips on a missing tool") would be the local half; the owner
   decides whether CI-only tools justify any exception.
5. **Corpus rows for gh destructive verbs and filesystem wipes** (upstream issue). Propose
   `gh repo delete`, `gh release delete`, `gh issue close`, `rm -rf .`/`*`/project root,
   `dd of=/dev/…`, `mkfs` as rows of upstream `scripts/qa/dangerous-invocation-corpus.yaml`, so
   each gets an explicit verdict. `rm -rf` is an owner ruling upstream (A6); re-opening it is the
   owner's call. In ccy containers the project checkout is the host's own, so a stray
   `rm -rf` on it is the largest unguarded loss (guess at relative risk, not measured).
6. **ReDoS test over every guard regex** (upstream issue, clear win if not already present).
   Their `tests/regex-backtracking.test.js` runs each exported rule regex against a quote-heavy
   non-match under a time bound. Not verified whether upstream has an equivalent; the issue
   should ask.
7. **CLAUDE.md compliance scoring** (needs discussion, plan or watch). `dead-rules-audit`'s idea
   — find rules agents chronically break and promote them to deterministic checks — is the
   Defence Before Fix funnel run in reverse. Could feed `routine_qa_sweep` or a periodic report.

**Code worth lifting.** The rule tables are the useful artefact, as data, not the Node code:
`plugins/protect-secrets/protect-secrets.js:71-105` (path rules) and `:188-235` (command rules);
`plugins/protect-tests/protect-tests.js` marker lists per framework; the invisible-character sets
in `plugins/instructions-audit/instructions-audit.js`. MIT: keep the copyright notice in whatever
file receives them (e.g. a comment block in the YAML or Go table).

**Risks / avoid.** Ask modes and `*_ALLOW=true` environment escape hatches (an agent can set
env); regex-on-raw-command parsing (their own changelog shows repeated bypass fixes); auto-stage.

---

## pchalasani/claude-code-tools

Studied: baseline at 51e4854101da527a194d74b7a39221a22759f29f

**What / who.** A large personal toolbox for heavy Claude Code and Codex users on macOS + tmux:
a Python package (`claude_code_tools/`, ~90 modules), a Rust TUI (`rust-search-ui/`), Node UI,
plugins and skills. Main tools: `aichat` (session search, trim, smart-trim, rollover with lineage,
port Claude↔Codex sessions), `tmux-cli` (an agent drives another CLI in a tmux pane), `amux`
(dashboard of agents across tmux panes, "which agent is waiting on me"), `msg` (inter-agent
messaging addressed by tmux pane), `agent-tunnel` (publish a session so teammates query a
read-only fork over Discord/Mattermost), `github-wake` (wake the originating session when an
issue gets a reply), `env-safe` (list `.env` keys without values), `fix-session` (repair broken
`parentUuid` chains in transcripts), `safety-hooks` plugin, `voxtype` (macOS dictation),
`sasy-guard` pointer (an external Datalog policy engine).

**Maturity / activity.** 1308 commits since 2025-07-31, last 2026-11-10; very active
(150+ commits/month at peaks in 2025-12, 2026-07, 2026-08). Published on PyPI and crates.io.

**Licence.** MIT (`LICENSE`, 2025 Prasad Chalasani). Sub-packages carry their own MIT licences
(`packages/voxtype/LICENSE`, `packages/jev-prose/LICENSE`, `packages/visual-brief/LICENSE`);
`packages/jev-prose/pyproject.toml` declares `MIT AND CC-BY-4.0` (content under CC-BY);
`plugins/writing/skills/remove-ai-patterns/upstream/LICENSE` is MIT from a third party and
`plugins/writing/skills/agent-style/references/LICENSES/` holds further notices — check those
before lifting anything from `plugins/writing/`. Reuse class: **lift** (keep notice).

**Comparison.**

| Their tool                                                                                                                | Ours                                                                                            | Verdict                                                                                                                                        |
| ------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------------------------------------------------------------- |
| amux                                                                                                                      | `ccy-sessions` (list, attach, take over, end, reboot warn/restore; `src-go/cmd/ccy-sessions`)   | Comparable; ours is container-aware and reboot-aware, theirs ranks by "waiting on me" urgency                                                  |
| msg                                                                                                                       | pingbus / teamchat agent bus (`docs/agent-bus*.md`)                                             | We are well ahead (cross-network, LXC members, protocol docs); theirs is tmux-pane-scoped                                                      |
| github-wake                                                                                                               | daemon `idle_workable_issue_stop_gate`, declared crons                                          | **Different**: theirs is event-driven (a reply to a watched issue wakes the session); ours polls on cron ticks. Worth a look for the agent bus |
| agent-tunnel                                                                                                              | none                                                                                            | Novel: answer teammates' questions from a read-only `--fork-session` of a context-rich session. Overlaps teamchat                              |
| aichat search / trim / rollover                                                                                           | ccy supervisor idle compaction (dry-run upstream, Plan 00603); Plan 00098 encrypted transcripts | **Missing**: full-text search across all transcripts; lineage-linked rollover as an alternative to lossy compaction                            |
| fix-session                                                                                                               | none                                                                                            | Workaround for a Claude Code bug (their link: anthropics/claude-code#22107); watch, do not adopt                                               |
| env-safe                                                                                                                  | `hooks-daemon secret-meta` (existence, size, mtime, digest)                                     | Same idea; theirs lists key names of a `.env`, ours never parses content. Ours is stricter                                                     |
| safety-hooks (`rm` → move to TRASH/, block `git add -A`/`.`/`*`, block `git checkout <ref> -- <file>`, file-length limit) | `destructive_git` covers checkout discard; nothing blocks blanket `git add`                     | `git add -A` / `git add .` guard is **missing**; their rm-to-trash is an interesting answer to the upstream rm ruling                          |
| voxtype                                                                                                                   | `stt` / `wsi` GNOME speech-to-text (`docs/features/speech-to-text.md`)                          | Theirs is macOS-only; ideas only (spoken submit word, filler-word removal, wake word)                                                          |

### Ideas worth adopting

1. **Blanket `git add` guard** (needs discussion, upstream issue). Deny `git add -A`, `--all`,
   `.`, `:/`, `*` and ask for named paths (`plugins/safety-hooks/hooks/git_add_block_hook.py`).
   This repo's history of leaked identifiers and the "never commit `.bak` files" problem (two
   untracked `localhost.yml.bak-*` files sit in the root today) make blanket staging a real risk.
2. **Event-driven issue wake for the agent bus** (needs discussion, plan). `github-wake` keeps a
   durable watch per issue and delivers the first new comment to the waiting session. For ccy
   teams, a seat waiting on an owner's GitHub reply could be woken by pingbus instead of a cron.
   Lands in `src-go/internal/bus` or teamchat; check Plan 00246 first.
3. **Transcript search and lineage rollover** (needs discussion, plan, and must square with
   Plan 00098 encryption at rest). A full-text index over session transcripts and a "fresh
   session with pointers to its ancestors" alternative to compaction.
4. **Read-only fork for questions** (watch). `agent-tunnel`'s `--resume <id> --fork-session` with
   tools restricted to Read/Grep/Glob is a cheap way to let another seat ask a busy seat a
   question without disturbing it.

**Code worth lifting.** `plugins/safety-hooks/hooks/git_add_block_hook.py` and its tests
(`test_git_add_*.py`) as a reference corpus of blanket-add spellings (`:/`, `:(top)`, absolute
path of the cwd, empty pathspec) for an upstream handler. MIT, keep notice.

**Risks / avoid.** macOS-first assumptions; large surface; agent-tunnel exposes a session to chat
platforms (a public-repo and privacy hazard for us); session-file surgery (`fix-session`,
`trim-in-place`) edits Claude Code's private format.

---

## Yu-369/VibeCurb

Studied: baseline at c324ba7695a3f23229cfe9068e4cbab98a3c38f3

**What / who.** A set of seven very long design "skills" (`skills/*/SKILL.md`, 587–1771 lines
each) that force a coding agent through a fixed pipeline for web UI work: design read, quality
gate, build, visual diff with PASS/FAIL tables, "drift rejection" of generic AI styling. A 75-line
`index.js` CLI copies a chosen `SKILL.md` into a project. Audience: front-end developers.

**Maturity.** 11 commits, 2026-07-08 to 2026-08-05; dormant since. **Licence.** MIT
(`LICENSE`, 2026 Yu-369). Reuse class: lift.

**Comparison / ideas.** Little overlap: we build GNOME Shell extensions, not websites. One
transferable pattern: skills structured as phases each closed by an explicit "Quality Gate"
checklist the agent must pass before continuing (`skills/visual-redesign/SKILL.md` headings
"Quality Gate: Audit / Extraction / Prescription"). Our skills (`files/opt/claude-yolo/skills/`,
`.claude/skills/`) could use the same gate headings where a phase must not be skipped. Watch
only; no issue.

**Risks.** Very long skills cost context on every load; the CLI pulls `inquirer`/`chalk`/`ora`
for a file copy.

---

## Edmonds-Commerce-Limited/claude-code-hooks-daemon

Studied: baseline at 72bfdf4e3ceb26b10812db755123d12be7ed79df

The upstream of the daemon we run.

Only what upstream has that our deployment does not enable or use.

**Maturity / licence.** 9496 commits since 2026-01-26; tags to v3.75.0 (what we run, per
`expected_version` in `.claude/hooks-daemon.yaml`); 184 commits on main since that tag, heading
to 3.76.0. MIT (`LICENSE`). `screen/vendor/` holds vendored crates, all permissive
(`screen/VENDORED.md`: vt100 MIT, vte Apache-2.0 OR MIT, arrayvec, itoa, memchr, unicode-width).

**Handlers that ship off by default and we have not enabled** (from `default_enabled = False`
in the handler classes; the rest of the off-by-default set we do enable):

| Handler                                                                                       | Purpose                                                                                                                                                  | Relevance to us                                                                                                                        |
| --------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------- |
| `write_protected_paths`                                                                       | Deny every write route (tools and Bash) to listed globs; reads stay allowed                                                                              | **High**: see karanb192 idea 2                                                                                                         |
| `long_running_command_guard` (new in 3.75.0)                                                  | Deny a long foreground Bash on the main thread (a `timeout N`, a wait loop, a long `sleep`, known-slow programs), returning the `run_in_background` form | **High** for team coordinators: a foreground wait blocks bus messages to the seat. Needs a trial in warn-only first (needs discussion) |
| `github_issue_assignment_guard`                                                               | Issue-tied work requires the issue be assigned to the signed-in account                                                                                  | Medium: the watch-list scout files issues; teams could collide on one issue                                                            |
| `flaggable_work_advisor`, `flaggable_content_channel_guard`, `quarantine_artefact_read_guard` | Delegate content that trips an API-side safety classifier to a quarantine sub-agent                                                                      | Low today; relevant if security research plans grow                                                                                    |
| `daemon_stats` (status line)                                                                  | Health metrics                                                                                                                                           | Disabled on purpose by the owner                                                                                                       |

**Features we do not use.**

- `hooks-screen` (`screen/`): a static binary that classifies a terminal byte stream as
  main / transcript / panel view, used by the ccy supervisor's focus check. Our
  `.claude/hooks-daemon/untracked/bin/` holds only `hooks-relay`; the supervisor is in dry-run
  (`.claude/hooks-daemon.yaml` comment, `ccy:` block). Relevant when the supervisor is armed.
- Supervisor idle compaction (`CCY_IDLE_COMPACT=on`, Plan 00603, dry-run by default).
- `persistent_crons` declarations: none declared in our config, although the rule table
  carries R-CRON-STOP-DECLARED.
- No upstream handler exists for `InstructionsLoaded`, `ConfigChange`, `FileChanged`,
  `PermissionDenied`, `TeammateIdle`, `TaskCreated`/`TaskCompleted`, `SubagentStart` — our
  forwarders for these (`.claude/hooks/`) are wired but inert. Fine today; the karanb192 study
  shows two of them are useful (InstructionsLoaded, ConfigChange).
- Upstream's `scripts/qa/dangerous-invocation-corpus.yaml` (77 rows with verdicts) is the right
  home for karanb192 coverage gaps (idea 5 above).

**Upstream-side gaps found in this study** (route as upstream issues via
`hooks-daemon issue-report`): no `InstructionsLoaded` handler; no test-disabling guard; default
`secret_file_guard` globs omit common credential files; gh destructive verbs absent from the
corpus.

---

## awslabs/git-secrets

Studied: baseline at 7d6b970cbd3c216353cb22b383b70c150140662e

**What / who.** A single 360-line bash script (`git-secrets`) that installs `pre-commit`,
`commit-msg` and `prepare-commit-msg` hooks and scans with `git grep -E` against patterns kept in
git config (`secrets.patterns`), allowed exceptions (`secrets.allowed`, `.gitallowed`) and
"providers" (commands whose output becomes patterns). AWS-oriented (`--register-aws`).

**Maturity.** 119 commits, 2015-12 to 2025-09; effectively maintenance-only (2 commits in 2025,
adding Bedrock key patterns). **Licence.** Apache-2.0 (`LICENSE.txt`), `NOTICE.txt` (Amazon
copyright; bundles bats under MIT in `test/bats`). Reuse class: lift, carrying the NOTICE.

**Comparison with ours.** We are well ahead: gitleaks full default ruleset with narrowly scoped
allowlists (`.gitleaks.toml`), run pre-push over each pushed commit and in CI
(`scripts/qa-gitleaks.bash`); a shared pre-commit/commit-msg scanner
(`scripts/git-hooks/lib/secret-scan.bash`) with a private-identifier denylist from the
gitignored `localhost.yml`; `qa-private-names.bash` learning install-specific names; the daemon's
`sensitive_content` at Write/Edit time. Our CI deliberately scans the tree, not history
(`.github/workflows/qa.yml` gitleaks job comment).

**What it does that we do not.**

1. **Providers: scan for the literal values of this machine's real secrets.** `aws_provider`
   (`git-secrets:254-264`) reads `~/.aws/credentials` and turns each actual key into a pattern,
   so a commit containing that exact key fails with zero false positives. We do this for
   *names* (private terms) but not for *secret values*. Applied here: build, inside the hook and
   never printed, a set of the machine's live token values (GitHub CLI token, Claude Code OAuth
   token, vault password) and refuse any staged line or commit message containing one. Needs
   discussion (plan): the hook would read secret files, which must stay outside agent context
   (it runs as a git hook, not via the agent), and must fail closed when a source is unreadable.
2. **Merge-introduced history scan** (`prepare_commit_msg_hook`, `git-secrets:183-193`): on a
   merge, scan `git log dest..incoming -p`. We have `pre-merge-commit` and pre-push gitleaks per
   commit, which covers this.
3. **Desktop-wide protection via a git template directory** (`--install` into a template plus
   `init.templateDir`). fedora-desktop provisions developer desktops: a play could give every
   repository on the machine a gitleaks pre-commit, not just this one. Needs discussion (plan):
   a global `core.hooksPath` would override per-repo hooks; `init.templateDir` only affects new
   clones; opt-in per audience (`audience: developer`, `playbooks/imports/play-python.yml`
   already installs gitleaks for developers).

**Code worth lifting.** Pattern strings only, if ever needed: AWS / Bedrock key regexes at
`git-secrets:239-243` (gitleaks already ships equivalents, so probably never). Apache-2.0: keep
NOTICE.

**Risks.** Unmaintained in practice; `GREP_OPTIONS`-era bash; `--no-verify` advertised as the
false-positive escape.

---

## displaylink-rpm/displaylink-rpm

Studied: baseline at fc67429300f940a4766be5ee2e9b938efb72f1e1

**What / who.** The spec, Makefile and CI that build the DisplayLink driver + evdi DKMS RPM for
Fedora / CentOS Stream / Rocky / Alma, published as GitHub release assets. It is the exact
package our play installs (`play-displaylink.yml:149-152`).

**Maturity.** 331 commits since 2015-09, last 2026-11-11 ("Add Fedora 45 as a build target");
steady (28 commits in 2026). **Licence.** MIT (`LICENSE`, "2016-2022 The displaylink-rpm
contributors"). The packaged DisplayLinkManager binary is DisplayLink's proprietary EULA and the
udev rule carries "Copyright (c) 2016 - 2023 DisplayLink (UK) Ltd." (`99-displaylink.rules:1`):
do not copy those files, only the MIT recipe.

**What the package already does** (so our play need not): a systemd preset enables
`displaylink-driver.service` (`95-displaylink.preset`); a vendor `system-sleep` hook
(`displaylink.spec:171`, extracted from DisplayLink's installer by
`displaylink-sleep-extractor.sh`); udev rules that start the manager on a 17e9 device and tune
cdc_ncm buffers (`99-displaylink.rules`); `evdi initial_device_count=4` (`evdi.conf`); an X11
`OutputClass` for evdi (`20-displaylink.conf`); DKMS add/build/install in `%post`
(`displaylink.spec:181-187`).

**Comparison with our play.** We are ahead on recovery and safety: MOK enrolment via stdin
(no password in argv), DKMS for the next-boot kernel, stale `/usr/src/evdi-*` clean-up, staged
dock recovery (`helpers/displaylink_recovery/`), an unlock extension, a post-install check
(`scripts/check-displaylink-status.sh`). Behind or questionable:

1. **The RPM is installed with `disable_gpg_check: true` and no checksum**
   (`play-displaylink.yml:149-152`). Upstream releases are unsigned (no GPG key in the spec or
   README), so pinning a `sha256` per release is the only integrity check available. Clear win
   (issue): pin a checksum next to `displaylink_version` and have the update-versions skill
   (`.claude/skills/update-versions/`, `helpers/version_pins/`) bump it with the version, as it
   already does for other pins.
2. **Logrotate mode bug fixed downstream only.** Our play chmods `/etc/logrotate.d/displaylink`
   to 0644 because the spec installs it with `cp -a` (`displaylink.spec:165`), keeping a
   group-writable 0664. That is an upstream packaging bug with a one-line fix
   (`install -m 0644`). Clear win: file it upstream (public repository, MIT; check the wording
   names no machine). Our workaround stays until a release carries it.
3. **Two suspend hooks.** The RPM's `/usr/lib/systemd/system-sleep/displaylink.sh` and our
   `displaylink-suspend.service` (plus the suspend-and-lid play's own system-sleep hook) all run
   on resume. Whether they interfere was not checked (guess; Plan 00056 is the place to verify
   when a dock is available).
4. **"Must be latest" assertion** (`play-displaylink.yml:94-125`) fails the play as soon as
   upstream publishes, which turns every upstream release into a blocking edit. A pinned
   version + checksum (point 1) plus the existing pin-drift report would let the play stay
   green on a known-good pin. Needs discussion: the assertion exists because old evdi breaks
   on new kernels.

**Code worth lifting.** None needed. Upstream's CI matrix (`ci/fedora.sh`) is a model if we ever
build evdi ourselves.

---

## gyf304/dotenv

Studied: baseline at bb5cc71133552c5d0702811d312b8f84d7891acb

Light pass.

A 294-line C utility (`dotenv.c`) that loads `.env` and execs a program, usable in a shebang
(`#!/usr/bin/env -S dotenv bash`). 11 commits, April 2024, dormant. BSD-2-Clause (`LICENSE`,
2024 Yifan Gu); reuse class lift. No overlap worth adopting: ccy has its own `ccy.env`, and the
latest change ("do not error if .env is not present") is the silent-missing-input pattern our
Fail Fast rule forbids. The `env -S` shebang trick is the one we already use for playbooks.
Not for the watch list.

---

## Group takeaways (ranked)

1. **Our guards are better engineered but narrower in coverage than the plugin catalogue.**
   Close the coverage gap cheaply through configuration we already have: broaden
   `secret_file_guard` protected paths, and enable `write_protected_paths` for `.claude/hooks/*`,
   `.claude/hooks-daemon/**` and `CLAUDE/core/**`. Two issues, no new code.
2. **Untrusted instruction files are an unguarded channel.** Plan 00258 itself has agents
   reading clones that carry their own `CLAUDE.md`. Add a QA gate against invisible Unicode and
   bidi controls in tracked files (issue), and ask upstream for an `InstructionsLoaded` audit
   handler with the lock-the-session pattern (upstream issue / plan).
3. **"Fake green" is an uncovered defect class here.** No guard stops skip markers or test
   deletion, and `src-go` already has several skip-when-tool-absent tests that contradict the
   Missing Dependencies rule. A Defence Before Fix pass with a `src-go/qa/rules` analyzer (plan),
   plus an upstream guard request.
4. **DisplayLink RPM integrity.** Pin a sha256 for the unsigned release asset we install with
   GPG checks off (issue), and send the logrotate `cp -a` fix upstream (issue upstream).
5. **Known-value secret scanning and event-driven wake-ups are the two ideas with the most
   depth.** git-secrets' providers (refuse the machine's literal live tokens) and
   claude-code-tools' `github-wake` (wake a waiting seat on an issue reply) each need a plan.

**Watch-list candidates:** karanb192/claude-code-hooks (fast-moving, rule data worth tracking);
pchalasani/claude-code-tools (agent-ops ideas; high churn, so scan in batches). Upstream
claude-code-hooks-daemon is already tracked by daemon upgrades. Not worth watching: VibeCurb,
git-secrets, dotenv; displaylink-rpm is already tracked through the pinned-version checker.
