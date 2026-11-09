# Independent search: self-erasing clipboard for a secret

Searched without opening src-go/qa/rules/ or .semgrep/ (excluded by pathspec) and without running the detector. The dispatch did not contain the rule.

Technique: T = text search (git grep), R = reading the surrounding code or doc.

Only one mechanism in the repo is in the class: `wl-copy --paste-once` in the ccy-teams password copy. There is no `xclip -loops`, no unconditional clear timer, no `wl-copy --clear`, no gpaste, no xsel use for secrets, no OSC 52 and no tmux buffer for secrets. The unconditional-timer form appears only in history (items 34, 37).

## Live: behaviour (code)

1. src-go/internal/teams/login/login.go:507 - runs `wl-copy --paste-once` with the new human password on stdin. The mechanism itself. T. Carries the hazard. Confident.
2. src-go/internal/teams/login/login.go:469 - tells the person "The password is on the clipboard for one paste; after that it is gone from the clipboard." T. False claim on GNOME. Confident.
3. src-go/internal/teams/login/login.go:459 - consent text "Put <name>'s new password on the clipboard for one paste, into Element; after that it is gone ...". T found the start, R was needed to read the continued string. Same false claim. Confident.
4. src-go/internal/teams/login/login.go:431-436 - doc comment: served "to one paste only ... nothing clears the clipboard later". T. Comment carrying the reliance. Confident.
5. src-go/internal/teams/login/login.go:504 - comment on copyToClipboard: "puts text on the clipboard for one paste". T. Confident (comment).
6. src-go/internal/teams/login/login.go:434-455 - Clipboard Indicator guard: refuses the offer when the extension is on or unreadable, saying the extension "would use up the one paste". R. It assumes paste-once works with the extension off; the same premise. Unsure whether it is a separate instance.

Related, not an instance: login.go:69 (comment "No 'copy it': with Clipboard Indicator on ..."), R, carries no claim.

## Live: tests that pin the claim or the mechanism

7. src-go/internal/teams/login/password_test.go:198 - asserts args are exactly `--paste-once`. T. Confident.
8. src-go/internal/teams/login/password_test.go:205-210 - comment "the password serves one paste and is gone" and asserts the "gone from the clipboard" text. T. Confident.
9. src-go/internal/teams/login/password_test.go:322 - asserts `wl-copy --paste-once` was called. T. Confident.
10. src-go/internal/teams/login/password_test.go:260-261, 276-277 - comments and expected text "would use up the one paste". T. Same premise as item 6. Unsure.
11. scripts/test-ccy-teams-command.bash:1029 - "served to one paste only" checks `wl-copy --paste-once` was logged. T. Confident.
12. scripts/test-ccy-teams-command.bash:1031 - asserts the person is told "for one paste; after that it is gone from the clipboard". T. Confident.
13. scripts/test-ccy-teams-command.bash:309-313, 1026-1043 - fake wl-copy and checks. R. A fake cannot show compositor behaviour, so these pass on the hazard. Gap, not a separate claim. Unsure.
14. src-go/internal/teams/login/element_test.go:148-172 - comment and cases about "copy it" after a first password, fake wl-copy. T and R. Related only. Unsure.

## Live: documentation and plans

15. docs/agent-bus.md:60 - "offers it to the clipboard (for one paste, then gone; not while Clipboard Indicator is on ...)". T. False claim. Confident.
16. docs/agent-bus.md:953-957 - Limits: offers the password via `wl-copy --paste-once` only when Clipboard Indicator is off; "uses up the one paste". T. Relies on the mechanism and implies it works with the extension off. Confident.
17. CLAUDE/Plan/00201-ccy-teams-many-teams-per-session-descriptions-and-a-picker/ACCEPTANCE-owner.md:132-137 - step 11 expects "a second Ctrl-V anywhere pastes nothing" after the copy. T and R. Live acceptance step expecting the erase; fails on mutter. Confident.
18. CLAUDE/Plan/00201-ccy-teams-many-teams-per-session-descriptions-and-a-picker/PLAN.md:142-150 - open Task 2.12 expects "`wl-paste` showing a second paste is empty". T. Live open task. Confident.
19. CLAUDE/Plan/00201-ccy-teams-many-teams-per-session-descriptions-and-a-picker/PLAN.md:104-112 - check 11 status text (copy with Clipboard Indicator off). R. Related to 18. Unsure.
20. docs/ccy-changelog.md:1436-1437, 1452 - 3.98.10/3.98.11 entries: "a password copied to the clipboard serves one paste and is then gone". T. Old changelog entries (history by the brief) but user-visible shipped text. Unsure if live.

