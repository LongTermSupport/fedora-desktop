#!/bin/bash
# DisplayLink Installation Status Checker

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color
BOLD='\033[1m'

echo -e "${BOLD}========================================================================${NC}"
echo -e "${BOLD}                 DISPLAYLINK INSTALLATION STATUS CHECK                 ${NC}"
echo -e "${BOLD}========================================================================${NC}"
echo ""

# Function to print status
print_status() {
    if [ "$1" -eq 0 ]; then
        echo -e "  ${GREEN}✅${NC} $2"
    else
        echo -e "  ${RED}❌${NC} $2"
    fi
}

# 1. Check Secure Boot Status
echo -e "${BLUE}${BOLD}1. SECURE BOOT & MOK STATUS${NC}"
echo -e "${BLUE}───────────────────────────${NC}"

SB_STATE=$(mokutil --sb-state 2>&1)
if echo "$SB_STATE" | grep -q "SecureBoot enabled"; then
    echo -e "  ${GREEN}✅${NC} Secure Boot: ${BOLD}Enabled${NC}"
    SECURE_BOOT=1
else
    echo -e "  ${YELLOW}⚠${NC}  Secure Boot: ${BOLD}Disabled${NC}"
    SECURE_BOOT=0
fi

if [ -f /var/lib/dkms/mok.pub ]; then
    MOK_TEST=$(mokutil --test-key /var/lib/dkms/mok.pub 2>&1)
    if echo "$MOK_TEST" | grep -q "is already enrolled"; then
        print_status 0 "MOK Key: ${BOLD}Successfully enrolled${NC}"
        MOK_ENROLLED=1
    elif echo "$MOK_TEST" | grep -q "is not enrolled"; then
        print_status 1 "MOK Key: ${BOLD}Not enrolled${NC} (requires reboot and enrollment)"
        MOK_ENROLLED=0
    else
        print_status 1 "MOK Key: ${BOLD}Unknown status${NC}"
        MOK_ENROLLED=0
    fi
else
    if [ "$SECURE_BOOT" -eq 1 ]; then
        print_status 1 "MOK Key: ${BOLD}Not found${NC} at /var/lib/dkms/mok.pub"
    else
        echo -e "  ${YELLOW}ℹ${NC}  MOK Key: Not needed (Secure Boot disabled)"
    fi
    MOK_ENROLLED=0
fi
echo ""

# 2. Check Package Installation
echo -e "${BLUE}${BOLD}2. PACKAGE INSTALLATION${NC}"
echo -e "${BLUE}────────────────────────${NC}"

DISPLAYLINK_PKG=$(rpm -qa | grep -i displaylink)
if [ -n "$DISPLAYLINK_PKG" ]; then
    print_status 0 "DisplayLink RPM: ${BOLD}$DISPLAYLINK_PKG${NC}"
else
    print_status 1 "DisplayLink RPM: ${BOLD}Not installed${NC}"
fi

if [ -d /usr/src/evdi-* ]; then
    EVDI_DIR=$(ls -d /usr/src/evdi-* 2>/dev/null | head -1)
    print_status 0 "Driver source: ${BOLD}$EVDI_DIR${NC}"
else
    print_status 1 "Driver source: ${BOLD}Not found${NC} in /usr/src/"
fi
echo ""

# 3. Check Kernel Module Status
echo -e "${BLUE}${BOLD}3. KERNEL MODULE STATUS${NC}"
echo -e "${BLUE}────────────────────────${NC}"

DKMS_STATUS=$(dkms status 2>/dev/null | grep evdi)
if [ -n "$DKMS_STATUS" ]; then
    if echo "$DKMS_STATUS" | grep -q "installed"; then
        print_status 0 "DKMS compilation: ${BOLD}$DKMS_STATUS${NC}"
        DKMS_OK=1
    else
        print_status 1 "DKMS compilation: ${BOLD}$DKMS_STATUS${NC}"
        DKMS_OK=0
    fi
else
    print_status 1 "DKMS compilation: ${BOLD}evdi module not found${NC}"
    DKMS_OK=0
fi

LSMOD_EVDI=$(lsmod | grep evdi)
if [ -n "$LSMOD_EVDI" ]; then
    REFS=$(echo "$LSMOD_EVDI" | awk '{print $3}')
    print_status 0 "Module loaded: ${BOLD}evdi${NC} (${REFS} references)"
    MODULE_LOADED=1
else
    print_status 1 "Module loaded: ${BOLD}evdi not loaded${NC}"
    MODULE_LOADED=0
fi

if [ "$MODULE_LOADED" -eq 1 ] && [ "$SECURE_BOOT" -eq 1 ]; then
    print_status 0 "Module signed: ${BOLD}Working with Secure Boot${NC}"
fi
echo ""

# 4. Check Service Status
echo -e "${BLUE}${BOLD}4. SERVICE STATUS${NC}"
echo -e "${BLUE}──────────────────${NC}"

