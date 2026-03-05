#!/usr/bin/env bash
set -euo pipefail

# ----------------------------------------------------------------------------
# Flutter WebRTC Server — Deployment Script
#
# Installs/updates the server under /opt/flutter-webrtc/<environment>.
# Runs the service as a dedicated system user (flutter-webrtc).
# ----------------------------------------------------------------------------

GO_MAIN_PATH="cmd/server/main.go"
BUILD_OUTPUT="webrtc-server"

# Standard Linux directory layout
APP_BASE_DIR="/opt/flutter-webrtc"
DEPLOY_BASE_DIR="/var/lib/flutter-webrtc-deploy"
BACKUP_BASE_DIR="/var/backups/flutter-webrtc"
SYSTEMD_SERVICE_FILE="/etc/systemd/system/flutter-webrtc.service"
SERVICE_USER="flutter-webrtc"

# Legacy path used before this refactor (develop only)
LEGACY_DIR="/home/ubuntu/flutter-webrtc-server-master"

ZIP_FILE=""
S3_BUCKET=""
DOMAIN=""
ENVIRONMENT=""
SERVICE_NAME="flutter-webrtc.service"

usage() {
  cat <<'EOF'
Usage:
  deploy-flutter-webrtc-server.sh -b <bucket> -f <zip-file> -d <domain> -e <environment> [options]

Required:
  -b   S3 bucket name (without s3://)
  -f   ZIP file name in the bucket (without prefix)
  -d   Domain name for TLS certificates
  -e   Environment name (develop | main2 | qa | staging)

Optional:
  -s   Systemd service name (default: flutter-webrtc.service)
  -h   Show help

Example:
  ./deploy-flutter-webrtc-server.sh \
    -b ota-img-main2.logicmarkcloud.com \
    -f flutter-webrtc-server-main2-v1.0.0.zip \
    -d flutter-webrtc.main2.logicmarkcloud.com \
    -e main2
EOF
}

while getopts ":b:f:s:d:e:h" opt; do
  case "$opt" in
    b) S3_BUCKET="$OPTARG" ;;
    f) ZIP_FILE="$OPTARG" ;;
    s) SERVICE_NAME="$OPTARG" ;;
    d) DOMAIN="$OPTARG" ;;
    e) ENVIRONMENT="$OPTARG" ;;
    h) usage; exit 0 ;;
    :)  echo "ERROR: Option -$OPTARG requires an argument." >&2; usage; exit 2 ;;
    \?) echo "ERROR: Invalid option -$OPTARG" >&2;            usage; exit 2 ;;
  esac
done

if [[ -z "${S3_BUCKET}" || -z "${ZIP_FILE}" || -z "${DOMAIN}" || -z "${ENVIRONMENT}" ]]; then
  echo "ERROR: -b, -f, -d and -e are required." >&2
  usage; exit 2
fi

# Environment-scoped paths
S3_PREFIX="flutter-webrtc-server-${ENVIRONMENT}"
TARGET_DIR="${APP_BASE_DIR}/${ENVIRONMENT}"
WORKDIR="${DEPLOY_BASE_DIR}/${ENVIRONMENT}"
STAGING_DIR="${WORKDIR}/staging"
BACKUP_DIR="${BACKUP_BASE_DIR}/${ENVIRONMENT}"

need_cmd() { command -v "$1" >/dev/null 2>&1 || { echo "ERROR: missing cmd: $1" >&2; exit 1; }; }
need_cmd aws
need_cmd unzip
need_cmd systemctl
need_cmd sudo

echo "==> Environment: ${ENVIRONMENT}"
echo "==> Domain:      ${DOMAIN}"
echo "==> Service:     ${SERVICE_NAME}"
echo "==> App dir:     ${TARGET_DIR}"
echo "==> S3 object:   s3://${S3_BUCKET}/${S3_PREFIX}/${ZIP_FILE}"

# ============================================================================
# Ensure dedicated system user exists
# ============================================================================

