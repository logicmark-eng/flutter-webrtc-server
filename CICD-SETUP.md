# CI/CD Setup - Flutter WebRTC Server

This document describes the configuration and usage of the CI/CD pipeline for deploying the Flutter WebRTC Server to AWS EC2.

## 📋 Table of Contents

- [Pipeline Architecture](#pipeline-architecture)
- [Prerequisites](#prerequisites)
- [Initial Configuration](#initial-configuration)
- [Pipeline Usage](#pipeline-usage)
- [Environment Management](#environment-management)
- [Troubleshooting](#troubleshooting)

---

## 🏗️ Pipeline Architecture

```
┌─────────────────────────────────────────────────────────────┐
│  GitHub Repository (flutter-webrtc-server)                  │
│  Branch: master → deploy to develop environment             │
└────────────────┬────────────────────────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────────────────────────┐
│  GitHub Actions Workflow (.github/workflows/deploy.yml)    │
│  ┌──────────────────────────────────────────────────────┐  │
│  │ Job 1: Build                                         │  │
│  │  - Create versioned ZIP package                     │  │
│  │  - Upload as artifact                               │  │
│  └──────────────────────────────────────────────────────┘  │
│  ┌──────────────────────────────────────────────────────┐  │
│  │ Job 2: Deploy                                        │  │
│  │  - Upload ZIP to S3                                 │  │
│  │  - Find EC2 instance by environment tag             │  │
│  │  - Execute deployment via AWS SSM                   │  │
│  │  - Verify service health                            │  │
│  └──────────────────────────────────────────────────────┘  │
└────────────────┬────────────────────────────────────────────┘
                 │
                 ▼
┌─────────────────────────────────────────────────────────────┐
│  AWS Infrastructure                                         │
│  ├── S3: ota-img-dev.lgmk-eng.com (deployment packages)    │
│  └── EC2: lgmk-flutter-webrtc-server-develop               │
│      - Managed by lgmk-pers-base-infra Terraform          │
│      - SSM agent for remote execution                      │
│      - Deployment script: /home/ubuntu/deploy-flutter-     │
│        webrtc-server.sh                                    │
└─────────────────────────────────────────────────────────────┘
```

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
- IAM Role with policies: SSM, CloudWatch
- SSM agent installed and configured

### 2. Deployment Script on EC2

The `deploy-flutter-webrtc-server.sh` script is located in the `scripts/` directory of this repository and must be copied to `/home/ubuntu/` on each EC2 instance:

```bash
# Copy script from repository to EC2 instance
scp -i ~/.ssh/lgmk-pers-{env}-key-pair.pem \
    scripts/deploy-flutter-webrtc-server.sh \
    ubuntu@<instance-ip>:/home/ubuntu/

# Set execute permissions
ssh -i ~/.ssh/lgmk-pers-{env}-key-pair.pem ubuntu@<instance-ip>
chmod +x /home/ubuntu/deploy-flutter-webrtc-server.sh
```

**Script location in repository:** `scripts/deploy-flutter-webrtc-server.sh`
**Required location on EC2:** `/home/ubuntu/deploy-flutter-webrtc-server.sh`

### 3. SSL Certificates

Let's Encrypt certificates must be on the instance:

```bash
# Expected path on EC2:
/etc/letsencrypt/live/flutter-webrtc-{environment}.lgmk-eng.com/
├── fullchain.pem
└── privkey.pem
```

---

## 🔧 Initial Configuration

### 1. GitHub Secrets

Configure the following secrets in GitHub (Settings → Secrets and variables → Actions):

| Secret Name | Description | Example |
|------------|-------------|---------|
| `AWS_ROLE_TO_ASSUME_DEVELOP` | IAM Role ARN for develop environment | `arn:aws:iam::123456789012:role/GitHubActions-WebRTC-Deploy-Develop` |
| `AWS_ROLE_TO_ASSUME_QA` | IAM Role ARN for qa environment (future) | `arn:aws:iam::123456789012:role/GitHubActions-WebRTC-Deploy-QA` |
| `AWS_ROLE_TO_ASSUME_STAGING` | IAM Role ARN for staging environment (future) | `arn:aws:iam::123456789012:role/GitHubActions-WebRTC-Deploy-Staging` |
| `AWS_ROLE_TO_ASSUME_MAIN2` | IAM Role ARN for main2/production (future) | `arn:aws:iam::987654321098:role/GitHubActions-WebRTC-Deploy-Main2` |

**Note:** Currently only `AWS_ROLE_TO_ASSUME_DEVELOP` is required. Other secrets will be needed when expanding to additional environments.

### 2. GitHub Environments

Create the following environment in GitHub (Settings → Environments):

**develop** (auto-deployment)
- No protection rules
- Secrets: None additional

### 3. AWS IAM Roles for GitHub Actions (OIDC)

Create separate IAM Roles for each environment with trust policy for GitHub:

**Trust Policy** (same for all environments):
```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Principal": {
        "Federated": "arn:aws:iam::123456789012:oidc-provider/token.actions.githubusercontent.com"
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

**IAM Roles to create:**
- `GitHubActions-WebRTC-Deploy-Develop` (required now)
- `GitHubActions-WebRTC-Deploy-QA` (create when QA environment is enabled)
- `GitHubActions-WebRTC-Deploy-Staging` (create when Staging environment is enabled)
- `GitHubActions-WebRTC-Deploy-Main2` (create when Production environment is enabled)

**Required policies** (attach to each role):
```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": [
        "s3:PutObject",
        "s3:GetObject",
        "s3:ListBucket"
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

---

## 🚀 Pipeline Usage

**⚠️ Current Simplified Configuration:**
- Only `master` branch active
- Deploys only to `develop` environment
- Base version: `v0.0.7`

### Automatic Deployment (push to master)

The pipeline executes automatically when pushing to `master`:

```bash
# Deploy to develop environment
git checkout master
git add .
git commit -m "feat: new functionality"
git push origin master

# Pipeline automatically deploys to develop
```

### Manual Deployment (workflow_dispatch)

Execute manual deployment from GitHub Actions:

```bash
# Option 1: From GitHub UI
# Actions → Deploy Flutter WebRTC Server → Run workflow
# - Branch: master
# - Version: (optional, e.g., v0.0.8)

# Option 2: Using GitHub CLI
gh workflow run deploy.yml --ref master
gh workflow run deploy.yml --ref master -f version=v0.0.8
```

### Versioning with Tags

Create versioned releases (next version: v0.0.8):

```bash
# Create semantic tag
git tag -a v0.0.8 -m "Release v0.0.8: Change description"
git push origin v0.0.8

# Pipeline automatically deploys to develop with that tag
```

---

## 🌍 Environment Management

### Current Simplified Configuration

| Git Branch | AWS Environment | EC2 Instance Name | Status |
|----------|--------------|-------------------|--------|
| `master` | develop | `lgmk-flutter-webrtc-server-develop` | ✅ Active |

**Currently deployed version:** `v0.0.7`

### Disabled Environments

The following environments are temporarily disabled in the pipeline:
- `qa` - Not configured
- `staging` - Not configured
- `main2` / `production` - Not configured

### Future Environment Expansion

When additional environments need to be enabled:

1. **Create new branch** (e.g., `develop`, `qa`, `staging`)
2. **Modify workflow** in `.github/workflows/deploy.yml`:
   ```yaml
   on:
     push:
       branches:
         - master
         - develop    # Add
         - qa         # Add
         - staging    # Add
   ```
3. **Update environment mapping** in the workflow
4. **Enable infrastructure** in `lgmk-pers-base-infra` project:
   ```bash
   # Edit terraform/environments/{environment}.tfvars
   enable_webrtc_server = true

   # Apply changes
   terraform apply -var-file=environments/{environment}.tfvars
   ```

---

## 🔍 Troubleshooting

### Error: "Instance not found"

**Cause:** The environment doesn't have an EC2 instance with WebRTC server enabled.

**Solution:**
```bash
# Verify in lgmk-pers-base-infra
cat terraform/environments/{environment}.tfvars | grep enable_webrtc_server

# If false, change to true and apply terraform
```

### Error: "SSM agent is not online"

**Cause:** EC2 instance doesn't have SSM agent running or doesn't have correct IAM role.

**Solution:**
```bash
# Connect via SSH and verify SSM agent
ssh ubuntu@<instance-ip>
sudo systemctl status amazon-ssm-agent

# If not installed:
sudo snap install amazon-ssm-agent --classic
sudo snap start amazon-ssm-agent

# Verify IAM role in AWS Console:
# EC2 → Instances → Instance → Security → IAM Role
# Must have policy: AmazonSSMManagedInstanceCore
```

### Error: "Deployment script not found"

**Cause:** The script `deploy-flutter-webrtc-server.sh` is not at `/home/ubuntu/`.

**Solution:**
```bash
# Copy script from repository to EC2
scp -i ~/.ssh/key.pem scripts/deploy-flutter-webrtc-server.sh ubuntu@<ip>:/home/ubuntu/
ssh ubuntu@<ip> "chmod +x /home/ubuntu/deploy-flutter-webrtc-server.sh"
```

### Error: "Service failed to start"

**Cause:** The systemd service `flutter-webrtc.service` has issues.

**Solution:**
```bash
# SSH to instance
ssh ubuntu@<instance-ip>

# View service logs
sudo journalctl -u flutter-webrtc.service -n 50 --no-pager

# Verify configuration
sudo systemctl status flutter-webrtc.service

# Retry manually
cd /home/ubuntu/flutter-webrtc-server-master
go build -o webrtc-server cmd/server/main.go
sudo systemctl restart flutter-webrtc.service
```

### Error: "TLS cert not found"

**Cause:** SSL certificates are not at `/etc/letsencrypt/live/`.

**Solution:**
```bash
# Install certbot and generate certificates
sudo snap install --classic certbot
sudo certbot certonly --standalone \
  -d flutter-webrtc-{environment}.lgmk-eng.com \
  --email admin@lgmk-eng.com \
  --agree-tos

# Verify
sudo ls -la /etc/letsencrypt/live/flutter-webrtc-{environment}.lgmk-eng.com/
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
aws logs tail /aws/ec2/webrtc-server-{environment} --follow

# View instance metrics
aws cloudwatch get-metric-statistics \
  --namespace AWS/EC2 \
  --metric-name CPUUtilization \
  --dimensions Name=InstanceId,Value=<instance-id> \
  --start-time $(date -u -d '1 hour ago' +%Y-%m-%dT%H:%M:%S) \
  --end-time $(date -u +%Y-%m-%dT%H:%M:%S) \
  --period 300 \
  --statistics Average
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
✅ **Least privilege IAM:** Minimum necessary permissions
✅ **Environment protection:** Approvals for staging/production (future)
✅ **Secrets management:** No hardcoded secrets
✅ **SSL/TLS:** Automatic Let's Encrypt certificates
✅ **SSM for deployment:** No direct SSH required

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

Before pushing to production:

- [ ] Local tests passed
- [ ] Code reviewed (PR approval)
- [ ] Deployed and tested in develop
- [ ] Rollback plan documented
- [ ] Team notification
- [ ] Active post-deployment monitoring

---

## 🆘 Support

**Pipeline logs:** GitHub Actions → Deploy Flutter WebRTC Server
**Infrastructure:** `lgmk-pers-base-infra` project
**Deployment script:** `/home/ubuntu/deploy-flutter-webrtc-server.sh` on EC2
**AWS Documentation:** [AWS Systems Manager](https://docs.aws.amazon.com/systems-manager/)

---

**Last updated:** 2026-01-28
**Maintained by:** SRE Team