Not instances: ACCEPTANCE-owner.md:30-34 (step 4, typed from screen, R); CLAUDE/Plan/00191-go-cli-tooling/SPEC-ccy-teams.md:415-416 (older text, plain wl-copy, no erase claim, only "A clipboard manager may keep a copy", T).

## Other clipboard uses, no hazard

- files/home/.local/bin/wsi-stream:550-575 and src-go/internal/wsi/{deliver,paste}.go - dictation text via wl-copy/xsel, read back with wl-paste. Not a secret, no erase claim. T and R.
- files/home/.local/bin/wsi-article-window:446 - wl-copy of article text. No hazard.
- extensions/speech-to-text@fedora-desktop/extension.js:272, 1685 and postProcess.js - St.Clipboard / wl-copy for dictation text. No hazard.
- extensions/fedora-desktop@fedora-desktop/sections/containers.js:108 and health.js:72, 117 - St.Clipboard set_text of exec_hint, findings, a command. No secret, no erase claim.
- files/home/.local/bin/clean-paste:41, 57 - read and rewrite the clipboard; no erase claim.
- files/var/local/claude-yolo/Dockerfile:113-130 and docs/features/container-clipboard.md - wl-paste guard for image paste.
- extensions/CLAUDE.md:128 - example `echo hello | wl-copy`.
- files/home/.local/bin/imgpaste - avoids the clipboard.

## History (not live)

21. CLAUDE/Plan/00201-.../DONE-tasks.md:119, 128 - "L4: a copied password serves one paste". T.
22. CLAUDE/Plan/00201-.../JOURNAL/00201-Journal-26-10-15.md:208, 255 - L4 first used a 45 s clear timer, replaced by paste-once. T. Only record of the timer form; not in code now.
23. CLAUDE/Plan/00201-.../subagent-reports/261015-security-client-opus.md:13, 29 - "one paste, then gone". T.
24. CLAUDE/Plan/00201-.../subagent-reports/261015-final-release-security-fable.md:53-65 and 261015-final-review-fixes*.md - S1 findings on Clipboard Indicator. T.
25. CLAUDE/Plan/00201-.../subagent-reports/261015-teams-security-audit-fable.md:178 and ...-infra-opus.md:49 - audit suggested "clear after 30 s" (the unconditional-timer form). T.

## Text search found that reading could not have checked

- The complete occurrence set of `paste-once`, `wl-copy`, `xclip`, `xsel`, `gpaste`, `St.Clipboard`, `set_text(`, `OSC 52`; that nothing else exists (no -loops, gpaste, tmux buffer, clear timer) outside the journal.
- The "gone from the clipboard" and "one paste" wording across docs, tests, plans (items 2, 8, 12, 15, 20).

## Reading found that text search could not have

- Item 3: the consent string is continued across lines.
- Items 6, 10, 16: the Clipboard Indicator guard rests on the premise that paste-once works with the extension off; the patterns do not say so.
- Item 13: the test fake cannot expose compositor behaviour.
- Items 17, 18: acceptance steps that expect the erase without naming the mechanism.
- That the other clipboard writers copy no secrets.

## Searches run

`paste-once`, `wl-copy`, `xclip`, `xsel`, `gpaste`, `xdotool`, `St.Clipboard`, `set_text(`, `wl-paste`, `copyq`, `cliphist`, `loops 1`, `clipboard-clear`, clipboard near clear/gone/erase/vanish/wipe (both orders), `one paste`, `single paste`, `paste once`, `pasted once`, `Clipboard Indicator`, `gone from the clipboard`, `OSC 52`, `set-buffer`, `load-buffer`, `pbcopy`, `copy-pipe`, `xdg-copy`, `on the clipboard`, `to the clipboard`, `copied to clipboard`. Later passes excluded journals, subagent-reports and Plan 00148/00109; history items were read from the first pass.

Counts: 20 live items (1-20): confident 1-5, 7-9, 11-12, 15-18 (14); unsure 6, 10, 13, 14, 19, 20 (6). History 21-25. Others listed are not instances.