SERVICE_STATUS=$(systemctl is-active displaylink-driver 2>/dev/null)
if [ "$SERVICE_STATUS" = "active" ]; then
    print_status 0 "DisplayLink service: ${BOLD}Running${NC}"
    UPTIME=$(systemctl show displaylink-driver -p ActiveEnterTimestamp --value 2>/dev/null)
    if [ -n "$UPTIME" ]; then
        echo -e "      Started: $UPTIME"
    fi
else
    print_status 1 "DisplayLink service: ${BOLD}$SERVICE_STATUS${NC}"
fi

SERVICE_ENABLED=$(systemctl is-enabled displaylink-driver 2>/dev/null)
echo -e "      Boot startup: ${BOLD}$SERVICE_ENABLED${NC}"

DLM_PROCESS=$(pgrep -f DisplayLinkManager)
if [ -n "$DLM_PROCESS" ]; then
    print_status 0 "Process active: ${BOLD}DisplayLinkManager${NC} (PID: $DLM_PROCESS)"
else
    print_status 1 "Process active: ${BOLD}DisplayLinkManager not running${NC}"
fi
echo ""

# 5. Check Graphics Integration
echo -e "${BLUE}${BOLD}5. GRAPHICS INTEGRATION${NC}"
echo -e "${BLUE}────────────────────────${NC}"

DRI_COUNT=$(ls /dev/dri/card* 2>/dev/null | wc -l)
if [ "$DRI_COUNT" -gt 0 ]; then
    print_status 0 "DRI devices: ${BOLD}$DRI_COUNT card devices${NC} available"
else
    print_status 1 "DRI devices: ${BOLD}No card devices found${NC}"
fi

RENDER_COUNT=$(ls /dev/dri/renderD* 2>/dev/null | wc -l)
if [ "$RENDER_COUNT" -gt 0 ]; then
    print_status 0 "Render nodes: ${BOLD}$RENDER_COUNT render nodes${NC} for acceleration"
else
    echo -e "  ${YELLOW}⚠${NC}  Render nodes: None found"
fi
echo ""

# 6. Check Connected Devices
echo -e "${BLUE}${BOLD}6. CONNECTED DEVICES${NC}"
echo -e "${BLUE}─────────────────────${NC}"

USB_DEVICES=$(lsusb | grep -i displaylink)
if [ -n "$USB_DEVICES" ]; then
    print_status 0 "USB devices: ${BOLD}DisplayLink device detected${NC}"
    echo "$USB_DEVICES" | while read -r line; do
        echo "      $line"
    done
else
    echo -e "  ${YELLOW}ℹ${NC}  USB devices: ${BOLD}No DisplayLink devices connected${NC}"
fi
echo ""

# 7. Recent Log Analysis
echo -e "${BLUE}${BOLD}7. RECENT SERVICE LOGS${NC}"
echo -e "${BLUE}───────────────────────${NC}"

RECENT_ERRORS=$(journalctl -u displaylink-driver -p err -n 5 --no-pager 2>/dev/null)
if [ -z "$RECENT_ERRORS" ]; then
    print_status 0 "Recent errors: ${BOLD}None found${NC}"
else
    echo -e "  ${YELLOW}⚠${NC}  Recent errors found:"
    echo "$RECENT_ERRORS" | head -5 | while read -r line; do
        echo "      $line"
    done
fi
echo ""

# Summary
echo -e "${BOLD}========================================================================${NC}"
echo -e "${BOLD}                              SUMMARY                                  ${NC}"
echo -e "${BOLD}========================================================================${NC}"

OVERALL_STATUS=0

if [ "$SECURE_BOOT" -eq 1 ]; then
    if [ "$MOK_ENROLLED" -eq 1 ] && [ "$MODULE_LOADED" -eq 1 ] && [ "$SERVICE_STATUS" = "active" ]; then
        echo -e "${GREEN}${BOLD}✅ INSTALLATION COMPLETE AND FUNCTIONAL${NC}"
        echo -e "   DisplayLink is properly installed with Secure Boot enabled."
        echo -e "   The system is ready for DisplayLink devices."
    elif [ "$MOK_ENROLLED" -eq 0 ]; then
        echo -e "${YELLOW}${BOLD}⚠  ACTION REQUIRED: MOK ENROLLMENT${NC}"
        echo -e "   1. Reboot your system"
        echo -e "   2. Enroll the MOK key when prompted"
        echo -e "   3. Use the password from your vault configuration"
        OVERALL_STATUS=1
    else
        echo -e "${RED}${BOLD}❌ INSTALLATION INCOMPLETE${NC}"
        echo -e "   Some components are not properly configured."
        echo -e "   Please review the errors above."
        OVERALL_STATUS=1
    fi
else
    if [ "$MODULE_LOADED" -eq 1 ] && [ "$SERVICE_STATUS" = "active" ]; then
        echo -e "${GREEN}${BOLD}✅ INSTALLATION COMPLETE AND FUNCTIONAL${NC}"
        echo -e "   DisplayLink is working (Secure Boot disabled)."
    else
        echo -e "${RED}${BOLD}❌ INSTALLATION INCOMPLETE${NC}"
        echo -e "   Some components are not properly configured."
        OVERALL_STATUS=1
    fi
fi

echo -e "${BOLD}========================================================================${NC}"
echo ""

exit $OVERALL_STATUS