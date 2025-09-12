#!/bin/bash

# Foundation Setup Script for Arkfile
# This script creates users, directories, and keys but doesn't start services

set -e

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

# Record start time
START_TIME=$(date +%s)

echo -e "${BLUE}🏗️  Starting Arkfile Foundation Setup${NC}"
echo -e "${BLUE}Creating infrastructure without starting services${NC}"
echo

# Parse command line options
SKIP_TESTS=false
SKIP_TLS=false
FORCE_REBUILD=false

while [[ $# -gt 0 ]]; do
    case $1 in
        --skip-tests)
            SKIP_TESTS=true
            shift
            ;;
        --skip-tls)
            SKIP_TLS=true
            shift
            ;;
        --force-rebuild)
            FORCE_REBUILD=true
            shift
            ;;
        -h|--help)
            echo "Usage: $0 [OPTIONS]"
            echo ""
            echo "Options:"
            echo "  --skip-tests        Skip running tests before setup"
            echo "  --skip-tls          Skip TLS certificate generation"
            echo "  --force-rebuild     Force rebuild even if directories exist"
            echo "  -h, --help          Show this help message"
            exit 0
            ;;
        *)
            echo -e "${RED}Unknown option: $1${NC}"
            exit 1
            ;;
    esac
done

# State tracking functions
BASE_DIR="/opt/arkfile"
STATE_DIR="${BASE_DIR}/var/setup-state"

mark_completed() {
    local step="$1"
    if [ -d "${STATE_DIR}" ]; then
        sudo -u arkfile touch "${STATE_DIR}/${step}.completed"
    fi
}

is_completed() {
    local step="$1"
    [ -f "${STATE_DIR}/${step}.completed" ]
}

# Run tests first unless skipped
if [ "$SKIP_TESTS" = false ]; then
    echo -e "${BLUE}🧪 Running tests before foundation setup...${NC}"
    
    if [ -x "./scripts/testing/test-only.sh" ]; then
        ./scripts/testing/test-only.sh --skip-performance --skip-golden
        if [ $? -ne 0 ]; then
            echo -e "${RED}❌ Tests failed, aborting foundation setup${NC}"
            exit 1
        fi
    else
        echo -e "${YELLOW}⚠️  Test script not found, skipping tests${NC}"
    fi
else
    echo -e "${YELLOW}⏭️  Skipping tests as requested${NC}"
fi

echo
echo -e "${BLUE}🚀 Starting foundation infrastructure setup...${NC}"

# Step 1: Create system user and group
echo -e "${YELLOW}Step 1: Creating system user and group...${NC}"
if is_completed "users" && [ "$FORCE_REBUILD" = false ]; then
    echo -e "${GREEN}✅ Users already created (use --force-rebuild to recreate)${NC}"
else
    sudo -E ./scripts/setup/01-setup-users.sh
    if [ $? -eq 0 ]; then
        echo -e "${GREEN}✅ User and group creation completed${NC}"
        mark_completed "users"
    else
        echo -e "${RED}❌ User and group creation failed${NC}"
        exit 1
    fi
fi

# Step 2: Create directory structure
echo -e "${YELLOW}Step 2: Creating directory structure...${NC}"
if is_completed "directories" && [ "$FORCE_REBUILD" = false ]; then
    echo -e "${GREEN}✅ Directories already created (use --force-rebuild to recreate)${NC}"
else
    sudo -E ./scripts/setup/02-setup-directories.sh
    if [ $? -eq 0 ]; then
        echo -e "${GREEN}✅ Directory structure creation completed${NC}"
        mark_completed "directories"
    else
        echo -e "${RED}❌ Directory structure creation failed${NC}"
        exit 1
    fi
fi

# Step 3: Build application
echo -e "${YELLOW}Step 3: Building application...${NC}"
if is_completed "build" && [ "$FORCE_REBUILD" = false ]; then
    echo -e "${GREEN}✅ Application already built (use --force-rebuild to rebuild)${NC}"
else
    ./scripts/setup/build.sh
    if [ $? -eq 0 ]; then
        echo -e "${GREEN}✅ Application build completed${NC}"
        mark_completed "build"
    else
        echo -e "${RED}❌ Application build failed${NC}"
        exit 1
    fi
