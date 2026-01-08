#!/bin/bash
# NVIDIA Driver Installation Status Checker
# Verifies NVIDIA proprietary drivers, Vulkan, CUDA, and hardware acceleration

# Check for --check flag (silent mode for automation)
CHECK_ONLY=0
if [[ "$1" == "--check" ]]; then
    CHECK_ONLY=1
fi

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color
BOLD='\033[1m'

if [[ $CHECK_ONLY -eq 0 ]]; then
    echo -e "${BOLD}========================================================================${NC}"
    echo -e "${BOLD}                 NVIDIA DRIVER INSTALLATION STATUS CHECK               ${NC}"
    echo -e "${BOLD}========================================================================${NC}"
    echo ""
fi

# Function to print status
print_status() {
    if [[ $CHECK_ONLY -eq 0 ]]; then
        if [ "$1" -eq 0 ]; then
            echo -e "  ${GREEN}✅${NC} $2"
        else
            echo -e "  ${RED}❌${NC} $2"
        fi
    fi
}

print_warning() {
    if [[ $CHECK_ONLY -eq 0 ]]; then
        echo -e "  ${YELLOW}⚠${NC}  $1"
    fi
}

print_info() {
    if [[ $CHECK_ONLY -eq 0 ]]; then
        echo -e "  ${YELLOW}ℹ${NC}  $1"
    fi
}

# 1. Check Secure Boot Status
if [[ $CHECK_ONLY -eq 0 ]]; then
    echo -e "${BLUE}${BOLD}1. SECURE BOOT & MOK STATUS${NC}"
    echo -e "${BLUE}───────────────────────────${NC}"
fi

SB_STATE=$(mokutil --sb-state 2>&1)
if echo "$SB_STATE" | grep -q "SecureBoot enabled"; then
    echo -e "  ${GREEN}✅${NC} Secure Boot: ${BOLD}Enabled${NC}"
    SECURE_BOOT=1
else
    print_warning "Secure Boot: ${BOLD}Disabled${NC}"
    SECURE_BOOT=0
fi

# Check for akmods MOK key enrollment
MOK_ENROLLED=0

# Method 1: Check the standard akmods key file location
if [ -f /etc/pki/akmods/certs/public_key.der ]; then
    MOK_TEST=$(mokutil --test-key /etc/pki/akmods/certs/public_key.der 2>&1)
    if echo "$MOK_TEST" | grep -q "is already enrolled"; then
        print_status 0 "akmods MOK Key: ${BOLD}Successfully enrolled${NC} (/etc/pki/akmods/certs/public_key.der)"
        MOK_ENROLLED=1
    elif echo "$MOK_TEST" | grep -q "is not enrolled"; then
        print_status 1 "akmods MOK Key: ${BOLD}Not enrolled${NC} (requires reboot and enrollment)"
    else
        print_warning "akmods MOK Key: ${BOLD}Unknown status${NC}"
    fi
else
    # Method 2: Check if ANY MOK keys are enrolled (may be enrolled from earlier Fedora version)
    if [ "$SECURE_BOOT" -eq 1 ]; then
        ENROLLED_COUNT=$(mokutil --list-enrolled 2>/dev/null | grep -c "^SHA1 Fingerprint:")
        if [ "$ENROLLED_COUNT" -gt 0 ]; then
            print_warning "akmods key file not found, but ${BOLD}$ENROLLED_COUNT MOK key(s) enrolled${NC}"
            print_info "Keys may be from previous Fedora version or manual enrollment"
            # If modules are actually loaded with Secure Boot, the key MUST be enrolled
            # We'll verify this later by checking loaded modules
            MOK_ENROLLED=2  # Mark as "uncertain but possible"
        else
            print_status 1 "akmods MOK Key: ${BOLD}Not found and no MOK keys enrolled${NC}"
        fi
    else
        print_info "akmods MOK Key: Not needed (Secure Boot disabled)"
        MOK_ENROLLED=1  # Not needed, so consider it "OK"
    fi
