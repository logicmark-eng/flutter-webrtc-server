# CI/CD Setup - Flutter WebRTC Server

This document describes the configuration and usage of the CI/CD pipeline for deploying the Flutter WebRTC Server to AWS EC2.

## 📋 Table of Contents

- [Pipeline Architecture](#pipeline-architecture)
- [Branch to Environment Mapping](#branch-to-environment-mapping)
- [Prerequisites](#prerequisites)
- [Initial Configuration](#initial-configuration)
- [Pipeline Usage](#pipeline-usage)
- [Environment Management](#environment-management)
- [Troubleshooting](#troubleshooting)

---

## 🏗️ Pipeline Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│  GitHub Repository (flutter-webrtc-server)                      │
│  develop branch → deploy to develop environment                 │
│  master branch  → deploy to main2 environment (production)      │
└────────────────┬────────────────────────────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────────────────────────────┐
│  GitHub Actions Workflow (.github/workflows/deploy.yml)         │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │ Job 1: Build                                             │   │
│  │  - Determine environment from branch                    │   │
│  │  - Compile Go binary (CGO_ENABLED=0, linux/amd64)       │   │
│  │  - Create versioned ZIP package                         │   │
│  │  - Upload as artifact                                   │   │
│  └──────────────────────────────────────────────────────────┘   │
│  ┌──────────────────────────────────────────────────────────┐   │
│  │ Job 2: Deploy                                            │   │
│  │  - Assume environment-specific IAM Role (OIDC)          │   │
│  │  - Upload ZIP to S3 (env-specific bucket + prefix)      │   │
│  │  - Rotate S3 packages (keep last 5)                     │   │
│  │  - Find EC2 instance by Name tag                        │   │
│  │  - Extract deploy script from ZIP via SSM               │   │
│  │  - Execute deployment script via SSM                    │   │
│  │  - Verify service health                                │   │
│  └──────────────────────────────────────────────────────────┘   │
└────────────────┬────────────────────────────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────────────────────────────┐
│  AWS Infrastructure                                             │
│  ├── develop                                                    │
│  │   ├── S3: ota-img-dev.lgmk-eng.com                          │
│  │   │       └── flutter-webrtc-server-develop/ (last 5 ZIPs)  │
│  │   └── EC2: lgmk-flutter-webrtc-server-develop               │
│  │           └── /opt/flutter-webrtc/develop/                  │
│  └── main2                                                      │
│      ├── S3: ota-img-main2.logicmarkcloud.com                   │
│      │       └── flutter-webrtc-server-main2/ (last 5 ZIPs)    │
│      └── EC2: lgmk-flutter-webrtc-server-main2                 │
│              └── /opt/flutter-webrtc/main2/                    │
└─────────────────────────────────────────────────────────────────┘
```

---

## 🌿 Branch to Environment Mapping

| Git Branch | Environment | S3 Bucket | Domain | IAM Secret |
|-----------|-------------|-----------|--------|------------|
| `develop` | `develop` | `ota-img-dev.lgmk-eng.com` | `flutter-webrtc-develop2.lgmk-eng.com` | `AWS_ROLE_TO_ASSUME_DEVELOP` |
| `master` | `main2` | `ota-img-main2.logicmarkcloud.com` | `flutter-webrtc.main2.logicmarkcloud.com` | `AWS_ROLE_TO_ASSUME_MAIN2` |
| `v*.*.*` (tag) | `main2` | `ota-img-main2.logicmarkcloud.com` | `flutter-webrtc.main2.logicmarkcloud.com` | `AWS_ROLE_TO_ASSUME_MAIN2` |

> Tags always deploy to `main2`. `workflow_dispatch` determines environment from the selected branch.

---

## ✅ Prerequisites

### 1. AWS Infrastructure

EC2 instances must be deployed from the `lgmk-pers-base-infra` project:

```bash
# In lgmk-pers-base-infra project
cd /path/to/lgmk-pers-base-infra

# Enable WebRTC server in desired environment
# Edit: terraform/environments/{environment}.tfvars
enable_webrtc_server = true

# Deploy infrastructure
source config-{environment}.env
cd terraform
terraform plan -var-file=environments/{environment}.tfvars
terraform apply -var-file=environments/{environment}.tfvars
```

**Created resources:**
- EC2 instance: `lgmk-flutter-webrtc-server-{environment}`
- Security Group with ports: 22, 80, 8086, 19302/UDP, 19303/TCP
- IAM Role with policies: SSM, CloudWatch, S3 read access
- SSM agent installed and configured

### 2. First-Deploy Requirements per EC2 Instance

Each EC2 instance needs the following before the first pipeline run:

**Required packages:**
```bash
# AWS CLI (required by SSM inline commands and by the deploy script)
sudo snap install aws-cli --classic

# zip / unzip (required by the deploy script)
sudo apt-get install -y zip unzip

# Verify
aws --version
unzip -v | head -1
```

**SSL Certificates (certbot):**
```bash
# Install certbot
sudo snap install --classic certbot

# Generate certificate for the environment domain
# develop:
sudo certbot certonly --standalone -d flutter-webrtc-develop2.lgmk-eng.com

# main2:
sudo certbot certonly --standalone -d flutter-webrtc.main2.logicmarkcloud.com

# Verify
sudo ls -la /etc/letsencrypt/live/<domain>/
# Expected files: fullchain.pem, privkey.pem
```

**Handled automatically by the pipeline:**
- Compiles the Go binary in GitHub Actions (no Go needed on EC2)
- Extracts the updated deploy script from the ZIP package before executing it
- Creates the `flutter-webrtc` system user on first deploy
- Installs and enables the systemd service on first deploy

### 3. S3 Buckets

| Environment | Bucket | Prefix |
|-------------|--------|--------|
| develop | `ota-img-dev.lgmk-eng.com` | `flutter-webrtc-server-develop/` |
| main2 | `ota-img-main2.logicmarkcloud.com` | `flutter-webrtc-server-main2/` |

The pipeline automatically rotates packages, keeping only the last 5 ZIPs per prefix.

---

## 🔧 Initial Configuration

### 1. GitHub Secrets

Configure the following secrets in GitHub (Settings → Secrets and variables → Actions):

| Secret Name | Description | Status |
|------------|-------------|--------|
| `AWS_ROLE_TO_ASSUME_DEVELOP` | IAM Role ARN for develop environment | ✅ Configured |
| `AWS_ROLE_TO_ASSUME_MAIN2` | IAM Role ARN for main2/production | ✅ Configured |
| `AWS_ROLE_TO_ASSUME_QA` | IAM Role ARN for qa environment (future) | — |
| `AWS_ROLE_TO_ASSUME_STAGING` | IAM Role ARN for staging environment (future) | — |

**main2 role ARN:** `arn:aws:iam::058264321995:role/lgmk-github-oidc-main-role2`

### 2. GitHub Environments

Create the following environments in GitHub (Settings → Environments):

| Environment | Protection | Purpose |
|-------------|-----------|---------|
| `develop` | No rules | Automatic on push to `develop` branch |
| `main2` | Recommended: require approval | Automatic on push to `master` branch |

### 3. AWS IAM Roles for GitHub Actions (OIDC)

Separate IAM Roles exist for each environment with OIDC trust policy:

**Trust Policy template:**
```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "Federated": "arn:aws:iam::ACCOUNT_ID:oidc-provider/token.actions.githubusercontent.com"
      },
      "Action": "sts:AssumeRoleWithWebIdentity",
      "Condition": {
        "StringEquals": {
          "token.actions.githubusercontent.com:aud": "sts.amazonaws.com"
        },
        "StringLike": {
          "token.actions.githubusercontent.com:sub": "repo:ORG_NAME/flutter-webrtc-server:*"
        }
      }
    }
  ]
}
```

**Required permissions per environment (example for develop):**
```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "s3:PutObject",
        "s3:GetObject",
        "s3:ListBucket",
        "s3:DeleteObject"
      ],
      "Resource": [
        "arn:aws:s3:::ota-img-dev.lgmk-eng.com/*",
        "arn:aws:s3:::ota-img-dev.lgmk-eng.com"
      ]
    },
    {
      "Effect": "Allow",
      "Action": [
        "ec2:DescribeInstances",
        "ec2:DescribeTags"
      ],
      "Resource": "*"
    },
    {
      "Effect": "Allow",
      "Action": [
        "ssm:SendCommand",
        "ssm:ListCommandInvocations",
        "ssm:GetCommandInvocation",
        "ssm:DescribeInstanceInformation"
      ],
      "Resource": "*"
    }
  ]
}
```

> For main2, replace the S3 bucket ARNs with `ota-img-main2.logicmarkcloud.com`.

---

## 🚀 Pipeline Usage

### Automatic Deployment

| Action | Result |
|--------|--------|
| Push to `develop` | Deploys to `develop` environment |
| Push to `master` | Deploys to `main2` environment |
| Push tag `v*.*.*` | Deploys to `main2` environment |

```bash
# Deploy to develop (standard flow)
git checkout develop
git add .
git commit -m "feat: new functionality"
git push origin develop
```

### Manual Deployment (workflow_dispatch)

```bash
# From GitHub UI:
# Actions → Deploy Flutter WebRTC Server → Run workflow
# Select branch: develop (for develop) or master (for main2)

