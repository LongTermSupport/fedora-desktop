# R84 follow-up: the git hooks play acted on the wrong checkout (Defence Before Fix)

**Defect.** `playbooks/imports/play-git-hooks-security.yml` set
`repo_path: "{{ lookup('env', 'HOME') }}/Projects/fedora-desktop"`. Every hooks task (the stat
checks, `core.hooksPath`, the verify step) used it, while the pin task used `root_dir`. A
checkout cloned anywhere else never got its hooks, and the play's verify step passed by looking
at the other checkout. The host confirmed that this is R84's cause.

**Class.** A play that acts on this repository's own checkout through a hard-coded home path
instead of the checkout running the play (`root_dir`).

**Detector.** A new rule, `checkout-by-home-path`, in the existing blocking `qa-ansible` gate
(`src-go/qa/ansible/checkouthome.go`, run from `scanPlayUX`, reported as a play-ux finding). A
semgrep rule would have needed a gate of its own (Go command, spec, routing). `qa-ansible`
already reads exactly the Ansible trees (`playbooks/ tasks/ vars/ environment/ roles/`) and is a
`stage` row of `scripts/qa-gates.tsv`, so the rule blocks in `qa-all`, `qa-changed` and CI. The
rule reports any line that is not a comment and names `Projects/fedora-desktop` as a whole path
segment. `run.bash`, where that path is the legitimate bootstrap clone, is outside the trees.

- Proof: `tests/fixtures/ansible-checkout-by-home-path/{flagged,clean}.yml` (5 marked cases,
  including `~`, `$HOME`, `/home/{{ user_login }}`, the env lookup and the facts env). Tests are
  in `src-go/qa/ansible/checkouthome_test.go`, including a scan-level test. A routing row is in
  `scripts/qa-routing.tsv`, and docs are in `CLAUDE/QA.md#checkout-by-home-path`.
- **Red:** `./scripts/qa-ansible.bash` on the unfixed tree gave
  `playbooks/imports/play-git-hooks-security.yml:15: checkout-by-home-path: ...` and
  `✗ ansible: 1 violation(s)`.
- **Sweep:** `playbooks/ tasks/ vars/ environment/` hold only that one instance. Repo-wide, the
  other hits are docs, `run.bash`, kickstart, the VM guest checkers and tests, which are bash
  bootstrap or human instructions and outside this class.
- **Fix:** `repo_path: "{{ root_dir }}"`.
- **Green:** `./scripts/qa-ansible.bash` printed `✓ ansible: ...` and exited 0, and
  `go test ./ansible/` passes.

**Still to do.**

- The play must run on the host. It is already queued in `meta-deploy.list`, through
  `r79-private-names.bash`.
- R84's session-start `core.hooksPath` detector is not built yet.

**Tooling fault met.** Edit-time Ansible lint fails on every YAML write in this container
because `vault-pass.secret` is absent. Issue #215 already logs this.
