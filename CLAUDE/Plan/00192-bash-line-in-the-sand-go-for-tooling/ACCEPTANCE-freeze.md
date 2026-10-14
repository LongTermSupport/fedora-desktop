# Freeze tools: host acceptance (the owner's step)

`podfreeze` and `lxcfreeze` became Go binaries. The golden replay (`src-go/internal/freeze/golden`)
proves the Go tools print what the bash tools printed, under fakes. Only a host with real engines
can settle that `podman ps --format` and `lxc-info` emit what the parsers expect, so this is the
check. Run it from the checkout, on the host, in this order.

## 1. Before `./CLAUDE/Plan/meta-deploy.bash`: save what the bash tools print

`meta-deploy` replaces them, so this cannot be done afterwards.

```bash
mkdir -p untracked/freeze-acceptance
podfreeze list > untracked/freeze-acceptance/before-podfreeze-list.txt 2> /dev/null
podfreeze freeze --all -n > untracked/freeze-acceptance/before-podfreeze-dry.txt 2> /dev/null
lxcfreeze list > untracked/freeze-acceptance/before-lxcfreeze-list.txt 2> /dev/null
lxcfreeze freeze --all -n > untracked/freeze-acceptance/before-lxcfreeze-dry.txt 2> /dev/null
```

Only stdout is saved: the list and the dry-run block are the tools' payload. `lxcfreeze` asks for
your sudo password.

## 2. Run `./CLAUDE/Plan/meta-deploy.bash`

It builds and installs both tools from the checkout, and removes `~/.local/lib/freeze`. Check:

```bash
file ~/.local/bin/podfreeze ~/.local/bin/lxcfreeze     # both ELF executables
ls ~/.local/lib/freeze                                 # No such file or directory
```

## 3. Same commands afterwards, then compare

```bash
podfreeze list > untracked/freeze-acceptance/after-podfreeze-list.txt 2> /dev/null
podfreeze freeze --all -n > untracked/freeze-acceptance/after-podfreeze-dry.txt 2> /dev/null
lxcfreeze list > untracked/freeze-acceptance/after-lxcfreeze-list.txt 2> /dev/null
lxcfreeze freeze --all -n > untracked/freeze-acceptance/after-lxcfreeze-dry.txt 2> /dev/null
for f in podfreeze-list podfreeze-dry lxcfreeze-list lxcfreeze-dry; do
    diff untracked/freeze-acceptance/before-$f.txt untracked/freeze-acceptance/after-$f.txt && echo "same: $f"
done
```

Expected: no difference. A difference is a parser reading a real engine's output differently from
the fakes: keep both files and report it.

## 4. Freeze and thaw a throwaway container with each tool

Use a container that matters to nobody, one per engine. Replace `NAME`.

- Podman: `podfreeze freeze NAME`, then `podman ps -a` shows it Paused; `podfreeze thaw NAME`
  shows it Up again.
- LXC: `lxcfreeze freeze NAME`, then `sudo lxc-info -n NAME -s` shows FROZEN; `lxcfreeze thaw NAME`
  shows RUNNING and reports the DHCP renewal.
- The menu: run each tool with no arguments, pick a group, pick "all", see the act and the return
  to the menu, then quit. The picker is `fzf`; where it is not installed the tools fall back to a
  numbered menu, which is worth one look too.
- A container the bash tool froze is thawed by the Go tool, and the reverse; the state is held by
  the engine, not the tool.

`lxcfreeze` should say why it asks for your password, and a menu left idle past sudo's timestamp
asks again with the reason instead of prompting silently.

## 5. Run `./CLAUDE/Plan/meta-deploy.bash` again

The freeze tasks report ok, not changed: the build is byte-for-byte reproducible and the install
compares checksums.
