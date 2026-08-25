# Plan 00071: qa gate correctness

**Status**: Complete
**Created**: 2026-07-31
**Owner**: joseph
**Priority**: High

## Overview

`./scripts/qa-all.bash` — the gate `CLAUDE.md` makes **mandatory** before every Bash/Python/Ansible
commit — exited **1 on a clean tree**. Three separate defects, all found while running the gate for
an unrelated plan. All three are fixed and the gate is green.

Every defect is the same shape as Plan 00067's: **a gate whose answer was wrong while its exit code
looked authoritative.** 00067 fixed gates that checked *nothing*; these checked the *wrong things*.

## Goals

- `./scripts/qa-all.bash` exits 0 on a clean tree.
- Each fix proven by a control that could have failed, not by the gate merely turning green.

## Non-Goals

- Enumerating a `select` list in `ruff.toml` — deliberately rejected; see Decision 1.
- Fixing the 105 advisory shellcheck findings (`warning`/`info`/`style` do not gate).

## The three defects

| #   | Gate                                    | Defect                                                                      |
| --- | --------------------------------------- | --------------------------------------------------------------------------- |
| D1  | `scripts/qa-ansible.bash:70-76`         | Flagged a **comment** documenting the *removal* of `ignore_errors: true`    |
| D2  | `scripts/qa-python.bash` (two passes)   | Linted third-party venv binaries and a vendored file                        |
| D3  | ruff unpinned across **three** installs | The gate's strictness was set by whichever version happened to be installed |

## Tasks

### Phase 1 — D1: the fail-fast check punished documentation

- [x] ✅ **Task 1.1**: `grep -rni` scanned raw lines with no YAML comment stripping, so
  `play-systemd-user-tweaks.yml:242` — a comment reading *"old `ignore_errors: true` headless escape
  hatch (removed — a failure here now means the manager really is broken)"* — was reported as a
  violation. **A gate that punishes writing down why an anti-pattern is absent teaches people to
  delete the explanation.**
- [x] ✅ **Task 1.2**: Strip YAML comments before matching; look for `FAIL-FAST-OK` on the **full**
  line first, because that annotation itself lives in a comment.
- [x] ✅ **Task 1.3**: Proven with four controls in a fixture tree — all four behaved correctly:
  comment-only → not flagged; real directive → flagged; real + `# FAIL-FAST-OK:` → not flagged;
  real + unrelated trailing comment → flagged. The last two matter most: the annotation lives *in*
  a comment, and a real directive can *have* a comment — a naive strip breaks both.

### Phase 2 — D2: the Python gate linted code this repo does not own

- [x] ✅ **Task 2.1**: `qa-python.bash` had **two** discovery passes. The `.py`-extension pass
  excluded `venv`, `.venv` and `__pycache__`; the executable-shebang pass **did not**. So
  `.claude/untracked/venv/bin/pip` — a third-party console script with a `#!…python` line and no
  `.py` extension — was linted as repo-owned code. The same directory being *half*-excluded is
  exactly why the gap was invisible.
- [x] ✅ **Task 2.2**: Hoisted the exclusions into one `PY_EXCLUDES` array used by **both** passes,
  so they cannot drift apart again.
- [x] ✅ **Task 2.3**: Broadened `.claude/ccy/plugins` + `.claude/ccy/file-history` to
  `.claude/ccy/*`, and added `.claude/skills/*`, matching `qa-bash.bash:48-50`. The gate had been
  linting the vendored CCY supervisor that a daemon upgrade overwrites.
- [x] ✅ **Task 2.4**: 46 findings → 37, and all 9 removed were files this repo does not own.

### Phase 3 — D3: the gate's strictness was whatever ruff shipped that week

- [x] ✅ **Task 3.1**: `ruff.toml` does not enumerate `select`, so the enforced ruleset **is** ruff's
  default set — which grows every release. `ruff.toml:5` claimed *"Use default rule set (E, F, W)"*;
  0.16.0's actual default is ~415 rules including UP, BLE, SIM, DTZ and B. **The comment was wrong by
  roughly 350 rules**, and that is what let an unpinned ruff turn main red with no commit behind it.
- [x] ✅ **Task 3.2**: Verified with `ruff check --isolated` that the broad set is ruff's own default
  and **not** a config leaking in from the outer lts-infra checkout — which was my first hypothesis,
  and was wrong.
- [x] ✅ **Task 3.3**: `/.ruff-version` is now the single source of truth, read by
  `.claude/ccy/Dockerfile` and `.github/workflows/qa.yml`.