# Using GitHub CLI:
gh workflow run deploy.yml --ref develop
gh workflow run deploy.yml --ref master
gh workflow run deploy.yml --ref master -f version=v1.2.0
```

### Versioned Releases

```bash
# Create and push a semantic tag (deploys to main2)
git checkout master
git tag -a v1.2.0 -m "Release v1.2.0: description"
git push origin v1.2.0
```

### Git Flow

```
feature/xxx ──┐
              ├──► develop ──► master
feature/yyy ──┘
```

1. Create feature branch from `develop`
2. Open PR: `feature/xxx` → `develop`
3. After testing in develop: open PR `develop` → `master` for main2 deploy

---

## 🌍 Environment Management

### Active Environments

| Environment | Branch | EC2 Instance | App Directory | Status |
|-------------|--------|--------------|---------------|--------|
| `develop` | `develop` | `lgmk-flutter-webrtc-server-develop` | `/opt/flutter-webrtc/develop/` | ✅ Active |
| `main2` | `master` | `lgmk-flutter-webrtc-server-main2` | `/opt/flutter-webrtc/main2/` | ✅ Active |

### Directory Layout on EC2

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

Created automatically on first deploy.

### Future Environment Expansion

When additional environments (qa, staging) need to be enabled:

1. Add IAM Role + GitHub Secret for the new environment
2. Update `.github/workflows/deploy.yml` trigger branches and environment map
3. Add `configs/config-{env}.ini` with the environment-specific config
4. Enable infrastructure in `lgmk-pers-base-infra`
5. Run certbot on the new EC2 instance

---

## 🔍 Troubleshooting

### Error: "Instance not found"

**Cause:** No EC2 instance with matching Name tag for the environment.

**Solution:**
```bash
# Verify in lgmk-pers-base-infra
cat terraform/environments/{environment}.tfvars | grep enable_webrtc_server
# If false, change to true and apply terraform
```

### Error: "SSM agent is not online"

**Cause:** EC2 instance SSM agent not running or missing IAM role.

**Solution:**
```bash
ssh ubuntu@<instance-ip>
sudo systemctl status amazon-ssm-agent

