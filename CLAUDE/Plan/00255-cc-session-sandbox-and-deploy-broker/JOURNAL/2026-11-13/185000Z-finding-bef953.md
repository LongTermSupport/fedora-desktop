## 18:50 · finding · —

Fact-check of RESEARCH.md section 13 against the triage report: nothing refuted, six overreaches
narrowed. Two of them are in the previous finding entry, so they are corrected here:

- The report shows sudo, under the probe's bwrap, refusing because the "no new privileges" flag
  is set. That bwrap is what sets it is bwrap's documented behaviour, not measured.
- "The self-update runner is not set up": the report shows its clone absent and no self-update
  rule in `sudo -l`; whether the drop-in file exists is not established.

Also narrowed in RESEARCH.md: Lightning's native folder was not probed (so "all clients are
Flatpaks" is not established); the bwrap-in-bwrap probe stands in for Claude Code's Bash
sandbox, which was not run; browsers present are given as forms, not names.
