# Post-Upgrade Repair Guide

After a major Fedora version upgrade (e.g. F42 → F43), use this checklist to repair and reconcile your system against the matching branch of this repo.

## Scope

**This guide picks up *after* the official Fedora upgrade is complete.**

- The official upgrade process is **out of scope**. Follow Fedora's own docs — they are the single source of truth and change between releases:
  [https://docs.fedoraproject.org/en-US/quick-docs/upgrading-fedora-offline/](https://docs.fedoraproject.org/en-US/quick-docs/upgrading-fedora-offline/)
- This is **not scripted**. The steps are short enough to run by hand, and the failure modes differ release-to-release.
- Intended for someone who ran `dnf system-upgrade` (or equivalent), rebooted into the new Fedora version, and now wants their tooling back in working order.

## Symptoms This Guide Fixes

- `ansible-playbook` fails with `bad interpreter: No such file or directory`
- `pipx list` warns `package X has invalid interpreter /usr/bin/python3.NN`
- Other `~/.local/bin/*` tools installed via `pipx` fail to launch
- System is otherwise booted and functional, but anything stacked on top of the old Python is broken

## The Repair Process

### 1. Confirm the upgrade actually completed

```bash
cat /etc/fedora-release
uname -r
```

Expected: the release matches the version you upgraded to, kernel is a new one from that release. If not, the official upgrade is incomplete — stop and go back to the Fedora docs.

### 2. Rebuild all pipx venvs

The most common breakage. Fedora upgrades replace the system Python (e.g. 3.13 → 3.14), which removes the interpreter every `pipx` venv is symlinked to. `pipx` itself detects this and prints the fix.

```bash
# Check what pipx thinks is broken
pipx list

# Rebuild everything against the new Python
pipx reinstall-all
```

Verify:

```bash
ansible --version
ls -la ~/.local/share/pipx/venvs/ansible/bin/python
```

The symlink should now point at the new Python (e.g. `python3.14`), and `ansible --version` should run cleanly.

### 3. Check out the matching branch for your new Fedora version

This repo uses a branch per Fedora version (`F42`, `F43`, etc.). After a version upgrade, your working copy is on the **old** branch.

```bash
cd ~/Projects/fedora-desktop

# See what branch you are on vs what your system needs
git branch --show-current
cat /etc/fedora-release
cat vars/fedora-version.yml

# Fetch and switch to the matching branch
git fetch origin
git checkout F<NEW_VERSION>   # e.g. git checkout F43
git pull
```

If the branch for your Fedora version does not yet exist upstream, stay on the previous branch — the playbooks from the old branch will still mostly work, and the new branch is the maintainer's signal that the upgrade has been tested.

### 4. Refresh Ansible collections and roles

```bash
cd ~/Projects/fedora-desktop
ansible-galaxy install -r requirements.yml --force
```

`--force` ensures collections pinned in `requirements.yml` are actually updated, not skipped because an older copy exists.

### 5. Re-run the main playbook to reconcile drift

The main playbook is idempotent. Running it after an upgrade will:

- Reinstall any DNF packages that were dropped or renamed by the new Fedora release
- Re-apply config file deployments that the upgrade may have replaced with `.rpmnew`
- Fix ownership on directories that `dnf system-upgrade` touched as root
- Reconfigure repos that the upgrade disabled (e.g. RPM Fusion often needs the version-specific URL refreshed — handled by `play-rpm-fusion.yml`)

```bash
cd ~/Projects/fedora-desktop
./run.bash
```

`run.bash` detects that Ansible is already installed (step 3 in its output) and moves straight to the main playbook. If it prompts for GitHub auth, vault password, etc., your existing config is still in place and you can accept the "keep existing" options.

### 6. Re-run any optional playbooks you rely on

Optional playbooks are not run by the main playbook. If you previously installed things like `docker`, `ddev`, `vscode`, `python` (pyenv), re-run them so any version-specific logic picks up the new Fedora release:

```bash
cd ~/Projects/fedora-desktop
./run.bash --optional-only
```

This skips the main-install phase and goes straight to the optional menu.

### 7. Deal with `.rpmnew` / `.rpmsave` files

The upgrade leaves these when it can't safely merge a config change:

```bash
# Find them
sudo find /etc -name '*.rpmnew' -o -name '*.rpmsave' 2>/dev/null
```

For files this repo manages via Ansible (under `files/etc/...`), the main playbook in step 5 will have already overwritten them with the repo's version — you can delete the `.rpmnew`/`.rpmsave` leftovers. For files the repo does *not* manage, diff them and merge manually:

```bash
sudo diff /etc/some.conf /etc/some.conf.rpmnew
```

## What to Do If Something Still Fails

1. **Check the branch matches the Fedora version** — most "the playbook broke" reports after an upgrade come down to running the old branch against the new release.
2. **Look for an open issue on GitHub** filed against the new Fedora version — someone else may have hit it first.
3. **Run the failing playbook directly with `-vvv`** to see the actual Ansible error:
   ```bash
   ansible-playbook playbooks/imports/play-<name>.yml -vvv --ask-become-pass
   ```
4. **File an issue** using the automated reporter in `run.bash` (it will offer on failure) or manually at the repo's issue tracker.

## Why This Isn't a Script

The steps are stable in *shape* but not in *detail*:

- Which `.rpmnew` files appear depends on which packages Fedora changed that release
- Which optional playbooks you care about is per-user
- Python version transitions happen some releases and not others
- The Fedora upgrade procedure itself is the authoritative process, and baking a copy of it into this repo would invite drift

A checklist you read through once per upgrade is the right size for this job.
