## 18:46 · decision · —

Why `play-ZZZ-passwordless-sudo.yml` removes the kickstart's `99-fedora-desktop-setup` on every host, not only where a grant is kept:

- The earlier "only where kept" condition guarded against removing the rule partway through a run. A run whose only passwordless rule it was would then fail every later become task.
- At the end of the run that hazard is gone. Every later play run.bash starts probes sudo again and asks for the password.
- Keeping the condition would leave a hole. When the person answers yes to "Ready to reboot now?" at the end of first boot, the reboot kills the first-boot terminal before the wrapper's own removal runs. A host that declared false would then keep NOPASSWD:ALL for good.

The coordinator agreed (2026-11-18).

Known gap: issue #348. The lab's headless developer-desktop scenario stops at the declaration, because headless run.bash writes no switch.
