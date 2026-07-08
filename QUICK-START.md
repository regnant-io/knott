# KW SAGITTARII - QUICK START GUIDE

**Get your AI workflow orchestration platform running in 5 minutes.**

---

## STEP 1: Configure Your AI Provider

Edit the `.env` file and choose ONE option:

### Option A: Anthropic Claude (Recommended for Production)
```env
AI_PROVIDER=anthropic
ANTHROPIC_API_KEY=sk-ant-api03-YOUR-KEY-HERE
```

Get your API key: https://console.anthropic.com/

### Option B: Ollama (For On-Premise/Offline)
```env
AI_PROVIDER=ollama
OLLAMA_BASE_URL=http://localhost:11434
OLLAMA_MODEL=llama3.1:latest
```

Install Ollama: https://ollama.com/download  
Then run: `ollama pull llama3.1`

### Option C: Simulation (For Testing)
```env
AI_PROVIDER=auto
ANTHROPIC_API_KEY=
```
Leave API key empty - system will use rule-based simulation.

---

## STEP 2: Start the Platform

### Windows
```cmd
start.bat
```

### Linux / Mac
```bash
chmod +x start.sh
./start.sh
```

**Wait 5 seconds** for all services to start.

---

## STEP 3: Open the Dashboard

**URL**: http://localhost:8002

You should see:
- Dashboard with statistics
- Sidebar with navigation
- Light/Dark theme toggle (bottom of sidebar)

---

## STEP 4: Verify All Services Are Online

1. Click **"Settings"** in the sidebar
2. Check **"Service Endpoints"** section
3. All 5 services should show **"● Online"**

If any service shows offline:
- Wait 10 more seconds (services take time to start)
- Check logs in `logs/` directory
- See TROUBLESHOOTING section below

---

## STEP 5: Create Your First Workflow

1. Click **"Workflows"** in sidebar
2. Click **"+ Create Workflow"** button
3. Enter:
   - **Name**: "Demo Workflow"
   - **Description**: "My first workflow"
4. Click **"Create"**
5. Click **"Open Designer"** on your new workflow

---

## STEP 6: Build a Simple Workflow

**Drag nodes from the left panel:**

1. **Start** node (green) - Already there
2. **AI Decision** node (purple) - Drag next to Start
3. **End** node (red) - Drag next to AI Decision

**Connect them:**
- Drag from Start's right handle → AI Decision's left handle
- Drag from AI Decision's right handle → End's left handle

**Configure AI Decision node:**
- Click on the purple AI Decision node
- Set **Task Type**: "fraud_risk_assessment"
- Click **"Save Changes"** at top

---

## STEP 7: Run Your Workflow

1. Click **"Save Workflow"** button (top right)
2. Click **"Workflows"** in sidebar
3. Find "Demo Workflow"
4. Click **"▶ Run"** button
5. Enter input data:
```json
{
  "transaction": {
    "amount": 150.00,
    "merchant": "Amazon",
    "country": "US"
  }
}
```
6. Click **"Start Execution"**

---

## STEP 8: Monitor Your Workflow

1. Click **"Runs"** in sidebar
2. You'll see your workflow execution with status
3. Click on the run to see:
   - Event timeline
   - Node execution details
   - Current status

---

## STEP 9: Check AI Decisions

1. Click **"AI Decisions"** in sidebar
2. You'll see the decision made by the AI:
   - Decision: APPROVE / REJECT / ESCALATE
   - Confidence score
   - Reasoning
   - Model used

---

## CONGRATULATIONS! 🎉

You've successfully:
- ✅ Started KW Sagittarii
- ✅ Created a workflow
- ✅ Executed a workflow
- ✅ Reviewed AI decision logs

---

## NEXT STEPS

### Add Human Review
Drag a **Human Task** node into your workflow to require human approval.

### Add Conditions
Use **Condition** nodes to route based on AI decisions:
- If APPROVE → Auto-process
- If REJECT → Send notification
- If ESCALATE → Human review

### Connect External Agents
1. Go to **Agents** page
2. Click **"Register Agent"**
3. Enter your agent's API endpoint

### Install Connectors
1. Go to **Connectors** page
2. Toggle ON the connectors you need (Salesforce, Slack, etc.)

---

## TROUBLESHOOTING

### "AI Decision Engine is Offline"

**Check AI Provider Configuration:**
```bash
# Windows
type .env | findstr AI_PROVIDER

# Linux/Mac
cat .env | grep AI_PROVIDER
```

**Test AI Engine Manually:**
```bash
curl http://localhost:8003/internal/v1/health
```