# If not installed:
sudo snap install amazon-ssm-agent --classic
sudo snap start amazon-ssm-agent
```

### Error: "fullchain.pem not found"

**Cause:** certbot certificates not yet generated on the instance.

**Solution:**
```bash
sudo snap install --classic certbot
sudo certbot certonly --standalone -d <domain>
```

### Error: "Service failed to start"

**Cause:** Binary, config, or permission issue.

**Solution:**
```bash
ssh ubuntu@<instance-ip>
sudo journalctl -u flutter-webrtc.service -n 50 --no-pager
sudo systemctl status flutter-webrtc.service

# Check ownership
sudo ls -la /opt/flutter-webrtc/<environment>/
# Fix ownership if needed:
sudo chown -R flutter-webrtc:flutter-webrtc /opt/flutter-webrtc/<environment>/
```

### Manual Rollback

```bash
# List available snapshots
ls -lt /var/backups/flutter-webrtc/<environment>/

# Stop service
sudo systemctl stop flutter-webrtc.service

# Restore snapshot
sudo rm -rf /opt/flutter-webrtc/<environment>
sudo cp -a /var/backups/flutter-webrtc/<environment>/snap_<TIMESTAMP> /opt/flutter-webrtc/<environment>

# Restart service
sudo systemctl restart flutter-webrtc.service
```

---

## 📊 Pipeline Monitoring

### GitHub Actions UI

- **Workflow runs:** Actions → Deploy Flutter WebRTC Server
- **Real-time logs:** Click on active run
- **Deployment summary:** Available at end of each successful deployment

### AWS CloudWatch

```bash
# View application logs
aws logs tail /aws/ec2/flutter-webrtc-<environment> --follow

