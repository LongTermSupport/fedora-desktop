#!/usr/bin/env bash

## Setup
set -e
set -u
set -o pipefail
standardIFS="$IFS"
IFS=$'\n\t'

## Assertions
if [[ "$(whoami)" == "root" ]];
then
  printf "\n\n ERROR - please do not run this as root\n\nSimply run as your normal user\n\n"
  exit 1
fi

## Functions

title(){
  printf "\n$1\n"
}

completed(){
  printf "\nDone...\n"
}

confirm(){
  local msg="$1"
  local yn=n
  while [[ "$yn" != "y" ]]; do
    echo
    read -sp "$msg (y/n)" -n 1 yn
  done
  printf "\n\n$msg confirmed\n\n"
}

promptForValue(){
  local item v yn
  item="$1"
  yn=n
  while [[ "$yn" != "y" ]]; do
    echo
    read -p "Please enter your $item: " v
    echo
    read -sp "Confirm correct value for $item is $v (y/n) " -n 1 yn
  done
  echo "$v"
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
sudo grubby --update-kernel=ALL --args="systemd.unified_cgroup_hierarchy=0"
completed

title "Installing Ansible with Pip"
pip3 install ansible
completed

title "Creating SSH Key Pair\n\nNOTE - you must set a password\n\nSuggest you use your login password"
if [[ ! -f ~/.ssh/id_ed25519 ]]; then
  ssh-keygen -t ed25519
else
  echo " - found existing key"
fi
completed

title "You now need to save this public key to your github account"
echo "URL: https://github.com/settings/ssh/new"
printf "\nSSH Key to copy/paste below:\n\n"
cat ~/.ssh/id_ed25519.pub
printf "\n\nplease confirm you have saved your new key in github\n"
confirm "key saved in github"
completed

title "Creating Projects directory"
mkdir -p ~/Projects
completed

title "Cloning Fedora Desktop Repo"
cd ~/Projects
if [[ ! -d ~/Projects/fedora-desktop ]]; then
  git clone git@github.com:LongTermSupport/fedora-desktop.git
fi
completed


if [[ ! -f ~/Projects/fedora-desktop/environment/localhost/host_vars/localhost.yml ]]; then
  title "Collecting required configs"
  echo "Your user login (probably $(whoami))"
  user_login="$(promptForValue user_login)"

  echo "Your full name"
  user_name="$(promptForValue user_full_name)"

  echo "Your email address"
  user_email="$(promptForValue user_email)"
  completed

  title "Updating ansible localhost config in environment/localhost/host_vars/localhost.yml"
  cat <<EOF > ~/Projects/fedora-desktop/environment/localhost/host_vars/localhost.yml
user_login: "$user_login"
user_name: "$user_name"
user_email: "$user_email"
EOF
  completed
fi

title "Ensuring repo is up to date"
cd ~/Projects/fedora-desktop
git pull
completed

title "Now running Ansible to complete configuration"
# checking for passwordless sudo
if sudo -n true; then
  ./playbooks/playbook-main.yml
else
  ./playbooks/playbook-main.yml --ask-become-pass
fi
completed

title "And now your system needs to be rebooted"
confirm "Happy to Reboot ?"
sudo reboot now
