#!/usr/bin/env bash
set -euo pipefail

SERVICE_NAME="flutter-webrtc.service"
GO_MAIN_PATH="cmd/server/main.go"
BUILD_OUTPUT="webrtc-server"

# Fixed paths (per your requirement)
BASE_DIR="/home/ubuntu"
TARGET_DIR="${BASE_DIR}/flutter-webrtc-server-master"
WORKDIR="${BASE_DIR}/flutter-webrtc-deploy"
STAGING_DIR="${WORKDIR}/staging"

ZIP_FILE=""
S3_BUCKET=""

usage() {
  cat <<'EOF'
Usage:
  deploy_flutter_webrtc.sh -b <bucket-name> -f <zip-file-name> [options]

Required:
  -b   S3 bucket name (without s3://)
  -f   ZIP file name in the bucket (e.g. flutter-webrtc-server-master_1_.zip)

Optional:
  -s   Systemd service name (default: flutter-webrtc.service)
  -h   Show help

Example:
  ./deploy_flutter_webrtc.sh -b ota-img-dev.lgmk-eng.com -f flutter-webrtc-server-master_1_.zip
EOF
}

while getopts ":b:f:s:h" opt; do
  case "$opt" in
    b) S3_BUCKET="$OPTARG" ;;
    f) ZIP_FILE="$OPTARG" ;;
    s) SERVICE_NAME="$OPTARG" ;;
    h) usage; exit 0 ;;
    :)
      echo "ERROR: Option -$OPTARG requires an argument." >&2
      usage
      exit 2
      ;;
    \?)
      echo "ERROR: Invalid option -$OPTARG" >&2
      usage
      exit 2
      ;;
  esac
done

if [[ -z "${S3_BUCKET}" || -z "${ZIP_FILE}" ]]; then
  echo "ERROR: -b <bucket> and -f <zip-file> are required." >&2
  usage
  exit 2
fi

need_cmd() { command -v "$1" >/dev/null 2>&1 || { echo "ERROR: missing cmd: $1" >&2; exit 1; }; }
need_cmd aws
need_cmd unzip
need_cmd go
need_cmd systemctl
need_cmd sudo

# Hard requirement: fixed ubuntu home path
if [[ ! -d "${BASE_DIR}" ]]; then
  echo "ERROR: ${BASE_DIR} does not exist. This script requires /home/ubuntu." >&2
  exit 1
fi

mkdir -p "${WORKDIR}"

echo "==> Service: ${SERVICE_NAME}"
echo "==> Target dir: ${TARGET_DIR}"
echo "==> S3 object: s3://${S3_BUCKET}/${ZIP_FILE}"

echo "==> Stopping service (if exists)..."
sudo systemctl stop "${SERVICE_NAME}" || true

