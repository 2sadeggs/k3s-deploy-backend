K3s Deploy Backend
Backend for K3s deployment tool, built with Golang and Gin.
Setup

Install Go: Version 1.21+.
Clone Repository:git clone <repo-url>
cd k3s-deploy-backend


Install Dependencies:go mod tidy


Create .env:PORT=8080


Run Server:go run cmd/main.go

Or use hot-reload with air:air



API Endpoints

POST /api/ssh/test: Test single SSH connection.
POST /api/ssh/test-batch: Test multiple SSH connections.
GET /api/webssh/ws: WebSocket for interactive SSH terminal.
POST /api/k3s/deploy: Start K3s deployment.
GET /api/k3s/progress/:taskId: Poll deployment progress.

Notes

Runs on localhost:8080 by default.
CORS configured for http://localhost:3000.
WebSSH requires node credentials (temporary map in webssh.go).
Production: Replace InsecureIgnoreHostKey, add JWT, use Redis for progress.
