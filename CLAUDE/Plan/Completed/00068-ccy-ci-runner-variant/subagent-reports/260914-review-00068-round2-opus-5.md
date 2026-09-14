# QA Review — Plan 00068 closing work, round 2 (`94dee731`)

**Reviewer**: qa-reviewer (Opus 5) · **Date**: 2026-09-14 · **Read-only review**

**Verdict**: **BLOCK** — do not mark Complete yet. All 14 round-1 findings are genuinely
discharged; one already-ticked success criterion is provably false, and the fix for round-1's
finding 1 reintroduced a weaker version of its own defect.

## Blocking

### 1. Success Criterion `PLAN.md:141` is ticked ✅ and is false — a surviving report specifies the opposite of Decision §6, and Phase 2b just made the contradiction live

`reports/ci-required-config.md` §4.3(f) `:276-285` specifies CI must **not** start compose
(`_do_compose_start` → "do not start; a job that needs compose starts it before invoking `ccy`")
and closes: *"No compose opt-in flag is specified… a new flag would be a mechanism with no
caller."* §4.3(c) `:234` files starting compose as **wrong for CI**; `:235-236` justify their
verdicts as "the opposite of a restricted egress posture", a posture Decision 8 dropped.

`DECISIONS.md §6` reversed all of that on the owner's steer — commit `9f514222`, **2026-08-01**,
*"CI needs compose and networking — the negotiation is what it drops"*. `ci-required-config.md`
was last touched by `5785c55f`, 2026-07-31, the day before, and has been contradicted for six
weeks.

This commit is what makes it bite: Phase 2b Task 2.5 now instructs an implementer to **keep**
`_do_compose_start`, while a live 00068 report tells them the CI answer for that same function is
"do not start". Two surviving documents, opposite instructions, same function.

The file already uses supersession markers (`:270`, `:30`, `:64`), so the convention exists —
compose never got one.

**Fix**: supersession notes on §4.3(c) rows `2091`/`2266`/`2301` and §4.3(f)'s `_do_compose_start`
row pointing at `DECISIONS.md §6`, or untick `PLAN.md:141`.

## Should fix

### 2. Task 0.4's B1/B2 method takes one clause from its source and drops the two that refute it — `00113 PLAN.md:76-85`

The 11:20 journal entry cites `JOURNAL/00068-Journal-26-07-31.md:124` for "burning quota". The
same bullet list says two more things:

- `:121-125` — **the discriminating signal is not the exit code**: a spin bounded by `timeout`
  exits 124, and so does a real session killed by the same timeout; telling them apart needs the
  captured output. Task 0.4 says *"hang-vs-exit observed"*, which is exactly the discrimination
  that passage rules out.
- `:126-128` — the specific token-file population *"means a script manipulating the owner's
  credential store, **which this plan will not do** under the same rule that keeps it away from
  plaintext credentials."* Task 0.4 prescribes precisely that: *"a controlled token-file
  population (≥2 files for B1; exactly 1 for B2)"*. `:130` concludes group B is *"an interactive
  investigation with the owner, not a hand-over script."*

On the question asked: the reconstruction from the deleted file is **verbatim faithful**
(`5785c55f^:reports/hardware-proof-checklist.md:37-40` for B1–B4, `:50-51` for C1/C2; B3's
compression to "podman-with-a-network versus Docker-with-none" loses the expected `exit 1` at
`claude-yolo:2597` but garbles nothing). The defect is not the deleted source; it is that the
*live* journal superseded that one-liner and the newer analysis was not carried.

**Fix**: state the observation as captured stdout/stderr rather than exit status, and carry the
"no script touches the owner's token store — owner-driven investigation" constraint.

### 3. The same task invites the substitution round-1 blocked — `00113 PLAN.md:83-85`

*"Task 0.2's instrumentation and Task 3.2's stdin-closed run cover the same ground as B1/B2 —
settle them there and cite it here."* Task 3.2 (`:175-176`) runs **after** Phase 1 has closed
every prompt site; B1/B2 are claims about the launcher **before** the fix. A post-fix pass cannot
evidence pre-fix behaviour. Scope the invitation to Task 0.2, or drop it.

