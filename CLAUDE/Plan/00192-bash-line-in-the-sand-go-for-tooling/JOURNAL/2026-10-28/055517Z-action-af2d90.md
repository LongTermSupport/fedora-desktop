## 05:55 · action · —   — the sweep keeps a running build's disk

The Fable review of the sweep fix (subagent-reports/261028-fable-review-sweep.md, one FIX-BEFORE-MERGE, no blocker): sweepBuilds removed `disk.qcow2` from any fast-base `bases/<base>.build` directory, and that directory is also the live build's, so a nightly tick during an evening `build-base` would have unlinked the installer's disk.

Red: TestE51SweepKeepsALiveBuild (a `server-fast-44.build` with its disk, libvirt listing `vmtest-build-server-fast-44`) failed: the sweep removed the disk.

Green: the sweep asks libvirt once, and only when a fast-base build directory is found (an unreachable libvirt stops the sweep, as prune-runs' does), and keeps a build whose `vmtest-build-<base>` domain is listed, logging `kept build <name> libvirt has the domain <domain>`. The domain name has one home, `buildDomain`, which the build uses too. The sweep's tests now install the fake libvirt. Package tests pass at 90.5% coverage and golangci-lint reports nothing. SPEC-vmtest E51 says it. The review's five follow-ups stay in its report.