if ! id "${SERVICE_USER}" &>/dev/null; then
  echo "==> Creating system user: ${SERVICE_USER}"
  sudo useradd \
    --system \
    --no-create-home \
    --shell /usr/sbin/nologin \
    --comment "Flutter WebRTC Server" \
    "${SERVICE_USER}"
  echo "==> User created: ${SERVICE_USER}"
else
  echo "==> User already exists: ${SERVICE_USER}"
fi

# ============================================================================
# Install / update systemd service
# ============================================================================

install_service() {
  echo "==> Installing systemd service: ${SYSTEMD_SERVICE_FILE}"
  sudo tee "${SYSTEMD_SERVICE_FILE}" > /dev/null <<EOF
[Unit]
Description=Flutter WebRTC Signaling Server (${ENVIRONMENT})
After=network.target

[Service]
Type=simple
User=${SERVICE_USER}
Group=${SERVICE_USER}
WorkingDirectory=${TARGET_DIR}
ExecStart=${TARGET_DIR}/${BUILD_OUTPUT}
Restart=always
RestartSec=10
StandardOutput=journal
StandardError=journal

# Security hardening
NoNewPrivileges=true
ProtectSystem=strict
ProtectHome=true
ReadWritePaths=${TARGET_DIR}
PrivateTmp=true

[Install]
WantedBy=multi-user.target
EOF

  sudo systemctl daemon-reload
  sudo systemctl enable "${SERVICE_NAME}"
  echo "==> Service installed and enabled"
}

if [[ ! -f "${SYSTEMD_SERVICE_FILE}" ]]; then
  echo "==> Service not found, performing initial installation..."
  install_service
else
  CURRENT_WD=$(grep "WorkingDirectory" "${SYSTEMD_SERVICE_FILE}" | cut -d= -f2 || true)
  if [[ "${CURRENT_WD}" != "${TARGET_DIR}" ]]; then
    echo "==> Updating service (WorkingDirectory: ${CURRENT_WD} -> ${TARGET_DIR})"
    install_service
  else
    echo "==> Service already up to date"
  fi
fi

# ============================================================================
# Stop service
# ============================================================================

echo "==> Stopping service..."
sudo systemctl stop "${SERVICE_NAME}" || true

# ============================================================================
# Migrate from legacy directory (develop only)
# ============================================================================

if [[ "${ENVIRONMENT}" == "develop" && -d "${LEGACY_DIR}" && ! -d "${TARGET_DIR}" ]]; then
  echo "==> Migrating from legacy directory: ${LEGACY_DIR} -> ${TARGET_DIR}"
  sudo mkdir -p "${APP_BASE_DIR}"
  sudo mv "${LEGACY_DIR}" "${TARGET_DIR}"
  sudo chown -R "${SERVICE_USER}:${SERVICE_USER}" "${TARGET_DIR}"
  echo "==> Migration complete"
fi

# ============================================================================
# Prepare directories
# ============================================================================

sudo mkdir -p "${APP_BASE_DIR}" "${WORKDIR}" "${BACKUP_DIR}"
sudo chown ubuntu:ubuntu "${WORKDIR}"

# ============================================================================
# Download package
# ============================================================================

