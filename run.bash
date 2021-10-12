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
  python3-pip
done
  
title "Installing Ansible with Pip"
pip3 install ansible
done

title "Creating Projects directory"
mkdir ~/Projects
cd ~/Projects
done

title "Cloning Fedora Desktop Repo"
git clone https://github.com/LongTermSupport/fedora-desktop.git
done

cd fedora-desktop
