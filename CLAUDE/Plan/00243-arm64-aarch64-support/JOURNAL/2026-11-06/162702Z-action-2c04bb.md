## 16:27 · action · —   — F44-arm fixes ported onto F44, x86_64 unchanged

The owner decided that the arm64 fixes land on F44 now, gated so that x86_64 is unchanged. An aarch64 headless host following the F44 tip needs them, and F44-arm was 1151 commits behind F44 with 11 conflicting files, so it was not merged. Branch `arm/onto-f44`, cut from the F44 tip, re-applies its fixes:

- b24e323e6 (main playbook);
- 18d3b7485 (optional plays);
- 22192555e and 17f1d578d (ccy image), as ccy 3.112.3 and container 2.79.27;
- 7404b0a2f (ISOs, docker-in-lxc, VM lab);
- the eight arm64-install.yml commits (the CI lane).

What changed in the port:

- intel-media-driver and Slack gained a debug task naming the skip off x86_64.
- yq, hifi-rs and golangci-lint keep F44's x86_64 behaviour (latest, no pin, the checksums.txt check). Only the asset arch is mapped.
- Not ported:
  - the arch-pin gate (8daa52700, c2b352311, c0e257509, 31cd49698, 7fdbad15a);
  - the .claude/ccy/Dockerfile pins (22192555e, dc813761a);
  - the qa.yml uname -m case;
  - a38e49656 (QA script modes).

A sweep of every line added to F44 since the branch point found no new x86-only assumption on the headless path: fzf and tuwunel already select by arch. qa-changed is green on each commit, apart from the toolchain-version, go vuln, tmux and status-line findings. All four come from this container or from F44 itself.
