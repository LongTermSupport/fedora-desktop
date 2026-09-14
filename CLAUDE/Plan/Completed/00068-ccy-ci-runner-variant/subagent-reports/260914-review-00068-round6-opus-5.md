# QA Review — Plan 00068 round 6: narrow verification of `dbd60ae4`

**Reviewer**: qa-reviewer (Opus 5) · **Date**: 2026-09-14 · **Read-only review**

**Verdict**: **FIX-BEFORE-MERGE** — one item, three one-line additions. Everything round 5 asked
for landed and is accurate.

Scope: verify that `dbd60ae4` discharges round 5's blocking 1–2, should-fix 3–6 and nits 7–11,
plus anything those edits broke, plus the sweep. Rounds 1–5 findings are not reopened.

## Round 5's findings — all eleven re-derived independently, all discharged

| #    | Item                                   | Verified                                                                                                                                                                                                                                                                                                                                                                                                                                                                     |
| ---- | -------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| B1   | `00113 PLAN.md:17-22` Overview         | Now "an **unmeasured** number of prompt sites — 00068 derived roughly six, all credential resolution, and Task 0.2 supersedes and measures that figure". Attributed, marked, points at the measurement. Nothing asserted falsely: it states what 00068 *derived*, not what is true.                                                                                                                                                                                            |
| B2   | `DECISIONS.md:75-83` §3 item 1         | **Both** clauses marked: "derived that the flow reaches about 6 of the 46, all credential resolution — **both clauses superseded by §6 below**". The note is accurate about §6: §6 does restore compose/networking as CI requirements, and the 13 figure is right — `ci-flow.md:97-102`'s four superseded rows total 14 sites, minus `:822` (config restore, carved out of (c)) = 13. "any that return are not credential resolution" matches `00068 PLAN.md:105`. Old "with six callers" replaced by "only its caller count is unknown". |
| SF3  | `ci-flow.md:110-113` named pair        | Correct. Against the census: token resolution = `claude-yolo:968 :992 :1013 :1104 :1121` → 5 YES; `select_token` = `token-management.bash:611` → 1 YES; migration is the "Probably" row. 5 + 1 = the `≈6`.                                                                                                                                                                                                                                                                    |
| n11  | "33 of the census's 46"                | Arithmetic checks out. The table's twelve rows sum to exactly **33** (4+4+1+4+2+2+7+1+1+5+1+1). The named absentees — (f)'s 5, (e)'s Dockerfile-authoring 4, debug-layer 3, container-manager 1 — are 13. 33+13 = 46. The (d)/(e) Dockerfile distinction is real, not a slip: census (d) holds `dockerfile-custom.bash:763`, (e) holds `:37 :117 :157 :718`.                                                                                                                   |
| SF4a | `DECISIONS.md:320-330` §7 items 1–2    | Both constructs grep to exactly what they claim. `container_cmd rm -f "$CONTAINER_NAME"` is **unique** in the launcher and the libraries — `claude-yolo:3014`, under the comment `# Safety net: remove any leftover container` at `:3011`. `save_launch_config` has exactly one definition (`:454`) and one call (`:2880` of 3183 lines — "tail-of-script" is fair).                                                                                                           |
| SF4b | `ci-flow.md:8-12` caveat               | The claim "**every** launcher number below has since drifted" is **literally true**, not an overstatement. All 38 launcher line numbers in the file were compared against the launcher at `e67abde2` (the commit that authored the report): every one was correct then, and **not one** still holds the same text today. Sample: `:41` was `set -e`, now a comment (`set -e` moved to `:73`); `:2767` was `--device /dev/dri:/dev/dri`, now `if [[ -n "$SELECTED_NETWORK" ]]`; `:1104` was `read -rp "Select option [1-3]: "`, now `read -rp "Create a new token now? (Y/n): "` — a plausible neighbour exactly as described. |
| SF5  | `00113-Journal-26-09-14.md:87`         | New entry, correct grammar, accurate on all four items. Checked against the current `PLAN.md`: Task 0.2 does say "Start from no number" and "roughly 6–19" (`:69-76`); the Overview change is as described; Task 0.4 does cite by construct (`:105-111`) and that construct resolves — the `exit 1` at `claude-yolo:2864` closes the branch printing `CCY_SKIP_NETWORK_PREFLIGHT=1 ccy` at `:2861`; Task 2.5's third bullet reads as described (`:211-218`).                    |
| SF6  | `00113 PLAN.md:199-201`                | Accurate. §6's unqualified numbers *are* `network-management.bash` (stated at `DECISIONS.md:299-301`) and its launcher references *are* constructs.                                                                                                                                                                                                                                                                                                                          |
| n7   | `DECISIONS.md:315-316` `stty`          | Correct as rewritten: `_CCY_STTY_SAVED=$(stty -g)` / `stty susp undef` at `claude-yolo:3000-3001`, restored by `stty "$_CCY_STTY_SAVED"` at `:1918`, which is inside `cleanup()` (opens `:1914`).                                                                                                                                                                                                                                                                            |
| n8   | 12:41 grammar                          | Handled correctly — a **new** 13:24 entry (`00068-Journal-26-09-14.md:361`), not an edit, using the file's own `## HH:MM · CATEGORY · — — title` form matching the twelve entries before it. Append-only respected.                                                                                                                                                                                                                                                          |
| n9   | `00068 PLAN.md:62`                     | `ci-tool-surface.md` added to the Context report list.                                                                                                                                                                                                                                                                                                                                                                                                                       |
| n10  | `00068 PLAN.md:148-149`                | `claude-yolo:2091` now reads "as the census numbered it — the compose prompt inside the cross-engine mismatch wizard". `00113 PLAN.md:215` already carried its own description.                                                                                                                                                                                                                                                                                              |

