# Freeze tools: host acceptance (the owner's step)

`podfreeze` and `lxcfreeze` became Go binaries. The golden replay (`src-go/internal/freeze/golden`)
proves the Go tools print what the bash tools printed, under fakes. Only a host with real engines
can settle that `podman ps --format` and `lxc-info` emit what the parsers expect, so this is the
check. The comparison is mechanical; the freeze-and-thaw look is yours.

## 1. Run `./CLAUDE/Plan/meta-deploy.bash`

Its entry for this plan runs `deploy.bash`, which does the whole mechanical part in order:

1. While the bash tools are still installed (an interpreter line at `~/.local/bin/podfreeze` and
   `lxcfreeze`), it saves what each prints for `list` and for `freeze --all -n` under
   `untracked/freeze-acceptance/before-*`, stdout and stderr in separate files. A bash tool that
   fails here stops the deploy before anything is replaced. `lxcfreeze` needs root; the sudo
   password was asked for at the start of the run. A tool that is already a compiled program keeps
   the baseline an earlier run saved.
2. The three plays build and install both tools from the checkout and remove `~/.local/lib/freeze`.
3. `acceptance.bash` runs the Go tools through the same four commands and compares stdout with the
   baseline. It also checks both tools are compiled programs and `~/.local/lib/freeze` is gone.

Its verdict is the `COVERAGE: n of n checks passed` line and the exit status. A difference is a
parser reading a real engine's output differently from the fakes: the diff is in the run
directory (`diff-*-report.txt`), and the Go tool's stderr beside it (`after-*.err`). Keep them and
report it. `NOT ESTABLISHED` names a command with no bash baseline, which fails the run: the
baseline cannot be made once the bash tool is gone.

`lxcfreeze` reads `/dev/tty` for its sudo password whatever stdin and stderr are, as sudo does, so
its stderr is saved to a file without losing the explanation of why it asks.

## 2. Freeze and thaw a throwaway container with each tool

Use a container that matters to nobody, one per engine. Replace `NAME`.

- Podman: `podfreeze freeze NAME`, then `podman ps -a` shows it Paused; `podfreeze thaw NAME`
  shows it Up again.
- LXC: `lxcfreeze freeze NAME`, then `sudo lxc-info -n NAME -s` shows FROZEN; `lxcfreeze thaw NAME`
  shows RUNNING and reports the DHCP renewal.
- The menu: run each tool with no arguments, pick a group, pick "all", see the act and the return
  to the menu, then quit. The picker is `fzf`; where it is not installed the tools fall back to a
  numbered menu, which is worth one look too. Ctrl-C part-way through a batch ends the run with
  the count of what was done and the exit status 130.
- A container the bash tool froze is thawed by the Go tool, and the reverse; the state is held by
  the engine, not the tool.

`lxcfreeze` should say why it asks for your password, and a menu left idle past sudo's timestamp
asks again with the reason instead of prompting silently.

## 3. Run `./CLAUDE/Plan/meta-deploy.bash` again

The freeze tasks report ok, not changed: the build is byte-for-byte reproducible and the install
compares checksums. `acceptance.bash` runs again and still passes.
