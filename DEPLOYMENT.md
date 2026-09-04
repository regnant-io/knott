# 🚀 KW Sagittarii - Deployment Guide

## Pre-Deployment Checklist

### System Requirements
- [ ] **OS:** Windows 10/11, Ubuntu 20.04+, or macOS 11+
- [ ] **Memory:** 4GB RAM minimum (8GB recommended)
- [ ] **Storage:** 2GB available space
- [ ] **Ports:** 8001-8005 available and not blocked by firewall

### Software Prerequisites

**Required:**
- [ ] Go 1.22+ installed and in PATH
- [ ] Python 3.11+ installed and in PATH

**Optional (for AI):**
- [ ] Anthropic API key (for Claude)
- [ ] Ollama installed (for local AI)

**Development Only:**
- [ ] Node.js 18+ (only if rebuilding frontend)

---

## Quick Deploy (Production)

### Windows

```cmd
# 1. Extract release package
unzip kw-sagittarii-v1.0.zip
cd kw-sagittarii

# 2. Configure environment
copy .env.example .env
notepad .env

# 3. Start platform
start.bat

# 4. Access at http://localhost:8002
```

### Linux/macOS

```bash
# 1. Extract and navigate
tar -xzf kw-sagittarii-v1.0.tar.gz
cd kw-sagittarii

# 2. Configure environment
cp .env.example .env
nano .env

# 3. Make scripts executable
chmod +x start.sh stop.sh

# 4. Start platform
./start.sh

# 5. Access at http://localhost:8002
```

---

## Configuration

### Environment File (.env)

**Minimum Configuration:**

```bash
# AI Provider (choose one)
AI_PROVIDER=auto

# If using Anthropic Claude
ANTHROPIC_API_KEY=sk-ant-your-key-here

# If using Ollama
OLLAMA_BASE_URL=http://localhost:11434
OLLAMA_MODEL=llama3.1:latest

# Ports (defaults shown)
REGISTRY_PORT=8001
ENGINE_PORT=8002
AI_PORT=8003
TASK_PORT=8004
AGENT_PORT=8005
```

### AI Provider Selection

| Provider | Setup | Best For |
|----------|-------|----------|
| **Anthropic** | Get API key from console.anthropic.com | Production, highest quality |
| **Ollama** | Install from ollama.ai, pull model | Privacy, offline, cost control |
| **Simulation** | No setup needed | Development, testing |

### Ollama Setup (Optional)

```bash
# 1. Install Ollama
# Windows: Download from https://ollama.ai/download/windows
# Mac: brew install ollama
# Linux: curl https://ollama.ai/install.sh | sh

# 2. Start Ollama service
ollama serve  # Runs on port 11434

# 3. Download a model
ollama pull llama3.1:latest

# 4. Verify
curl http://localhost:11434/api/tags
```

**Recommended Models:**
- `llama3.1:latest` - Best balance (4.7GB)
- `llama3.1:70b` - Highest quality (40GB, needs GPU)
- `llama3.2:latest` - Fastest (2GB)

---

## Deployment Scenarios

### Scenario 1: Cloud + Anthropic (Recommended)

**Best for:** Production deployments, teams wanting best AI quality

```bash
# .env configuration
ANTHROPIC_API_KEY=sk-ant-your-actual-key
AI_PROVIDER=anthropic
```

**Pros:**
- Highest quality AI decisions
- No local GPU needed
- Always available
- Latest models

**Cons:**
- Requires internet
- API costs (~$3-15 per 1000 decisions)
- Data sent to Anthropic

---

### Scenario 2: On-Premise + Ollama

**Best for:** Privacy-sensitive, regulated industries, offline environments

```bash
# .env configuration
OLLAMA_BASE_URL=http://localhost:11434
OLLAMA_MODEL=llama3.1:latest
AI_PROVIDER=ollama
```

**Pros:**
- Complete data privacy
- No external API calls
- Offline capable
- No per-use costs

**Cons:**
- Requires local GPU (recommended)
- Need to manage Ollama
- Slightly lower quality than Claude

**Hardware Recommendations:**
- **CPU Only:** llama3.2 (slower but works)
- **8GB GPU:** llama3.1:latest
- **24GB+ GPU:** llama3.1:70b