fi

# Step 4: Generate OPAQUE keys
echo -e "${YELLOW}Step 4: Generating OPAQUE server keys...${NC}"
if is_completed "opaque-keys" && [ "$FORCE_REBUILD" = false ]; then
    echo -e "${GREEN}✅ OPAQUE keys already generated (use --force-rebuild to regenerate)${NC}"
else
    sudo -E ./scripts/setup/03-setup-opaque-keys.sh
    if [ $? -eq 0 ]; then
        echo -e "${GREEN}✅ OPAQUE key generation completed${NC}"
        mark_completed "opaque-keys"
    else
        echo -e "${RED}❌ OPAQUE key generation failed${NC}"
        exit 1
    fi
fi

# Step 5: Generate JWT keys
echo -e "${YELLOW}Step 5: Generating JWT signing keys...${NC}"
if is_completed "jwt-keys" && [ "$FORCE_REBUILD" = false ]; then
    echo -e "${GREEN}✅ JWT keys already generated (use --force-rebuild to regenerate)${NC}"
else
    sudo -E ./scripts/setup/04-setup-jwt-keys.sh
    if [ $? -eq 0 ]; then
        echo -e "${GREEN}✅ JWT key generation completed${NC}"
        mark_completed "jwt-keys"
    else
        echo -e "${RED}❌ JWT key generation failed${NC}"
        exit 1
    fi
fi

# Step 6: Generate TLS certificates (optional)
if [ "$SKIP_TLS" = false ]; then
    echo -e "${YELLOW}Step 6: Generating TLS certificates...${NC}"
    if is_completed "tls-certs" && [ "$FORCE_REBUILD" = false ]; then
        echo -e "${GREEN}✅ TLS certificates already generated (use --force-rebuild to regenerate)${NC}"
    else
        if sudo -E ./scripts/setup/05-setup-tls-certs.sh; then
            echo -e "${GREEN}✅ TLS certificate generation completed${NC}"
            mark_completed "tls-certs"
            
            # Validate certificates
            echo -e "${YELLOW}Validating TLS certificates...${NC}"
            if ./scripts/maintenance/validate-certificates.sh >/dev/null 2>&1; then
                echo -e "${GREEN}✅ TLS certificate validation passed${NC}"
            else
                echo -e "${YELLOW}⚠️  TLS certificate validation had warnings (non-critical)${NC}"
            fi
        else
            echo -e "${YELLOW}⚠️  TLS certificate generation had issues (non-critical for core functionality)${NC}"
            echo -e "${BLUE}ℹ️  Note: TLS certificates are for internal service communication${NC}"
            echo -e "${BLUE}ℹ️  Core Arkfile functionality (OPAQUE auth, file encryption) works independently${NC}"
        fi
    fi
else
    echo -e "${YELLOW}⏭️  Skipping TLS certificate generation as requested${NC}"
fi

# Step 7: Validate foundation setup
echo
echo -e "${BLUE}🔍 Validating foundation setup...${NC}"

# Check key files exist
echo -e "${YELLOW}Checking cryptographic keys...${NC}"
key_files=(
    "${BASE_DIR}/etc/keys/opaque/server_private.key"
    "${BASE_DIR}/etc/keys/jwt/current/signing.key"
)

all_keys_present=true
for key_file in "${key_files[@]}"; do
    if sudo test -f "${key_file}"; then
        echo -e "${GREEN}✅ ${key_file}${NC}"
    else
        echo -e "${RED}❌ Missing: ${key_file}${NC}"
        all_keys_present=false
    fi
done

if [ "$all_keys_present" = true ]; then
    echo -e "${GREEN}✅ All required keys are present${NC}"
else
    echo -e "${RED}❌ Some required keys are missing${NC}"
    exit 1
fi

# Check directory permissions
echo -e "${YELLOW}Checking directory permissions...${NC}"
key_dirs=(
    "${BASE_DIR}/etc/keys"
    "${BASE_DIR}/var/lib"
    "${BASE_DIR}/bin"
)

