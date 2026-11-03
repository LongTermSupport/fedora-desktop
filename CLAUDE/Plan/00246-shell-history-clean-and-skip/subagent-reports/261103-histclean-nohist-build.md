# histclean and nohist: build report

- histclean: Go command (`src-go/cmd/histclean`, logic in `internal/histclean`, expectations H1-H21 in the package doc). Built and installed to `~/.local/bin/histclean` by `play-basic-configs.yml` through `tasks/build-go-command.yml`. Row in `scripts/go-binary-suites.tsv`.
- nohist: shell function in `files/home/bashrc-includes/nohist.bash`, copied by the same play. A single string is read by the current shell from descriptor 3 (`. /dev/fd/3`); several words are run as words. No `eval`.
- HISTIGNORE in `files/etc/profile.d/zz_lts-fedora-desktop.bash` gains `nohist`, `nohist *`, `histclean`, `histclean *`.
- Proof: `scripts/test-bash-history-search.bash` drives an interactive bash with the real HISTIGNORE line; the typed lines are in neither the shell history, the history file nor the Ctrl+R context file, the control commands are; removing the patterns turns three checks red. Go tests cover multi-line entries, both files, the prompt, non-terminal refusal, atomic mode-0600 rewrite, and an entry appended during the prompt.
- Not run: ansible-syntax (vault password file absent in a worktree); no playbook was run. The play is queued in `CLAUDE/Plan/meta-deploy.list`.
