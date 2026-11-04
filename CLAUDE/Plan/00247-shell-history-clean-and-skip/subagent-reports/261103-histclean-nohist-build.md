# histclean and nohist: build report

- histclean: Go command (`src-go/cmd/histclean`, logic in `internal/histclean`, expectations H1-H21 in the package doc). Built and installed to `~/.local/bin/histclean` by `play-basic-configs.yml` through `tasks/build-go-command.yml`. Row in `scripts/go-binary-suites.tsv`.
- nohist: no function; a line starting with `nohist=1` in any case is kept out by one HISTIGNORE bracket pattern (no extglob, no nocasematch). The Ctrl+R picker has an fzf `--header` naming both helpers (`--footer` needs fzf 0.63; 0.60 is here).
- HISTIGNORE in `files/etc/profile.d/zz_lts-fedora-desktop.bash` gains `histclean`, `histclean *`.
- Proof: `scripts/test-bash-history-search.bash` drives an interactive bash with the real HISTIGNORE line; the typed lines are in neither the shell history, the history file nor the Ctrl+R context file, the control commands are; removing the patterns turns three checks red. Go tests cover multi-line entries, both files, the prompt, non-terminal refusal, atomic mode-0600 rewrite, and an entry appended during the prompt.
- Not run: ansible-syntax (vault password file absent in a worktree); no playbook was run. The play is queued in `CLAUDE/Plan/meta-deploy.list`.
