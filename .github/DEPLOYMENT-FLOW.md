# Deployment Flow - Flutter WebRTC Server

## 🔄 Complete Deployment Flow

```
┌─────────────────────────────────────────────────────────────────────┐
│                        DEVELOPER WORKFLOW                           │
└─────────────────────────────────────────────────────────────────────┘

    Developer                    Git Repository                 GitHub Actions
       │                              │                              │
       │  git push master             │                              │
       ├──────────────────────────────>                              │
       │                              │                              │
       │                              │  Trigger: push to master     │
       │                              ├─────────────────────────────>│
       │                              │                              │
       │                              │                         ┌────┴────┐
       │                              │                         │  BUILD  │
       │                              │                         │  JOB    │
       │                              │                         └────┬────┘
       │                              │                              │
       │                              │              1. Determine environment
       │                              │                 (develop)
       │                              │                              │
       │                              │              2. Create version tag
       │                              │                 (v0.0.0-master-abc1234)
       │                              │                              │
       │                              │              3. Zip source code
       │                              │                 (flutter-webrtc-server-master*.zip)
       │                              │                              │
       │                              │              4. Upload artifact
       │                              │                              │
       │                              │                         ┌────┴─────┐
       │                              │                         │  DEPLOY  │
       │                              │                         │   JOB    │
       │                              │                         └────┬─────┘
       │                              │                              │
┌──────┴──────┐                       │                              │
│   AWS IAM   │<──────────────────────┼──────────────────────────────┤
│ OIDC Auth   │  5. Assume Role       │              AWS credentials │
└──────┬──────┘                       │                 (temporary)  │
       │                              │                              │
┌──────▼──────┐                       │                              │
│     S3      │<──────────────────────┼──────────────────────────────┤
│   Bucket    │  6. Upload ZIP        │                              │
│             │  (ota-img-dev.lgmk-   │                              │
│             │   eng.com)            │                              │
└─────────────┘                       │                              │
                                      │                              │
┌─────────────┐                       │                              │
│     EC2     │                       │                              │
│  describe-  │<──────────────────────┼──────────────────────────────┤
│  instances  │  7. Find instance ID  │                              │
│             │     by tag:Name=      │                              │
│             │     lgmk-flutter-     │                              │
│             │     webrtc-server-    │                              │
│             │     develop           │                              │
└─────────────┘                       │                              │
                                      │                              │
┌─────────────┐                       │                              │
│ Systems     │<──────────────────────┼──────────────────────────────┤
│ Manager     │  8. Send SSM Command  │                              │
│  (SSM)      │                       │                              │
└──────┬──────┘                       │                              │
       │                              │                              │
       │  Execute remote script       │                              │
       │                              │                              │
┌──────▼──────────────────────────────────────────────────────────────┐
│              EC2 Instance (Target Environment)                      │
│  lgmk-flutter-webrtc-server-develop                                │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  9. SSM Agent receives command                                     │
│                                                                     │
│  10. Execute: /home/ubuntu/deploy-flutter-webrtc-server.sh        │
│      ├─ Stop service (systemctl stop flutter-webrtc.service)      │
│      ├─ Download ZIP from S3                                      │
│      ├─ Backup current version                                    │
│      │  (flutter-webrtc-server-master.backup_TIMESTAMP)           │
│      ├─ Extract new version                                       │
│      ├─ Build Go binary (go build cmd/server/main.go)            │
│      ├─ Copy TLS certificates                                     │
│      │  (/etc/letsencrypt/live/*/fullchain.pem → configs/certs/) │
│      ├─ Reload systemd (daemon-reload)                            │
│      └─ Restart service (systemctl restart flutter-webrtc.service)│
│                                                                     │
│  11. Service running                                               │
│      ├─ HTTPS WebSocket: :8086                                    │
│      ├─ TURN UDP: :19302                                          │
│      └─ TURN TCP: :19303                                          │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
       │                              │                              │
       │                              │                              │
       │                              │   12. Verify service health  │
       │                              │       (systemctl status)     │
       │                              │<─────────────────────────────┤
       │                              │                              │
       │                              │   13. Create deployment      │
       │                              │       summary (✅ Success)   │
       │                              │                              │
       │  ✉️ Notification             │<─────────────────────────────┤
       │  (GitHub Actions UI)         │                              │
       <──────────────────────────────┤                              │
       │                              │                              │
```

---

## 🎯 Mapping: Branches → Environments

### Current Configuration (Simplified)

```
┌──────────────────────────────────────────────────────────────────────┐
│  Git Branch    →    Target Environment    →    EC2 Instance Name    │
├──────────────────────────────────────────────────────────────────────┤
│  master        →    develop               →    lgmk-flutter-webrtc- │
│                                                 server-develop       │
│                                                                      │
│  Current Version: v0.0.7                                             │
│  Status: ✅ Active                                                   │
└──────────────────────────────────────────────────────────────────────┘
```

**⚠️ Note:** Only `master` branch currently exists. Environments `qa`, `staging`, and `main2` are disabled in the pipeline.

### Future Configuration (Multi-Environment)

When multi-environment expansion is required:

