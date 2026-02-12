# CI/CD Quick Start Guide

Quick guide to set up the deployment pipeline in 5 minutes.

**Current Configuration:**
- **Active branch:** `master` only
- **Environment:** `develop`
- **Current version:** `v0.0.7`

## ⚡ Setup in 3 Steps

### 1️⃣ Configure OIDC in AWS

```bash
# Create IAM OIDC provider for GitHub (once per account)
aws iam create-open-id-connect-provider \
  --url https://token.actions.githubusercontent.com \
  --client-id-list sts.amazonaws.com \
  --thumbprint-list 6938fd4d98bab03faadb97b34396831e3780aea1

# Create IAM Role
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

# Replace ACCOUNT_ID and YOUR_ORG before executing:
# Create role for develop environment
aws iam create-role \
  --role-name GitHubActions-WebRTC-Deploy-Develop \
  --assume-role-policy-document file://trust-policy.json

# For future: create roles for other environments
# aws iam create-role --role-name GitHubActions-WebRTC-Deploy-QA --assume-role-policy-document file://trust-policy.json
# aws iam create-role --role-name GitHubActions-WebRTC-Deploy-Staging --assume-role-policy-document file://trust-policy.json
# aws iam create-role --role-name GitHubActions-WebRTC-Deploy-Main2 --assume-role-policy-document file://trust-policy.json

# Attach required policies
cat > permissions-policy.json <<'EOF'
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
EOF

aws iam put-role-policy \
  --role-name GitHubActions-WebRTC-Deploy-Develop \
  --policy-name WebRTCDeploymentPolicy \
  --policy-document file://permissions-policy.json

# Repeat for other environments when needed:
# aws iam put-role-policy --role-name GitHubActions-WebRTC-Deploy-QA --policy-name WebRTCDeploymentPolicy --policy-document file://permissions-policy.json
# aws iam put-role-policy --role-name GitHubActions-WebRTC-Deploy-Staging --policy-name WebRTCDeploymentPolicy --policy-document file://permissions-policy.json
# aws iam put-role-policy --role-name GitHubActions-WebRTC-Deploy-Main2 --policy-name WebRTCDeploymentPolicy --policy-document file://permissions-policy.json
```

### 2️⃣ Configure GitHub Secrets

```bash
# In GitHub UI:
# Settings → Secrets and variables → Actions → New repository secret

# For develop environment (required now):
# Name: AWS_ROLE_TO_ASSUME_DEVELOP
# Value: arn:aws:iam::ACCOUNT_ID:role/GitHubActions-WebRTC-Deploy-Develop

# For future environments (configure when needed):
# AWS_ROLE_TO_ASSUME_QA
# AWS_ROLE_TO_ASSUME_STAGING
# AWS_ROLE_TO_ASSUME_MAIN2
```

Or using GitHub CLI:

```bash
# Configure develop environment role (required)
gh secret set AWS_ROLE_TO_ASSUME_DEVELOP \
  --body "arn:aws:iam::ACCOUNT_ID:role/GitHubActions-WebRTC-Deploy-Develop"

# Future environments (optional for now)
# gh secret set AWS_ROLE_TO_ASSUME_QA --body "arn:aws:iam::ACCOUNT_ID:role/..."
# gh secret set AWS_ROLE_TO_ASSUME_STAGING --body "arn:aws:iam::ACCOUNT_ID:role/..."
# gh secret set AWS_ROLE_TO_ASSUME_MAIN2 --body "arn:aws:iam::ACCOUNT_ID:role/..."
```

### 3️⃣ Configure GitHub Environments

```bash
# Create environment (GitHub UI or gh CLI)
gh api repos/:owner/:repo/environments/develop -X PUT
```

---

## 🚀 Immediate Usage

### Automatic Deployment

```bash
# Deploy to develop
git checkout master
git add .
git commit -m "feat: new functionality"
git push origin master

# Pipeline executes automatically
```

### Manual Deployment

```bash
# From GitHub UI:
# Actions → Deploy Flutter WebRTC Server → Run workflow
# - Branch: master
# - Version: (leave empty or specify v0.0.8)

# Or using CLI:
gh workflow run deploy.yml --ref master
gh workflow run deploy.yml --ref master -f version=v0.0.8
```

### Create Release

```bash
# Next version: v0.0.8
git tag -a v0.0.8 -m "Release v0.0.8: Change description"
git push origin v0.0.8

# Automatically deploys with that tag
```

---

## ✅ Verification

### Test the Pipeline

```bash
# 1. Make a small change
echo "# Test" >> README.md
git add README.md
git commit -m "test: verify pipeline"
git push origin master

# 2. Go to GitHub Actions
# https://github.com/YOUR_ORG/flutter-webrtc-server/actions

# 3. View the workflow executing
# Should complete in ~5-10 minutes

# 4. Verify deployment
ssh ubuntu@<instance-ip>
sudo systemctl status flutter-webrtc.service
sudo journalctl -u flutter-webrtc.service -n 20
```

### Verification Checklist

- [ ] IAM Role created in AWS (per environment)
- [ ] `AWS_ROLE_TO_ASSUME_DEVELOP` configured in GitHub Secrets
- [ ] Environment `develop` created in GitHub
- [ ] Script `deploy-flutter-webrtc-server.sh` on EC2 (`/home/ubuntu/`)
- [ ] SSM agent running on EC2
- [ ] SSL certificates on EC2 (`/etc/letsencrypt/live/`)
- [ ] First deployment successful

---

## 🔍 Current Project Status

| Item | Configuration |
|------|---------------|
| **Active branch** | `master` |
| **Environment** | `develop` |
| **EC2 Instance** | `lgmk-flutter-webrtc-server-develop` |
| **Current version** | `v0.0.7` |
| **Status** | ✅ Active and functional |
| **Next version** | `v0.0.8` |

### Simplified Configuration

Currently the project is configured simply:
- Only `master` branch exists
- Deploys only to `develop` environment
- Environments `qa`, `staging`, and `main2` are disabled
- Base version set at `v0.0.7`

---

## 🆘 Quick Troubleshooting

**Error: "Instance not found"**
```bash
# Verify enable_webrtc_server=true in environment
cat /path/to/lgmk-pers-base-infra/terraform/environments/{env}.tfvars | grep enable_webrtc_server
```

**Error: "SSM agent not online"**
```bash
ssh ubuntu@<instance-ip>
sudo systemctl restart amazon-ssm-agent
sudo systemctl status amazon-ssm-agent
```

**Error: "Deployment script not found"**
```bash
# Copy script from repository to instance
scp -i ~/.ssh/key.pem scripts/deploy-flutter-webrtc-server.sh ubuntu@<ip>:/home/ubuntu/
ssh ubuntu@<ip> "chmod +x /home/ubuntu/deploy-flutter-webrtc-server.sh"
```

---

## 📚 Complete Documentation

See `CICD-SETUP.md` for detailed documentation with:
- Complete pipeline architecture
- Advanced troubleshooting
- Monitoring and observability
- Security best practices

---

## 🎯 Next Steps

1. ✅ Configure OIDC and secrets (this guide)
2. 🔄 Test deployment to develop
3. 🔄 Test deployment with new tag (v0.0.8)
4. 📋 Future: Enable infrastructure for QA/Staging/Production

---

**Need help?** View logs at: GitHub Actions → Deploy Flutter WebRTC Server