## The sweep — one finding

### 1. The pointers caveat stopped at `ci-flow.md`. Three live reports still carry stale launcher numbers with no caveat, and one of them now cross-references itself wrongly.

`ci-flow.md` and `DECISIONS.md:10-11` both have it. These do not:

- `reports/ci-required-config.md` — ~35 launcher citations, no caveat anywhere in 397 lines.
  24 of them were checked against today's tree: **every one is stale.**
- `reports/host-run-verdicts.md` — `:2773`, `:2703-2727`, both stale, no caveat.
- `reports/mcp-and-egress.md` — `:1254`, `:1343`, `:2792`, `:2613`, all stale. Its `:282` note says
  the report is "preserved as written (line numbers are cited by later review rounds)" — a
  preservation statement, not a staleness warning, and it sits at the end of the file.

The concrete hazard, not a hypothetical one. `ci-required-config.md:233` cites `claude-yolo:822` as
the *"Use same configuration? [Y/n]"* reuse prompt, default set at `:823`. At the census commit
`0dde4f0c` that was exact:

```
822:             read -rp "Use same configuration? [Y/n] " use_config
823:             use_config=${use_config:-Y}
```

Today `claude-yolo:822` is `print_error "--headless flag requires --prompt with content"` — which is
the construct **the same report cites at `:728-740`** (line 49, precondition J7; it was `:731` at
census time). The two citations have swapped onto each other's neighbourhoods, so a reader
spot-checking `:822` lands on live ccy code from the same document and concludes the citation holds.
This is the report `00113 PLAN.md:211` tells an implementer to read "precisely", and 00113 is
Not Started.

This is AgentNotes' *"a lesson written down beside the thing it fixed, never generalised"*, one file
further out again.

**Fix**: copy `DECISIONS.md:10-11` verbatim into the head of those three files. One line each, no
renumbering — the same minimum round 5 set for `ci-flow.md`.

### Sweep results otherwise clean

- **The prompt-site figure**: all twelve live files (both folders, excluding `PLAN_archive.md`,
  journals and subagent-reports) were grepped for twelve spellings — `≈6`, `~6`, `about 6`,
  `about six`, `roughly six`, `roughly 6`, `six credential`, `six callers`, `6 of the 46`,
  `six/6 sites`, `not 46`, `forty-six`, `six versus`. **No unqualified forwarding survives.** Every
  live occurrence is either the 46-site census figure (legitimate), or carries its supersession:
  `00068 PLAN.md:102-106`, `ci-flow.md:73/97-113`, `DECISIONS.md:75-83`,
  `00113 PLAN.md:18-19/70/74`. The only unqualified ones left are inside `ci-flow.md`'s struck
  section two lines above its own note — closed by round 5.
- **PLAN.md vs the reports it cites**: no contradiction found. `00068 PLAN.md:144-154`'s supersession
  inventory matches `ci-required-config.md` (one superseded row at `:284`, rationale correction at
  `:306`, `:2091` excluded with reason at `:314`) and `ci-flow.md` (two passages). `00113`'s Task 2.5
  and `ci-required-config.md:284-318` agree row for row.

## Nits

