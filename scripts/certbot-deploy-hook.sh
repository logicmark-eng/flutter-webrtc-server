#!/bin/bash
# Certbot deploy hook: copy renewed Let's Encrypt certificates to the
# flutter-webrtc app directory and restart the service.
#
# Install path (on EC2): /etc/letsencrypt/renewal-hooks/deploy/flutter-webrtc.sh
# Certbot runs every hook in this directory after each SUCCESSFUL renewal,
# exporting RENEWED_LINEAGE (e.g. /etc/letsencrypt/live/<domain>) and
# RENEWED_DOMAINS. Runs as root — no sudo required.
#
# Without this hook the app keeps serving the certificate copied at deploy
# time, which expires even though certbot renews the lineage successfully.

set -euo pipefail

SERVICE_USER="flutter-webrtc"
SERVICE_NAME="flutter-webrtc.service"

# Map renewed lineage to the environment app directory.
# Each environment's cert lineage is matched by its domain.
case "${RENEWED_LINEAGE:-}" in
  */flutter-webrtc-develop2.lgmk-eng.com)
    TARGET_DIR="/opt/flutter-webrtc/develop"
    ;;
  */flutter-webrtc.main2.logicmarkcloud.com)
    TARGET_DIR="/opt/flutter-webrtc/main2"
    ;;
  *)
    echo "certbot-deploy-hook: lineage '${RENEWED_LINEAGE:-unset}' not managed by flutter-webrtc, skipping."
    exit 0
    ;;
esac

CERT_DST_DIR="${TARGET_DIR}/configs/certs"

if [ ! -d "${CERT_DST_DIR}" ]; then
  echo "certbot-deploy-hook: ${CERT_DST_DIR} does not exist, skipping." >&2
  exit 0
fi

echo "certbot-deploy-hook: deploying renewed cert from ${RENEWED_LINEAGE} to ${CERT_DST_DIR}"

cp "${RENEWED_LINEAGE}/fullchain.pem" "${CERT_DST_DIR}/cert.pem"
cp "${RENEWED_LINEAGE}/privkey.pem"   "${CERT_DST_DIR}/key.pem"
chown "${SERVICE_USER}:${SERVICE_USER}" "${CERT_DST_DIR}/cert.pem" "${CERT_DST_DIR}/key.pem"
chmod 640 "${CERT_DST_DIR}/cert.pem" "${CERT_DST_DIR}/key.pem"

systemctl restart "${SERVICE_NAME}"
echo "certbot-deploy-hook: ${SERVICE_NAME} restarted with renewed certificate."
