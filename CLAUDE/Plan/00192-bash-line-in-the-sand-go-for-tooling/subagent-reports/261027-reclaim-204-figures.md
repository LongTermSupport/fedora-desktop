# reclaim 2.0.4: image figures

- Summed image size dropped. `podman images --format json` gives a size per image and no layer ids or sizes, so unique bytes cannot be computed without `podman system df`. The images line now reads "total size not known: podman system df could not run, and images share layers". The dangling and unused size sums (also overlapping) are dropped from the report and both prune questions; per-image sizes in the Largest lists stay.
- Dangling 638 against 628: the report counted every dangling image; the prune keeps those a container uses. The report line now says "k of them used by a container and kept by the prune", and the prune step says so before its question (also when none is prunable).
- Tests (podman_test.go, actions_test.go) seen red first, then green. SPEC-reclaim.md, PLAN.md, the journal and meta-deploy.list updated.