### 4. The set-diff design breaks at §4 — the derivation rule is refuted by §1's own measurement

New text at `ci-tool-surface.md:96-98`: *"what is subtracted becomes the `--disallowedTools`
string, what remains is what the session must be observed to have."* But `:17-18` (and
`DECISIONS.md:255-257`) measured that **removing `Bash` adds `Glob` and `Grep`** — 29−3=26,
measured 28. For class B the observed set is default − denied **+ substituted**, not "what
remains".

§2's class B row `:62` states this correctly. §4 does not, and the error propagates into both
00113 tasks: Task 0.3 captures the *"**default** tool vocabulary"* (`:70-75`) and Task 2.1 says
*"what remains is what assertion 2 diffs against"* (`:121-125`). Two of the three routes to the
declared set are wrong; only §2's table saves an implementer.

**Fix**: Task 0.3 captures the vocabulary **per class, under that class's `--disallowedTools`
string** — the class deny-lists are already specified, so this is runnable in Phase 0.

Assertion 3 is *not* redundant-in-a-way-the-document-denies: `:126` says outright "Formally the
presence half of assertion 2". Honest.

### 5. `ci-tool-surface.md:19` now contradicts §5

*"Every assertion below is on a **tool name being absent**, never on a count."* Assertion 2 is a
set equality ("missing members fail") and assertion 3 is a presence check. Only "never on a
count" survives the rewrite.

### 6. Task 2.5's keep/drop list is partial while claiming "exactly per §6's table" — and the omitted member is a live prompt — `00113 PLAN.md:152-156`

`DECISIONS.md:289` keeps `_do_compose_start` and drops *"that confirmation"*. That confirmation is
`files/var/local/claude-yolo/lib/network-management.bash:586`:

```
read -rp "Start services with $compose_name up -d? [Y/n]: " start_choice
```

Task 2.5 keeps `_do_compose_start` and names four prompting members to drop — the confirmation is
not among them. Followed literally it ships a `read` on a stdin-closed CI path: the exact hang
Phase 1 exists to remove. The keep list also omits `_compose_already_running` and
`network_has_running_containers` from §6's keep column while reading as exhaustive ("…stay").

### 7. Task 2.9 is misfiled — `00113 PLAN.md:166-168`

`entrypoint.sh` with no SSH key and no `GITHUB_USERNAME` is credential/entrypoint work, under a
heading that reads "Compose and networking". Round-1 named it as a *second, separate* drop; it was
bundled into the compose phase rather than placed. Belongs in Phase 1, or Phase 0 as a fact to
establish.

### 8. `CLAUDE/Plan/README.md:149` describes 00068 by two retracted mechanisms

*"an image `LABEL` identity and a small CI entrypoint"*. Decision 2 retired the LABEL convention
(`PLAN.md:44`); `ci-entrypoint-spec.md` was deleted by `5785c55f` as retracted — stated in this
very commit at `mcp-and-egress.md:227-231`. This row becomes the permanent Completed-list
description at the move, and this was the round that swept retracted-mechanism references out of
four other files.

## Nits

9. `DECISIONS.md §6`'s function line numbers are ~40 lines stale against
   `lib/network-management.bash` today (`:10`→15, `:448`→488, `:613`→653, `:427`→464, `:703`→743,
   `:98`→105, `:505`→545, `:667`→707; `:546` is now `local expected_network="$1"`). Names all
   correct; Task 2.5 sends an implementer to that table.
