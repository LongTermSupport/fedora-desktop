# Defence Before Fix report: gh-scope-outside-ssot

The owner asked for `/dbf` on the rule that GitHub OAuth scopes live in one place. Defence
commit `27aee075`; final commit `b9475431` on `F44` (the merge is `4b48686f`).

## Specification followed

Method specification 1.0.1, detector specification 1.0.0, tooling specification 0.2.0.
Fetched from the site by the plugin's `refresh-spec.bash` when the run began, and fetched
again for this report after the container's cache was cleared. The plugin had been installed
after the session started, so its skill and agents could not be loaded, and its runbook
(`skills/dbf/SKILL.md`) was followed by hand.

## Toolchain

No conforming DBF toolchain is declared in this project. The detector chosen from the
register is semgrep 1.177.0, which the project already runs for its bash conventions
(`.semgrep/bash-conventions.yml`, `scripts/qa-semgrep.bash`). The commands:

- listing: none exists (see the gaps below). The project's list of gates is
  `scripts/qa-all.bash` and the table in `CLAUDE/QA.md`;
- identifier resolution: none exists. The id is documented in `CLAUDE/QA.md` under the
  heading `gh-scope-outside-ssot`, and the rule's message cites that heading;
- single-rule harness: `semgrep --test --config .semgrep/gh-scopes.yml .semgrep/gh-scopes.fixture`, which `scripts/qa-gh-scopes.bash` runs before every scan.

The project records no calibration. Assumed: every tracked regular file is first-party
unless the rule's own excludes say otherwise, and the gate must fail on a file it was
handed but did not scan.

## Defect and class

- **Originating instance.** `run.bash`, the GitHub authentication step: a bare
  `gh auth login`, then a separate `auth refresh -s admin:public_key` behind a private
  scope table (`ghCheckTokenPermission`). The owner asked for every permission in one pass
  of the `run.bash` flow, with one source for which ones are needed. Commit signing then
  needed a further scope, which existed only in `docs/configuration.md` and the plan's
  deploy message.
- **Class.** GitHub OAuth scope requirements stated outside `vars/github-required-scopes.yml`,
  and scope-hierarchy logic ("admin implies write implies read") outside one
  implementation.
- **Hazard.** The copies drift. One consumer asks for, or accepts, a different set from the
  others, and the owner is sent through GitHub's browser authorisation more than once, or a
  token one part accepts is refused by another.

## Rule

- **Detector:** semgrep, `languages: [generic]`, in `.semgrep/gh-scopes.yml`.
- **Identifier:** `gh-scope-outside-ssot`, severity ERROR.
- **Message:** "gh-scope-outside-ssot: a GitHub OAuth scope named outside
  vars/github-required-scopes.yml. Read the list from that file and ask
  helpers/github_scopes what is missing; name the file, not its contents. See CLAUDE/QA.md
  "gh-scope-outside-ssot"."
- **Documentation:** `CLAUDE/QA.md`, section `gh-scope-outside-ssot` (what it is, why it
  exists, how to fix a finding).
- **Width.** Two forms. The first is a token that can only be a GitHub scope: a prefixed
  name (`admin:`, `write:` or `read:` with a GitHub scope family, `user:email`,
  `repo:status`, and so on), or an underscore name such as `public_repo`. The second is a
  literal list handed to `gh auth login|refresh --scopes/-s`, which is where the bare
  names (repo, gist, workflow, project, user) occur.
- **Deliberate excludes, as not carrying the hazard:**
  - the source and the one implementation (`vars/github-required-scopes.yml`,
    `helpers/github_scopes/**`, `tests/helpers/github_scopes/**`);
  - the rule and its fixture;
  - records nobody reads to decide what to ask GitHub for: plan prose
    (`CLAUDE/Plan/**/*.md`) and changelogs (`docs/*changelog*.md`). Plan scripts stay in;
  - upstream hooks-daemon files replaced wholesale on upgrade (`.claude/hooks-daemon/**`,
    `.claude/skills/**`), and `untracked/**`.
