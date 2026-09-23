# Quick start

Five minutes from download to a running, AI-assisted workflow.

## 1. Install

| | |
|---|---|
| **Windows** | Run `KNOTT-…-windows-x64-setup.exe` from [Releases](https://github.com/regnant-io/knott/releases). The defaults (just for you, Start menu shortcut) are fine. |
| **macOS** | Open the `.dmg` (`arm64` for Apple silicon, `amd64` for Intel) and drag KNOTT to Applications. The first time, right-click KNOTT → **Open**. |
| **Linux** | `sudo apt install ./knott_*.deb ./knott-desktop_*.deb` (or the `.rpm`s). |

Open **KNOTT** from your applications. Everything runs on this computer; your
data lives in `%LOCALAPPDATA%\KNOTT`, `~/Library/Application Support/KNOTT` or
`~/.local/share/knott` (**File → Show Data Folder**).

## 2. Turn on local AI (optional, recommended)

Install [Ollama](https://ollama.com) and pull a model:

```bash
ollama pull llama3.2
```

That is all. KNOTT finds Ollama on its own — **Settings → AI** shows the model
it is using and can send a test prompt. Prefer a hosted model? Paste an
Anthropic API key there instead. With neither, AI Decision steps use built-in
rules that escalate anything uncertain to a person.

## 3. Build a workflow

1. **Workflows → New workflow.**
2. On the empty canvas, describe it — *"When a support email arrives, classify
   its urgency with AI; post urgent ones to Slack and file the rest in
   Zendesk"* — and press **Draft with AI**. Or click **Add the first step**
   and pick a trigger.
3. Click the **+** on a step to add the next one. Search for what it should do
   ("summarise", "slack", "wait", "if"), or drag a result onto the canvas.
4. Select a step to configure it in the inspector. App steps show exactly which
   credentials they need; paste them inline or on the **Connectors** page.

## 4. Run it

Press **Test run**, give it some input JSON, and watch the run play out on the
canvas. Select a step and open its **Output** tab to see what it produced.
**Executions** lists every run; **Decision log** shows every AI decision with
its model, confidence and reasoning; **Human review** is where paused runs
wait for a person.

Set the workflow **Active** and save: webhooks, schedules and polling triggers
now start runs on their own (while KNOTT is running — the installer can start
it when you sign in).

## Troubleshooting

| | |
|---|---|
| Settings → AI says *no model* | Is Ollama running (`ollama list`)? Is a chat model pulled? Press **Recheck**. |
| A step says *credentials needed* | Open the app on the **Connectors** page, save its credentials, press **Test connection**. |
| KNOTT will not open | See `logs/knott-desktop.log` in the data folder (**File → Show Logs**). |
| Port 8002 is taken | KNOTT picks another port automatically; **Help → About** shows the address. |

More: [README](README.md) · [Building workflows](docs/workflows.md) ·
[Connectors](docs/connectors.md) · [Deploying on a server](docs/deployment.md) ·
[Security](SECURITY.md)