fi
[[ $CHECK_ONLY -eq 0 ]] && echo ""

# 2. Check Package Installation
if [[ $CHECK_ONLY -eq 0 ]]; then
    echo -e "${BLUE}${BOLD}2. PACKAGE INSTALLATION${NC}"
    echo -e "${BLUE}────────────────────────${NC}"
fi

PACKAGES_OK=1

# Core driver packages
check_pkg() {
    if rpm -q "$1" &>/dev/null; then
        VERSION=$(rpm -q "$1" --queryformat '%{VERSION}')
        print_status 0 "$2: ${BOLD}$VERSION${NC}"
    else
        print_status 1 "$2: ${BOLD}Not installed${NC}"
        PACKAGES_OK=0
    fi
}

check_pkg "akmod-nvidia" "NVIDIA driver (akmod)"
check_pkg "xorg-x11-drv-nvidia-cuda" "CUDA/NVENC/NVDEC support"
check_pkg "nvidia-vaapi-driver" "VA-API video acceleration"

# Vulkan packages
VULKAN_INSTALLED=0
if rpm -q vulkan-loader &>/dev/null; then
    print_status 0 "Vulkan runtime: ${BOLD}Installed${NC}"
    VULKAN_INSTALLED=1
else
    print_warning "Vulkan runtime: ${BOLD}Not installed${NC}"
fi

if rpm -q vulkan-tools &>/dev/null; then
    print_status 0 "Vulkan tools: ${BOLD}Installed${NC}"
else
    print_warning "Vulkan tools: ${BOLD}Not installed${NC}"
fi
[[ $CHECK_ONLY -eq 0 ]] && echo ""

# 3. Check Kernel Module Status
if [[ $CHECK_ONLY -eq 0 ]]; then
    echo -e "${BLUE}${BOLD}3. KERNEL MODULE STATUS${NC}"
    echo -e "${BLUE}────────────────────────${NC}"
fi

MODULES_OK=0

# Check if nvidia module exists
if modinfo nvidia &>/dev/null; then
    NVIDIA_VERSION=$(modinfo -F version nvidia 2>/dev/null)
    print_status 0 "nvidia module built: ${BOLD}Version $NVIDIA_VERSION${NC}"
    MODULE_EXISTS=1
else
    print_status 1 "nvidia module: ${BOLD}Not found${NC} (module not built yet)"
    MODULE_EXISTS=0
fi

# Check loaded modules
LOADED_MODULES=()
for mod in nvidia nvidia_drm nvidia_modeset nvidia_uvm; do
    if lsmod | grep -q "^${mod}"; then
        LOADED_MODULES+=("$mod")
    fi
done

