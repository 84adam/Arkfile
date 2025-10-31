#!/bin/bash
set -e

# Configuration
APP_NAME="arkfile"
BASE_DIR="/opt/arkfile"
BUILD_DIR="build"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m'

echo -e "${GREEN}Deploying ${APP_NAME} locally...${NC}"

# Verify we have build artifacts
if [ ! -d "$BUILD_DIR" ]; then
    echo -e "${RED}[X] Build directory ${BUILD_DIR} not found. Run build first.${NC}"
    exit 1
fi

# Verify build directory has content
if [ ! "$(ls -A $BUILD_DIR)" ]; then
    echo -e "${RED}[X] Build directory ${BUILD_DIR} is empty. Run build first.${NC}"
    exit 1
fi

# Copy build artifacts to installation directory
echo -e "${YELLOW}Copying build artifacts to ${BASE_DIR}...${NC}"
sudo cp -r ${BUILD_DIR}/* ${BASE_DIR}/

# Set proper ownership
echo -e "${YELLOW}[KEY] Setting permissions...${NC}"
sudo chown -R arkfile:arkfile ${BASE_DIR}
sudo find ${BASE_DIR} -type f -executable -exec chmod 755 {} \;

# Copy systemd service files
echo -e "${YELLOW}[CONFIG]  Installing systemd services...${NC}"
sudo cp ${BASE_DIR}/systemd/${APP_NAME}.service /etc/systemd/system/
sudo cp ${BASE_DIR}/systemd/rqlite.service /etc/systemd/system/ 2>/dev/null || true
sudo cp ${BASE_DIR}/systemd/minio.service /etc/systemd/system/ 2>/dev/null || true
sudo cp ${BASE_DIR}/systemd/caddy.service /etc/systemd/system/ 2>/dev/null || true

# Reload systemd daemon
echo -e "${YELLOW}Reloading systemd...${NC}"
sudo systemctl daemon-reload

# Enable services (but don't auto-start)
echo -e "${YELLOW}[INFO] Enabling services (without auto-start)...${NC}"
sudo systemctl enable ${APP_NAME} 2>/dev/null || true

# Services can be started manually by the user when ready
echo -e "${GREEN}[OK] Deployment complete!${NC}"
echo
echo -e "${YELLOW}[STATS] Deployment Summary:${NC}"
echo "• Build artifacts copied to: ${BASE_DIR}"
echo "• Permissions set for arkfile:arkfile user"
echo "• Systemd services installed and enabled"
echo
echo -e "${BLUE}[START] To start the services:${NC}"
echo "  sudo systemctl start ${APP_NAME}"
echo "  sudo systemctl start rqlite 2>/dev/null || true"
echo "  sudo systemctl start minio 2>/dev/null || true"
echo "  sudo systemctl start caddy 2>/dev/null || true"
echo
echo -e "${BLUE}[INFO] Check service status:${NC}"
echo "  sudo systemctl status ${APP_NAME}"
echo
echo -e "${GREEN}[TARGET] Ready for deployment validation!${NC}"
