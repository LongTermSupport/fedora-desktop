# Renumber and fzf CI fixes

- Plan renumbered 00246 to 00247 (counter said 00247; no origin branch held it). Branch feat/histclean-nohist at 7e38006d8. The first journal entry keeps the old number (append-only); a new journal entry records the move.
- feat/fzf-upstream-pin: merged feat/histclean-nohist (meta-deploy.list conflict resolved to the changed-plays entry, which already names play-basic-configs) and origin/F44. CCY 3.110.0 and container 2.79.24 kept.
- Pins test: RECORDED_COMMAND_OUTPUT gained fzf = "0.74.5 (c0a193e0)", measured from the checksum-verified linux_amd64 release tarball. The assertion is unchanged.
- Hook: scripts/git-hooks/pre-commit now lists vars/fzf-pin.yml as an image input. The hook suite derives its expected set from tasks/claude-yolo-image.yml; all 121 checks pass, so no other image input was missing.
- qa-changed: ansible-syntax fails only for the absent vault file. It also runs scripts/qa-private-names.bash as a "suite" whenever pre-commit is changed; with no arguments it prints usage and exits 2. This is a routing defect that predates this work.
