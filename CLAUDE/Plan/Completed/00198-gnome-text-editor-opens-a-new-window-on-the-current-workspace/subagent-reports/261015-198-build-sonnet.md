# 198 build report

Facts: RPM gnome-text-editor 50.1 (no Flatpak); --new-window and --standalone exist; desktop file DBusActivatable=true, Exec=gnome-text-editor %U; text/plain and text/markdown default to the editor; no editor or mutter setting controls this (focus-new-windows is smart).

Fix: play-gsettings.yml writes a per-user desktop file generated from the system one (DBusActivatable=false, Exec with --new-window), asserting the source lines first, refreshing the desktop database on change. acceptance.bash added; meta-deploy.list has the play then the plan folder.

Not run: Ansible, qa-all (container). Owner check remains: open files from Nautilus on two workspaces.