If you see "anthropic": false, "ollama": false → Configure .env

### "Workflow Won't Start"

**Common causes:**
1. No Start node in workflow
2. Nodes not connected
3. AI Decision node not configured

**Solution:**
- Open Designer
- Ensure Start → nodes → End are connected
- Click each node and fill required fields
- Click "Save Workflow"

### "Can't Access http://localhost:8002"

**Check if Execution Engine is running:**
```bash
# Windows
netstat -ano | findstr :8002

# Linux/Mac
lsof -i :8002
```

If no output:
```bash
# Windows
type logs\execution-engine.log

# Linux/Mac  
cat logs/execution-engine.log
```

Look for errors and resolve.

### "Services Won't Start"

**Check for port conflicts:**
```bash
# Windows
netstat -ano | findstr :8001
netstat -ano | findstr :8002
netstat -ano | findstr :8003
netstat -ano | findstr :8004
netstat -ano | findstr :8005

# Kill conflicting process
taskkill /F /PID <process_id>
```

**Restart services:**
```bash
# Windows
stop.bat
start.bat

# Linux/Mac
./stop.sh
./start.sh
```

---

## SYSTEM REQUIREMENTS

### Minimum
- **OS**: Windows 10+, Ubuntu 20.04+, macOS 11+
- **RAM**: 4 GB
- **Disk**: 2 GB free
- **Python**: 3.11+ (for AI engine)
- **Node.js**: Not required in production

### Recommended
- **RAM**: 8 GB+
- **CPU**: 4+ cores
- **Python**: 3.11 or 3.12 (avoid 3.14 for now)

### For Development
- **Go**: 1.21+ (to build services)
- **Node.js**: 18+ (to rebuild frontend)
- **npm**: 9+

---

## ARCHITECTURE

```
┌─────────────────┐
│  Browser :3000  │ (Dev only - Vite)
│  Browser :8002  │ (Production - Built frontend)
└────────┬────────┘
         │
         ▼
┌─────────────────────────────────┐
│  Execution Engine :8002         │
│  - Serves frontend              │
│  - Orchestrates workflow runs   │
│  - Coordinates services         │
└────────┬────────────────────────┘
         │
    ┌────┴───┬────────┬───────┬─────────┐
    ▼        ▼        ▼       ▼         ▼
┌────────┐ ┌────┐ ┌─────┐ ┌──────┐ ┌────────┐
│Registry│ │ AI │ │Human│ │Agents│ │Connec- │
│  :8001 │ │:8003│ │:8004│ │:8005 │ │ tors   │
└────────┘ └────┘ └─────┘ └──────┘ └────────┘
```

---

## SUPPORT

### Documentation
- **README.md**: Complete feature documentation
- **DEPLOYMENT.md**: Advanced deployment scenarios
- **PRODUCTION-READY.md**: Development changelog

### Logs
All service logs are in the `logs/` directory:
- `workflow-registry.log`
- `execution-engine.log`
- `ai-decision-engine.log`
- `human-task-service.log`
- `agent-integration.log`

### Health Checks
Every service has a health endpoint:
- http://localhost:8001/api/v1/health
- http://localhost:8002/api/v1/health
- http://localhost:8003/internal/v1/health
- http://localhost:8004/api/v1/health
- http://localhost:8005/api/v1/health

---

## CONFIGURATION REFERENCE

**Complete `.env` template:**

```env
# AI Provider (required)
AI_PROVIDER=auto                              # auto | anthropic | ollama
ANTHROPIC_API_KEY=                            # Claude API key
OLLAMA_BASE_URL=http://localhost:11434
OLLAMA_MODEL=llama3.1:latest

# Service Ports (defaults shown)
REGISTRY_PORT=8001
ENGINE_PORT=8002
AI_PORT=8003
TASK_PORT=8004
AGENT_PORT=8005

# Service URLs (auto-configured)
REGISTRY_URL=http://localhost:8001
AI_DECISION_URL=http://localhost:8003
HUMAN_TASK_URL=http://localhost:8004
AGENT_URL=http://localhost:8005
EXECUTION_ENGINE_URL=http://localhost:8002

# Database paths (auto-created)
REGISTRY_DB=./data/workflows.db
ENGINE_DB=./data/runs.db
TASK_DB=./data/tasks.db
AGENT_DB=./data/agents.db

# Frontend
FRONTEND_PATH=./apps/designer/dist
```

---

**You're all set!** Build workflows, orchestrate AI decisions, and let your team focus on outcomes instead of infrastructure.

For advanced deployment (Docker, AWS, production hardening), see **DEPLOYMENT.md**.
