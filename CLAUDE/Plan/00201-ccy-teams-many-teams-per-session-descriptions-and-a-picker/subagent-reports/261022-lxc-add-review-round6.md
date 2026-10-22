# Plan 00201 Task 2.20: adversarial review round 6 (5809a0ac5, e84861753)

Branch `fix/lxc-add-already-declared`, tip e84861753. Scope: what came after round 5's MERGE
at 093f9bc94.

**Verdict: MERGE.** Nothing blocks. Three cosmetic nits, none of which makes the text wrong
enough to mislead the owner.

## 1. 5809a0ac5: answers to round 5 and conformance review 5

- **GoProjectRules.md:433.** The `good.go` list now has "`errors.As` in the Init with its `ok`
  alone", which is `asInTheInit` (good.go:104). It is accurate. The list describes shapes, not
  function names, so the journal's "names `asInTheInit`" is loose wording (see nit 3).
- **DBF report:93-96.** Its new account checks out against the fixtures and 74d67de9d.
  `filtersInTheInit` (bad.go:166, want at :168) and `filtersByTheSecondName` (bad.go:186, want
  at :188) were red. `filtersBesideTheInit` (bad.go:176, want at :178) and `asInTheInit`
  (good.go:104) were added in the same commit as guards that already held. This matches
  conformance review 5 note A and the 13:39 journal entry ("bad.go:168 and :188 not
  reported"). 74d67de9d touches only bad.go, good.go and two reports, which is consistent.
  "Green at the commit after" is 093f9bc94.
- **PLAN.md:264.** Task 2.20 says "the merge of this branch is held for the owner's answer". It
  is accurate, and the task stays 🔄.
- **Journal 14:31.** Its content is accurate: re-review 5 CONFORMS, round 5 MERGE, red
  74d67de9d and green 093f9bc94.

## 2. e84861753: the merge of F44

Only four files conflicted (`git show --cc`): PLAN.md, meta-deploy.list, ccy-changelog.md and
claude-yolo.

- **Versions are consistent.** CCY_VERSION is 3.99.15.5 (claude-yolo:17), above F44's
  3.99.15.4. REQUIRED_CONTAINER_VERSION is 2.78.18 (claude-yolo:93), matching the Dockerfile
  `LABEL claude-yolo-version="2.78.18"` (Dockerfile:36). The changelog heading is
  "3.99.15.5 — container 2.78.18" and sits above F44's 3.99.15.4 entry, which is kept
  verbatim.
- **No stale versions.** Outside reports and the journal, `git grep` finds no 3.99.14.15. It
  finds 3.99.15.4 only in Plan 00216's own records and the changelog's 3.99.15.4 entry, which
  is correct. In meta-deploy.list, 2.78.15 became 2.78.18 at every mention: :48, :67, :96 and
  :136. The ccylaunch difftest goldens F44 changed carry no version string.
- **meta-deploy.list keeps both descriptions.** Compared with `git show F44:` and
  `git show 5809a0ac5:`:
  - Plan 00216's whole build-lock description and check survive word for word (:87-96).
  - Plan 00201's ccy-teams lxc-add description and check survive (:97-106).
  - The Go-front text follows once (:107). Nothing is duplicated or lost.
  - F44's other additions are intact: the "four kinds" header, `changed-plays`, the 00214
    entries and the 00209 S1 text.
- **No install-specific text.** The F44..merge diff adds only placeholders (`<user>@<container>`,
  `/home/u/proj` in a fixture) and generic tool-cache paths that were already on the branch.
  It adds no container names, hostnames or checkout paths.

## Nits (non-blocking)

1. **CLAUDE/Plan/meta-deploy.list:97-98.** The merge broke "Plan 00201" across the line
   ("Plan\\n# 00201: the play rebuilds ... with Plan 00201's lxc-add fix"). That says
   Plan 00201 twice and leaves :98 at 120 characters; the file is otherwise wrapped at about 96.
   Rewrap as "Plan 00201: the play rebuilds ~/.local/bin/ccy-teams with its lxc-add fix: ...".
2. **docs/ccy-changelog.md:22.** "containers 2.78.16 to 2.78.18, were this branch's earlier
   cuts, never released". 2.78.18 is this entry's own container, so it is being released now.
   Say "3.99.14.13 to 3.99.14.15 (containers 2.78.16 to 2.78.18) were this branch's earlier
   cuts; 2.78.18 is kept", or list 2.78.16 and 2.78.17 only.
3. **Journal 14:31.** It says the GoProjectRules.md list "names `asInTheInit`". The list
   describes the shape, not the name. This is wording only, and the journal is append-only, so
   leave it as it is.

The play-claude-yolo.yml entry states the container twice, at :96-97 and :136. Both sides
already did this, both are now 2.78.18, and they agree. It is not a finding.
