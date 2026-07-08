import React, { useState, useEffect } from 'react';
import { Plus, Play, Pencil, Trash2, Tag, Clock, GitBranch, Search, Sparkles, Wand2, Bot, AlertTriangle } from 'lucide-react';
import { workflows as wfApi, runs as runsApi, examples as examplesApi, aiGenerate } from '../lib/api.js';
import { StatusBadge, useToast } from '../components/Layout.jsx';
import { format } from 'date-fns';

export default function Workflows({ onNav, onDesign }) {
  const [list, setList]     = useState([]);
  const [loading, setLoading] = useState(true);
  const [search, setSearch] = useState('');
  const [showCreate, setShowCreate] = useState(false);
  const [showGenerate, setShowGenerate] = useState(false);
  const [showRun, setShowRun]     = useState(null); // workflow to run
  const [seeding, setSeeding]     = useState(false);
  const [runInput, setRunInput]   = useState('{\n  "transaction_id": "TXN-001",\n  "amount": 1500,\n  "merchant": "Unknown Merchant"\n}');
  const { toast } = useToast();

  async function load() {
    try { const r = await wfApi.list(); setList(r.data || []); }
    catch (e) { toast('Failed to load workflows', 'error', e.message); }
    finally { setLoading(false); }
  }

  useEffect(() => { load(); }, []);

  async function handleSeedExamples() {
    setSeeding(true);
    try {
      const r = await examplesApi.seed();
      toast(r.message || 'Examples loaded', 'success');
      load();
    } catch (e) { toast('Could not load examples', 'error', e.message); }
    finally { setSeeding(false); }
  }

  async function handleDelete(id, name) {
    if (!confirm(`Archive workflow "${name}"?`)) return;
    try {
      await wfApi.delete(id);
      toast(`"${name}" archived`, 'success');
      load();
    } catch (e) { toast('Delete failed', 'error', e.message); }
  }

  async function handleRun() {
    if (!showRun) return;
    let inputData;
    try { inputData = JSON.parse(runInput); }
    catch { toast('Invalid JSON input', 'error'); return; }
    try {
      const r = await runsApi.create({ workflow_id: showRun.id, input_data: inputData });
      toast(`Run started`, 'success', `ID: ${r.id.slice(0, 8)}…`);
      setShowRun(null);
      onNav('runs');
    } catch (e) { toast('Failed to start run', 'error', e.message); }
  }

  const filtered = list.filter(w =>
    !search || w.name.toLowerCase().includes(search.toLowerCase()) ||
    w.description?.toLowerCase().includes(search.toLowerCase())
  );

  return (
    <div style={{ display: 'flex', flexDirection: 'column', height: '100%', overflow: 'hidden' }}>
      <div className="page-header">
        <div>
          <div className="page-title">Workflow Registry</div>
          <div className="page-subtitle">{list.length} workflows registered</div>
        </div>
        <div className="page-actions">
          <div className="search-box">
            <Search size={13} />
            <input placeholder="Search workflows…" value={search} onChange={e => setSearch(e.target.value)} />
          </div>
          <button className="btn btn-secondary" onClick={handleSeedExamples} disabled={seeding} title="Add ready-made example workflows (skips any already present)">
            {seeding ? <span className="spinner-sm" /> : <Sparkles size={14} />} Examples
          </button>
          <button className="btn btn-secondary" onClick={() => setShowGenerate(true)} title="Describe an automation in plain English and let AI build the workflow">
            <Wand2 size={14} /> Generate with AI
          </button>
          <button className="btn btn-primary" onClick={() => onNav('designer')}>
            <Plus size={14} /> New Workflow
          </button>
        </div>
      </div>

      <div className="page-content">
        {loading ? (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
            {[1,2,3].map(i => <div key={i} className="skeleton" style={{ height: 96, borderRadius: 12 }} />)}
          </div>
        ) : filtered.length === 0 ? (
          <div className="empty-state">
            <GitBranch size={40} />
            <h3>No workflows found</h3>
            <p>{search ? 'Try a different search term' : 'Create your first workflow, or load ready-made examples for finance, marketing, supply chain, and HR/IT.'}</p>
            <div style={{ display: 'flex', gap: 8, marginTop: 4 }}>
              <button className="btn btn-primary" onClick={() => onNav('designer')}>
                <Plus size={14} /> Create Workflow
              </button>
              {!search && (
                <button className="btn btn-secondary" onClick={handleSeedExamples} disabled={seeding}>
                  {seeding ? <span className="spinner-sm" /> : <Sparkles size={14} />} Load Examples
                </button>
              )}
            </div>
          </div>
        ) : (
          <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
            {filtered.map(wf => (
              <WorkflowCard key={wf.id} wf={wf}
                onEdit={() => onDesign(wf.id)}
                onRun={() => { setShowRun(wf); setRunInput(makeDefaultInput(wf)); }}
                onDelete={() => handleDelete(wf.id, wf.name)}
              />
            ))}
          </div>
        )}
      </div>

      {/* Run Modal */}
      {showRun && (
        <div className="modal-overlay" onClick={() => setShowRun(null)}>
          <div className="modal" onClick={e => e.stopPropagation()}>
            <div className="modal-header">
              <div>
                <div className="modal-title">Trigger Run</div>
                <div className="modal-sub">{showRun.name}</div>
              </div>
              <button className="btn btn-ghost btn-icon btn-sm" onClick={() => setShowRun(null)}>✕</button>
            </div>
            <div className="form-group">
              <label className="form-label">Input Data (JSON)</label>
              <textarea className="textarea" rows={8} value={runInput} onChange={e => setRunInput(e.target.value)}
                style={{ fontFamily: 'var(--font-mono)', fontSize: 12 }} />
              <div className="form-hint">Provide the trigger input payload for this workflow</div>
            </div>
            <div className="modal-footer">
              <button className="btn btn-secondary" onClick={() => setShowRun(null)}>Cancel</button>
              <button className="btn btn-primary" onClick={handleRun}><Play size={13} />Start Run</button>
            </div>
          </div>
        </div>
      )}

      {/* Create Modal */}
      {showCreate && <CreateWorkflowModal onClose={() => setShowCreate(false)} onCreated={(wf) => { setShowCreate(false); load(); onDesign(wf.id); }} />}

      {/* Generate-with-AI Modal */}
      {showGenerate && (
        <GenerateWorkflowModal
          onClose={() => setShowGenerate(false)}
          onCreated={(wf) => { setShowGenerate(false); load(); onDesign(wf.id); }}
        />
      )}
    </div>
  );
}