10. Journal timestamps run ahead of the commit carrying them — 00068 at 11:05/11:20/11:35/11:45
    and 00113 at 11:50/11:55, inside `94dee731` authored **10:40:52 UTC** on a UTC host. Repo-wide
    habit, not introduced here (00114's 09-14 file ends at 13:05 in a 10:37 commit), so a nit —
    but a log's times should be observed, not chosen.

## Checked and clean

- **Round-1 finding 1**: discharged. Task 0.1 `:61-65` claims E1/E6/C3 only and drops "never been
  run". C1/C2's method matches `probe-network.bash:224-228` exactly. **Citing a deleted file as a
  source is sound here** — git holds it, the truncation notice forbids reconstruction, and the
  tasks state the method *inline* rather than linking a path that 404s. The failure was not the
  deleted source.
- **Round-1 finding 2**: criterion `:145` **is** substantively met — two event classes, per-class
  present/absent lists, server-granularity MCP, four assertions. Withholding the tick for the
  status flip is correct; the plan-QA edit hook does block the last box under an `In Progress`
  header.
- **Round-1 finding 3**: fully discharged. `5785c55f` deletes exactly **29** files in the folder —
  25 `reports/`, 3 `analysis/`, `probe-label.bash`. `PLAN.md:154` and `PLAN_archive.md:10-12` both
  correct; the three surviving `0dde4f0` mentions all name it *as the wrong hash*.
- **Round-1 finding 4**: verified 9 of 9 against the sweep — same nine links, same file
  (`00068-Journal-26-07-30.md`), the three named omissions exact, lines 543/632/725 confirmed. All
  seven files in the superseded note (`26-07-31:1049-1056`) confirmed deleted by `5785c55f` and
  absent today, so "all seven are gone" holds. Supersession reach is as good as append-only
  permits; `PLAN.md:64-68` is the durable statement a reader meets first.
- **Round-1 findings 5, 7, 8, 10–14**: all discharged. `ci-flow.md:92-98` (E8 struck, residue
  narrowed to Task 0.3); Decision 7 citation correct (`DECISIONS.md:202`, inside 181-204);
  `PLAN.md:131`; `ci-flow.md:53-57` recast as the `--disallowedTools` addition;
  `mcp-and-egress.md:227-231` fixed in place and gone from the sweep (10 folder findings → 9);
  00113 journal has three real entries; §2 scopes the credential to push and files dirty-tree on
  lts-infra 00030 (`:50-53`, `:153-154`); 00089 stated as code-landed/proof-unticked in both
  places with Task 0.6 to discharge it.
- **Public-repo safety**: clean. Scanned all 8 changed files for home paths, non-`example.com`
  emails, RFC1918 addresses, hostnames, container/project names — nothing. `**Owner**: joseph` is
  established convention; "another repo's runner" and "lts-infra" carry no identifiers.
- **Prose honesty**: the journal entries name the author's own errors directly and do not flatter.
  No invented facts found beyond findings 2 and 4.
- **Plan Commit Rule / index**: nothing from 00068 or 00113 dangling; the modified and untracked
  files in the tree are Plan 00109's. 00113 README row present and accurate at `:39`. Branch level
  with `origin/F44`.

## Mechanical gates

- `qa-all.bash`: **PASS**, exit 0, **812** files.
- `plan-qa --sweep`: exit 1, **0 block / 2 advise** — 00046 path-existence, journal-freshness;
  neither in 00068 or 00113.
- `docs-qa --sweep`: exit 1, 0 block — the 9 append-only journal dead links plus the
  `00068-Journal-26-07-29.md` `duplicate-block`, as reported.
- `ansible-playbook --syntax-check`: **not triggered** — 8 markdown files, no playbook (qa-all's
  `ansible-syntax` stage ran 79 playbooks green regardless).
- `qa-helper-tests.bash`, `check_extension_compat`, extension ESLint: **not triggered** — no
  `helpers/`, `tests/helpers/`, `extensions/` or `metadata.json` in this diff.

**Minimum to flip to APPROVE**: finding 1 (supersession notes in `ci-required-config.md`, or
untick `PLAN.md:141`) and findings 2/4/6 — all small edits in three files.

## Review hygiene

Working tree unchanged by this review apart from this report; sweep output went to the gitignored
`untracked/scratch/`.