echo "==> Downloading ZIP from S3..."
cd "${WORKDIR}"
rm -f -- ./*.zip
aws s3 cp "s3://${S3_BUCKET}/${S3_PREFIX}/${ZIP_FILE}" "./${ZIP_FILE}"

# ============================================================================
# Backup current deployment
# ============================================================================

if [[ -d "${TARGET_DIR}" ]]; then
  TS="$(date +%Y%m%d_%H%M%S)"
  SNAP="${BACKUP_DIR}/snap_${TS}"
  echo "==> Creating backup: ${SNAP}"
  sudo cp -a "${TARGET_DIR}" "${SNAP}"
fi

# ============================================================================
# Extract package
# ============================================================================

echo "==> Extracting to staging..."
sudo rm -rf "${STAGING_DIR}"
mkdir -p "${STAGING_DIR}"
unzip -o "./${ZIP_FILE}" -d "${STAGING_DIR}"

# Detect extraction layout (subdirectory vs flat)
EXTRACTED_DIR="${STAGING_DIR}/flutter-webrtc-server-${ENVIRONMENT}"
if [[ ! -d "${EXTRACTED_DIR}" ]]; then
  if [[ -f "${STAGING_DIR}/go.mod" ]] || [[ -f "${STAGING_DIR}/${BUILD_OUTPUT}" ]]; then
    echo "==> Files extracted directly to staging"
    EXTRACTED_DIR="${STAGING_DIR}"
  else
    EXTRACTED_DIR="$(find "${STAGING_DIR}" -mindepth 1 -maxdepth 1 -type d | head -n 1 || true)"
  fi
fi

if [[ -z "${EXTRACTED_DIR}" || ! -d "${EXTRACTED_DIR}" ]]; then
  echo "ERROR: Could not find extracted source in ${STAGING_DIR}" >&2
  exit 1
fi

# ============================================================================
# Promote to target directory
# ============================================================================

echo "==> Promoting to: ${TARGET_DIR}"
sudo rm -rf "${TARGET_DIR}"
if [[ "${EXTRACTED_DIR}" == "${STAGING_DIR}" ]]; then
  sudo cp -a "${STAGING_DIR}/." "${TARGET_DIR}/"
else
  sudo mv "${EXTRACTED_DIR}" "${TARGET_DIR}"
fi

# ============================================================================
# Apply environment config
# ============================================================================

cd "${TARGET_DIR}"

CONFIG_FILE="configs/config-${ENVIRONMENT}.ini"
if [[ -f "${CONFIG_FILE}" ]]; then
  echo "==> Applying config: ${CONFIG_FILE}"
  sudo cp "${CONFIG_FILE}" configs/config.ini
else
  echo "==> No environment config found, using config.ini as-is"
fi

# ============================================================================
# Binary (pre-compiled or build)
# ============================================================================

if [[ -f "${BUILD_OUTPUT}" ]]; then
  echo "==> Using pre-compiled binary"
  sudo chmod +x "${BUILD_OUTPUT}"
  ls -lh "${BUILD_OUTPUT}"
else
  echo "==> Building Go binary..."
  need_cmd go
  sudo -u ubuntu go build -o "${BUILD_OUTPUT}" "${GO_MAIN_PATH}"
fi

# ============================================================================
# Set ownership — flutter-webrtc user owns the entire app directory
# ============================================================================

sudo chown -R "${SERVICE_USER}:${SERVICE_USER}" "${TARGET_DIR}"
sudo chmod 750 "${TARGET_DIR}"

# ============================================================================
# TLS certificates
# ============================================================================

LE_DOMAIN_PATH="/etc/letsencrypt/live/${DOMAIN}"
CERT_DST_DIR="${TARGET_DIR}/configs/certs"

echo "==> Checking TLS certs at: ${LE_DOMAIN_PATH}"

if ! sudo test -f "${LE_DOMAIN_PATH}/fullchain.pem"; then
  echo "ERROR: fullchain.pem not found at: ${LE_DOMAIN_PATH}" >&2
  echo "       Run: sudo certbot certonly --standalone -d ${DOMAIN}" >&2
  exit 1
fi

echo "==> Copying TLS certs..."
sudo mkdir -p "${CERT_DST_DIR}"
sudo cp "${LE_DOMAIN_PATH}/fullchain.pem" "${CERT_DST_DIR}/cert.pem"
sudo cp "${LE_DOMAIN_PATH}/privkey.pem"   "${CERT_DST_DIR}/key.pem"
sudo chown "${SERVICE_USER}:${SERVICE_USER}" "${CERT_DST_DIR}"/*.pem
sudo chmod 640 "${CERT_DST_DIR}"/*.pem

# ============================================================================
# Start service
# ============================================================================

echo "==> Reloading systemd and restarting service..."
sudo systemctl daemon-reload
sudo systemctl restart "${SERVICE_NAME}"

echo "==> Service status:"
sudo systemctl status "${SERVICE_NAME}" --no-pager