- **Next wider rule considered:** flagging every bare word a scope can be (repo, gist,
  workflow, project, user) anywhere. It was not built because those are ordinary words
  in this codebase, so it would match code that does not carry the hazard.
- **Known limit:** a restated list made only of bare names, outside a `gh auth` command, is
  not seen. The one such table the search found was also caught through its prefixed rows.

## Proof

- **The fixture.** `.semgrep/gh-scopes.fixture` holds 8 `ruleid` lines and 6 `ok` lines.
  The ruleid lines are the two `gh auth refresh -s` forms, a literal `--scopes` list, a
  "file + scope" message, two hierarchy case-table lines, a doc line and a bare
  underscore-name list item. The ok lines are `--scopes` from a variable or a helper call,
  a message that names only the file, `gh ssh-key add --type signing`, a scopes read, and
  an unrelated clone. `semgrep --test` passes. A mutant with the `gh auth` pattern removed
  fails it with `missed lines: [11]`.
- **The red run.** `./scripts/qa-gh-scopes.bash` at `27aee075` exits 1: 56 lines in 10
  files, of 806 scanned. The files: `run.bash`, `playbooks/imports/play-github-cli-multi.yml`,
  `scripts/gh-account-setup.bash`, `docs/github-multi-account.md`, `docs/configuration.md`,
  `docs/headless-provisioning.md`, `docs/headless-server-install.md`,
  `docs/vm-acceptance-testing.md`, `files/home/.local/bin/vmtest` and
  `CLAUDE/Plan/00139-commit-signing-everywhere/deploy.bash`.
- **The red commit.** The red proof survives on its own in `27aee075`, which adds the rule,
  the fixture, the gate and its `qa-all.bash` wiring, and fixes nothing.

## Independent search

- **Dispatch.** A general-purpose agent following the plugin's `independent-searcher`
  instructions ran before the rule existed. It was given the class and the hazard only,
  not the rule. Its report is
  [subagent-reports/260925-dbf-independent-searcher-sonnet.md](subagent-reports/260925-dbf-independent-searcher-sonnet.md).
- **Result.** 18 sightings: 15 live, 1 medium-confidence plan prose, 2 historical records.
- **What text search found that reading could not.** How many copies there were. There
  were eight "the file plus admin:public_key" restatements and three hierarchy tables.
  Each site looked reasonable on its own.
- **What reading found that text search could not.** Three things:
  - `run.bash` never read the scopes file at all. Its one scope check was free-floating.
  - The three hierarchy tables were byte-identical.
  - The signing scope's absence from the file was known, in-flight drift.
- **Reconciliation, in the rule's favour.**
  - All 15 live sightings are within the rule's findings.
  - The other 3 (the PLAN.md task prose, `docs/run-bash-changelog.md`, and a Plan 00035
    design note) fall under the records exclude, which is justified above.
  - The rule found 2 that the search did not: the `vmtest` message, and a comment in
    the play's PhpStorm-token block showing the parsing of an example scope string.
  - No widening was needed, because no live sighting fell outside the rule.

## Sweep and count

- **Scope.** Every tracked regular file, in every language: `git ls-files`, symlinks left
  out because semgrep refuses them and each points at a file scanned in its own right.
  Files are handed to semgrep by name. A directory scan applies semgrep's default ignores,
  which skip `tests/`. The gate fails (exit 2) on any handed file semgrep neither scanned
  nor reported as skipped, after allowing for the rule's own excludes.
- **This is recorded as the project's decision.** The project had none before.
- **Count at the defence commit.** 56 lines in 10 files.
- **Narrowing.** Only the excludes listed under Rule.
- **During the fix.** The rule caught one instance introduced by the remediation itself:
  `scripts/test-run-bash-gh-scopes.bash` held a copy of the list as its fixture. That makes
  57 lines in 11 files in all.

## Fixes

Every instance was examined individually. None was changed by pattern.

