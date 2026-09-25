# Independent search: GitHub OAuth scope requirements expressed outside `vars/github-required-scopes.yml`

Dispatch did not contain the detector's rule or pattern — I did not see semgrep syntax
or a rule body anywhere in the prompt, so I searched from the class/hazard description
alone. I did not open `.semgrep/`, `helpers/github_scopes/`, `tests/helpers/github_scopes/`,
or any excluded plan subtree. I found one file, `scripts/qa-gh-scopes.bash`, whose one-line
description ("This gate finds every other place a GitHub OAuth scope is named") strongly
suggests it may itself be the detector (or a companion to it) even though it is not under
`.semgrep/`. I did **not** open or read it, to keep this search independent — noted below
as an unopened, unverified sighting, not as a class instance.

## Forms I expected before searching

- A literal `gh auth login --scopes` / `gh auth refresh --scopes <list>` with a hardcoded scope list.
- A hardcoded array/string of scope names (`repo`, `workflow`, `gist`, `project`, `read:org`, `admin:public_key`, `user:email`, …) in bash, YAML/Jinja, or Python, not read from the vars file.
- Doc prose/tables that recite "you need these scopes: …".
- Error/abort messages naming a specific scope instead of pointing only at the vars file.
- Independent scope-hierarchy ("implies") logic: `admin:*` implies `write:*` implies `read:*`, `user` implies `user:email`/`read:user`/`user:follow`, `project` implies `read:project`.
- GitHub API scope-check plumbing (`X-OAuth-Scopes` header parsing, `gh auth status --json hosts .scopes`) duplicated with its own comparison logic rather than sharing one implementation.

## Instances found

### Class (b): independent scope-hierarchy implementations (high confidence, the headline finding)

Three byte-for-byte identical, independently maintained copies of the same "which scope implies which" case-statement exist. Nothing ties them together; each is free-standing bash:

1. **`run.bash:2417-2461`** — function `ghCheckTokenPermission()`. Comment at 2418-2420 states the hierarchy in prose ("Honours GitHub's scope hierarchy: admin:\* implies write:\* implies read:\*, and `user` implies user:email/read:user/user:follow"), then a `case "$permission" in … esac` block (lines 2436-2446) hand-encodes it for `org`, `public_key`, `repo_hook`, `gpg_key`, and `user`. Found by: text search for `case "$permission"` / `admin:org`. Confident: carries the hazard (own implementation of scope relationships).
2. **`playbooks/imports/play-github-cli-multi.yml:227-251`** — inline shell function `scope_satisfied()` inside the "Audit OAuth scopes for each authenticated GitHub account" task. Same prose comment (227-229), same `case "$required" in … esac` table, verbatim. Found by: text search for `scope_satisfied` / `admin:org`. Confident.
3. **`scripts/gh-account-setup.bash:146-168`** — function `_scope_satisfied()`. Same prose comment (146-148), same case table, verbatim. Found by: text search for `_scope_satisfied` / reading the header comment block. Confident.

I diffed the three `case … esac` bodies (reading technique) — they are currently identical, character for character, including comment wording. That is exactly the hazard: three places independently "know" the hierarchy, so the next scope added to `vars/github-required-scopes.yml` that needs a hierarchy exception can be patched into one, two, or three of these copies with no mechanism forcing all three to change together.

### Class (a): scope names/lists stated instead of just pointing at the file

**The "`vars/github-required-scopes.yml` + `admin:public_key`" pattern** — appears independently, worded slightly differently, in at least 8 places. All of them point at the SSoT file *and* separately name `admin:public_key` as an addition — redundant today, since `admin:public_key` is already the first entry inside `vars/github-required-scopes.yml`. This is a live drift artifact: each of these 8 sites has its own opinion of "the file's scopes, plus one more named scope," so if `admin:public_key` were ever renamed/removed from the SSoT, or another operational scope were added instead, all 8 would still say the old thing.