```
┌──────────────────────────────────────────────────────────────────────┐
│  Git Branch    →    Target Environment    →    EC2 Instance Name    │
├──────────────────────────────────────────────────────────────────────┤
│  develop       →    develop               →    lgmk-flutter-webrtc- │
│                                                 server-develop       │
│                                                                      │
│  qa            →    qa                    →    lgmk-flutter-webrtc- │
│                                                 server-qa            │
│                                                                      │
│  staging       →    staging               →    lgmk-flutter-webrtc- │
│                                                 server-staging       │
│                                                                      │
│  main2         →    main2 (production)    →    lgmk-flutter-webrtc- │
│                                                 server-main2         │
│                     (Requires approval)                              │
└──────────────────────────────────────────────────────────────────────┘
```

---

## 🔄 Promotion Flow (Current Simplified)

### Current Development and Deployment

```
┌─────────────┐
│   feature/  │  Developer works on feature branch
│   new-func  │
└──────┬──────┘
       │
       │  PR Review + Approval
       │
       ▼
┌─────────────┐
│   master    │────────────> Auto-deploy to DEVELOP environment
└──────┬──────┘               ├─ Build ZIP package
       │                      ├─ Upload to S3
       │                      ├─ Deploy via SSM
       │                      └─ Verify service
       │
       │  Create version tag (optional)
       │
       ▼
    Tagged release (v0.0.8, v0.0.9, etc.)
```

### Future Flow (Multi-Environment)

When multiple environments are enabled:

```
┌─────────────┐
│   feature/  │  Developer works on feature branch
│   new-func  │
└──────┬──────┘
       │
       │  PR to develop
       ▼
┌─────────────┐
│   develop   │────────────> Auto-deploy to DEVELOP
└──────┬──────┘
       │
       │  PR to qa
       ▼
┌─────────────┐
│     qa      │────────────> Auto-deploy to QA
└──────┬──────┘
       │
       │  PR to staging
       ▼
┌─────────────┐
│   staging   │────────────> Auto-deploy to STAGING
└──────┬──────┘
       │
       │  PR to main2
       ▼
┌─────────────┐
│    main2    │────────────> Manual approval → PRODUCTION
└──────┬──────┘
       │
       ▼
    Tagged release (v1.0.0)
```

---

## 🚦 Deployment Gates

### Develop (Auto)
- ✅ No approval required
- ✅ Auto-deploy on push
- ✅ Fast iteration

### Future: QA (Auto)
- ⚠️ Optional: Require PR approval
- ✅ Auto-deploy after merge
- ✅ QA team testing

### Future: Staging (Protected)
- ⚠️ Require PR approval (1+ reviewers)
- ✅ Auto-deploy after approval
- ✅ Pre-production validation

### Future: Production (Highly Protected)
- 🔒 Require PR approval (2+ reviewers)
- 🔒 Manual workflow approval in GitHub
- 🔒 Branch protection rules
- 🔒 Status checks must pass
- 🔒 Deployment window (optional)

---

## 🔐 Security Checkpoints

```
Developer → GitHub → AWS → EC2
    │         │       │      │
    │         │       │      └─ 5. Service isolation
    │         │       │         (systemd, non-root)
    │         │       │
    │         │       └─ 4. IAM instance role
    │         │          (SSM, CloudWatch)
    │         │
    │         └─ 3. Temporary AWS credentials
    │            (OIDC, 1-hour session)
    │
    └─ 2. GitHub environment protection
       (Approvals, branch rules)

    1. Code review (PR process)
```

---

## 📊 Deployment Metrics

### Success Criteria

✅ **Build time:** < 2 minutes
✅ **Upload to S3:** < 1 minute
✅ **Deployment execution:** < 5 minutes
✅ **Service restart:** < 30 seconds
✅ **Health check:** Pass within 1 minute

### Total Deployment Time

**Target:** ~8-10 minutes from push to running service

```
Push → Build (2m) → Deploy (5m) → Verify (1m) → Done
```

---

## 🎛️ Deployment Controls

### Rollback Strategy

**Option 1: Redeploy previous version**
```bash
# Manual trigger with specific version
gh workflow run deploy.yml \
  -f version=v0.0.7
```

**Option 2: Use backup on server**
```bash
# SSH to EC2
ssh ubuntu@<instance-ip>

# List backups
ls -lt /home/ubuntu/ | grep backup

# Restore backup
sudo systemctl stop flutter-webrtc.service
sudo rm -rf /home/ubuntu/flutter-webrtc-server-master
sudo mv /home/ubuntu/flutter-webrtc-server-master.backup_TIMESTAMP \
       /home/ubuntu/flutter-webrtc-server-master
sudo systemctl restart flutter-webrtc.service
```

**Option 3: Git revert + redeploy**
```bash
# Revert commit
git revert <bad-commit-sha>
git push origin master

# Auto-deploys reverted version
```

---

## 📈 Monitoring Points

### GitHub Actions
- Workflow execution time
- Success/failure rate
- Deployment frequency

### AWS CloudWatch
- EC2 instance metrics (CPU, memory, disk)
- Service logs (`/aws/ec2/webrtc-server-develop`)
- SSM command history

### Application Metrics
- Active WebSocket connections
- TURN server sessions
- Error rates

---

**Status:** Implemented ✅
**Last updated:** 2026-01-28
