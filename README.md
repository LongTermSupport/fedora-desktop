# Fedora Desktop

Taking a freshly installed Fedora and getting it ready for development work

Uses a bash script which installs basic dependencies including git and ansible and from there, clones this repo and uses ansible to do the full provision

## Run
curl, wget or just copy paste the [run.bash](./run.bash) script

Suggested to copy paste into your bash terminal:

```
(source <(curl -sS https://raw.githubusercontent.com/LongTermSupport/fedora-desktop/main/run.bash))
```

## Manual Tasks

Some manual, optional tasks

### Use Fedy to Install Various Things

https://github.com/rpmfusion-infra/fedy#installation

Fedy is a very convenient way to install various apps and common tweaks, such as:

* Google Chrome
* Slack
* Skype
* Microsoft Truetype Fonts
* Multimedia Codecs
* Lots of other things...

### Gnome Shell Extensions

**Suggest that you keep the number of extensions to a bare minimum to aid in stability and performance**

You need to open firefox, go to https://extensions.gnome.org/ and install the addon that is suggested

Once this is installed, you can add and enable/disable gnome shell extensions.

#### Pixel Saver
This extension is great for small screens/laptops or those who like to get the most out of their screen real estate

https://extensions.gnome.org/extension/723/pixel-saver/

### Firefox Extensions

https://addons.mozilla.org/en-GB/firefox/addon/ublock-origin/