04. `run.bash:386` — abort message: `"Provide a 0600 file holding a scoped PAT (scopes: vars/github-required-scopes.yml + admin:public_key), or set RUN_BASH_GITHUB_ACCOUNTS=none."`
05. `run.bash:1109` — usage/help comment: `GitHub token scope: the full vars/github-required-scopes.yml set + admin:public_key.`
06. `run.bash:2474` — abort message: `"gh auth login --with-token was rejected — check the PAT in RUN_BASH_GITHUB_TOKEN_FILE (needs scopes: vars/github-required-scopes.yml + admin:public_key)"`
07. `run.bash:2538` — abort message: `"provide a PAT carrying vars/github-required-scopes.yml + admin:public_key in RUN_BASH_GITHUB_TOKEN_FILE, or set RUN_BASH_GITHUB_ACCOUNTS=none"`
08. `scripts/gh-account-setup.bash:333` — user-facing hint: `"Provide a PAT that already has all required scopes (vars/github-required-scopes.yml + admin:public_key)."`
09. `docs/headless-provisioning.md:108` — `**GitHub token scope:** the full \`vars/github-required-scopes.yml\` set **plus** \`admin:public_key\`.\`
10. `docs/headless-server-install.md:142-143` — bulleted requirement: "every scope listed in `vars/github-required-scopes.yml` in the repo, **plus** … `admin:public_key` (so `run.bash` can upload your SSH key)."
11. `docs/vm-acceptance-testing.md:247-248` — "The PAT needs the scopes in `vars/github-required-scopes.yml` plus `admin:public_key`, because the run uploads the login key it generates."

All found by text search on `admin:public_key`; each read in context to confirm it independently asserts the scope rather than only citing the file. Confident these carry the hazard — they are 8 separately-worded, separately-maintained statements of the same fact.

Related, in `run.bash` itself: **`run.bash:2533`** — `if ! ghCheckTokenPermission "admin:public_key" > /dev/null 2>&1; then` — this is `run.bash` hardcoding a specific required scope literal to check for, and it is the *only* scope run.bash checks by itself; run.bash never reads or parses `vars/github-required-scopes.yml` anywhere (confirmed: the only mentions of that path in run.bash are the four prose messages above, never a `cat`/`yq`/python read of it). So run.bash's own enforcement of "what scope must this token carry" is entirely independent of the SSoT for this one scope. Found by reading the surrounding code once the literal turned up in the text search. Confident.

**`docs/github-multi-account.md` — the "Required OAuth Scopes" section (lines 113-127)**: a full markdown table reproducing every row of `vars/github-required-scopes.yml` (scope name + "why"), even though the paragraph immediately above it (115-118) correctly states the file is the single source of truth. Found by text search for "Required OAuth Scopes" / reading the section. Confident this is the hazard: the table is a second, hand-maintained copy of the same list that has to be edited in lockstep with the YAML file every time a scope is added, removed, or its rationale changes — nothing enforces that.

12. `docs/github-multi-account.md:113-127` (table, 7 rows, one per SSoT entry, plus a hierarchy aside "implies `read:project`" duplicating the YAML file's own header comment).
13. `docs/github-multi-account.md:29-31` — separate prose restatement, partial list: "without the OAuth scopes this project requires (`admin:public_key`, `repo`, `workflow`, and others)." Found by reading the same file end-to-end past the table. Confident — a second, independent (and only partial/three-of-seven) restatement of the same list in the same document.

**A genuinely new scope, stated only outside the SSoT, for a feature the SSoT does not yet cover — commit-signing key registration:**

14. `docs/configuration.md:215-222` ("Commit Signing" section) — states outright: "This needs the `admin:ssh_signing_key` scope, which the play's other scopes do not include," then gives the exact CLI incantation `gh auth refresh --scopes admin:ssh_signing_key`. Found by text search on `admin:ssh_signing_key`. Confident — this is the class by definition: a scope requirement stated in docs, expressly acknowledged as living outside the shared list.
15. `CLAUDE/Plan/00139-commit-signing-everywhere/deploy.bash:79-83` — a live plan deploy script's `printf` block, shown to the operator as the "NEXT" steps, includes the literal line `printf '       gh auth refresh --scopes admin:ssh_signing_key\n'`. This is a message shown to the user requesting a specific scope, independent of the SSoT file. Found by text search on `admin:ssh_signing_key`, confirmed by reading the surrounding `deploy.bash` script (an explicitly in-scope "live plan script" per the dispatch). Confident.
16. `CLAUDE/Plan/00139-commit-signing-everywhere/PLAN.md:77` — task prose: "`admin:ssh_signing_key` joins the scope audit, with its refresh command," under an **open, in-progress task** (Task 1.3, marked 🔄 with its "Implementation" checkbox still ⬜ unchecked). Read in context: as of today this scope is *not yet* in `vars/github-required-scopes.yml` (I confirmed the file's 7 entries do not include it) and not yet in the shared audit — so `docs/configuration.md` and `deploy.bash` are, right now, the only two places this requirement exists, and they only agree with each other because someone kept them in sync by hand. This is documentation of an active decision rather than an implementation, so I list it at somewhat lower confidence than 14/15, but it is a live (not archived/JOURNAL) plan document naming a scope.

### Lower-confidence / historical, found by reading, listed for completeness

17. `docs/run-bash-changelog.md:77-80` — a changelog entry narrating a past fix, naming `admin:public_key` by name ("a headless run without `admin:public_key` aborts…", "When the GitHub token lacks `admin:public_key`…"). This is changelog narrative describing already-shipped behavior rather than a live instruction, similar in kind to the JOURNAL/subagent-reports material the dispatch excludes, but `docs/run-bash-changelog.md` itself wasn't named in the exclusion list, so I record it at low confidence rather than omit it.
18. `CLAUDE/Plan/00035-gh-multi-account-hardening/DECISIONS.md:160-161` and `PLAN_archive.md:479-480` — design-decision prose from the plan that originally built this scope-audit system, describing "(2) scope audit via the `X-Oauth-Scopes` header + `gh auth refresh` if missing." Historical rationale for a decision already implemented (matches what's now in `play-github-cli-multi.yml`), not a second live implementation. Low confidence / informational only.

## Not counted (checked and ruled out)

- `.github/workflows/qa.yml`'s `permissions:` block — this is a GitHub Actions job-token permission grant (a different GitHub mechanism from personal-access-token/OAuth "scopes"), not an instance of this class.
- The many hits on the bare word "scope" across `CLAUDE/*.md`, `docs/architecture.md`, `docs/ccy.md`, `files/var/local/claude-yolo/lib/*.bash` etc. — these are Ansible play `scope: general|gnome|server`, systemd `--scope` units, bash variable scope, or unrelated "in/out of scope" prose. Read each hit in context and excluded.
- `playbooks/imports/play-github-cli-multi.yml`'s PhpStorm-token block (~1055-1161) — reads `required_scopes` from the shared `github_required_scopes` Ansible var (Jinja-templated from the SSoT) and does a plain substring containment check with no independent hierarchy logic. Compliant, not an instance.
- `scripts/gh-account-setup.bash`'s own `REQUIRED_SCOPES` array (loaded via `load_required_scopes()`, lines 24-39) — correctly parses `vars/github-required-scopes.yml` with a small inline Python/yaml snippet. Compliant.
- `fedora-install/push.bash`, `fedora-install/pull-projects.bash`, `files/var/local/claude-yolo/entrypoint.sh`, `files/home/.local/share/vmtest/guest-acceptance-server-github-token.bash` — all call `gh auth status`/`gh auth login` but never name or request a specific scope set. Not instances.
- `docs/README.md`, `docs/playbooks.md`, `README.md` top-level mentions of "required scopes" — all point at `docs/github-multi-account.md` or the setup script without restating the list. Not instances.
- `helpers/github443/*`, `tests/helpers/github443/*` — unrelated (SSH-over-443 fallback), no scope content.
- No `helpers/github_scopes/` or `tests/helpers/github_scopes/` directory currently exists in the tree (checked, per the dispatch's "if they exist" caveat) — so no files were skipped there.

## What text search found that reading could not have

- The exact 8-way and 3-way duplication counts (items 1-11): only a literal grep for `admin:public_key`, `case "$permission"`/`case "$required"`, and `github-required-scopes` across every file type surfaced how many independent restatements exist; reading any single file would have looked locally reasonable (each site cites the SSoT file by name) and hidden that seven *other* sites do the same thing with slightly different wording.
- The byte-identical three-way duplication of the hierarchy table (item 1-3) — grepping for the distinctive tokens (`admin:org`, `read:repo_hook`) across `.bash`/`.yml` found all three occurrences in one pass.

## What reading found that text search could not have

- That `run.bash` never actually parses `vars/github-required-scopes.yml` — only text search's hits were prose *mentioning* the filename; reading confirmed there is no `cat`/`python`/`yq` read of that file anywhere in `run.bash`, so its one direct scope check (`admin:public_key`, line 2533) is genuinely free-floating, not merely quoting the SSoT.
- That `docs/configuration.md`'s `admin:ssh_signing_key` mention is not accidental drift but a deliberately-scoped gap — the doc says outright "which the play's other scopes do not include" — and that the companion plan (`CLAUDE/Plan/00139`) has an open, unchecked task to fold it into the shared audit. Only reading `PLAN.md`'s task tree revealed this is *known, in-flight* drift, not yet-undiscovered drift.
- That the three hierarchy-table copies (run.bash / play-github-cli-multi.yml / gh-account-setup.bash) are currently byte-identical — confirmed only by reading and comparing all three bodies side by side, which a text search reporting "3 matches" would not show.

## Searches run

- `grep -rIl "scope"` across `*.yml,*.yaml,*.bash,*.sh,*.py,*.md,*.j2` (broad sweep, mostly noise from unrelated "scope" usages — systemd scopes, Ansible play `scope:` classification, variable scope, ptrace_scope).
- `grep -rn "\-\-scopes\|gh auth login\|gh auth refresh\|gh auth status"` (found every gh-auth call site).
- `grep -n "scope\|github_required_scopes"` inside `playbooks/imports/play-github-cli-multi.yml`.
- `grep -n "REQUIRED_SCOPES\|scope_satisfied\|admin:\|write:\|read:org\|read:user\|hierarchy\|implies"` inside `scripts/gh-account-setup.bash`.
- `grep -rn "admin:public_key\|admin:ssh_signing_key\|user:email\|read:org\b\|admin:org\b\|write:org\b"` across the whole tree (excluding named exclusions), the search that surfaced the 8-way `+ admin:public_key` duplication and the `admin:ssh_signing_key` sightings.
- `grep -n -i "scope"` targeted at each doc file individually (`docs/github-multi-account.md`, `docs/playbooks.md`, `docs/installation.md`, `docs/architecture.md`, `docs/ccy.md`, `CLAUDE/AgentNotes.md`, `CLAUDE.md`, `files/etc/profile.d/gh-multi-profile.sh`, `files/var/local/claude-yolo/**`) to rule false positives in/out.
- `grep -rn -i "oauth scope\|token scope\|PAT.*scope\|scope.*PAT\|X-Oauth-Scopes\|scopes:"` across the whole tree, filtered down to GitHub-relevant hits — this is what surfaced `scripts/qa-gh-scopes.bash` (not opened) and confirmed no further doc/script hits beyond what was already found.
- `find` for `*gh-account*`, `*scope*`/`*github*` under `helpers/`,`tests/helpers/` to confirm no stray implementation and that `helpers/github_scopes/` does not yet exist.
- Manual read-through of every hit's surrounding 15-30 lines (technique two) for: `run.bash` (×4 sites + the function + the direct check), `play-github-cli-multi.yml` (audit task + PhpStorm block), `gh-account-setup.bash` (load function + hierarchy function + hint message), `docs/github-multi-account.md`, `docs/configuration.md`, `docs/headless-provisioning.md`, `docs/headless-server-install.md`, `docs/vm-acceptance-testing.md`, `docs/run-bash-changelog.md`, `CLAUDE/Plan/00139-commit-signing-everywhere/{PLAN.md,deploy.bash,acceptance.bash}`, `CLAUDE/Plan/00035-gh-multi-account-hardening/{DECISIONS.md,PLAN_archive.md}`, `vars/github-required-scopes.yml` (the SSoT itself, read once for vocabulary/baseline, per dispatch not excluded from reading, only from being counted as an instance).

## Summary count

- **High confidence, current, live-code/doc instances: 13** (items 1-13 — three duplicate hierarchy implementations, eight duplicate "+admin:public_key" restatements plus the free-floating run.bash check, and docs/github-multi-account.md's duplicate table + partial-list prose).
- **High confidence, current, new-scope-not-yet-in-SSoT instances: 2** (items 14-15 — `docs/configuration.md`, `CLAUDE/Plan/00139/deploy.bash`, both naming `admin:ssh_signing_key`).
- **Medium confidence, live plan prose: 1** (item 16, `PLAN.md`).
- **Low confidence / historical, listed for completeness: 2** (items 17-18).
- **Total distinct sightings: 18**, across 3 bash scripts/inline-shell blocks with duplicate scope-hierarchy logic, and 6 files (`run.bash` ×4 sites, `gh-account-setup.bash`, `docs/headless-provisioning.md`, `docs/headless-server-install.md`, `docs/vm-acceptance-testing.md`, `docs/github-multi-account.md` ×2, `docs/configuration.md`, `CLAUDE/Plan/00139-commit-signing-everywhere/{PLAN.md,deploy.bash}`, `docs/run-bash-changelog.md`, `CLAUDE/Plan/00035-gh-multi-account-hardening/{DECISIONS.md,PLAN_archive.md}`) stating scope names/lists.