2. `ci-required-config.md:245` — the heading reads "**(e) Must fail fast — 35 sites**", but its eight
   rows list **30** sites, and 30 is what makes the document's own arithmetic work:
   1 + 4 + 4 + 2 + 30 + 5 = 46, the total stated at `:194`. "35" is the pre-expansion census total,
   still live in `:132` and the 26-07-29 journal. One digit. Pre-existing, unrelated to this commit,
   and it does not affect the new "33 of 46" claim.
3. `DECISIONS.md:329-330` — the §7 item 2 rewrite left two lines at 101 and 111 characters in a file
   that otherwise wraps prose at 100. Reflow.

## Checked and clean

- **Public-repo safety** — clean. No email, home path, RFC1918 address, hostname, container or
  project name in any line this commit added, across all seven files.
- **Plan Commit Rule** — clean. No modified or untracked file under either plan folder; branch level
  with `origin/F44` (0/0, pushed).
- **README index** — `CLAUDE/Plan/README.md:47` (00113, Active) and `:157` (00068, Active) both
  present and accurate.
- **CCY version bump** — not triggered; the diff is seven markdown files under `CLAUDE/Plan/`,
  nothing under `files/`.
- **Journal discipline** — both new entries append at the bottom, edit nothing above, and 13:24
  follows 12:41 monotonically.
- **Success criterion "The CI tool surface is specified per event"** — not re-derived this round;
  rounds 4 and 5 both confirmed it against `ci-tool-surface.md`, and nothing in this commit touches
  that report.

## Mechanical gates

- **`scripts/qa-all.bash`: FAILS — but not from this commit, and not from anything in the repo's
  committed state.** Every committed stage passed (bash 235, python 127, patterns, ansible,
  ansible-syntax 79 playbooks, js 7, docs 71, helper-tests 1023, and the ten suites after it). The
  single `✗` is a gate that is **not yet committed**: another session has unstaged edits to
  `scripts/qa-all.bash` and `scripts/check-pinned-versions.bash` plus four untracked files, adding a
  Plan 00109 version-pin manifest check. It fails on `vars/version-pins.yml names 'evdi_verzion',
  which playbooks/imports/optional/hardware-specific/play-displaylink.yml no longer declares` — the
  playbook declares `evdi_version` (`play-displaylink.yml:12`) and the untracked manifest at
  `vars/version-pins.yml:59` says `evdi_verzion`. **Worth telling that session**: until they fix it,
  `qa-all.bash` is red repo-wide for everyone, including whoever runs it before committing the 00068
  flip.
- **`hooks-daemon plan-qa --sweep`**: exit 1, **0 block / 2 advise** — 00046 path-existence and
  journal-freshness for twelve other plans. Neither 00068 nor 00113 appears.
- **`hooks-daemon docs-qa --sweep`**: exit 1, **0 block / 47 advise** — the 00068 entries are the
  nine deliberately-dead links in `00068-Journal-26-07-30.md` and one duplicate block in `26-07-29`.
  Identical to round 5; unchanged by this commit.
- **`ansible-playbook --syntax-check`**: not triggered, no playbook in the diff. `qa-all`'s
  ansible-syntax stage ran 79 playbooks green anyway.
- **`qa-helper-tests.bash`, `check_extension_compat`, extension ESLint**: not triggered (no
  `helpers/`, `tests/helpers/`, `extensions/` or `metadata.json` in the diff). `qa-all` ran
  helper-tests (1023) and extension-compat (4) green regardless.

## The decision asked for

**Not yet — but it is one commit away, and that commit is three sentences of copy-paste.**

Minimum remaining, in a single commit:

1. Add the `DECISIONS.md:10-11` caveat to the head of `reports/ci-required-config.md`,
   `reports/host-run-verdicts.md` and `reports/mcp-and-egress.md`. Required because the plan freezes
   as the specification an unstarted 00113 will act on, and one citation demonstrably now points at
   another citation's code in the same document.
2. Tick `00068 PLAN.md:158` and flip `:3` to `Complete` **in that same commit** — the plan-QA edit
   hook blocks the final tick under an `In Progress` header.

Optional, cheap, same commit: the `35` → `30` heading fix at `ci-required-config.md:245`, and the
reflow at `DECISIONS.md:329-330`.

Nothing from rounds 1–5 is outstanding. No defect was introduced by this commit's own edits.

## Review hygiene

Nothing in the working tree was modified by this review except this report file. Sweep output went
to `/tmp/echd-captures/`.
