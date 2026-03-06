# CI/CD Quick Start Guide

Quick guide to set up the deployment pipeline.

**Current Configuration:**
- `develop` branch → deploys to `develop` environment
- `master` branch → deploys to `main2` environment (production)
- Both environments active

## ⚡ Setup in 3 Steps

### 1️⃣ Configure OIDC in AWS

```bash
# Create IAM OIDC provider for GitHub (once per account)
aws iam create-open-id-connect-provider \
  --url https://token.actions.githubusercontent.com \
  --client-id-list sts.amazonaws.com \
  --thumbprint-list 6938fd4d98bab03faadb97b34396831e3780aea1

# Trust policy template (replace ACCOUNT_ID and YOUR_ORG)
cat > trust-policy.json <<'EOF'
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
          "token.actions.githubusercontent.com:sub": "repo:YOUR_ORG/flutter-webrtc-server:*"
        }
      }
    }
  ]
}
EOF

# Permissions policy template (adjust S3 bucket per environment)
cat > permissions-policy.json <<'EOF'
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": ["s3:PutObject", "s3:GetObject", "s3:ListBucket", "s3:DeleteObject"],
      "Resource": [
        "arn:aws:s3:::BUCKET_NAME/*",
        "arn:aws:s3:::BUCKET_NAME"
      ]
    },
    {
      "Effect": "Allow",
      "Action": ["ec2:DescribeInstances", "ec2:DescribeTags"],
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
EOF

# Create role for develop (bucket: ota-img-dev.lgmk-eng.com)
aws iam create-role \
  --role-name GitHubActions-WebRTC-Deploy-Develop \
  --assume-role-policy-document file://trust-policy.json

aws iam put-role-policy \
  --role-name GitHubActions-WebRTC-Deploy-Develop \
  --policy-name WebRTCDeploymentPolicy \
  --policy-document file://permissions-policy.json

# Create role for main2 (bucket: ota-img-main2.logicmarkcloud.com)
# Note: main2 role already exists: arn:aws:iam::058264321995:role/lgmk-github-oidc-main-role2
```

### 2️⃣ Configure GitHub Secrets

```bash
# In GitHub UI: Settings → Secrets and variables → Actions → New repository secret

# develop environment (required):
# Name: AWS_ROLE_TO_ASSUME_DEVELOP
# Value: arn:aws:iam::ACCOUNT_ID:role/GitHubActions-WebRTC-Deploy-Develop

# main2/production (required):
# Name: AWS_ROLE_TO_ASSUME_MAIN2
# Value: arn:aws:iam::058264321995:role/lgmk-github-oidc-main-role2
```

Using GitHub CLI:

```bash
gh secret set AWS_ROLE_TO_ASSUME_DEVELOP \
  --body "arn:aws:iam::ACCOUNT_ID:role/GitHubActions-WebRTC-Deploy-Develop"

gh secret set AWS_ROLE_TO_ASSUME_MAIN2 \
  --body "arn:aws:iam::058264321995:role/lgmk-github-oidc-main-role2"
```

### 3️⃣ Configure GitHub Environments

```bash
# Create environments in GitHub UI (Settings → Environments)
# Or using gh CLI:
gh api repos/:owner/:repo/environments/develop -X PUT
gh api repos/:owner/:repo/environments/main2 -X PUT
```

### 4️⃣ Prepare EC2 Instances (first deploy only)

Run the following on each instance before the first deploy:

```bash
# 1. AWS CLI (required by SSM commands and deploy script)
sudo snap install aws-cli --classic

# 2. zip / unzip (required by deploy script)
sudo apt-get install -y zip unzip

# 3. SSL certificate (certbot)
sudo snap install --classic certbot

# develop instance:
sudo certbot certonly --standalone -d flutter-webrtc-develop2.lgmk-eng.com

# main2 instance:
sudo certbot certonly --standalone -d flutter-webrtc.main2.logicmarkcloud.com
```

---

## 🚀 Usage

### Deploy to develop

```bash
git checkout develop
git add .
git commit -m "feat: new functionality"
git push origin develop
# Pipeline deploys automatically to develop environment
```

### Deploy to main2 (production)

```bash
# Merge develop → master via PR, then:
git checkout master
git merge develop
git push origin master
# Pipeline deploys automatically to main2 environment
```

### Create a versioned release (deploys to main2)

```bash
git checkout master
git tag -a v1.2.0 -m "Release v1.2.0"
git push origin v1.2.0
```

### Manual trigger

```bash
gh workflow run deploy.yml --ref develop    # → develop environment
gh workflow run deploy.yml --ref master     # → main2 environment
```

---

## ✅ Verification Checklist

### develop environment
- [ ] `AWS_ROLE_TO_ASSUME_DEVELOP` secret configured in GitHub
- [ ] `develop` environment created in GitHub
- [ ] EC2 `lgmk-flutter-webrtc-server-develop` running with SSM agent
- [ ] certbot cert at `/etc/letsencrypt/live/flutter-webrtc-develop2.lgmk-eng.com/`
- [ ] `configs/config-develop.ini` present in repository
- [ ] First deployment successful → service running at port 8086

### main2 environment
- [ ] `AWS_ROLE_TO_ASSUME_MAIN2` secret configured in GitHub
- [ ] `main2` environment created in GitHub
- [ ] EC2 `lgmk-flutter-webrtc-server-main2` running with SSM agent
- [ ] certbot cert at `/etc/letsencrypt/live/flutter-webrtc.main2.logicmarkcloud.com/`
- [ ] `configs/config-main2.ini` present in repository
- [ ] First deployment successful → service running at port 8086

---

## 🔍 Current Project Status

| Item | develop | main2 |
|------|---------|-------|
| **Branch** | `develop` | `master` |
| **EC2 Instance** | `lgmk-flutter-webrtc-server-develop` | `lgmk-flutter-webrtc-server-main2` |
| **S3 Bucket** | `ota-img-dev.lgmk-eng.com` | `ota-img-main2.logicmarkcloud.com` |
| **S3 Prefix** | `flutter-webrtc-server-develop/` | `flutter-webrtc-server-main2/` |
| **Domain** | `flutter-webrtc-develop2.lgmk-eng.com` | `flutter-webrtc.main2.logicmarkcloud.com` |
| **App directory** | `/opt/flutter-webrtc/develop/` | `/opt/flutter-webrtc/main2/` |
| **Service user** | `flutter-webrtc` | `flutter-webrtc` |
| **Status** | ✅ Active | ✅ Active |

---

## 🔍 Quick Troubleshooting

**Error: "Instance not found"**
```bash
# Verify instance tag in AWS
aws ec2 describe-instances \
  --filters "Name=tag:Name,Values=lgmk-flutter-webrtc-server-<env>" \
  --query 'Reservations[].Instances[].State.Name'
```

**Error: "SSM agent not online"**
```bash
ssh ubuntu@<instance-ip>
sudo systemctl restart amazon-ssm-agent
sudo systemctl status amazon-ssm-agent
```

**Error: "fullchain.pem not found"**
```bash
sudo certbot certonly --standalone -d <domain>
```

**Service not starting after deploy**
```bash
ssh ubuntu@<instance-ip>
sudo journalctl -u flutter-webrtc.service -n 50 --no-pager
```

---

## 📚 Complete Documentation

See `CICD-SETUP.md` for detailed documentation:
- Full pipeline architecture diagram
- IAM policy templates per environment
- Directory layout on EC2
- Rollback procedure
- Advanced troubleshooting
- Security best practices

---

**Last updated:** 2026-03-05
**Maintained by:** SRE Team