function WorkflowCard({ wf, onEdit, onRun, onDelete }) {
  const tags = wf.tags || [];
  const def = wf.definition ? (typeof wf.definition === 'string' ? JSON.parse(wf.definition) : wf.definition) : {};
  const stepCount = def.steps?.length || 0;

  return (
    <div className="card" style={{ display: 'flex', gap: 16, alignItems: 'flex-start', padding: '16px 20px' }}>
      <div style={{ flex: 1, minWidth: 0 }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: 10, marginBottom: 4 }}>
          <span style={{ fontFamily: 'var(--font-display)', fontWeight: 700, fontSize: 15 }}>{wf.name}</span>
          <StatusBadge status={wf.status} />
          <span style={{ fontSize: 11, color: 'var(--text-muted)', marginLeft: 'auto', fontFamily: 'var(--font-mono)' }}>
            v{wf.current_version}
          </span>
        </div>
        <p style={{ fontSize: 13, color: 'var(--text-secondary)', marginBottom: 10, lineHeight: 1.5 }}>
          {wf.description || 'No description'}
        </p>
        <div style={{ display: 'flex', alignItems: 'center', gap: 12, flexWrap: 'wrap' }}>
          <span style={{ fontSize: 11, color: 'var(--text-muted)', display: 'flex', alignItems: 'center', gap: 4 }}>
            <GitBranch size={11} /> {stepCount} steps
          </span>
          <span style={{ fontSize: 11, color: 'var(--text-muted)', display: 'flex', alignItems: 'center', gap: 4 }}>
            <Clock size={11} /> {wf.updated_at ? format(new Date(wf.updated_at), 'MMM d, yyyy') : '—'}
          </span>
          {tags.map(t => (
            <span key={t} style={{ fontSize: 10, background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 99, padding: '2px 8px', color: 'var(--text-muted)' }}>
              {t}
            </span>
          ))}
        </div>
      </div>
      <div style={{ display: 'flex', gap: 6, flexShrink: 0, alignItems: 'center' }}>
        <button className="btn btn-success btn-sm" onClick={onRun}><Play size={12} />Run</button>
        <button className="btn btn-secondary btn-sm" onClick={onEdit}><Pencil size={12} />Edit</button>
        <button className="btn btn-ghost btn-icon btn-sm" onClick={onDelete}><Trash2 size={13} /></button>
      </div>
    </div>
  );
}

