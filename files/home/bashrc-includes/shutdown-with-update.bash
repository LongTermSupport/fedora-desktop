#!/bin/bash
# Shutdown with Update - Updates firmware and packages before shutdown

alias shutdown-with-update='sudo bash -c "
    echo \"Starting firmware updates...\" && 
    fwupdmgr refresh --force && 
    fwupdmgr update -y && 
    echo \"Starting system package updates...\" && 
    dnf -y upgrade && 
    echo \"Updates complete. Checking for kernel module builds...\" && 
    if systemctl is-active --quiet akmods; then 
        echo \"Waiting for akmods to complete kernel module builds...\"; 
        timeout 300 bash -c \"while systemctl is-active --quiet akmods; do sleep 5; done\" && 
        echo \"Kernel modules built successfully.\"; 
    fi && 
    echo \"Attempting shutdown...\" && 
    if ! shutdown -h now 2>&1; then 
        echo \"\"; 
        echo \"Shutdown blocked by inhibitors or logged-in users.\"; 
        echo \"Current inhibitors:\"; 
        systemd-inhibit --list --no-pager | grep -v \"^WHO\"; 
        echo \"\"; 
        read -p \"Force shutdown anyway? (y/N): \" -n 1 -r; 
        echo \"\"; 
        if [[ \$REPLY =~ ^[Yy]$ ]]; then 
            echo \"Forcing shutdown...\"; 
            systemctl poweroff -i; 
        else 
            echo \"Shutdown cancelled.\"; 
        fi; 
    fi
"'