## 16:36 · finding · —   — An unattended --yes run waited at an optional secret prompt

The host agent's meta-deploy (run.bash --changed --yes, from a terminal) sat at play-ftp-camera's "Prompt for the camera hotspot Wi-Fi password" for about 50 minutes, until it pressed Enter. The queue text said an optional secret is skipped on that unattended run, but nothing did that: the camera FTP password was not asked only because it was already saved there.

Fix: run.bash exports ASSUME_YES for --yes, and tasks/ask-and-save-secret.yml reads it. On such a run an optional secret is skipped at once (the skip message says the run is unattended) and a required one fails at once with the vault.bash way to save it. A headless run needs nothing new: with no terminal on stdin, Ansible's pause returns at once with no answer, so an optional secret is skipped and a required one fails after its three attempts with the same advice.

run.bash is at its bash-limits ratchet, so the export replaces the plain assignment and the --yes help text was reworded into the same two lines.
