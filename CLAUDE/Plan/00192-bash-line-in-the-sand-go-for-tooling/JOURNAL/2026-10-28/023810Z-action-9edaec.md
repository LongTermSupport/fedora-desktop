## 02:38 · action · —

ssh-with-key family: red. SPEC-ssh-with-key.md written (D1-D5, E1-E19, B1-B10, H1-H5). The
expectation tests are in src-go/internal/sshkey (args_test.go E1-E4 and E13 and the probe
targets of E11, keys_test.go E5-E9, run_test.go E10-E19, proc_test.go the /proc parent read
and NewEnv) and the three cmd packages' main_test.go; `go test` over them fails to build
(`undefined: Env`, `Tool`, `SSH`, `run`). term_test.go's TestExpandHome was red the same way
(`undefined: ExpandHome`) and is green: the probe needs `~` expanded, and project_homeabbrev
lets only internal/term own the home abbreviation, so the inverse of term.AbbrevHome lives
there.

Found while reading for requirements (B1): with IdentitiesOnly=yes ssh still offers every
IdentityFile ~/.ssh/config gives the host. `ssh -G -F cfg -i mine -o IdentitiesOnly=yes host`
lists both files, and `-o IdentityFile=none` clears nothing, so the bash's "only one key" was
false for any configuration with a Host * IdentityFile. E11 asks ssh -G and stops; the choice
between stopping, saying and going on, or keeping the bash's behaviour was sent to the lead
for the owner.