# Service status from EC2
ssh ubuntu@<instance-ip>
sudo systemctl status flutter-webrtc.service
sudo journalctl -u flutter-webrtc.service -n 50
```

### SSM Command History

```bash
# View recent SSM commands
aws ssm list-commands \
  --filters Key=DocumentName,Values=AWS-RunShellScript \
  --max-results 10

# View specific command details
aws ssm get-command-invocation \
  --command-id <command-id> \
  --instance-id <instance-id>
```

---

## 🔐 Security

### Implemented Best Practices

✅ **OIDC for AWS:** No static access keys
✅ **Least privilege IAM:** Minimum necessary permissions per environment
✅ **Dedicated system user:** `flutter-webrtc` (no shell, no sudo)
✅ **systemd hardening:** `NoNewPrivileges`, `ProtectSystem=strict`, `ProtectHome`, `PrivateTmp`
✅ **App in `/opt/`:** Not in home directory, owned by service user
✅ **Secrets management:** No hardcoded secrets
✅ **SSL/TLS:** Let's Encrypt certificates, mode 640, owned by service user
✅ **SSM for deployment:** No direct SSH required
✅ **Pre-compiled binary:** Go not required on EC2
✅ **S3 rotation:** Maximum 5 packages retained per environment

### Auditing

```bash
# View recent deployments in GitHub
gh run list --workflow=deploy.yml --limit 10

# View executed SSM commands
aws ssm list-commands --max-results 20

# View S3 accesses
aws cloudtrail lookup-events \
  --lookup-attributes AttributeKey=ResourceName,AttributeValue=ota-img-dev.lgmk-eng.com \
  --max-results 50
```

---

## 📝 Deployment Checklist

Before promoting develop → master (main2 deploy):

- [ ] All tests passed in develop environment
- [ ] Code reviewed (PR approval)
- [ ] Deployed and verified in develop
- [ ] `configs/config-main2.ini` is up to date
- [ ] Rollback plan documented
- [ ] Team notified
- [ ] Active post-deployment monitoring

---

## 🆘 Support

**Pipeline logs:** GitHub Actions → Deploy Flutter WebRTC Server
**Infrastructure:** `lgmk-pers-base-infra` project
**Deployment script:** `scripts/deploy-flutter-webrtc-server.sh` (extracted from ZIP, no pre-install needed)
**App directory on EC2:** `/opt/flutter-webrtc/<environment>/`
**AWS Documentation:** [AWS Systems Manager](https://docs.aws.amazon.com/systems-manager/)

---

**Last updated:** 2026-03-06
**Maintained by:** SRE Team