echo "==> Preparing download..."
cd "${WORKDIR}"
rm -f -- ./*.zip

echo "==> Downloading ZIP from S3..."
aws s3 cp "s3://${S3_BUCKET}/${ZIP_FILE}" "./${ZIP_FILE}"	

# Backup existing target dir BEFORE replacing
if [[ -d "${TARGET_DIR}" ]]; then
  TS="$(date +%Y%m%d_%H%M%S)"
  BACKUP_DIR="${BASE_DIR}/flutter-webrtc-server-master.backup_${TS}"
  echo "==> Creating backup: ${BACKUP_DIR}"
  # Prefer mv (fast, preserves perms/ownership); fallback to cp if mv fails (different FS)
  if ! sudo mv "${TARGET_DIR}" "${BACKUP_DIR}"; then
    echo "==> mv failed, doing copy backup..."
    sudo mkdir -p "${BACKUP_DIR}"
    sudo cp -a "${TARGET_DIR}/." "${BACKUP_DIR}/"
    sudo rm -rf "${TARGET_DIR}"
  fi
fi

# Extract to staging, then move into place (atomic-ish)
echo "==> Extracting to staging..."
sudo rm -rf "${STAGING_DIR}"
mkdir -p "${STAGING_DIR}"
unzip -o "./${ZIP_FILE}" -d "${STAGING_DIR}"

# Check if files were extracted to a subdirectory or directly to staging
EXTRACTED_DIR="${STAGING_DIR}/flutter-webrtc-server-master"
if [[ ! -d "${EXTRACTED_DIR}" ]]; then
  # No subdirectory found, check if files are directly in staging
  if [[ -f "${STAGING_DIR}/go.mod" ]] || [[ -f "${STAGING_DIR}/${BUILD_OUTPUT}" ]]; then
    echo "==> Files extracted directly to staging, using staging as source"
    EXTRACTED_DIR="${STAGING_DIR}"
  else
    # Try to find first directory
    EXTRACTED_DIR="$(find "${STAGING_DIR}" -mindepth 1 -maxdepth 1 -type d | head -n 1 || true)"
  fi
fi

if [[ -z "${EXTRACTED_DIR}" || ! -d "${EXTRACTED_DIR}" ]]; then
  echo "ERROR: Could not find extracted source directory in ${STAGING_DIR}" >&2
  exit 1
fi

echo "==> Promoting extracted dir to target: ${TARGET_DIR}"

# If EXTRACTED_DIR is staging itself, copy contents instead of moving the directory
if [[ "${EXTRACTED_DIR}" == "${STAGING_DIR}" ]]; then
  sudo mkdir -p "${TARGET_DIR}"
  sudo cp -a "${STAGING_DIR}/." "${TARGET_DIR}/"
else
  sudo mv "${EXTRACTED_DIR}" "${TARGET_DIR}"
fi

# Ensure ubuntu user owns working tree
sudo chown -R ubuntu:ubuntu "${TARGET_DIR}"

cd "${TARGET_DIR}"

# Check if binary already exists (pre-compiled from GitHub Actions)
if [[ -f "${BUILD_OUTPUT}" ]]; then
  echo "==> Binary already exists (pre-compiled), skipping build..."
  chmod +x "${BUILD_OUTPUT}"
  ls -lh "${BUILD_OUTPUT}"
else
  echo "==> Building Go binary..."
  go build -o "${BUILD_OUTPUT}" "${GO_MAIN_PATH}"
fi

# TLS cert copy
LE_DOMAIN_PATH="/etc/letsencrypt/live/flutter-webrtc-develop2.lgmk-eng.com"
CERT_SRC="${LE_DOMAIN_PATH}/fullchain.pem"
KEY_SRC="${LE_DOMAIN_PATH}/privkey.pem"

CERT_DST_DIR="/home/ubuntu/flutter-webrtc-server-master/configs/certs"
CERT_DST="${CERT_DST_DIR}/cert.pem"
KEY_DST="${CERT_DST_DIR}/key.pem"

echo "==> Checking TLS certs..."
echo "    CERT_SRC: ${CERT_SRC}"
echo "    KEY_SRC:  ${KEY_SRC}"

# IMPORTANT: check with sudo, since certs are usually root-managed
if ! sudo test -f "${CERT_SRC}"; then
  echo "ERROR: fullchain.pem not found at: ${CERT_SRC}" >&2
  sudo ls -al "${LE_DOMAIN_PATH}" || true
  exit 1
fi

if ! sudo test -f "${KEY_SRC}"; then
  echo "ERROR: privkey.pem not found at: ${KEY_SRC}" >&2
  sudo ls -al "${LE_DOMAIN_PATH}" || true
  exit 1
fi

echo "==> Copying TLS certs..."
sudo mkdir -p "${CERT_DST_DIR}"
sudo cp "${CERT_SRC}" "${CERT_DST}"
sudo cp "${KEY_SRC}"  "${KEY_DST}"



sudo chown ubuntu:ubuntu "${CERT_DST_DIR}"/*.pem

echo "==> Reloading systemd..."
sudo systemctl daemon-reload

echo "==> Restarting service..."
sudo systemctl restart "${SERVICE_NAME}"

echo "==> Service status:"
sudo systemctl status "${SERVICE_NAME}" --no-pager