- **`run.bash`.**
  - Removed `ghCheckTokenPermission` and its hierarchy table.
  - The first `gh auth login` now carries every scope, as printed by
    `helpers.github_scopes.cli required`.
  - The separate refresh is replaced by `gh_request_missing_scopes`: one refresh for
    everything missing, re-checked afterwards, and a single refusal naming everything when
    headless.
  - Four messages now name only the file. The scopes list comes from the checkout, cloned
    over public HTTPS when `run.bash` is streamed.
- **`scripts/gh-account-setup.bash`.** Removed `_scope_satisfied`. It loads and judges
  scopes through the helper. A headless run audits every account before it fails once.
- **`play-github-cli-multi.yml`.**
  - The per-account `scope_satisfied` audit is replaced by `helpers.github_scopes.cli audit`,
    which reads each account with its own token.
  - The PhpStorm-token wrapper asks the helper what is missing.
  - The comment showing the parsing of an example scope string went with the code it
    described.
- **Docs.** `docs/github-multi-account.md` lost its copy of the table and its partial list.
  The headless docs, `docs/vm-acceptance-testing.md` and the `vmtest` message now name the
  file. `docs/configuration.md` and the plan's `deploy.bash` lost the hand-registration
  steps, because registration is now done by `play-github-cli-multi.yml`. The signing scope
  joined `vars/github-required-scopes.yml`.
- **`scripts/test-run-bash-gh-scopes.bash`.** Now uses a scopes file of its own.

No instance was satisfied by a suppression, a baseline, a new exclude or by leaving the
hazard in place. The rule's excludes are byte-identical at the defence and the final commit.

## Decisions referred to the owner

None. Every instance was within the practitioner's authority under section 4, and all 57
are fixed with none remaining. The rule is merged at full width with no recorded known
instance.

## Permanence

- **Wiring.** `scripts/qa-gh-scopes.bash` is a hard gate in `scripts/qa-all.bash`, the
  project's entry point for its checks. It is required before every commit touching Bash
  or Python (CLAUDE.md, "QA Mandatory Before Commits").
- **Green run.** Through `./scripts/qa-all.bash` at `b9475431`: exit 0,
  `✓ gh-scope-outside-ssot: passed: 813`, `✓ QA passed: 1111 files checked`, every gate
  green.

## Toolchain and detector gaps

- There is no listing of the project's active defences. The tooling specification asks for
  one, and the project has only `qa-all.bash` and the `CLAUDE/QA.md` table.
- There is no resolver from a rule identifier to its documentation. The id resolves only by
  searching `CLAUDE/QA.md` for its heading.
- semgrep's directory scan silently skips `tests/` through its default ignores, and does
  not list files dropped by a rule's `paths.exclude` as skipped. The gate hands files by
  name and computes the expected set itself, but the detector should report both.
- semgrep refuses symlinks (exit 2) rather than skipping them with a reason.
- semgrep hides the matched text in its output unless logged in, so the gate prints
  `file:line` and the rule id only.

## The original defect

`run.bash` now asks for every required scope in one pass: all of them at the first login,
and every missing one in a single refresh after that. The same holds in
`gh-account-setup.bash` and the play.

The reproducing test is `scripts/test-run-bash-gh-scopes.bash`, wired into `qa-all.bash`
as `run-bash-gh-scopes` and green at 19/19 on the final commit. Its checks:

- a token short of two scopes gets exactly one refresh, asking for both;
- a refresh that leaves one missing is refused, naming it;
- headless is refused, naming every scope at once, and starts no browser flow;
- the first interactive login carries `--scopes`;
- no private scope table remains in `run.bash`;
- `gh-account-setup.bash` in headless mode names every short account in one failure.

Against the pre-remediation `run.bash` (`5ca8db83`) it exits 1:
`FAIL: could not extract gh_scopes_repo from …/run.bash.pre`. The one-pass functions do
not exist there. That red is structural, not a behavioural assertion: the old code had no
single entry point for the test to drive.
