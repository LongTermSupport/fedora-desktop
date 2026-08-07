# Agent Notes — Working Practices and Project Gotchas

This file holds working-practice notes and project gotchas accumulated while
working on this repository. It was previously kept in Claude Code's file-based
agent memory; it has been consolidated here so the knowledge is tracked,
reviewable, and shared. Items already covered by other tracked docs are listed
under [Already Documented Elsewhere](#already-documented-elsewhere) with a
pointer rather than restated.

---

## Working Practices

How to work effectively on this repo (the `feedback`-type notes).

### Never assume or hallucinate — decide only from grounded, triaged facts

Every data point that drives a decision or a claim must be **confirmed from a
real source**, never inferred, filled in, or narrated. This was learned the hard
way on Plan 00062: a "full disk" cause was invented and then repeated as fact
across a playbook, journal, and commit messages — the user had never had a full
disk. Confident-sounding narrative is not evidence.

**Why:** an unfounded cause sends the whole fix in the wrong direction and
destroys trust. On a live, heavily-loaded host (the incident touched 6 running
CCY sessions + 2 container stacks) acting on a guess is dangerous.

**How to apply:**

- **Establish ground truth with plan-local triage scripting**, not from memory.
  Write a read-only, re-runnable `triage.bash` in the plan folder that gathers
  the exact data points a decision needs. Have it **write its own report into
  that same plan folder** (`CLAUDE/Plan/NNNNN-name/logs/`, resolved from
  `BASH_SOURCE[0]` so it survives archiving). Being inside the repo means the
  CCY bind-mount makes it readable by the agent at the same repo path (no
  copy-paste of terminal output); `CLAUDE/Plan/**/logs/` is gitignored so live
  host state never reaches this public repo. **Not** a shared repo-wide reports
  tree — plan output is plan-local, like every other plan artifact. See
  [PlanTriage.md](PlanTriage.md).
- **Separate fact from hypothesis, always.** Keep confirmed facts and unconfirmed
  hypotheses in different buckets in the plan; never let a hypothesis harden into
  an asserted cause.
- **Under uncertainty, extend triage — don't guess.** If a needed data point is
  missing, add a probe to `triage.bash` and have the user run it again. The user
  can run it as many times as needed; iterate until the picture is complete.
- **Confirm outcomes before claiming them.** "Fixed"/"works" is stated only after
  triage confirms it (e.g. `podman system df` rc=0 in the report), never assumed
  from having applied a change. See also [triage vs verify below](#triage-is-fact-finding-verifyacceptance-is-the-pass-fail-gate).

### Triage is fact-finding; verify/acceptance is the pass/fail gate

Keep the two plan-local roles distinct (user steer, Plan 00062): **`triage.bash`**
= establish grounded facts (gather + log, no verdict); the **confirm-things-are-OK**
pass/fail gate is separate — the repo's documented trio is
`deploy.bash`/`triage.bash`/`acceptance.bash`, so that gate is `acceptance.bash`
(`verify.bash` is an acceptable synonym). A verdict ("store loads ✅") belongs in
the acceptance/verify gate, **not** in triage.

### Always fetch PR comments, not just the body

When asked whether a PR is up to date, to review it, or to sync its state, pull
the **full comment thread** in addition to the body — stale comments count as
outdated PR state.

- `gh pr view <N> --json body,comments` returns body **and** comments.
- `gh api repos/<owner>/<repo>/issues/<N>/comments` returns the full comment
  timeline.

**Why:** A PR body can be updated while an earlier comment still lists completed
phases as "Next phases" — leaving the PR self-contradictory and out of date.

**How to apply:** Whenever the task involves PR review, merge-readiness, status
sync, or "is this up to date", fetch comments as part of the check. If one of
your own earlier comments is now stale, edit it in place
(`PATCH repos/<owner>/<repo>/issues/comments/<id>`) rather than posting a
follow-up correction.

### Review PRs holistically against the full IaC system

This repo is a full Infrastructure-as-Code system: `playbooks/playbook-main.yml`
declares the complete machine state, so any "is X installed?" question already
has a known answer from the playbook source. Evaluate every change against that
whole system, not as an isolated surgical edit.

**Why:** A PR that probes runtime state (e.g. `command -v docker` to decide
whether to apply Docker-specific rules) is treating IaC like a guess. The repo
*knows* what is installed because it installs it. Probe-then-conditional patterns
hide that knowledge and create drift between what the repo declares and what its
plays act on.

**How to apply:**

- Before reviewing any PR, read `playbooks/playbook-main.yml` to understand what
  is unconditionally installed and in what order.
- Reject patterns that probe for known-installed software instead of
  asserting/depending on it.
- Check install ordering: if play A depends on play B's state, A must run after B
  in `playbook-main.yml`. Flag any new play that is order-sensitive but
  misplaced.
- Prefer gating new behaviour on a `host_vars` variable (e.g. `container_engine`,
  `use_docker`) over runtime probing.
- When a play has the right behaviour but the wrong placement in the IaC graph,
  fix it by **reordering imports in `playbook-main.yml`** — do not extract the
  behaviour into a new play. Reordering is one line; a new play is bureaucratic
  separation that splits semantically related concerns across files. Only create
  a new play when the work has a genuinely independent lifecycle (different
  `become`, host group, or opt-in story).
- Remember rootless Podman: it uses `slirp4netns`/`pasta` and does **not** touch
  host iptables, so behaviour that breaks under Docker (DOCKER-USER chain,
  FORWARD policy) does not break under Podman. If a PR mentions "container
  engines", check whether the issue is Docker-specific (usually it is) or
  general.

### Scrub identifiers before posting to any public/external surface

Before any `gh issue create/edit`, `gh pr create/edit`, `gh gist create`, web
paste, or third-party renderer, scrub any identifiers sourced from
`environment/localhost/host_vars/localhost.yml` (or related vars files).

**Why — the gap:** The git `pre-commit`/`commit-msg` hooks only fire on
`git commit`. They do **nothing** for `gh` CLI invocations, `curl`, `wget`, or
web pastes, so a real alias pasted into a public issue body sails straight
through. This is a public repository. GitHub issue edit history preserves prior
bodies under the "edited" dropdown, so a post-hoc scrub-edit does **not**
retroactively redact the original — prevention beats cure.

**How to apply:**

- Before any external-post command, check: does this body contain any string
  that also appears in `localhost.yml`? If yes, scrub first.
- Scrub by default: every key and value in `github_accounts` / `project_personas`,
  every Cloudflare/AWS account ID, every email, every path under a real home
  directory. Replace with generic placeholders (`<alias-a>`, `<gh-username-a>`,
  `<account-id>`) per `CLAUDE/ExampleValues.md`.
- Treat `CLAUDE/Plan/` as public — it lives in the same public repo. Plans that
  quote `localhost.yml` content need the same scrubbing; use one or two generic
  illustrative entries, never a real multi-account dump.
- Two repo identifiers are intentionally public and OK to use: the maintainer's
  public handle and the public org/owner that owns the repo. Other aliases
  default to private.

### Never squash-merge PRs

Never use squash-merge when landing PRs in this repo. Use a **merge commit**
(preserves branch shape) or **rebase merge** (linear, still per-commit). When
merging a branch directly without a PR, use `git merge --no-ff` or a
fast-forward — never hand-recreate a "squashed equivalent" commit.

**Why:** Squash-merge rewrites the whole feature branch into one new commit with
a different SHA. Git then cannot recognise the feature branch as "merged":
`git branch -d` refuses to delete it, and any new commits on top become
content-duplicates of what is already on the target. This produced a painful
divergence (every overlapping file seen as a conflict, cleanup requiring
SHA-blind cherry-picks) on a prior plan branch.

**How to apply:**

- In the `gh pr` merge UI, choose **Create a merge commit** or **Rebase and
  merge**, never **Squash and merge**.

- Merging locally: `git merge --no-ff <branch>` so the branch structure stays
  visible (fast-forward is fine for a branch only a few commits ahead).

- Repo-level enforcement removes the footgun entirely:

  ```bash
  gh api -X PATCH repos/<owner>/<repo> \
    -F allow_squash_merge=false \
    -F delete_branch_on_merge=true
  ```

- If you catch yourself about to "manually squash" by re-committing everything as
  one commit, stop — that recreates the exact failure mode.

### A confirmed "no reboot-free fix exists" finding must be reported, never routed around

When research concludes a problem genuinely has no non-disruptive fix (e.g. no
reboot-free/logout-free recovery on Wayland), the correct output is a clear
diagnosis and user notification — **not** an automated mechanism that performs
the disruptive action anyway, even opt-in and default-off.

**Why:** during Plan 00056 (DisplayLink dock hotplug recovery), research
confirmed GNOME/mutter has no reboot-free or logout-free way to clear a
corrupted monitor-manager state on Wayland. The first implementation draft
still added an opt-in `displaylink_recovery_force_logout` flag that, when
enabled, would auto-detect the corruption via a journal grep and run
`loginctl terminate-session` to force a logout. The user rejected this
immediately: *"you have found there's no way to fix without logout so instead
of confirm[ing] that ... you are going to build an automated log out into the
playbook?? hmm fuck no."* Gating a destructive, hard-to-reverse action
(killing every app in a session) behind a default-off flag does not make it
safe to have built at all — it's still IaC quietly offering to automate a
human decision that should never be automated. The fix was to delete the
mechanism entirely (not just default it off): no `Action.FORCE_LOGOUT`, no
CLI flag, no executor function — only a passive desktop notification.

**How to apply:** when a finding is "X can only be fixed by a disruptive,
hard-to-reverse action," stop at reporting/notifying. Do not design an
automated path to perform X, regardless of default state or opt-in gating.
This is a specific instance of the general "Executing actions with care"
principle, but worth calling out because "make it opt-in" can *feel* like it
satisfies that principle when it does not.

---

## Project Gotchas

Repo-specific traps and conventions (the `project`-type notes).

### Ansible 2.19 shell-block parser checks quote balance across comments

This repo's host runs `ansible-core 2.19+`. Its argument splitter does a
pre-flight scan of the **entire** `shell` module content for balanced quotes
(`'` and `"`) and balanced Jinja2 blocks. It is **not** bash-aware — it does not
know that `#` starts a comment.

**Why:** Any unbalanced single quote anywhere in the shell content — even inside
a `# comment` line bash would ignore — fails the task at load time with:

```
[ERROR]: Error loading tasks: failed at splitting arguments,
either an unbalanced jinja2 block or quotes
```

**How to apply** when editing `ansible.builtin.shell: |` blocks (or any module
through the argument splitter):

- Avoid apostrophes in `# comments` (contractions, possessives).
- Avoid backticks in `# comments`.
- Avoid unbalanced single quotes anywhere in the content, even where bash would
  treat them as literal.
- Prefer plain Jinja substitution `{{ var | join(' ') }}` over
  `{% for %}…{% endfor %}` control blocks inside shell content — substitution is
  less parser-sensitive.

**Diagnosis:** `ansible-playbook --syntax-check path/to/play.yml` reproduces the
error locally without running anything. Bisect by reducing shell content until
the error disappears, then add chunks back.

### Ansible 2.19 rejects colon-space-dash patterns in unquoted task names

Under `ansible-core 2.19+`, a `: -` pattern (colon, space, then a dash-prefixed
flag) inside an **unquoted** task `name:` is parsed as a nested mapping key:

```
[ERROR]: YAML parsing failed: Colons in unquoted values must be followed
by a non-space character.
```

**The trap:** PyYAML accepts the same file, so `yaml.safe_load(...)` and
`./scripts/qa-all.bash` (which uses Python parsers) do **not** catch this — the
failure only appears at `ansible-playbook` time on the host.

**How to apply:**

- In task `name:` strings, avoid `<word>: <dash><letter>` patterns
  (e.g. `name: Add user to mock group (effective via become_flags: -i)`).
  Watch for documenting CLI flags or directive names with a value:
  `become_flags: -i`, `--root: foo`, `state: -1` all trip it.
- Either quote the whole name (`"…"` / `'…'`) or rephrase to drop the literal
  `key: value` substring.

**Diagnosis:** Always run `ansible-playbook --syntax-check path/to/play.yml`
before committing a playbook, even if `qa-all.bash` passes — it is fast and
catches the 2.19-specific scanner errors PyYAML misses.

### Ansible 2.19 self-default vars recurse at runtime — `--syntax-check` misses it

A var defined as its own default — `foo: "{{ foo | default([]) }}"` — used to
work via lazy evaluation (the value was either an override from `-e`/host_vars or
the default). Under `ansible-core 2.19+` it aborts the moment the template is
finalized with:

```
[ERROR]: ... Error while resolving value for 'argv':
Recursive loop detected in template: maximum recursion depth exceeded
```

**The trap — this one is nastier than the other two 2.19 gotchas:**
`ansible-playbook --syntax-check` parses structure but does **not** evaluate
templates, so it passes the broken playbook clean. The recursion only fires at
**runtime**, during task-arg finalization (e.g. templating a `command`'s `argv`).
So both `qa-all.bash` **and** `--syntax-check` go green and it still explodes on
the host. (This is now caught by a dedicated `qa-ansible.bash` self-default grep,
added precisely because neither existing gate could.)

**How to apply:**

- Never write `x: "{{ x | default(...) }}"`. To make a var optional, **drop the
  play-var redeclaration entirely** and apply `| default(...)` at the point of
  use: `"{{ x | default([]) | join(',') }}"`. host_vars / `-e` still override.
- A bare `x: "{{ x }}"` inside a `block: |` literal (e.g. a `blockinfile` writing
  host_vars from a `set_fact`) is **fine** — that is literal text templated into
  another file, not a self-reference; the QA grep deliberately only flags the
  `| default` idiom to avoid false-positiving on it.

**Diagnosis:** `./scripts/qa-ansible.bash` now flags it statically; to confirm a
fix actually resolves at runtime, template it in a throwaway play
(`vars: {x: "{{ x | default([]) }}"}` → recursion; point-of-use default → clean).

### Pre-commit secret scanner flags any non-example.com email

The `scripts/git-hooks/pre-commit` secret scanner rejects the commit if **any
staged file** contains an email-like string whose domain is not allowlisted.
`example.com` is allowlisted; generic-looking placeholders on any other domain
are **not** — a local-part against `company.com`, `email.com`, or a real consumer
mail domain is flagged as "Email address" and blocks the commit. (Those non-
allowlisted forms are deliberately not written out literally here: the scanner
scans this file too and would block it.)

**The trap:** It scans the **full content of staged files**, not just the diff.
Editing a doc for an unrelated reason can surface a pre-existing placeholder
email elsewhere in that file and block you.

**Why:** The repo is public; the hardened regex deliberately errs toward catching
real personal addresses, at the cost of false positives on
non-`example.com` placeholders.

**How to apply:** Always use `@example.com` for email examples in docs/configs
(`you@example.com`, `work@example.com`). If a commit is blocked on a pre-existing
placeholder in a file you touched, fix it to `example.com` rather than reaching
for `--no-verify` — it is now in your staged set. See also
[Scrub identifiers before posting](#scrub-identifiers-before-posting-to-any-publicexternal-surface)
and `CLAUDE/ExampleValues.md` for the full placeholder schema.

### CI clean-checkout requirements (.github/workflows/qa.yml)

CI runs `./scripts/qa-all.bash` (six stages) plus a gitleaks job. A clean CI
checkout lacks three things a local CCY container has, so the workflow must
provide them:

1. **`vault-pass.secret` must exist.** `ansible.cfg` sets
   `vault_password_file=./vault-pass.secret` (gitignored). The
   `qa-ansible-syntax` stage's `ansible-playbook --syntax-check` errors with
   "vault password file not found" at startup even though it never decrypts. CI
   writes a throwaway placeholder before QA.
2. **`ansible-galaxy install -r requirements.yml`** must install the **role +
   collections**, not just collections — syntax-check must resolve the
   `lts.vault-scripts` role.
3. **`roles/` is absent on a clean checkout** (`roles/vendor/*` is gitignored, no
   tracked roles). `qa-ansible.bash` builds its grep search-list from dirs that
   exist, so a missing `roles/` is not a hard error — keep it that way.

**gitleaks:** Use the free OSS **binary** (`gitleaks dir .`), **not**
`gitleaks/gitleaks-action@v2` — the action requires a paid `GITLEAKS_LICENSE` for
org-owned repos. Scan the **working tree**, not full history (history retains
accepted PII, which would red-fail a history scan).

**Push-bypass:** The repo owner has push-bypass on the PR-protected `F*` branches
(a direct `git push` works and prints "Bypassed rule violations").

### project_personas YAML uses flat, full-name platform keys

In `project_personas` config and downstream playbooks, each persona has a `name`
field plus N platform keys at the **top level** — no `tools:` wrapper:

```yaml
project_personas:
  <alias-a>:
    name: "Display Name A"
    github:
      username: <gh-username-a>
      use_for_orgs: [<org>]
    cloudflare:
      account_id: "<account-id>"
      account_name: "<account-name>"
    amazon_web_services:
      account_id: "<account-id>"
```

**Three rules:**

1. **No `tools:` wrapper.** Platform names do not collide with metadata fields
   (`name`, `email`, `default`, …), so nesting is YAGNI. The playbook enumerates
   platforms by treating any persona key not in the known-metadata list as a
   platform.
2. **Key by the platform, not the CLI binary.** Multiple CLI binaries can target
   one platform and share the persona's credentials, so key by the platform.
3. **Use the full platform name, no abbreviations.** A few extra tokens buy much
   more clarity for humans and LLMs.

| Use                     | Don't use        | Covers (CLI binaries sharing this identity)         |
| ----------------------- | ---------------- | --------------------------------------------------- |
| `github`                | `gh`, `git`      | `gh`, `git`, SSH key auth, clone helpers, gh tokens |
| `cloudflare`            | `wrangler`, `cf` | `wrangler`, future `cloudflared`, `flarectl`        |
| `amazon_web_services`   | `aws`, `aws-cli` | `aws`, `cdk`, `sam`                                 |
| `google_cloud_platform` | `gcp`, `gcloud`  | `gcloud`, `gsutil`, `bq`                            |
| `microsoft_azure`       | `azure`, `az`    | `az`, `azd`                                         |
| `npm_registry`          | `npm`            | `npm`, `pnpm`, `yarn` (publish auth)                |
| `docker_hub`            | `docker`         | `docker login`, `podman login` against hub          |
| `fly_io`                | `fly`            | `flyctl`                                            |

When in doubt, write the platform's full marketing/legal name in snake_case.
Field names *inside* a platform block may be CLI-specific where genuinely needed
(e.g. `cloudflare.api_token_keyring_key`); the rule applies only to the top-level
key.

---

## Already Documented Elsewhere

These memories duplicate guidance already in the tracked docs — pointers only,
no restatement:

- **Fail fast, never silently skip** → `CLAUDE.md` (#1 rule, "Fail Fast — HARD
  RULE") and `CLAUDE/InfrastructureAsCode.md`. The "skip-and-warn leaves broken
  state" rationale and the `failed_when: false` / `# FAIL-FAST-OK:` policy are
  fully covered there.
- **Per-Fedora-version default-branch model** (`F42`→`F43`→`F44`, one-line
  `vars/fedora-version.yml` bump, `gh repo edit --default-branch`, local
  `origin/HEAD` resync) → `docs/development.md` ("Branching Strategy" /
  "After Changing the Default Branch — Resync Local Clones"). Only the CI
  clean-checkout and gitleaks specifics from that memory were new and are written
  out above.
- **Use reserved example placeholders** (RFC 5737 IPs, `example.com` emails,
  `<user>` paths) → `CLAUDE/ExampleValues.md` and `CLAUDE/SecurityRules.md`. The
  pre-commit-specific behaviour (full-file scan, non-`example.com` rejection) was
  the new part and is written out above under Project Gotchas.
