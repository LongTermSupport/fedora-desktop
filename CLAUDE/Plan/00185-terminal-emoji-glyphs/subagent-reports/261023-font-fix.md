# Plan 00185 font fix: worker report (interim, handed off after review round 1)

Branch `agent-a4b30c7e52ae391c7-45766f1c`, tip `20d11fc7b`. qa-changed passed before the commit
(18 changed files). The tip is not on any remote and has not been merged.

## Built

- `files/etc/fonts/conf.d/69-colour-emoji-fallback.conf`: Noto Color Emoji appended weakly
  after monospace, sans-serif and serif, and "en" recorded for it when fontconfig scans the
  font.
- `playbooks/imports/play-fonts.yml`, renamed from `play-ms-fonts.yml`. It installs the font,
  deploys the file, then runs `fc-cache -f` and prints a reopen-terminals notice. Every
  reference to the old name is updated, and so is the retired-plays map.
- `scripts/test-colour-emoji-fallback.bash`: a new qa-gates suite that runs fontconfig against
  BDF fixture fonts.
- The plan's `deploy.bash` and `acceptance.bash` (12 checks). PLAN.md records the coordinator
  default. The journal entry was written through mkplan.

## The finding

The analysis proposed the weak append on its own, and that is not enough.
FcFontMatch ranks a font that supports the locale's language above any weak family entry, so
English-capable Symbola keeps winning. Recording "en" for Noto Color Emoji at scan time is
what changes the fc-match result.

## Review round 1: CHANGES

- B1 (blocking): the suite stays green with the family rules removed. Two causes: the fixture
  scan order breaks the tie, and all the generic families collapse into monospace.
- B2: no meta-deploy entry. My brief puts that file out of scope.
- S1: test the terminal's real font. S2: text symbols now draw from the colour font.
  S3: the suite needs `FC_LANG=en`.

Full review: `261023-font-fix-review-r1.md`.

## Not done

The B1 and S1-S3 fixes, review round 2 onward, and recording the rounds. The handoff, with
these in order, is `untracked/scratch/261023-a4b30c7e52ae391c7-handoff.md` in the worktree.

## Meta-deploy entry for the coordinator to queue

Entry: `CLAUDE/Plan/00185-terminal-emoji-glyphs`. It runs triage (read-only), then the deploy
(play-fonts.yml: Noto Color Emoji as the emoji fallback, font caches rebuilt), then the
acceptance (12 fontconfig checks). Queue it only once the review reaches MERGE.
