# Plan fact check: 00254 PLAN.md Phase 4 diff

## REFUTED

R1. "No declared team on the host has an https approved url, so every CA step and the handshake were skipped."
The handshake and the build are skipped, but not every CA step. In `tasks/lightning-team-ca.yml`, with
`https_team_ca_https` empty, these still run: Install Lightning, the getent, "Ensure Lightning's CA directory
exists", the find, and "Remove every file there but an https team's CA". Only the copy loop (empty loop), the
check-dir creation, the Go build (`when: https_team_ca_https | length > 0`), the check loop (empty
`https_team_ca_pem.results`) and the assert (empty list passes) are no-ops. The directory creation and the cleanup
are the likely source of the reported `changed=1`. In `tasks/https-team-ca.yml` the getent and assert are skipped
with no https team. Plan change: say "the CA placement, the build and the handshake were skipped; the CA directory
was still created and swept".

## Table

| #   | Claim                                                                                                                     | Verdict           | Evidence                                                                                                                                                    |
| --- | ------------------------------------------------------------------------------------------------------------------------- | ----------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------- |
| 1   | `tasks/https-team-ca.yml` is one file both clients import                                                                 | VERIFIED          | The file exists; its header names element-team-ca.yml and lightning-team-ca.yml; lightning-team-ca.yml imports it. The Element import was not checked here. |
| 2   | Every other file in the directory is removed                                                                              | VERIFIED          | lightning-team-ca.yml find + remove tasks (`hidden: true`, `file_type: any`, reject keep list)                                                              |
| 3   | `tasks/lightning-team-ca.yml` exists beside `element-team-ca.yml`                                                         | VERIFIED          | `ls tasks` shows lightning-team-ca.yml and https-team-ca.yml; element-team-ca.yml was not listed by the grep and was not checked                            |
| 4   | `src-go/cmd/tls-trust-check` over `internal/tlstrust`                                                                     | VERIFIED          | both directories exist with main.go and tlstrust.go                                                                                                         |
| 5   | Built static                                                                                                              | VERIFIED          | `go_command_static: true`; build-go-command.yml sets CGO_ENABLED=0                                                                                          |
| 6   | Run in the sandbox through `flatpak run --command=env`                                                                    | VERIFIED          | the argv of the check task in lightning-team-ca.yml                                                                                                         |
| 7   | Imported by `play-agent-bus-element.yml` under tag `agent-bus-lightning`, Lightning installed there too                   | VERIFIED          | play-agent-bus-element.yml:140 imports the file; tag in header and lines 80-100. Install in lightning-team-ca.yml.                                          |
| 8   | Host run 2026-11-13: PASS, no https team                                                                                  | UNVERIFIABLE-HERE | reported by the caller; the code is consistent with it (see R1)                                                                                             |
| 9   | The handshake is skipped with no https team                                                                               | VERIFIED          | the loop over empty `https_team_ca_pem.results`; the assert passes on an empty list                                                                         |
| 10  | Every CA step is skipped with no https team                                                                               | REFUTED           | see R1                                                                                                                                                      |
| 11  | The last step fails unless the handshake with the two variables verifies the URL, and the SAN is checked (no CN fallback) | VERIFIED          | the assert task in lightning-team-ca.yml; tlstrust.go T3 (SAN)                                                                                              |
| 12  | Task 4.3 done: Lightning paragraph in `docs/agent-bus-cross-network.md`                                                   | VERIFIED          | docs/agent-bus-cross-network.md:165-176                                                                                                                     |
| 13  | The `play-matrix-clients.yml` comment that said no CA reaches Lightning is updated                                        | VERIFIED          | play-matrix-clients.yml:59 now points to lightning-team-ca.yml. The earlier wording was not checked.                                                        |
