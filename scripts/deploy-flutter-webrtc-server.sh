#!/usr/bin/env bash
set -euo pipefail

SERVICE_NAME="flutter-webrtc.service"
GO_MAIN_PATH="cmd/server/main.go"
BUILD_OUTPUT="webrtc-server"
BASE_DIR="/home/ubuntu"

ZIP_FILE=""
S3_BUCKET=""
DOMAIN=""
ENVIRONMENT=""

usage() {
  cat <<'EOF'
Usage:
  deploy-flutter-webrtc-server.sh -b <bucket> -f <zip-file> -d <domain> -e <environment> [options]

Required:
  -b   S3 bucket name (without s3://)
  -f   ZIP file name in the bucket (without prefix)
  -d   Domain name for TLS certificates (e.g. flutter-webrtc.main2.logicmarkcloud.com)
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
    :)
      echo "ERROR: Option -$OPTARG requires an argument." >&2
      usage; exit 2 ;;
    \?)
      echo "ERROR: Invalid option -$OPTARG" >&2
      usage; exit 2 ;;
  esac
done

if [[ -z "${S3_BUCKET}" || -z "${ZIP_FILE}" || -z "${DOMAIN}" || -z "${ENVIRONMENT}" ]]; then
  echo "ERROR: -b, -f, -d and -e are required." >&2
  usage; exit 2
fi

# Environment-scoped paths
S3_PREFIX="flutter-webrtc-server-${ENVIRONMENT}"
TARGET_DIR="${BASE_DIR}/flutter-webrtc-server-${ENVIRONMENT}"
WORKDIR="${BASE_DIR}/flutter-webrtc-deploy-${ENVIRONMENT}"
STAGING_DIR="${WORKDIR}/staging"

need_cmd() { command -v "$1" >/dev/null 2>&1 || { echo "ERROR: missing cmd: $1" >&2; exit 1; }; }
need_cmd aws
need_cmd unzip
need_cmd systemctl
need_cmd sudo

if [[ ! -d "${BASE_DIR}" ]]; then
  echo "ERROR: ${BASE_DIR} does not exist." >&2
  exit 1
fi

mkdir -p "${WORKDIR}"

echo "==> Environment: ${ENVIRONMENT}"
echo "==> Domain:      ${DOMAIN}"
echo "==> Service:     ${SERVICE_NAME}"
echo "==> Target dir:  ${TARGET_DIR}"
echo "==> S3 object:   s3://${S3_BUCKET}/${S3_PREFIX}/${ZIP_FILE}"

echo "==> Stopping service (if running)..."
sudo systemctl stop "${SERVICE_NAME}" || true

echo "==> Preparing download..."
cd "${WORKDIR}"
rm -f -- ./*.zip

echo "==> Downloading ZIP from S3..."
aws s3 cp "s3://${S3_BUCKET}/${S3_PREFIX}/${ZIP_FILE}" "./${ZIP_FILE}"

# Backup existing deployment
if [[ -d "${TARGET_DIR}" ]]; then
  TS="$(date +%Y%m%d_%H%M%S)"
  BACKUP_DIR="${BASE_DIR}/flutter-webrtc-server-${ENVIRONMENT}.backup_${TS}"
  echo "==> Creating backup: ${BACKUP_DIR}"
  if ! sudo mv "${TARGET_DIR}" "${BACKUP_DIR}"; then
    echo "==> mv failed, doing copy backup..."
    sudo mkdir -p "${BACKUP_DIR}"
    sudo cp -a "${TARGET_DIR}/." "${BACKUP_DIR}/"
    sudo rm -rf "${TARGET_DIR}"
  fi
fi

# Extract to staging
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
  echo "ERROR: Could not find extracted source directory in ${STAGING_DIR}" >&2
  exit 1
fi

echo "==> Promoting to target: ${TARGET_DIR}"
if [[ "${EXTRACTED_DIR}" == "${STAGING_DIR}" ]]; then
  sudo mkdir -p "${TARGET_DIR}"
  sudo cp -a "${STAGING_DIR}/." "${TARGET_DIR}/"
else
  sudo mv "${EXTRACTED_DIR}" "${TARGET_DIR}"
fi

sudo chown -R ubuntu:ubuntu "${TARGET_DIR}"
cd "${TARGET_DIR}"

# Apply environment-specific config
CONFIG_FILE="configs/config-${ENVIRONMENT}.ini"
if [[ -f "${CONFIG_FILE}" ]]; then
  echo "==> Applying config: ${CONFIG_FILE}"
  cp "${CONFIG_FILE}" configs/config.ini
else
  echo "==> No environment config found (${CONFIG_FILE}), using config.ini as-is"
fi

# Use pre-compiled binary if available, otherwise build
if [[ -f "${BUILD_OUTPUT}" ]]; then
  echo "==> Using pre-compiled binary"
  chmod +x "${BUILD_OUTPUT}"
  ls -lh "${BUILD_OUTPUT}"
else
  echo "==> Building Go binary..."
  need_cmd go
  go build -o "${BUILD_OUTPUT}" "${GO_MAIN_PATH}"
fi

# Copy TLS certificates
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
sudo chown ubuntu:ubuntu "${CERT_DST_DIR}"/*.pem

echo "==> Reloading systemd..."
sudo systemctl daemon-reload

echo "==> Restarting service..."
sudo systemctl restart "${SERVICE_NAME}"

echo "==> Service status:"
sudo systemctl status "${SERVICE_NAME}" --no-pager
