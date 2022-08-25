# Fedora Desktop

Taking a freshly installed Fedora and getting it ready for development work

Uses a bash script which installs basic dependencies including git and ansible and from there, clones this repo and uses ansible to do the full provision

## Manual Task - Install Fedora

First you need to install Fedora. Currently, this repo is targeting F36.

For standard desktop use it is suggested that you install with custom partitioning and avoid having a separate root and home directory. Generally trying to maintain the same home directory whilst switching versions of OS is an advanced move and generally it's cleaner to just rebuild everything so its simpler to have one partition for everything.

A suggested partition configuration might be:

| mount point | size | format | notes         |
|-------------|------|--------|---------------|
| /boot     | 500M | ext4 |               |
| /boot/efi | 100M | efi |               |
| /swap     | half RAM size | swap |               |
| /         | all available spare space| ext4 or btrfs | **encrypted** |


It is **very strongly recommended** that you do opt to encrypt the main root filesystem.

### Enable Third Party Repos
There is an option to enable third party repos as you are installing Fedora. You need to accept this.

**Make sure you opt to enable third party repos on the Fedora install**



## Run

Once you have an install, have logged in and created your main desktop user - then run this command, as your normal desktop user.

curl, wget or just copy paste the [run.bash](./run.bash) script

Suggested to copy paste into your bash terminal:

```
(source <(curl -sS https://raw.githubusercontent.com/LongTermSupport/fedora-desktop/main/run.bash))
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

### Gnome Shell Extensions

**Suggest that you keep the number of extensions to a bare minimum to aid in stability and performance**

You need to open firefox, go to https://extensions.gnome.org/ and install the addon that is suggested

Once this is installed, you can add and enable/disable gnome shell extensions.

#### GTK Title Bar
This extension is great for small screens/laptops or those who like to get the most out of their screen real estate.
It removes the stupidly fat title bars on windows like PHPStorm

https://extensions.gnome.org/extension/1732/gtk-title-bar/

### Dash to Dock

This one makes the dash work more like a dock, basically you can access it by just moving your mouse to the bottom fo the screen instead of having to load the overview

https://extensions.gnome.org/extension/307/dash-to-dock/

### Ubuntu Like Panel

Purely aesthetic, this makes the top panel translucent and nice

https://extensions.gnome.org/extension/2660/transparent-panel/

### Firefox Extensions

https://addons.mozilla.org/en-GB/firefox/addon/ublock-origin/
