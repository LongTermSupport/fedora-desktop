#!/usr/bin/env bash

## Setup
set -euo pipefail
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

promptForValue(){
  local item v yn
  item="$1"
  while [[ "$yn" != "y" ]]; do
    read -sp "Please enter your $item:" v
    read -sp "Confirm correct value for $item is $v" -n 1 yn
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
    read -sp "please confirm you have saved your new key in github" -n 1 yn
    case $yn in
        [Yy]* ) break;;
        [Nn]* ) continue;;
        * ) echo "Please answer yes or no.";;
    esac
done

title "Creating Projects directory"
mkdir -p ~/Projects
completed

title "Cloning Fedora Desktop Repo"
cd ~/Projects
if [[ ! -d ~/Projects/fedora-desktop ]]; then
  git@github.com:LongTermSupport/fedora-desktop.git
fi
completed


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

title "Now running Ansible to compelte configuration"
cd ~/Projects/fedora-desktop
./playbooks/playbook-main.yml
completed
exit 0
