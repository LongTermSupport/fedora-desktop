
## Timed capture, 20s, starting 2026-09-30 12:07:05 BST

### per-second rates during the capture
READ THIS FOR: touchpad/s near 0 while moving = the pad stopped reporting; acpi-sci/s or throttle+ spikes at onset = H3; dgpu flipping state = H3

+ local secs=20 i gpu=
+ local i2c elan tp gfx gpe thr
+ local n_i2c n_elan n_tp n_gfx n_gpe n_thr
++ dgpu_dir
++ local d
++ for d in /sys/bus/pci/devices/*
+++ cat /sys/bus/pci/devices/0000:00:00.0/vendor
++ [[ 0x8086 == 0x10de ]]
++ for d in /sys/bus/pci/devices/*
+++ cat /sys/bus/pci/devices/0000:00:01.0/vendor
++ [[ 0x8086 == 0x10de ]]
++ for d in /sys/bus/pci/devices/*
+++ cat /sys/bus/pci/devices/0000:00:02.0/vendor
++ [[ 0x8086 == 0x10de ]]
++ for d in /sys/bus/pci/devices/*
+++ cat /sys/bus/pci/devices/0000:00:04.0/vendor
++ [[ 0x8086 == 0x10de ]]
++ for d in /sys/bus/pci/devices/*
+++ cat /sys/bus/pci/devices/0000:00:06.0/vendor
++ [[ 0x8086 == 0x10de ]]
++ for d in /sys/bus/pci/devices/*
+++ cat /sys/bus/pci/devices/0000:00:06.2/vendor
++ [[ 0x8086 == 0x10de ]]
++ for d in /sys/bus/pci/devices/*
+++ cat /sys/bus/pci/devices/0000:00:07.0/vendor
++ [[ 0x8086 == 0x10de ]]
++ for d in /sys/bus/pci/devices/*
+++ cat /sys/bus/pci/devices/0000:00:07.2/vendor
++ [[ 0x8086 == 0x10de ]]
++ for d in /sys/bus/pci/devices/*
+++ cat /sys/bus/pci/devices/0000:00:0a.0/vendor
++ [[ 0x8086 == 0x10de ]]
++ for d in /sys/bus/pci/devices/*
+++ cat /sys/bus/pci/devices/0000:00:0b.0/vendor
++ [[ 0x8086 == 0x10de ]]
++ for d in /sys/bus/pci/devices/*
+++ cat /sys/bus/pci/devices/0000:00:0d.0/vendor
++ [[ 0x8086 == 0x10de ]]
++ for d in /sys/bus/pci/devices/*
+++ cat /sys/bus/pci/devices/0000:00:0d.2/vendor
++ [[ 0x8086 == 0x10de ]]
++ for d in /sys/bus/pci/devices/*
+++ cat /sys/bus/pci/devices/0000:00:0d.3/vendor
++ [[ 0x8086 == 0x10de ]]
++ for d in /sys/bus/pci/devices/*
+++ cat /sys/bus/pci/devices/0000:00:14.0/vendor
++ [[ 0x8086 == 0x10de ]]
++ for d in /sys/bus/pci/devices/*
+++ cat /sys/bus/pci/devices/0000:00:14.2/vendor
++ [[ 0x8086 == 0x10de ]]
++ for d in /sys/bus/pci/devices/*
+++ cat /sys/bus/pci/devices/0000:00:14.3/vendor
++ [[ 0x8086 == 0x10de ]]
++ for d in /sys/bus/pci/devices/*
+++ cat /sys/bus/pci/devices/0000:00:15.0/vendor
++ [[ 0x8086 == 0x10de ]]
++ for d in /sys/bus/pci/devices/*
+++ cat /sys/bus/pci/devices/0000:00:16.0/vendor
++ [[ 0x8086 == 0x10de ]]
++ for d in /sys/bus/pci/devices/*
+++ cat /sys/bus/pci/devices/0000:00:16.3/vendor
++ [[ 0x8086 == 0x10de ]]
++ for d in /sys/bus/pci/devices/*
+++ cat /sys/bus/pci/devices/0000:00:1c.0/vendor
++ [[ 0x8086 == 0x10de ]]
++ for d in /sys/bus/pci/devices/*
+++ cat /sys/bus/pci/devices/0000:00:1f.0/vendor
++ [[ 0x8086 == 0x10de ]]
++ for d in /sys/bus/pci/devices/*
+++ cat /sys/bus/pci/devices/0000:00:1f.3/vendor
++ [[ 0x8086 == 0x10de ]]
++ for d in /sys/bus/pci/devices/*
+++ cat /sys/bus/pci/devices/0000:00:1f.4/vendor
++ [[ 0x8086 == 0x10de ]]
++ for d in /sys/bus/pci/devices/*
+++ cat /sys/bus/pci/devices/0000:00:1f.5/vendor
++ [[ 0x8086 == 0x10de ]]
++ for d in /sys/bus/pci/devices/*
+++ cat /sys/bus/pci/devices/0000:00:1f.6/vendor
++ [[ 0x8086 == 0x10de ]]
++ for d in /sys/bus/pci/devices/*
+++ cat /sys/bus/pci/devices/0000:01:00.0/vendor
++ [[ 0x10de == 0x10de ]]
+++ cat /sys/bus/pci/devices/0000:01:00.0/class
++ [[ 0x030200 == 0x03* ]]
++ printf '%s\n' /sys/bus/pci/devices/0000:01:00.0
++ return 0
+ gpu=/sys/bus/pci/devices/0000:01:00.0
+ :
+ printf 'time      i2c/s  touchpad/s  trackpoint/s  i915/s  acpi-sci/s  throttle+  dgpu\n'
time      i2c/s  touchpad/s  trackpoint/s  i915/s  acpi-sci/s  throttle+  dgpu
+ snap
++ irq_total i2c_designware
++ awk -v re=i2c_designware $'\n        $1 ~ /^[0-9]+:$/ && $0 ~ re {\n            s = 0\n            for (i = 2; i <= NF; i++) { if ($i ~ /^[0-9]+$/) s += $i; else break }\n            print s; found = 1; exit\n        }\n        END { if (!found) exit 1 }' /proc/interrupts
+ i2c=15548507
++ irq_total ELAN
++ awk -v re=ELAN $'\n        $1 ~ /^[0-9]+:$/ && $0 ~ re {\n            s = 0\n            for (i = 2; i <= NF; i++) { if ($i ~ /^[0-9]+$/) s += $i; else break }\n            print s; found = 1; exit\n        }\n        END { if (!found) exit 1 }' /proc/interrupts
+ elan=526667
++ irq_total ' 12-edge .*i8042'
++ awk -v 're= 12-edge .*i8042' $'\n        $1 ~ /^[0-9]+:$/ && $0 ~ re {\n            s = 0\n            for (i = 2; i <= NF; i++) { if ($i ~ /^[0-9]+$/) s += $i; else break }\n            print s; found = 1; exit\n        }\n        END { if (!found) exit 1 }' /proc/interrupts
+ tp=613
++ irq_total i915
++ awk -v re=i915 $'\n        $1 ~ /^[0-9]+:$/ && $0 ~ re {\n            s = 0\n            for (i = 2; i <= NF; i++) { if ($i ~ /^[0-9]+$/) s += $i; else break }\n            print s; found = 1; exit\n        }\n        END { if (!found) exit 1 }' /proc/interrupts
+ gfx=1486323
++ awk '{print $1}' /sys/firmware/acpi/interrupts/sci
+ gpe=90828
++ cat /sys/devices/system/cpu/cpu0/thermal_throttle/package_throttle_count
+ thr=264
+ (( i = 0 ))
+ (( i < secs ))
+ n_i2c=15548507
+ n_elan=526667
+ n_tp=613
+ n_gfx=1486323
+ n_gpe=90828
+ n_thr=264
+ sleep 1
+ snap
++ irq_total i2c_designware
++ awk -v re=i2c_designware $'\n        $1 ~ /^[0-9]+:$/ && $0 ~ re {\n            s = 0\n            for (i = 2; i <= NF; i++) { if ($i ~ /^[0-9]+$/) s += $i; else break }\n            print s; found = 1; exit\n        }\n        END { if (!found) exit 1 }' /proc/interrupts
+ i2c=15548507
++ irq_total ELAN
++ awk -v re=ELAN $'\n        $1 ~ /^[0-9]+:$/ && $0 ~ re {\n            s = 0\n            for (i = 2; i <= NF; i++) { if ($i ~ /^[0-9]+$/) s += $i; else break }\n            print s; found = 1; exit\n        }\n        END { if (!found) exit 1 }' /proc/interrupts
+ elan=526667
++ irq_total ' 12-edge .*i8042'
++ awk -v 're= 12-edge .*i8042' $'\n        $1 ~ /^[0-9]+:$/ && $0 ~ re {\n            s = 0\n            for (i = 2; i <= NF; i++) { if ($i ~ /^[0-9]+$/) s += $i; else break }\n            print s; found = 1; exit\n        }\n        END { if (!found) exit 1 }' /proc/interrupts
+ tp=613
++ irq_total i915
++ awk -v re=i915 $'\n        $1 ~ /^[0-9]+:$/ && $0 ~ re {\n            s = 0\n            for (i = 2; i <= NF; i++) { if ($i ~ /^[0-9]+$/) s += $i; else break }\n            print s; found = 1; exit\n        }\n        END { if (!found) exit 1 }' /proc/interrupts
+ gfx=1486483
++ awk '{print $1}' /sys/firmware/acpi/interrupts/sci
+ gpe=90828
++ cat /sys/devices/system/cpu/cpu0/thermal_throttle/package_throttle_count
+ thr=264
++ date +%T
++ [[ -n /sys/bus/pci/devices/0000:01:00.0 ]]
++ cat /sys/bus/pci/devices/0000:01:00.0/power/runtime_status
+ printf '%s %6d %11d %13d %7d %11d %10d  %s\n' 12:07:06 0 0 0 160 0 0 active
12:07:06      0           0             0     160           0          0  active
+ (( i++ ))
+ (( i < secs ))
+ n_i2c=15548507
+ n_elan=526667
+ n_tp=613
+ n_gfx=1486483
+ n_gpe=90828
+ n_thr=264
+ sleep 1
+ snap
++ irq_total i2c_designware
++ awk -v re=i2c_designware $'\n        $1 ~ /^[0-9]+:$/ && $0 ~ re {\n            s = 0\n            for (i = 2; i <= NF; i++) { if ($i ~ /^[0-9]+$/) s += $i; else break }\n            print s; found = 1; exit\n        }\n        END { if (!found) exit 1 }' /proc/interrupts
+ i2c=15548507
++ irq_total ELAN
++ awk -v re=ELAN $'\n        $1 ~ /^[0-9]+:$/ && $0 ~ re {\n            s = 0\n            for (i = 2; i <= NF; i++) { if ($i ~ /^[0-9]+$/) s += $i; else break }\n            print s; found = 1; exit\n        }\n        END { if (!found) exit 1 }' /proc/interrupts
+ elan=526667
++ irq_total ' 12-edge .*i8042'
++ awk -v 're= 12-edge .*i8042' $'\n        $1 ~ /^[0-9]+:$/ && $0 ~ re {\n            s = 0\n            for (i = 2; i <= NF; i++) { if ($i ~ /^[0-9]+$/) s += $i; else break }\n            print s; found = 1; exit\n        }\n        END { if (!found) exit 1 }' /proc/interrupts
+ tp=613
++ irq_total i915
++ awk -v re=i915 $'\n        $1 ~ /^[0-9]+:$/ && $0 ~ re {\n            s = 0\n            for (i = 2; i <= NF; i++) { if ($i ~ /^[0-9]+$/) s += $i; else break }\n            print s; found = 1; exit\n        }\n        END { if (!found) exit 1 }' /proc/interrupts
+ gfx=1486643
++ awk '{print $1}' /sys/firmware/acpi/interrupts/sci
+ gpe=90828
++ cat /sys/devices/system/cpu/cpu0/thermal_throttle/package_throttle_count
+ thr=264
++ date +%T
++ [[ -n /sys/bus/pci/devices/0000:01:00.0 ]]
++ cat /sys/bus/pci/devices/0000:01:00.0/power/runtime_status
+ printf '%s %6d %11d %13d %7d %11d %10d  %s\n' 12:07:07 0 0 0 160 0 0 active
12:07:07      0           0             0     160           0          0  active
+ (( i++ ))
+ (( i < secs ))
+ n_i2c=15548507
+ n_elan=526667
+ n_tp=613
+ n_gfx=1486643
+ n_gpe=90828
+ n_thr=264
+ sleep 1
+ snap
++ irq_total i2c_designware
++ awk -v re=i2c_designware $'\n        $1 ~ /^[0-9]+:$/ && $0 ~ re {\n            s = 0\n            for (i = 2; i <= NF; i++) { if ($i ~ /^[0-9]+$/) s += $i; else break }\n            print s; found = 1; exit\n        }\n        END { if (!found) exit 1 }' /proc/interrupts
+ i2c=15548507
++ irq_total ELAN
++ awk -v re=ELAN $'\n        $1 ~ /^[0-9]+:$/ && $0 ~ re {\n            s = 0\n            for (i = 2; i <= NF; i++) { if ($i ~ /^[0-9]+$/) s += $i; else break }\n            print s; found = 1; exit\n        }\n        END { if (!found) exit 1 }' /proc/interrupts
+ elan=526667
++ irq_total ' 12-edge .*i8042'
++ awk -v 're= 12-edge .*i8042' $'\n        $1 ~ /^[0-9]+:$/ && $0 ~ re {\n            s = 0\n            for (i = 2; i <= NF; i++) { if ($i ~ /^[0-9]+$/) s += $i; else break }\n            print s; found = 1; exit\n        }\n        END { if (!found) exit 1 }' /proc/interrupts
+ tp=613
++ irq_total i915
++ awk -v re=i915 $'\n        $1 ~ /^[0-9]+:$/ && $0 ~ re {\n            s = 0\n            for (i = 2; i <= NF; i++) { if ($i ~ /^[0-9]+$/) s += $i; else break }\n            print s; found = 1; exit\n        }\n        END { if (!found) exit 1 }' /proc/interrupts
+ gfx=1486759
++ awk '{print $1}' /sys/firmware/acpi/interrupts/sci
+ gpe=90828
++ cat /sys/devices/system/cpu/cpu0/thermal_throttle/package_throttle_count
+ thr=264
++ date +%T
++ [[ -n /sys/bus/pci/devices/0000:01:00.0 ]]
++ cat /sys/bus/pci/devices/0000:01:00.0/power/runtime_status
+ printf '%s %6d %11d %13d %7d %11d %10d  %s\n' 12:07:08 0 0 0 116 0 0 active
12:07:08      0           0             0     116           0          0  active
+ (( i++ ))
+ (( i < secs ))
+ n_i2c=15548507
+ n_elan=526667
+ n_tp=613
+ n_gfx=1486759
+ n_gpe=90828
+ n_thr=264
+ sleep 1
+ snap
++ irq_total i2c_designware
++ awk -v re=i2c_designware $'\n        $1 ~ /^[0-9]+:$/ && $0 ~ re {\n            s = 0\n            for (i = 2; i <= NF; i++) { if ($i ~ /^[0-9]+$/) s += $i; else break }\n            print s; found = 1; exit\n        }\n        END { if (!found) exit 1 }' /proc/interrupts
+ i2c=15548507
++ irq_total ELAN
++ awk -v re=ELAN $'\n        $1 ~ /^[0-9]+:$/ && $0 ~ re {\n            s = 0\n            for (i = 2; i <= NF; i++) { if ($i ~ /^[0-9]+$/) s += $i; else break }\n            print s; found = 1; exit\n        }\n        END { if (!found) exit 1 }' /proc/interrupts
+ elan=526667
++ irq_total ' 12-edge .*i8042'
++ awk -v 're= 12-edge .*i8042' $'\n        $1 ~ /^[0-9]+:$/ && $0 ~ re {\n            s = 0\n            for (i = 2; i <= NF; i++) { if ($i ~ /^[0-9]+$/) s += $i; else break }\n            print s; found = 1; exit\n        }\n        END { if (!found) exit 1 }' /proc/interrupts
+ tp=613
++ irq_total i915
++ awk -v re=i915 $'\n        $1 ~ /^[0-9]+:$/ && $0 ~ re {\n            s = 0\n            for (i = 2; i <= NF; i++) { if ($i ~ /^[0-9]+$/) s += $i; else break }\n            print s; found = 1; exit\n        }\n        END { if (!found) exit 1 }' /proc/interrupts
+ gfx=1486910
++ awk '{print $1}' /sys/firmware/acpi/interrupts/sci
+ gpe=90828
++ cat /sys/devices/system/cpu/cpu0/thermal_throttle/package_throttle_count
+ thr=264
++ date +%T
++ [[ -n /sys/bus/pci/devices/0000:01:00.0 ]]
++ cat /sys/bus/pci/devices/0000:01:00.0/power/runtime_status
+ printf '%s %6d %11d %13d %7d %11d %10d  %s\n' 12:07:09 0 0 0 151 0 0 active
12:07:09      0           0             0     151           0          0  active
+ (( i++ ))
+ (( i < secs ))
+ n_i2c=15548507
+ n_elan=526667
+ n_tp=613
+ n_gfx=1486910
+ n_gpe=90828
+ n_thr=264
+ sleep 1
+ snap
++ irq_total i2c_designware
++ awk -v re=i2c_designware $'\n        $1 ~ /^[0-9]+:$/ && $0 ~ re {\n            s = 0\n            for (i = 2; i <= NF; i++) { if ($i ~ /^[0-9]+$/) s += $i; else break }\n            print s; found = 1; exit\n        }\n        END { if (!found) exit 1 }' /proc/interrupts
+ i2c=15548507
++ irq_total ELAN
++ awk -v re=ELAN $'\n        $1 ~ /^[0-9]+:$/ && $0 ~ re {\n            s = 0\n            for (i = 2; i <= NF; i++) { if ($i ~ /^[0-9]+$/) s += $i; else break }\n            print s; found = 1; exit\n        }\n        END { if (!found) exit 1 }' /proc/interrupts
+ elan=526667
++ irq_total ' 12-edge .*i8042'
++ awk -v 're= 12-edge .*i8042' $'\n        $1 ~ /^[0-9]+:$/ && $0 ~ re {\n            s = 0\n            for (i = 2; i <= NF; i++) { if ($i ~ /^[0-9]+$/) s += $i; else break }\n            print s; found = 1; exit\n        }\n        END { if (!found) exit 1 }' /proc/interrupts
+ tp=613
++ irq_total i915
++ awk -v re=i915 $'\n        $1 ~ /^[0-9]+:$/ && $0 ~ re {\n            s = 0\n            for (i = 2; i <= NF; i++) { if ($i ~ /^[0-9]+$/) s += $i; else break }\n            print s; found = 1; exit\n        }\n        END { if (!found) exit 1 }' /proc/interrupts
+ gfx=1487082
++ awk '{print $1}' /sys/firmware/acpi/interrupts/sci
+ gpe=90850
++ cat /sys/devices/system/cpu/cpu0/thermal_throttle/package_throttle_count
+ thr=264
++ date +%T
++ [[ -n /sys/bus/pci/devices/0000:01:00.0 ]]
++ cat /sys/bus/pci/devices/0000:01:00.0/power/runtime_status
+ printf '%s %6d %11d %13d %7d %11d %10d  %s\n' 12:07:10 0 0 0 172 22 0 active
12:07:10      0           0             0     172          22          0  active
+ (( i++ ))
+ (( i < secs ))
+ n_i2c=15548507
+ n_elan=526667
+ n_tp=613
+ n_gfx=1487082
+ n_gpe=90850
+ n_thr=264
+ sleep 1
++ trap - EXIT INT
++ stop_background
++ local pid rc ok=0 f
++ for pid in "${BG_STOP_PIDS[@]+"${BG_STOP_PIDS[@]}"}"
++ kill -TERM 2575412
++ for pid in "${BG_STOP_PIDS[@]+"${BG_STOP_PIDS[@]}"}"
++ kill -TERM 2575516
++ for pid in "${BG_STOP_PIDS[@]+"${BG_STOP_PIDS[@]}"}"
++ kill -TERM 2575643
++ for pid in "${BG_WAIT_PIDS[@]+"${BG_WAIT_PIDS[@]}"}"
++ wait 2575411
++ rc=0
++ printf '[teardown] background job %s ended (rc=%s)\n' 2575411 0
[teardown] background job 2575411 ended (rc=0)
++ for pid in "${BG_WAIT_PIDS[@]+"${BG_WAIT_PIDS[@]}"}"
++ wait 2575412
++ rc=143
++ printf '[teardown] background job %s ended (rc=%s)\n' 2575412 143
[teardown] background job 2575412 ended (rc=143)
++ for pid in "${BG_WAIT_PIDS[@]+"${BG_WAIT_PIDS[@]}"}"
++ wait 2575516
++ rc=143
++ printf '[teardown] background job %s ended (rc=%s)\n' 2575516 143
[teardown] background job 2575516 ended (rc=143)
++ for pid in "${BG_WAIT_PIDS[@]+"${BG_WAIT_PIDS[@]}"}"
++ wait 2575642
++ rc=0
++ printf '[teardown] background job %s ended (rc=%s)\n' 2575642 0
[teardown] background job 2575642 ended (rc=0)
++ for pid in "${BG_WAIT_PIDS[@]+"${BG_WAIT_PIDS[@]}"}"
++ wait 2575643
++ rc=143
++ printf '[teardown] background job %s ended (rc=%s)\n' 2575643 143
[teardown] background job 2575643 ended (rc=143)
++ BG_STOP_PIDS=()
++ BG_WAIT_PIDS=()
++ [[ -n /sys/kernel/tracing/instances/plan00143-2575225 ]]
++ sudo -n test -d /sys/kernel/tracing/instances/plan00143-2575225
++ sudo -n rmdir /sys/kernel/tracing/instances/plan00143-2575225
++ TRACE_INSTANCE=
++ for f in "${FIFOS[@]+"${FIFOS[@]}"}"
++ rm -f <home>/Projects/fedora-desktop/untracked/scratch/00143/standalone/libinput-pointer-events.txt.fifo
++ for f in "${FIFOS[@]+"${FIFOS[@]}"}"
++ rm -f <home>/Projects/fedora-desktop/untracked/scratch/00143/standalone/display-trace.txt.fifo
++ FIFOS=()
++ return 0
++ kill -INT 2575225