if [ ${#LOADED_MODULES[@]} -gt 0 ]; then
    print_status 0 "Loaded modules: ${BOLD}${LOADED_MODULES[*]}${NC}"
    MODULES_OK=1

    # If modules are loaded with Secure Boot enabled, MOK MUST be enrolled
    if [ "$SECURE_BOOT" -eq 1 ]; then
        print_status 0 "Secure Boot signature: ${BOLD}Valid (modules working with Secure Boot)${NC}"
        # Override MOK_ENROLLED status - if modules work with SB, key is enrolled
        if [ "$MOK_ENROLLED" -eq 2 ] || [ "$MOK_ENROLLED" -eq 0 ]; then
            MOK_ENROLLED=1  # Proven by working modules
            print_info "MOK verification: Enrollment confirmed by loaded signed modules"
        fi
    fi
else
    print_status 1 "Loaded modules: ${BOLD}None loaded${NC} (requires reboot)"
fi
[[ $CHECK_ONLY -eq 0 ]] && echo ""

# 4. Check nvidia-smi (Driver Functionality)
if [[ $CHECK_ONLY -eq 0 ]]; then
    echo -e "${BLUE}${BOLD}4. DRIVER FUNCTIONALITY${NC}"
    echo -e "${BLUE}───────────────────────${NC}"
fi

DRIVER_OK=0
if command -v nvidia-smi &>/dev/null; then
    NVIDIA_SMI_OUTPUT=$(nvidia-smi --query-gpu=name,driver_version,memory.total --format=csv,noheader 2>&1)
    if [ $? -eq 0 ]; then
        print_status 0 "nvidia-smi: ${BOLD}Working${NC}"
        while IFS=',' read -r gpu_name driver_ver memory; do
            echo -e "      GPU: ${BOLD}$(echo $gpu_name | xargs)${NC}"
            echo -e "      Driver: ${BOLD}$(echo $driver_ver | xargs)${NC}"
            echo -e "      VRAM: ${BOLD}$(echo $memory | xargs)${NC}"
        done <<< "$NVIDIA_SMI_OUTPUT"
        DRIVER_OK=1
    else
        print_status 1 "nvidia-smi: ${BOLD}Failed to communicate with GPU${NC}"
        if [[ $CHECK_ONLY -eq 0 ]]; then
            echo "      Error: $NVIDIA_SMI_OUTPUT"
        fi
    fi
else
    print_status 1 "nvidia-smi: ${BOLD}Command not found${NC}"
fi
[[ $CHECK_ONLY -eq 0 ]] && echo ""

# 5. Check OpenGL Status
if [[ $CHECK_ONLY -eq 0 ]]; then
    echo -e "${BLUE}${BOLD}5. OPENGL STATUS${NC}"
    echo -e "${BLUE}─────────────────${NC}"
fi

OPENGL_OK=0
HYBRID_GRAPHICS=0

if command -v glxinfo &>/dev/null; then
    GL_VENDOR=$(glxinfo 2>/dev/null | grep "OpenGL vendor" | cut -d: -f2 | xargs)
    GL_RENDERER=$(glxinfo 2>/dev/null | grep "OpenGL renderer" | cut -d: -f2 | xargs)
    GL_VERSION=$(glxinfo 2>/dev/null | grep "OpenGL version" | cut -d: -f2 | xargs)

    # Detect hybrid graphics (Intel/AMD iGPU + NVIDIA dGPU)
    if (echo "$GL_RENDERER" | grep -Eqi "intel|radeon|amd") && [ "$DRIVER_OK" -eq 1 ]; then
        HYBRID_GRAPHICS=1
    fi

    if echo "$GL_VENDOR" | grep -qi "nvidia"; then
        print_status 0 "OpenGL vendor: ${BOLD}$GL_VENDOR${NC}"
        echo -e "      Renderer: ${BOLD}$GL_RENDERER${NC}"
        echo -e "      Version: ${BOLD}$GL_VERSION${NC}"
        OPENGL_OK=1
    else
        if [ "$HYBRID_GRAPHICS" -eq 1 ]; then
            print_warning "OpenGL default GPU: ${BOLD}$GL_VENDOR${NC} (integrated graphics)"
            echo -e "      Default renderer: ${BOLD}$GL_RENDERER${NC}"
            print_info "Hybrid graphics detected - NVIDIA available but not default"
            if [[ $CHECK_ONLY -eq 0 ]]; then
                echo -e "      ${YELLOW}To use NVIDIA GPU:${NC}"
                echo -e "      __NV_PRIME_RENDER_OFFLOAD=1 __GLX_VENDOR_LIBRARY_NAME=nvidia <command>"
                echo -e "      Or configure NVIDIA as primary GPU in system settings"
            fi
        else
            print_status 1 "OpenGL vendor: ${BOLD}$GL_VENDOR${NC} (expected: NVIDIA Corporation)"
            print_info "Using: $GL_RENDERER"
        fi
    fi
else
    print_warning "glxinfo: ${BOLD}Not available${NC} (install mesa-demos to check)"
fi
[[ $CHECK_ONLY -eq 0 ]] && echo ""

# 6. Check Vulkan Status
if [[ $CHECK_ONLY -eq 0 ]]; then
    echo -e "${BLUE}${BOLD}6. VULKAN STATUS${NC}"
    echo -e "${BLUE}─────────────────${NC}"
fi

VULKAN_OK=0
if command -v vulkaninfo &>/dev/null; then
    # Check if vulkaninfo can run without errors
    if vulkaninfo --summary &>/dev/null; then
        VULKAN_DEVICES=$(vulkaninfo --summary 2>/dev/null | grep -A1 "GPU" | grep "deviceName" | cut -d= -f2 | xargs)
        if echo "$VULKAN_DEVICES" | grep -qi "nvidia"; then
            print_status 0 "Vulkan runtime: ${BOLD}Working${NC}"
            echo -e "      GPU: ${BOLD}$VULKAN_DEVICES${NC}"

            # Get Vulkan version
            VK_VERSION=$(vulkaninfo --summary 2>/dev/null | grep "apiVersion" | head -1 | cut -d= -f2 | xargs)
            if [ -n "$VK_VERSION" ]; then
                echo -e "      API Version: ${BOLD}$VK_VERSION${NC}"
            fi
            VULKAN_OK=1
        else
            print_status 1 "Vulkan GPU: ${BOLD}$VULKAN_DEVICES${NC} (expected: NVIDIA device)"
        fi
    else
        print_status 1 "Vulkan runtime: ${BOLD}Failed to initialize${NC}"
    fi
else
    if [ "$VULKAN_INSTALLED" -eq 1 ]; then
        print_status 1 "vulkaninfo: ${BOLD}Not available${NC} (install vulkan-tools)"
    else
        print_warning "vulkaninfo: ${BOLD}Not installed${NC} (Vulkan support not configured)"
    fi
fi
[[ $CHECK_ONLY -eq 0 ]] && echo ""

# 7. Check Hardware Video Acceleration
if [[ $CHECK_ONLY -eq 0 ]]; then
    echo -e "${BLUE}${BOLD}7. HARDWARE VIDEO ACCELERATION${NC}"
    echo -e "${BLUE}───────────────────────────────${NC}"
fi

VAAPI_OK=0
VAAPI_PKG_MISSING=0

# Check if nvidia-vaapi-driver package is installed
if ! rpm -q nvidia-vaapi-driver &>/dev/null; then
    print_status 1 "nvidia-vaapi-driver: ${BOLD}Not installed${NC}"
    VAAPI_PKG_MISSING=1
    if [[ $CHECK_ONLY -eq 0 ]]; then
        print_info "Package missing from installation - update playbook to install it"
        echo -e "      Run: ansible-playbook playbooks/imports/optional/hardware-specific/play-nvidia.yml"
    fi
else
    print_status 0 "nvidia-vaapi-driver: ${BOLD}Installed${NC}"
fi

if command -v vainfo &>/dev/null; then
    VAAPI_OUTPUT=$(vainfo 2>&1)
    if echo "$VAAPI_OUTPUT" | grep -q "VAProfileNone"; then
        print_status 0 "VA-API: ${BOLD}Driver loaded${NC}"
        VAAPI_DRIVER=$(echo "$VAAPI_OUTPUT" | grep "Driver version" | cut -d: -f2 | xargs)
        if [ -n "$VAAPI_DRIVER" ]; then
            echo -e "      Driver: ${BOLD}$VAAPI_DRIVER${NC}"
        fi
        VAAPI_OK=1
    else
        print_warning "VA-API: ${BOLD}Driver loaded but no profiles${NC}"
    fi
else
    if [ "$VAAPI_PKG_MISSING" -eq 0 ]; then
        print_warning "vainfo: ${BOLD}Not available${NC}"
        if [[ $CHECK_ONLY -eq 0 ]]; then
            print_info "Install libva-utils package to test VA-API (add to playbook)"
        fi
    fi
fi
[[ $CHECK_ONLY -eq 0 ]] && echo ""

# 8. Recent Kernel/Driver Logs
if [[ $CHECK_ONLY -eq 0 ]]; then
    echo -e "${BLUE}${BOLD}8. RECENT DRIVER LOGS${NC}"
    echo -e "${BLUE}──────────────────────${NC}"
fi

RECENT_ERRORS=$(journalctl -k -p err -g nvidia -n 5 --no-pager 2>/dev/null)
if [ -z "$RECENT_ERRORS" ]; then
    print_status 0 "Recent errors: ${BOLD}None found${NC}"
else
    print_warning "Recent kernel errors found:"
    if [[ $CHECK_ONLY -eq 0 ]]; then
        echo "$RECENT_ERRORS" | head -5 | while read -r line; do
            echo "      $line"
        done
    fi
fi
[[ $CHECK_ONLY -eq 0 ]] && echo ""

# Summary
if [[ $CHECK_ONLY -eq 0 ]]; then
    echo -e "${BOLD}========================================================================${NC}"
    echo -e "${BOLD}                              SUMMARY                                  ${NC}"
    echo -e "${BOLD}========================================================================${NC}"
fi

OVERALL_STATUS=0

# Determine overall status
if [ "$SECURE_BOOT" -eq 1 ]; then
    if [ "$MOK_ENROLLED" -eq 1 ] && [ "$MODULES_OK" -eq 1 ] && [ "$DRIVER_OK" -eq 1 ]; then
        if [[ $CHECK_ONLY -eq 0 ]]; then
            echo -e "${GREEN}${BOLD}✅ INSTALLATION COMPLETE AND FUNCTIONAL${NC}"
            echo -e "   NVIDIA drivers are properly installed with Secure Boot enabled."
            echo -e "   GPU: Detected and working"
            echo -e "   Modules: Loaded and signed"
            if [ "$VULKAN_OK" -eq 1 ]; then
                echo -e "   Vulkan: Working"
            fi
            if [ "$OPENGL_OK" -eq 1 ]; then
                echo -e "   OpenGL: Using NVIDIA renderer"
            elif [ "$HYBRID_GRAPHICS" -eq 1 ]; then
                echo -e "   OpenGL: Using integrated GPU (hybrid graphics - normal)"
            fi
            if [ "$HYBRID_GRAPHICS" -eq 1 ]; then
                echo ""
                echo -e "   ${YELLOW}ℹ  Hybrid Graphics System Detected${NC}"
                echo -e "   Your laptop has both integrated and NVIDIA GPUs."
                echo -e "   System uses integrated GPU by default for power savings."
                echo -e "   Use PRIME render offload to run apps on NVIDIA GPU."
            fi
            if [ "$VAAPI_PKG_MISSING" -eq 1 ]; then
                echo ""
                echo -e "   ${YELLOW}⚠  Missing optional package: nvidia-vaapi-driver${NC}"
                echo -e "   Re-run playbook to install: play-nvidia.yml"
            fi
        fi
    elif [ "$MOK_ENROLLED" -eq 0 ]; then
        if [[ $CHECK_ONLY -eq 0 ]]; then
            echo -e "${YELLOW}${BOLD}⚠  ACTION REQUIRED: MOK ENROLLMENT${NC}"
            echo -e "   1. Reboot your system"
            echo -e "   2. Enroll the MOK key when prompted at the blue screen"
            echo -e "   3. Use the password from your vault configuration (mok_password)"
            echo -e "   4. System will reboot and NVIDIA drivers will work"
        fi
        OVERALL_STATUS=1
    elif [ "$MODULE_EXISTS" -eq 0 ]; then
        if [[ $CHECK_ONLY -eq 0 ]]; then
            echo -e "${YELLOW}${BOLD}⚠  KERNEL MODULE STILL BUILDING${NC}"
            echo -e "   The akmod-nvidia module is still being compiled."
            echo -e "   This can take 5-10 minutes after installation."
            echo -e "   Check status with: modinfo -F version nvidia"
            echo -e "   When complete, reboot to load the driver."
        fi
        OVERALL_STATUS=1
    elif [ "$MODULES_OK" -eq 0 ]; then
        if [[ $CHECK_ONLY -eq 0 ]]; then
            echo -e "${YELLOW}${BOLD}⚠  REBOOT REQUIRED${NC}"
            echo -e "   NVIDIA kernel modules are built but not loaded."
            echo -e "   Reboot to activate the NVIDIA drivers."
        fi
        OVERALL_STATUS=1
    else
        if [[ $CHECK_ONLY -eq 0 ]]; then
            echo -e "${RED}${BOLD}❌ INSTALLATION INCOMPLETE${NC}"
            echo -e "   Some components are not properly configured."
            echo -e "   Please review the errors above."
        fi
        OVERALL_STATUS=1
    fi
else
    # Secure Boot disabled
    if [ "$MODULE_EXISTS" -eq 0 ]; then
        if [[ $CHECK_ONLY -eq 0 ]]; then
            echo -e "${YELLOW}${BOLD}⚠  KERNEL MODULE STILL BUILDING${NC}"
            echo -e "   The akmod-nvidia module is still being compiled."
            echo -e "   This can take 5-10 minutes after installation."
            echo -e "   Check status with: modinfo -F version nvidia"
            echo -e "   When complete, reboot to load the driver."
        fi
        OVERALL_STATUS=1
    elif [ "$MODULES_OK" -eq 1 ] && [ "$DRIVER_OK" -eq 1 ]; then
        if [[ $CHECK_ONLY -eq 0 ]]; then
            echo -e "${GREEN}${BOLD}✅ INSTALLATION COMPLETE AND FUNCTIONAL${NC}"
            echo -e "   NVIDIA drivers are working (Secure Boot disabled)."
            echo -e "   GPU: Detected and working"
            if [ "$VULKAN_OK" -eq 1 ]; then
                echo -e "   Vulkan: Working"
            fi
            if [ "$OPENGL_OK" -eq 1 ]; then
                echo -e "   OpenGL: Using NVIDIA renderer"
            elif [ "$HYBRID_GRAPHICS" -eq 1 ]; then
                echo -e "   OpenGL: Using integrated GPU (hybrid graphics - normal)"
            fi
            if [ "$HYBRID_GRAPHICS" -eq 1 ]; then
                echo ""
                echo -e "   ${YELLOW}ℹ  Hybrid Graphics System Detected${NC}"
                echo -e "   Your laptop has both integrated and NVIDIA GPUs."
                echo -e "   System uses integrated GPU by default for power savings."
                echo -e "   Use PRIME render offload to run apps on NVIDIA GPU."
            fi
            if [ "$VAAPI_PKG_MISSING" -eq 1 ]; then
                echo ""
                echo -e "   ${YELLOW}⚠  Missing optional package: nvidia-vaapi-driver${NC}"
                echo -e "   Re-run playbook to install: play-nvidia.yml"
            fi
        fi
    elif [ "$MODULES_OK" -eq 0 ]; then
        if [[ $CHECK_ONLY -eq 0 ]]; then
            echo -e "${YELLOW}${BOLD}⚠  REBOOT REQUIRED${NC}"
            echo -e "   NVIDIA kernel modules are built but not loaded."
            echo -e "   Reboot to activate the NVIDIA drivers."
        fi
        OVERALL_STATUS=1
    else
        if [[ $CHECK_ONLY -eq 0 ]]; then
            echo -e "${RED}${BOLD}❌ INSTALLATION INCOMPLETE${NC}"
            echo -e "   Some components are not properly configured."
            echo -e "   Please review the errors above."
        fi
        OVERALL_STATUS=1
    fi
fi

if [[ $CHECK_ONLY -eq 0 ]]; then
    echo -e "${BOLD}========================================================================${NC}"
    echo ""
fi

exit $OVERALL_STATUS
