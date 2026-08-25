---
paths:
  - "scripts/qa-*.bash"
  - "helpers/docs/**"
  - "ruff.toml"
  - ".ruff-version"
  - ".semgrep/**"
  - ".github/workflows/**"
---

# You are editing a QA gate — these guard every commit

Full reference: [CLAUDE/QA.md](../../CLAUDE/QA.md)

A gate here is what `CLAUDE.md` makes **mandatory** before every Bash/Python/Ansible commit.
A bug in one is worse than a bug in the code it checks, because it is silent: the gate
returns a confident exit code either way.

## The two failure modes this repo has actually hit

- **A gate that scans nothing and reports a pass** (Plan 00067). `find -path` matches the
  **whole** printed path, so an unanchored `*/untracked/*` excluded an entire checkout —
  this repo is vendored at `untracked/repos/fedora-desktop` inside lts-infra. 112 bash files
  went unscanned, exit 0 throughout.
  → **Anchor repo-root-relative exclusions to `$REPO_ROOT`.** Leave genuinely any-depth ones
  (`.git`, `node_modules`, `__pycache__`, `.venv`, `venv`) unanchored.
  → **Never remove a zero-file guard** to make a gate "work" somewhere. Zero is the signal.
- **A gate that scans the wrong things** (Plan 00071). `qa-python.bash` had two discovery
  passes and only one carried the venv exclusions, so it linted third-party `pip` scripts.
  `qa-ansible.bash` matched a *comment* documenting the removal of `ignore_errors: true`.
  → **Keep multi-pass discovery on one shared exclusion list.**
  → **Strip comments before matching** a source pattern — but check any annotation that
  *lives* in a comment first.

## Prove a change with a control that could have failed

A gate turning green proves nothing on its own. Build a fixture and check that the gate
**discriminates**: the bad case flags, the good case does not, and the near-miss cases
(annotated, or with an unrelated trailing comment) each behave correctly. A uniform failure
across unrelated assertions means you broke the fixture, not that the checks work.

## ruff's ruleset is explicit, and the version is still pinned

`ruff.toml` enumerates `select` explicitly (`E4`, `E7`, `E9`, `F`) so the enforced ruleset does
NOT drift with ruff's own default set (Plan 00071 — an unenumerated default once turned `main`
red with no commit behind it, at 350 rules more than the comment claimed). `/.ruff-version` is
the single source of truth, read by `.claude/ccy/Dockerfile` and `.github/workflows/qa.yml` and
**asserted** by `scripts/qa-python.bash` — a version bump can still change how the same selected
rules behave, so it is pinned too. Bumping it means owning the triage of whatever changes, and
rebuilding the ccy image.

## Known gap — now partly closed

Most gates still have **no tests of their own**; fixes were proven with hand-built fixtures
that were then discarded. If you are here to change one, consider whether the fixture should
become permanent.

`qa-docs.bash` is the exception and the model: its logic lives in the stdlib-only helper
[helpers/docs/link_check.py](../../helpers/docs/link_check.py) with 42 unit tests at
`tests/helpers/docs/test_link_check.py`, which CI runs. Its slug cases are pinned against
anchors observed working in rendered documents — because that checker's **first**
implementation was wrong in the same way as the defects it hunts, and under-reported by 32.
