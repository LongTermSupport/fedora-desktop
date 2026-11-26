# Plan 00270: risks and mitigations

The task numbers are those of [PLAN.md](PLAN.md).

| Risk                                                        | Impact | Probability | Mitigation                                                                                          |
| ----------------------------------------------------------- | ------ | ----------- | --------------------------------------------------------------------------------------------------- |
| An install-specific name leaks into a new public repository | H      | M           | the private-names scan and gitleaks over the tree **before** the initial commit (Task 2.2's script) |
| The history is needed in a new repository                   | L      | L           | `HANDOVER.md` gives the sha, paths and commands to read it in fedora-desktop (Task 2.4)             |
| v1 bus code reaches a new repository                        | M      | M           | the boundary rule (Tasks 1.1, 1.5) and `extract.bash`'s v1 refusal (Task 4.1)                       |
| Concurrent teamchat work lands in a tree just extracted     | M      | H           | the freeze gate (Task 2.1) refuses it and names the new repository                                  |
| A pinned version drifts behind a fix                        | M      | M           | the pins join `scripts/check-pinned-versions.bash` (the update-versions skill)                      |
| Contained mode turns yolo on for a host                     | H      | L           | three independent checks with no override, and a plain-host refusal test (Task 5.2)                 |
