**Verdict: FIX-BEFORE-MERGE.** Nothing blocks: no leaks, no consumer box reaching GitHub, and the tests pass.

**FIX-BEFORE-MERGE**
1. **The plan file on F44 is out of date.** On origin/F44, PLAN.md:208 still has Task 3.2 open, and meta-deploy.list still queues 00228, so the host would re-run all three VM installs. The update (1f03b8275) is only on feat/00232-netpolicy-build: merge it. The Success Criteria (PLAN.md:366-371) are also all unticked.
2. **The docs contradict the build.** README.md:79 and docs/installation.md:100 say the password is asked "more than once", but Task 2.7 asks once per run. installation.md:105 says containers and Claude Code are left out, but play-podman.yml and play-claude-code-consumer.yml are `audience: all`.
3. **One goal was never built.** The Goal at PLAN.md:40 promises a consumer install on a server profile stops. Nothing refuses it: no assert in play-AA-preflight-sanity.yml:67, and app.go's checkFedora only compares VERSION_ID. No task or test covers it.
4. **Wrong "Podman absent" claim.** PLAN.md:206 says Podman is absent, and the checker comment at guest-acceptance-desktop-consumer-install.bash:159 says the podman play "did not run". It does run on a consumer box; only compose and the socket are gated.

**Should fix**
- **The VM run never tests two paths.** The lab's sudo needs no password and it chooses no optional app, so Tasks 2.6 and 2.7 are proved only by unit tests.
- **Task 6.5 (speech-to-text) should be fixed now.** The consumer menu offers the play, whose ccy-only items fail. Hide them or make them fail clearly.
- **An empty vault file is kept.** app.go `vault()` checks only that the file exists, so a zero-byte vault-pass.secret is never remade.

**Gates:** plan-qa sweep found nothing for 00228 (0 block). Syntax-check passed on the 4 playbooks I checked. Go tests for consumer and vmtest pass. qa-all.bash did not finish here (stopped by my 590 s time limit after 2 gates passed).