---

### Scenario 3: Hybrid (Auto Mode)

**Best for:** Maximum flexibility

```bash
# .env configuration
ANTHROPIC_API_KEY=sk-ant-your-key
OLLAMA_BASE_URL=http://localhost:11434
OLLAMA_MODEL=llama3.1:latest
AI_PROVIDER=auto
```

**Behavior:**
1. Tries Anthropic first
2. Falls back to Ollama if Anthropic unavailable
3. Falls back to simulation if both unavailable

---

## Docker Deployment

### Quick Start

```bash
# 1. Configure .env
cp .env.example .env
nano .env

# 2. Start with Docker Compose
docker-compose up -d

# 3. View logs
docker-compose logs -f

# 4. Stop
docker-compose down
```

### Production Docker Compose

```yaml
version: "3.9"

services:
  kw-platform:
    build: .
    ports:
      - "8001-8005:8001-8005"
    environment:
      - ANTHROPIC_API_KEY=${ANTHROPIC_API_KEY}
      - AI_PROVIDER=${AI_PROVIDER}
    volumes:
      - ./data:/app/data
      - ./logs:/app/logs
    restart: unless-stopped
```

---

## Nginx Reverse Proxy

For production deployments with SSL:

```nginx
server {
    listen 443 ssl http2;
    server_name workflows.yourcompany.com;

    ssl_certificate /path/to/cert.pem;
    ssl_certificate_key /path/to/key.pem;

    location / {
        proxy_pass http://localhost:8002;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection 'upgrade';
        proxy_set_header Host $host;
        proxy_cache_bypass $http_upgrade;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }

    # API endpoints
    location /api/ {
        proxy_pass http://localhost:8002;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
    }
}
```

---

## Cloud Deployment

### AWS EC2

```bash
# 1. Launch EC2 instance (t3.medium or larger)
# 2. Install prerequisites
sudo apt update
sudo apt install -y golang-go python3 python3-pip

# 3. Clone/upload application
scp kw-sagittarii.tar.gz ec2-user@your-instance:~
ssh ec2-user@your-instance
tar -xzf kw-sagittarii.tar.gz
cd kw-sagittarii

# 4. Configure
cp .env.example .env
nano .env

# 5. Start
./start.sh

# 6. Configure security group to allow ports 8001-8005
```

### Systemd Service (Linux)

```bash
# /etc/systemd/system/kw-sagittarii.service

[Unit]
Description=KW Sagittarii Workflow Platform
After=network.target

[Service]
Type=forking
User=kwuser
WorkingDirectory=/opt/kw-sagittarii
ExecStart=/opt/kw-sagittarii/start.sh
ExecStop=/opt/kw-sagittarii/stop.sh
Restart=on-failure
RestartSec=10

[Install]
WantedBy=multi-user.target
```

```bash
# Enable and start
sudo systemctl daemon-reload
sudo systemctl enable kw-sagittarii
sudo systemctl start kw-sagittarii
sudo systemctl status kw-sagittarii
```

---

## Monitoring & Health Checks

### Health Endpoints

```bash
# Check all services
curl http://localhost:8001/api/v1/health  # Workflow Registry
curl http://localhost:8002/api/v1/health  # Execution Engine
curl http://localhost:8003/internal/v1/health  # AI Decision Engine
curl http://localhost:8004/api/v1/health  # Human Task Service
curl http://localhost:8005/api/v1/health  # Agent Integration
```

### Monitoring Script

```bash
#!/bin/bash
# health-check.sh

SERVICES=(
    "http://localhost:8001/api/v1/health"
    "http://localhost:8002/api/v1/health"
    "http://localhost:8003/internal/v1/health"
    "http://localhost:8004/api/v1/health"
    "http://localhost:8005/api/v1/health"
)

for service in "${SERVICES[@]}"; do
    if curl -sf "$service" > /dev/null; then
        echo "✓ $service"
    else
        echo "✗ $service - DOWN"
        # Send alert here
    fi
done
```

---

## Backup & Recovery

### Backup Data

```bash
# Full backup
tar -czf backup-$(date +%Y%m%d).tar.gz data/ logs/ .env

# Database only
cp -r data/ data-backup-$(date +%Y%m%d)/
```