- [x] ✅ **Task 3.4**: `qa-python.bash` **asserts** the installed version matches. This is the part
  that matters: ruff arrives three ways and the third (`play-python.yml` → `dnf: ruff`) tracks
  whatever Fedora ships and cannot be pinned from this repo. The assertion converts that unpinnable
  divergence from *silent* into *loud*.
- [x] ✅ **Task 3.5**: Proven by a control — a deliberately wrong pin exits **2** naming both values;
  restoring it exits **0**.

### Phase 4 — the 37 real findings

- [x] ✅ **Task 4.1**: `EXE001` ×7 — exec bits set. Checked first that every helper is invoked as
  `python3 -m helpers.X.Y` (`play-displaylink.yml:270`, `play-python.yml:100`, `qa.yml:102`, …) and
  never by path, so this changes no behaviour.
- [x] ✅ **Task 4.2**: `PLW1510` ×2 — `check=False` made **explicit** in
  `helpers/gnome/verify_extension.py`. `check=True` would have been wrong: a non-zero exit there
  means "no GNOME session", which the function reports as `None`. The returncode is checked on the
  very next line — probe-then-check, not error hiding.
- [x] ✅ **Task 4.3**: `SIM117` ×6, `SIM201`, `RUF059` ×5, `C408`, `RUF046`, `RUF012` ×2, `I001` ×2,
  `UP045` ×7, `SIM113` — all fixed in source. **161 helper tests pass**, proving the `with`-statement
  rewrites are behaviour-preserving.
- [x] ✅ **Task 4.4**: `BLE001` ×2 — exempted in `ruff.toml`, **not** "fixed", because the code is
  correct. See Decision 2.

### Phase 5 — verify

- [x] ✅ **Task 5.1**: `./scripts/qa-all.bash` exits **0**. 432 files checked.

## Technical Decisions

### Decision 1 — pin the version, do not enumerate `select`

Enumerating `select` would freeze the ruleset but rot silently against upstream, and it hides which
rules are actually enforced behind a hand-maintained list. Pinning the *version* keeps the ruleset
well-defined and makes any change to it a deliberate, reviewable act. **Date**: 2026-07-31

### Decision 2 — `BLE001` in `clip-scan` is exempted, and the reason is stated plainly

Both handlers are correct. `:471` records the exception into `last_error` and returns it — nothing
is swallowed — and must stay blind because rawpy/LibRaw and Pillow raise a wide, lazily-imported set
of types, so a narrow catch would abort a whole batch scan on the first unanticipated one. `:895`
prints a full traceback and exits 1.

This repo's actual rule is that errors are never **silently** suppressed; `BLE001` cannot see that
distinction. **This is an exemption for a deliberate design decision, not a cause external to the
repo** — recorded as such rather than dressed up as forced, so nobody later mistakes it for
precedent that any blind catch may be exempted. **Date**: 2026-07-31

### Decision 3 — a `noqa` was attempted and correctly blocked

Fixing `C408` I first reached for `# noqa: C408`. The daemon's `qa_suppression` handler blocked it,
correctly — the real fix was to write the dict literal the rule asked for, which took one edit.
Recorded because the reflex to suppress arrived before the reflex to fix. **Date**: 2026-07-31

## Dependencies

- **Found during**: Plan 00068 (Task 2.4) and Plan 00070. Neither is blocked by this.
- **Related**: Plan 00067 fixed gates that scanned *nothing*; this fixes gates that scanned the
  *wrong things*. Same class — and the gates still have no tests of their own; see Risks.

## Success Criteria

- [x] ✅ `./scripts/qa-all.bash` exits 0 on a clean tree.
- [x] ✅ Every fix proven by a control that could have failed.
- [x] ✅ No QA suppression annotation added to any source file.
- [x] ✅ 161 helper tests pass after the test-file rewrites.

## Risks & Mitigations

| Risk                                        | Impact | Probability | Mitigation                                                                                                                                         |
| ------------------------------------------- | ------ | ----------- | -------------------------------------------------------------------------------------------------------------------------------------------------- |
| The two `qa-python.bash` passes drift apart | H      | M           | Single `PY_EXCLUDES` array — they cannot now differ                                                                                                |
| A future ruff bump silently re-reddens main | H      | L           | Version asserted; a mismatch exits 2 naming both values                                                                                            |
| **The QA gates have no tests of their own** | H      | H           | **Unmitigated.** Every fix here was proven by a hand-built fixture that was then thrown away. Three gate defects across two plans says this recurs |

## Delivery & Milestones

<!-- Curated milestones + delivery commit hashes only (git is the SSoT for
     "when" — do not add dates). The blow-by-blow activity log lives in
     JOURNAL/00071-Journal-YY-MM-DD.md — see CLAUDE/PlanJournalling.md. -->

- Gate red → green; 46 ruff findings → 0
