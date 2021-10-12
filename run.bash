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

done(){
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
done

title "Updating Grub Configs for Cgroups"
grubby --update-kernel=ALL --args="systemd.unified_cgroup_hierarchy=0"
done

title "Installing Ansible with Pip"
pip3 install ansible
done

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
mkdir ~/Projects
cd ~/Projects
done

title "Cloning Fedora Desktop Repo"
git@github.com:LongTermSupport/fedora-desktop.git
done

cd fedora-desktop