### Restore

```bash
# Stop services
./stop.sh  # or stop.bat on Windows

# Restore data
rm -rf data/
tar -xzf backup-20240315.tar.gz data/

# Start services
./start.sh
```

### Automated Backups (Linux)

```bash
# Add to crontab: crontab -e
0 2 * * * cd /opt/kw-sagittarii && tar -czf /backup/kw-$(date +\%Y\%m\%d).tar.gz data/
```

---

## Troubleshooting

### Services Won't Start

```bash
# Check if ports are in use
netstat -an | grep "8001\|8002\|8003\|8004\|8005"

# Kill existing processes
./stop.sh  # or stop.bat

# Check logs
tail -f logs/*.log
```

### AI Engine Offline

```bash
# Check Python
python --version
python -c "import fastapi, anthropic"

# Reinstall dependencies
pip install -r services/ai-decision-engine/requirements.txt

# Check environment
echo $ANTHROPIC_API_KEY  # Should not be empty if using Claude

# Test Ollama
curl http://localhost:11434/api/tags
```

### Database Corruption

```bash
# Stop services
./stop.sh

# Rebuild databases (DESTRUCTIVE)
rm -rf data/
./start.sh  # Creates fresh databases
```

---

## Security Hardening

### Production Checklist

- [ ] Change default ports in production
- [ ] Enable firewall (only allow necessary ports)
- [ ] Use reverse proxy with SSL/TLS
- [ ] Implement API authentication
- [ ] Restrict database file permissions
- [ ] Enable audit logging
- [ ] Use secrets management (not .env file)
- [ ] Regular backups
- [ ] Update dependencies regularly
- [ ] Monitor logs for suspicious activity

### Firewall Rules (Linux)

```bash
# Allow only frontend port externally
sudo ufw allow 443/tcp  # HTTPS only
sudo ufw allow 22/tcp   # SSH
sudo ufw enable

# Internal services should not be exposed
# Use reverse proxy to route to :8002
```

---

## Performance Tuning

### Database Optimization

```bash
# For high-volume deployments, migrate to PostgreSQL
# 1. Export data from SQLite
# 2. Configure PostgreSQL connection in services
# 3. Import data
# 4. Update DB_PATH to postgres:// URL
```

### Ollama GPU Acceleration

```bash
# Check GPU availability
nvidia-smi

# Configure Ollama to use GPU (automatic on Linux with CUDA)
ollama run llama3.1:latest

# Monitor GPU usage
watch -n 1 nvidia-smi
```

### Concurrent Execution Limits

Edit `services/execution-engine/main.go`:

```go
// Adjust worker pool size
const MAX_CONCURRENT_RUNS = 50
```

---

## Upgrade Guide

### Minor Version Upgrade

```bash
# 1. Backup current installation
tar -czf kw-backup-$(date +%Y%m%d).tar.gz .

# 2. Stop services
./stop.sh

# 3. Replace binaries
cp new-release/bin/* bin/

# 4. Restart
./start.sh
```

### Major Version Upgrade

```bash
# 1. Full backup
tar -czf full-backup-$(date +%Y%m%d).tar.gz .

# 2. Stop services
./stop.sh

# 3. Run migration scripts (if provided)
./migrate-v1-to-v2.sh

# 4. Test in staging first
# 5. Deploy to production
```

---

## Support

### Getting Help

1. Check logs in `./logs/` directory
2. Verify all health endpoints return `200 OK`
3. Review this deployment guide
4. Check README.md for API documentation

### Common Issues & Solutions

| Issue | Solution |
|-------|----------|
| Port already in use | Change port in .env or kill process |
| AI engine offline | Check Python install, verify API key |
| Workflow stuck | Check AI provider status, review logs |
| Build failure | Run `go mod tidy`, rebuild |
| Database locked | Stop all services, restart |

---

**Deployment Status Checklist:**

- [ ] Environment configured (.env)
- [ ] All services start successfully
- [ ] Health checks pass
- [ ] AI provider working (test with workflow)
- [ ] Frontend accessible
- [ ] Can create workflow
- [ ] Can execute workflow
- [ ] Can complete human task
- [ ] Logs are being written
- [ ] Backups configured

**You're ready for production! 🚀**
