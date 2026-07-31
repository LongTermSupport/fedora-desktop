---
paths:
  - "scripts/qa-*.bash"
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

## ruff is version-pinned

`ruff.toml` deliberately does **not** enumerate `select`, so the enforced ruleset is ruff's
default set — which grows every release. `/.ruff-version` is the single source of truth,
read by `.claude/ccy/Dockerfile` and `.github/workflows/qa.yml` and **asserted** by
`scripts/qa-python.bash`. Bumping it means owning the triage of whatever the new defaults
add, and rebuilding the ccy image.

## Known gap

**These gates have no tests of their own.** Every fix so far was proven with a hand-built
fixture that was then discarded. If you are here to change one, consider whether the
fixture should become permanent.
