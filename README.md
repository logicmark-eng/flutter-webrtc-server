# flutter-webrtc-server
 [![slack](https://img.shields.io/badge/join-us%20on%20slack-gray.svg?longCache=true&logo=slack&colorB=brightgreen)](https://join.slack.com/t/flutterwebrtc/shared_invite/zt-q83o7y1s-FExGLWEvtkPKM8ku_F8cEQ)
 
A simple WebRTC Signaling server for flutter-webrtc and html5.

## Features
- Support Windows/Linux/macOS
- Built-in web, signaling, [turn server](https://github.com/pion/turn/tree/master/examples/turn-server)
- Support [REST API For Access To TURN Services](https://tools.ietf.org/html/draft-uberti-behave-turn-rest-00)
- Use [flutter-webrtc-demo](https://github.com/cloudwebrtc/flutter-webrtc-demo) for all platforms.

## Usage

### Run from source

- Clone the repository.  

```bash
git clone https://github.com/flutter-webrtc/flutter-webrtc-server.git
cd flutter-webrtc-server
```

- Use `mkcert` to create a self-signed certificate.

```bash
brew update
brew install mkcert
mkcert -key-file configs/certs/key.pem -cert-file configs/certs/cert.pem  localhost 127.0.0.1 ::1 0.0.0.0
```

- Run

```bash
brew install golang
go run cmd/server/main.go
```

- Open https://0.0.0.0:8086 to use flutter web demo.
- If you need to test mobile app, please check the [webrtc-flutter-demo](https://github.com/cloudwebrtc/flutter-webrtc-demo). 

## Deployment

### CI/CD Pipeline

This project includes automated deployment to AWS EC2 using GitHub Actions. See the CI/CD documentation:

- **Quick Start**: [.github/CICD-QUICK-START.md](.github/CICD-QUICK-START.md) - Setup in 5 minutes
- **Complete Guide**: [CICD-SETUP.md](CICD-SETUP.md) - Detailed configuration and troubleshooting
- **Deployment Flow**: [.github/DEPLOYMENT-FLOW.md](.github/DEPLOYMENT-FLOW.md) - Visual diagrams and process details

### Deployment Scripts

The `scripts/` directory contains deployment automation:

- **[scripts/deploy-flutter-webrtc-server.sh](scripts/deploy-flutter-webrtc-server.sh)** - EC2 deployment script
  - Automated download from S3
  - Backup and rollback support
  - SSL certificate management
  - Systemd service control

See [scripts/README.md](scripts/README.md) for detailed script documentation.

### Current Deployment

- **Environment**: develop
- **Version**: v0.0.7
- **Infrastructure**: AWS EC2 (managed by Terraform)
- **Deployment Method**: GitHub Actions → S3 → SSM → EC2

## Note
If you need to use it in a production environment, you need more testing.

## Screenshots
# iOS/Android
<img width="180" height="320" src="screenshots/ios-01.jpeg"/> <img width="180" height="320" src="screenshots/ios-02.jpeg"/> <img width="180" height="320" src="screenshots/android-01.png"/> <img width="180" height="320" src="screenshots/android-02.png"/>

# PC/HTML5
<img width="360" height="293" src="screenshots/chrome-01.png"/> <img width="360" height="293" src="screenshots/chrome-02.png"/>