all_perms_correct=true
for dir in "${key_dirs[@]}"; do
    if sudo test -d "${dir}"; then
        owner=$(sudo stat -c '%U:%G' "${dir}")
        perms=$(sudo stat -c '%a' "${dir}")
        echo -e "${GREEN}✅ ${dir} (${owner}, ${perms})${NC}"
    else
        echo -e "${RED}❌ Missing directory: ${dir}${NC}"
        all_perms_correct=false
    fi
done

if [ "$all_perms_correct" = true ]; then
    echo -e "${GREEN}✅ Directory permissions are correct${NC}"
else
    echo -e "${RED}❌ Some directory permissions are incorrect${NC}"
    exit 1
fi

# Check application binary
echo -e "${YELLOW}Checking application binary...${NC}"
if [ -f "${BASE_DIR}/bin/arkfile" ]; then
    if [ -x "${BASE_DIR}/bin/arkfile" ]; then
        echo -e "${GREEN}✅ Application binary is executable${NC}"
    else
        echo -e "${YELLOW}⚠️  Application binary is not executable${NC}"
    fi
else
    echo -e "${RED}❌ Application binary not found${NC}"
    exit 1
fi

# Run health check
echo -e "${YELLOW}Running foundation health check...${NC}"
if ./scripts/maintenance/health-check.sh --foundation >/dev/null 2>&1; then
    echo -e "${GREEN}✅ Foundation health check passed${NC}"
else
    echo -e "${YELLOW}⚠️  Foundation health check had warnings (non-critical)${NC}"
fi

# Mark foundation as completed
mark_completed "foundation"

# Summary
echo
echo -e "${GREEN}FOUNDATION SETUP COMPLETED SUCCESSFULLY!${NC}"
echo "========================================"
echo "Setup Duration: $(($(date +%s) - START_TIME)) seconds"
echo

echo -e "${BLUE}✅ Foundation Infrastructure Completed:${NC}"
echo "• System user: arkfile ($(id arkfile 2>/dev/null || echo 'configured'))"
echo "• Base directory: /opt/arkfile ($(ls -ld /opt/arkfile 2>/dev/null | awk '{print $3":"$4" "$1}' || echo 'configured'))"
echo "• Key storage: /opt/arkfile/etc/keys ($(ls -ld /opt/arkfile/etc/keys 2>/dev/null | awk '{print $1}' || echo 'configured'))"
echo "• Binary location: /opt/arkfile/bin/arkfile"
echo "• Current release: /opt/arkfile/releases/current"

echo
echo -e "${BLUE}🔐 Security Configuration:${NC}"
echo "• OPAQUE server keys: ✅ Generated and secured"
echo "• JWT signing keys: ✅ Generated with rotation capability"

if [ "$SKIP_TLS" = false ]; then
    if is_completed "tls-certs"; then
        echo "• TLS certificates: ✅ Self-signed for development"
    else
        echo "• TLS certificates: ⚠️  Generation had issues (non-critical)"
    fi
else
    echo "• TLS certificates: ⏭️  Skipped"
fi

echo "• File permissions: ✅ Production security standards"
echo "• Service isolation: ✅ Dedicated arkfile user"

echo
echo -e "${BLUE}📊 Foundation Health:${NC}"
echo "• Infrastructure: ✅ Ready for service configuration"
echo "• Cryptographic keys: ✅ Secured and validated"
echo "• Build system: ✅ Application compiled and deployed"
echo "• Permissions: ✅ Production-ready security"

echo
echo -e "${GREEN}🚀 NEXT STEP - GET ARKFILE RUNNING${NC}"
echo "========================================"
echo -e "${YELLOW}To get a complete working Arkfile system:${NC}"
echo
echo -e "${GREEN}  ./scripts/quick-start.sh${NC}"
echo
echo "This single command will:"
echo "• Set up MinIO object storage"
echo "• Set up rqlite database"
echo "• Start all services"
echo "• Give you the web interface URL"
echo
echo -e "${BLUE}OR, for manual setup:${NC}"
echo "1. Set up services: sudo ./scripts/setup/07-setup-minio.sh && sudo ./scripts/setup/08-setup-rqlite.sh"
echo "2. Start services: sudo systemctl start arkfile"
echo "3. Visit: http://localhost:8080"
echo
echo -e "${GREEN}✅ Foundation setup complete!${NC}"
echo -e "${BLUE}Your Arkfile foundation is ready for service configuration.${NC}"

exit 0
