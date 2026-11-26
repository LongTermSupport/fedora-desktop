## 10:16 · finding · —

Laptop triage result, from the laptop host seat, relayed by the coordinator (10:16Z). The full
report is in the infrastructure repository's own plan, not this one.

- The memory sits in the single lightning-matrix process of each instance: anonymous heap, no
  child processes.
- From 09:15Z to 10:15Z, the busier team's instance (team A) grew by about 12.1 MiB/h (635 MB to
  648 MB). The other team's instance (team B) grew by about 1.8 MiB/h and stayed flat at about
  410 MB.
- Growth that slow fits the burst picture (H8, the stuck GUI thread around 17:09Z) better than a
  steady leak. The watch service (Task 1.7), once deployed with `lightning_diagnostics: true`,
  is what catches the next burst.