function CreateWorkflowModal({ onClose, onCreated }) {
  const [name, setName] = useState('');
  const [desc, setDesc] = useState('');
  const [saving, setSaving] = useState(false);
  const { toast } = useToast();

  async function handleCreate() {
    if (!name.trim()) { toast('Name is required', 'error'); return; }
    setSaving(true);
    try {
      const wf = await wfApi.create({ name, description: desc, status: 'draft', definition: { trigger: { type: 'api' }, steps: [] }, tags: [] });
      toast(`"${wf.name}" created`, 'success');
      onCreated(wf);
    } catch (e) { toast('Create failed', 'error', e.message); }
    finally { setSaving(false); }
  }

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal" onClick={e => e.stopPropagation()}>
        <div className="modal-header">
          <div className="modal-title">New Workflow</div>
          <button className="btn btn-ghost btn-icon btn-sm" onClick={onClose}>✕</button>
        </div>
        <div className="form-group">
          <label className="form-label">Name *</label>
          <input className="input" value={name} onChange={e => setName(e.target.value)} placeholder="e.g. Fraud Risk Assessment" autoFocus />
        </div>
        <div className="form-group">
          <label className="form-label">Description</label>
          <textarea className="textarea" rows={3} value={desc} onChange={e => setDesc(e.target.value)} placeholder="Describe what this workflow does…" />
        </div>
        <div className="modal-footer">
          <button className="btn btn-secondary" onClick={onClose}>Cancel</button>
          <button className="btn btn-primary" onClick={handleCreate} disabled={saving}>
            {saving ? <span className="spinner-sm" /> : <Plus size={13} />} Create & Design
          </button>
        </div>
      </div>
    </div>
  );
}

const GENERATE_EXAMPLES = [
  'When a customer support email arrives, analyze its sentiment and urgency, and escalate angry or urgent messages to a human agent on Slack.',
  'Every morning at 8am, read new rows from a Google Sheet of leads, score each one, and send hot leads to our sales channel.',
  'When an invoice webhook comes in, validate it against policy. Auto-approve clean invoices under $1000, escalate the rest for manager review.',
  'Monitor a GitHub repo for new issues, classify each one, and post a summary to Discord.',
];

