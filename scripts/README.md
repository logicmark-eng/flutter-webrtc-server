# Deployment Scripts

This directory contains scripts used for deploying the Flutter WebRTC Server.

## Files

| File | Description |
|------|-------------|
| `deploy-flutter-webrtc-server.sh` | Main deployment script, executed on EC2 via SSM |
| `flutter-webrtc.service.template` | Reference template for the systemd service unit |
| `certbot-deploy-hook.sh` | Certbot deploy hook — copies renewed certs to the app dir and restarts the service |

---

## deploy-flutter-webrtc-server.sh

Automated deployment script for the Flutter WebRTC Server on EC2 instances.

### What It Does

1. Creates the `flutter-webrtc` system user (if not present)
2. Installs or updates the systemd service unit
3. Stops the running service
4. Migrates legacy directory (develop only, one-time)
5. Downloads the ZIP package from S3
6. Backs up the current deployment
7. Extracts the new version to `/opt/flutter-webrtc/<environment>/`
8. Applies the environment-specific `config-<env>.ini`
9. Uses the pre-compiled binary (falls back to `go build` if missing)
10. Copies TLS certificates from Let's Encrypt
11. Installs the certbot deploy hook (`/etc/letsencrypt/renewal-hooks/deploy/flutter-webrtc.sh`)
12. Restarts the service

### Directory Layout

```
/opt/flutter-webrtc/
├── develop/                        # App — develop environment
│   ├── webrtc-server               # Pre-compiled binary
│   ├── configs/
│   │   ├── config.ini              # Active config (copied from config-develop.ini)
│   │   ├── config-develop.ini      # Environment-specific config
│   │   └── certs/
│   │       ├── cert.pem            # TLS certificate (from Let's Encrypt)
│   │       └── key.pem             # TLS private key
│   └── web/
└── main2/                          # App — main2 environment
    └── ...

/var/lib/flutter-webrtc-deploy/
├── develop/                        # Working directory (download + staging)
│   ├── staging/
│   └── *.zip
└── main2/

/var/backups/flutter-webrtc/
├── develop/
│   └── snap_YYYYMMDD_HHMMSS/      # Timestamped snapshots
└── main2/
    └── snap_YYYYMMDD_HHMMSS/
```

### System User

The service runs as a dedicated system user with no privileges:

```
User:  flutter-webrtc
Shell: /usr/sbin/nologin
Home:  none
Sudo:  no
```

Created automatically by the script on first deploy.

### Parameters

```bash
./deploy-flutter-webrtc-server.sh -b <bucket> -f <zip> -d <domain> -e <environment> [options]
```

| Flag | Required | Description |
|------|----------|-------------|
| `-b` | ✅ | S3 bucket name (without `s3://`) |
| `-f` | ✅ | ZIP filename (without S3 prefix) |
| `-d` | ✅ | Domain name for TLS certificates |
| `-e` | ✅ | Environment (`develop`, `main2`, `qa`, `staging`) |
| `-s` | — | Systemd service name (default: `flutter-webrtc.service`) |
| `-h` | — | Show help |

### Examples

```bash
# develop
./deploy-flutter-webrtc-server.sh \
  -b ota-img-dev.lgmk-eng.com \
  -f flutter-webrtc-server-develop-v1.2.0.zip \
  -d flutter-webrtc-develop2.lgmk-eng.com \
  -e develop

# main2
./deploy-flutter-webrtc-server.sh \
  -b ota-img-main2.logicmarkcloud.com \
  -f flutter-webrtc-server-main2-v1.2.0.zip \
  -d flutter-webrtc.main2.logicmarkcloud.com \
  -e main2
```

### Requirements

**On EC2 instance:**
- Ubuntu 24.04 LTS
- AWS CLI — `sudo snap install aws-cli --classic`
- `zip`, `unzip` — `sudo apt-get install -y zip unzip`
- `systemctl`, `sudo`
- IAM role attached with S3 read and SSM permissions (no static credentials)
- Let's Encrypt certificates at `/etc/letsencrypt/live/<domain>/`

**The script does NOT need to be pre-installed on the instance.** The CI/CD
pipeline extracts the latest version from the ZIP package before executing it.

### Rollback

```bash
# List available snapshots
ls -lt /var/backups/flutter-webrtc/develop/

# Stop service
sudo systemctl stop flutter-webrtc.service

# Replace current deployment with a snapshot
sudo rm -rf /opt/flutter-webrtc/develop
sudo cp -a /var/backups/flutter-webrtc/develop/snap_<TIMESTAMP> /opt/flutter-webrtc/develop

# Restart service
sudo systemctl restart flutter-webrtc.service
```

### Troubleshooting

| Error | Cause | Fix |
|-------|-------|-----|
| `missing cmd: aws` | AWS CLI not installed | Install AWS CLI |
| `fullchain.pem not found` | Certbot not run | `sudo certbot certonly --standalone -d <domain>` |
| Service fails to start | Binary or config issue | `sudo journalctl -u flutter-webrtc.service -n 50` |
| Permission denied | Wrong file ownership | `sudo chown -R flutter-webrtc:flutter-webrtc /opt/flutter-webrtc/<env>` |

### Legacy Migration (develop)

On first deploy after this update, the script automatically migrates:

```
/home/ubuntu/flutter-webrtc-server-master/  →  /opt/flutter-webrtc/develop/
```

The systemd service is updated to point to the new path. No manual action needed.

---

## certbot-deploy-hook.sh

Certbot deploy hook installed by the deployment script at
`/etc/letsencrypt/renewal-hooks/deploy/flutter-webrtc.sh`.

### Why It Exists

Certbot renews certificates under `/etc/letsencrypt/live/<domain>/`, but the
service reads its own copies at `/opt/flutter-webrtc/<env>/configs/certs/`
(copied only at deploy time). Without this hook, a successful renewal never
reaches the running service and it eventually serves an expired certificate —
this caused the July 2026 expired-cert incident on both environments.

### What It Does

Certbot runs every executable in `renewal-hooks/deploy/` after each
**successful** renewal, exporting `RENEWED_LINEAGE`. The hook:

1. Maps the renewed lineage to the environment app directory:
   - `flutter-webrtc-develop2.lgmk-eng.com` → `/opt/flutter-webrtc/develop`
   - `flutter-webrtc.main2.logicmarkcloud.com` → `/opt/flutter-webrtc/main2`
   - Any other lineage → exits silently (not managed by this hook)
2. Copies `fullchain.pem` → `cert.pem` and `privkey.pem` → `key.pem`
3. Sets ownership `flutter-webrtc:flutter-webrtc` and mode `640`
4. Restarts `flutter-webrtc.service`

### Verification

```bash
# Confirm the hook is installed and executable
ls -l /etc/letsencrypt/renewal-hooks/deploy/flutter-webrtc.sh

# Dry-run renewal (executes deploy hooks only on real renewals, but validates config)
sudo certbot renew --dry-run

# Check the cert the service is actually serving
echo | openssl s_client -connect localhost:8086 2>/dev/null | openssl x509 -noout -dates
```

> **Note:** renewal also requires the domain's DNS A record to point at the
> instance's current public IP — Let's Encrypt validates HTTP-01 on port 80
> against the resolved IP. If the instance has no Elastic IP, a stop/start
> changes the public IP and silently breaks renewals.

---

## flutter-webrtc.service.template

Reference template showing the systemd unit structure. The actual unit file is
generated and installed directly by `deploy-flutter-webrtc-server.sh` — this
template exists for documentation and manual installation reference only.

---

**Script Version:** 2.1.0
**Last Updated:** 2026-07-02
**Maintained By:** SRE Team
