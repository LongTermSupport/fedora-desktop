#!/usr/bin/env bash

## Setup
set -euo pipefail
standardIFS="$IFS"
IFS=$'\n\t'

## Assertions
## Functions

title(){
  printf "\n$1\n"
}

completed(){
  printf "\nDone...\n"
}

## Process

echo "

Process Starting

You will be asked for sudo password

"
title "Installing Dependencies with DNF"
sudo dnf -y install \
  git \
  python3 \
  python3-pip \
  grubby
completed

title "Updating Grub Configs for Cgroups"
grubby --update-kernel=ALL --args="systemd.unified_cgroup_hierarchy=0"
completed

title "Installing Ansible with Pip"
pip3 install ansible
completed

title "Creating SSH Key Pair\n\nNOTE - you must set a password\n\nSuggest you use your login password"
ssh-keygen -t ed25519

title "You now need to save this public key to your github account"
title "https://github.com/settings/ssh/new"
cat ~/.ssh/id_ed25519.pub
while true; do
    read -p "please confirm you have saved your new key in github" yn
    case $yn in
        [Yy]* ) break;;
        [Nn]* ) continue;;
        * ) echo "Please answer yes or no.";;
    esac
done

title "Creating Projects directory"
mkdir -p ~/Projects
cd ~/Projects
completed

title "Cloning Fedora Desktop Repo"
git@github.com:LongTermSupport/fedora-desktop.git
completed

cd fedora-desktop

echo "

Some manual configuration required:

First, copy the host config:

  cp environment/localhost/host_vars/localhost.yml.dist environment/localhost/host_vars/localhost.yml

You need to edit that file and replace all values with correct information

  vim environment/localhost/host_vars/localhost.yml

Then you need to run this command:

  ~/Projects/fedora-desktop/playbooks/playbook-main.yml

"
exit 0
