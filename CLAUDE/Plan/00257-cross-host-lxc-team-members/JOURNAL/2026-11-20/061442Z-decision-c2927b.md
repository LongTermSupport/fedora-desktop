## 06:14 · decision · —

**Decision: the #294 guard is redesigned (project hook handler ansible_enforcement.py).**

**Why.** Rounds 1 and 2 judged only the segments a line executes, then tried to win back the real-run cases that this loses through closed lists: wrappers, shells, the `-n` reader, written-script tracking. Each independent review found more real-run commands that the original handler denied and the new one allowed:

- round 1: `sudo ./s.sh` and similar;
- round 2: `bash +n -c`, `script -c`, `watch`, `find -exec`, `systemd-run`, `su -c`, a glued redirect.

A closed list cannot win this.

**New rule.** Judge the raw text, exactly as the original did, unless every executed segment's head is on a small allow-list of inert commands (echo, printf, grep, cat, jq, git commit/log, …). Only an all-inert line gets the text-only-mention pass. Such a line cannot run a system change, so no regression from the original is possible by construction.

**Cost, accepted.** A line that mixes an inert write with an unrelated shell run stays over-blocked, as it was under the original. It is listed in the report as a known false positive, not a regression.

Round 3 is with a fix agent.