function GenerateWorkflowModal({ onClose, onCreated }) {
  const [prompt, setPrompt] = useState('');
  const [generating, setGenerating] = useState(false);
  const [preview, setPreview] = useState(null); // generated workflow def
  const [meta, setMeta] = useState(null);
  const [warnings, setWarnings] = useState([]);
  const [saving, setSaving] = useState(false);
  const { toast } = useToast();

  async function handleGenerate() {
    if (!prompt.trim()) { toast('Describe the automation first', 'error'); return; }
    setGenerating(true);
    setPreview(null);
    setWarnings([]);
    try {
      const r = await aiGenerate.workflow(prompt.trim());
      if (!r.workflow) throw new Error('No workflow returned');
      setPreview(r.workflow);
      setMeta({ generator: r.generator, model: r.model_id });
      setWarnings(r.warnings || []);
      if (r.generator === 'simulation') {
        toast('Generated with rule-based fallback', 'info', 'Configure an AI provider in Settings for richer graphs.');
      } else {
        toast('Workflow generated', 'success', `via ${r.model_id}`);
      }
    } catch (e) {
      toast('Generation failed', 'error', e.message);
    } finally { setGenerating(false); }
  }

  async function handleSave() {
    if (!preview) return;
    setSaving(true);
    try {
      const { name, description, tags, trigger, steps } = preview;
      const definition = { trigger: trigger || { type: 'manual' }, steps: steps || [] };
      const wf = await wfApi.create({
        name: name || 'Generated Workflow',
        description: description || '',
        status: 'draft',
        definition,
        tags: tags || ['generated'],
      });
      toast(`"${wf.name}" created`, 'success', 'Opening in designer to review');
      onCreated(wf);
    } catch (e) { toast('Save failed', 'error', e.message); setSaving(false); }
  }

  const stepCount = preview?.steps?.length || 0;

  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal" onClick={e => e.stopPropagation()} style={{ maxWidth: 640, width: '90%' }}>
        <div className="modal-header">
          <div>
            <div className="modal-title" style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
              <Wand2 size={16} /> Generate Workflow with AI
            </div>
            <div className="modal-sub">Describe what you want to automate in plain English</div>
          </div>
          <button className="btn btn-ghost btn-icon btn-sm" onClick={onClose}>✕</button>
        </div>

        <div className="form-group">
          <label className="form-label">Describe your automation</label>
          <textarea className="textarea" rows={4} value={prompt} onChange={e => setPrompt(e.target.value)}
            placeholder="e.g. When a support email arrives, gauge sentiment and escalate urgent or angry ones to Slack…"
            autoFocus disabled={generating} />
          <div className="form-hint">Mention the trigger, the decision/steps, and where results should go.</div>
        </div>

        {!preview && (
          <div style={{ marginBottom: 14 }}>
            <div style={{ fontSize: 11, color: 'var(--text-muted)', marginBottom: 6 }}>Try one of these:</div>
            <div style={{ display: 'flex', flexDirection: 'column', gap: 6 }}>
              {GENERATE_EXAMPLES.map((ex, i) => (
                <button key={i} className="btn btn-ghost btn-sm" style={{ textAlign: 'left', justifyContent: 'flex-start', fontSize: 12, height: 'auto', padding: '8px 10px', whiteSpace: 'normal', lineHeight: 1.4 }}
                  onClick={() => setPrompt(ex)} disabled={generating}>
                  <Sparkles size={12} style={{ flexShrink: 0, marginTop: 2 }} /> {ex}
                </button>
              ))}
            </div>
          </div>
        )}

        {preview && (
          <div className="card" style={{ padding: '14px 16px', marginBottom: 14 }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 6 }}>
              <Bot size={15} />
              <span style={{ fontWeight: 700, fontSize: 14 }}>{preview.name}</span>
              <span style={{ marginLeft: 'auto', fontSize: 10, color: 'var(--text-muted)', fontFamily: 'var(--font-mono)' }}>
                {meta?.generator === 'simulation' ? 'rule-based' : meta?.model}
              </span>
            </div>
            <p style={{ fontSize: 12, color: 'var(--text-secondary)', marginBottom: 10, lineHeight: 1.5 }}>{preview.description}</p>
            <div style={{ display: 'flex', flexWrap: 'wrap', gap: 6 }}>
              {(preview.steps || []).map(s => (
                <span key={s.id} style={{ fontSize: 10, background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 6, padding: '3px 8px', fontFamily: 'var(--font-mono)' }}>
                  {s.type}: {s.name || s.id}
                </span>
              ))}
            </div>
            <div style={{ fontSize: 11, color: 'var(--text-muted)', marginTop: 10 }}>{stepCount} nodes · review and edit after saving</div>
          </div>
        )}

        {preview && warnings.length > 0 && (
          <div className="card" style={{ padding: '12px 14px', marginBottom: 14, borderColor: 'var(--amber)', background: 'var(--amber-dim)' }}>
            <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 8, fontWeight: 700, fontSize: 13, color: 'var(--amber)' }}>
              <AlertTriangle size={14} /> {warnings.length} thing{warnings.length > 1 ? 's' : ''} to review before this will run
            </div>
            <ul style={{ margin: 0, paddingLeft: 18, fontSize: 12, color: 'var(--text-secondary)', lineHeight: 1.6 }}>
              {warnings.map((w, i) => <li key={i}>{w}</li>)}
            </ul>
          </div>
        )}

        <div className="modal-footer">
          <button className="btn btn-secondary" onClick={onClose}>Cancel</button>
          {!preview ? (
            <button className="btn btn-primary" onClick={handleGenerate} disabled={generating}>
              {generating ? <span className="spinner-sm" /> : <Wand2 size={13} />} {generating ? 'Generating…' : 'Generate'}
            </button>
          ) : (
            <>
              <button className="btn btn-secondary" onClick={handleGenerate} disabled={generating}>
                {generating ? <span className="spinner-sm" /> : <RefreshCwIcon />} Regenerate
              </button>
              <button className="btn btn-primary" onClick={handleSave} disabled={saving}>
                {saving ? <span className="spinner-sm" /> : <Plus size={13} />} Save & Open
              </button>
            </>
          )}
        </div>
      </div>
    </div>
  );
}

function RefreshCwIcon() {
  return <Sparkles size={13} />;
}

function makeDefaultInput(wf) {  try {
    const def = typeof wf.definition === 'string' ? JSON.parse(wf.definition) : wf.definition;
    const schema = def?.trigger?.input_schema?.properties;
    if (schema) {
      const sample = {};
      Object.entries(schema).forEach(([k, v]) => {
        if (v.type === 'string')  sample[k] = `sample-${k}`;
        if (v.type === 'number')  sample[k] = 1000;
        if (v.type === 'boolean') sample[k] = false;
      });
      return JSON.stringify(sample, null, 2);
    }
  } catch {}
  return '{\n  \n}';
}
