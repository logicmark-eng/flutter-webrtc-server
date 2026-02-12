# Deployment Scripts

This directory contains scripts used for deploying the Flutter WebRTC Server.

## deploy-flutter-webrtc-server.sh

Automated deployment script for the Flutter WebRTC Server on EC2 instances.

### Purpose

This script handles the complete deployment process:
1. Downloads the packaged application from S3
2. Backs up the current running version
3. Extracts and builds the new version
4. Copies SSL/TLS certificates
5. Restarts the systemd service

### Requirements

**On EC2 Instance:**
- Ubuntu (tested on Ubuntu 24.04 LTS)
- Go compiler installed
- AWS CLI configured with appropriate credentials
- Systemd service configured: `flutter-webrtc.service`
- SSL certificates at: `/etc/letsencrypt/live/flutter-webrtc-develop2.lgmk-eng.com/`

**Required Commands:**
- `aws` (AWS CLI)
- `unzip`
- `go`
- `systemctl`
- `sudo`

### Installation

Copy the script to the EC2 instance:

```bash
# From your local machine (repository root)
scp -i ~/.ssh/lgmk-pers-develop-key-pair.pem \
    scripts/deploy-flutter-webrtc-server.sh \
    ubuntu@<instance-ip>:/home/ubuntu/

# Set execute permissions
ssh -i ~/.ssh/lgmk-pers-develop-key-pair.pem ubuntu@<instance-ip>
chmod +x /home/ubuntu/deploy-flutter-webrtc-server.sh
```

### Usage

```bash
./deploy-flutter-webrtc-server.sh -b <bucket-name> -f <zip-file-name> [options]
```

**Required Parameters:**
- `-b` : S3 bucket name (without `s3://` prefix)
- `-f` : ZIP file name in the bucket

**Optional Parameters:**
- `-s` : Systemd service name (default: `flutter-webrtc.service`)
- `-h` : Show help

**Example:**
```bash
./deploy-flutter-webrtc-server.sh \
  -b ota-img-dev.lgmk-eng.com \
  -f flutter-webrtc-server-masterv0.0.7.zip
```

### How It Works

1. **Pre-deployment Checks**
   - Validates required commands are available
   - Verifies `/home/ubuntu` directory exists
   - Stops the running service

2. **Download Package**
   - Downloads ZIP file from S3 bucket
   - Saves to working directory: `/home/ubuntu/flutter-webrtc-deploy/`

3. **Backup Current Version**
   - Creates timestamped backup: `/home/ubuntu/flutter-webrtc-server-master.backup_YYYYMMDD_HHMMSS`
   - Preserves previous deployment for rollback

4. **Extract and Build**
   - Extracts ZIP to staging directory
   - Moves to target location: `/home/ubuntu/flutter-webrtc-server-master`
   - Builds Go binary: `go build -o webrtc-server cmd/server/main.go`

5. **Configure SSL Certificates**
   - Copies Let's Encrypt certificates from `/etc/letsencrypt/live/`
   - Places in application directory: `configs/certs/`
   - Sets appropriate ownership for `ubuntu` user

6. **Service Restart**
   - Reloads systemd daemon
   - Restarts `flutter-webrtc.service`
   - Displays service status

### Directory Structure

```
/home/ubuntu/
├── flutter-webrtc-server-master/           # Current deployment
│   ├── cmd/
│   ├── configs/
│   │   └── certs/
│   │       ├── cert.pem                    # SSL certificate (copied)
│   │       └── key.pem                     # SSL key (copied)
│   ├── pkg/
│   ├── web/
│   └── webrtc-server                       # Built binary
├── flutter-webrtc-server-master.backup_*/  # Timestamped backups
├── flutter-webrtc-deploy/                  # Working directory
│   ├── staging/                            # Extract location
│   └── *.zip                               # Downloaded packages
└── deploy-flutter-webrtc-server.sh         # This script
```

### Rollback

To rollback to a previous version:

```bash
# List available backups
ls -lt /home/ubuntu/ | grep backup

# Stop service
sudo systemctl stop flutter-webrtc.service

# Remove current version
sudo rm -rf /home/ubuntu/flutter-webrtc-server-master

# Restore backup (replace TIMESTAMP with actual timestamp)
sudo mv /home/ubuntu/flutter-webrtc-server-master.backup_TIMESTAMP \
       /home/ubuntu/flutter-webrtc-server-master

# Restart service
sudo systemctl restart flutter-webrtc.service
```

### Troubleshooting

**Script fails with "missing cmd" error:**
- Install required command on the EC2 instance

**"Deployment script not found" in GitHub Actions:**
- Ensure script is copied to `/home/ubuntu/` on the EC2 instance
- Verify execute permissions: `ls -l /home/ubuntu/deploy-flutter-webrtc-server.sh`

**SSL certificate not found:**
- Verify Let's Encrypt certificates exist: `sudo ls -la /etc/letsencrypt/live/`
- Check domain name in script matches your certificate path
- Ensure certbot has generated certificates successfully

**Service fails to start:**
- Check service logs: `sudo journalctl -u flutter-webrtc.service -n 50`
- Verify Go build completed successfully
- Check configuration file: `/home/ubuntu/flutter-webrtc-server-master/configs/config.ini`

### Integration with CI/CD

This script is called automatically by the GitHub Actions workflow during deployment:

1. GitHub Actions builds and uploads ZIP to S3
2. GitHub Actions executes this script via AWS Systems Manager (SSM)
3. Script downloads, extracts, builds, and restarts the service
4. GitHub Actions verifies service health

See the main CI/CD documentation in the repository root for more details.

### Security Notes

- Script requires `sudo` for service management and certificate access
- AWS credentials must be configured on the EC2 instance (uses IAM role)
- SSL certificates are root-owned, script copies with `sudo`
- Backups are created with original permissions preserved

### Maintenance

**Updating the script:**
1. Modify `scripts/deploy-flutter-webrtc-server.sh` in the repository
2. Commit and push changes
3. Manually copy updated script to EC2 instances:
   ```bash
   scp -i ~/.ssh/key.pem scripts/deploy-flutter-webrtc-server.sh ubuntu@<ip>:/home/ubuntu/
   ```

**Cleaning old backups:**
```bash
# Keep only last 5 backups
cd /home/ubuntu
ls -t | grep backup | tail -n +6 | xargs rm -rf
```

---

**Script Version:** 1.0.0
**Last Updated:** 2026-01-28
**Maintained By:** SRE Team
