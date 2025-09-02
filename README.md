# Fedora Desktop

Taking a freshly installed Fedora and getting it ready for development work

Uses a bash script which installs basic dependencies including git and ansible and from there, clones this repo and uses ansible to do the full provision

## Manual Task - Install Fedora

First you need to install Fedora. The target version for this branch is defined in `vars/fedora-version.yml`.

## Fedora Version Branching Strategy

This repository uses a branching strategy where each Fedora version has its own branch:

- **Branch Naming**: `F<VERSION>` (e.g., `F42`, `F43`)
- **Default Branch**: Updated to the latest Fedora version being worked on
- **Version Configuration**: Each branch has its target Fedora version defined in `vars/fedora-version.yml`

### Branch Lifecycle

- **Active Development**: Latest Fedora version branch
- **Maintenance**: Previous version branches receive critical fixes only
- **Archive**: Older branches are kept for reference but not actively maintained

This repo is in active development and is generally updated shortly after each Fedora release.


It is **very strongly recommended** that you encrypt the main root filesystem.

### Enable Third Party Repos
There is an option to enable third party repos as you are installing Fedora. You need to accept this.

**Make sure you opt to enable third party repos on the Fedora install**



## Run

Once you have an install, have logged in and created your main desktop user - then run this command, as your normal desktop user.

curl, wget or just copy paste the [run.bash](./run.bash) script

Suggested to copy paste into your bash terminal:

```
(source <(curl -sS https://raw.githubusercontent.com/LongTermSupport/fedora-desktop/HEAD/run.bash?$(date +%s)))
```

## Manual Tasks

Some manual, optional tasks

### Run Extra Playbooks

There are some playbooks which are not currently run as part of the main playbook.

You can also create your own.

You would run these with, for example:

```bash
ansible-playbook ./playbooks/imports/play-install-flatpaks.yml
```

## Development

### Creating a New Fedora Version Branch

When a new Fedora version is released, follow these steps to create a new branch:

```bash
# 1. Update the Fedora version in the centralized config
vim vars/fedora-version.yml
# Change: fedora_version: 43

# 2. Commit the version update
git add vars/fedora-version.yml
git commit -m "Update target Fedora version to 43"

# 3. Create and push the new branch
git checkout -b F43
git push -u origin F43

# 4. Set the new branch as default on GitHub
gh repo edit --default-branch F43

# 5. Update any branch-specific documentation or configurations as needed
```

### Development

Written using pycharm community and these extensions

https://plugins.jetbrains.com/plugin/14893-ansible
https://plugins.jetbrains.com/plugin/14278-ansible-vault-editor
