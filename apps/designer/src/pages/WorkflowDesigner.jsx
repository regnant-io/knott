import React, { useState, useEffect, useCallback, useRef } from 'react';
import ReactFlow, {
  addEdge, useNodesState, useEdgesState,
  Background, Controls, MiniMap,
  BackgroundVariant, MarkerType,
} from 'reactflow';
import {
  Save, Play, CheckCircle, Code2, ArrowLeft, AlertTriangle,
  Brain, Zap, User, GitBranch, Wrench, Bot, CheckSquare, X,
  KeyRound, Check,
} from 'lucide-react';
import { workflows as wfApi, runs as runsApi, connectors as connectorsApi, agents as agentsApi, taskSpecs as taskSpecsApi, triggers as triggersApi, credentials as credsApi } from '../lib/api.js';
import { NODE_TYPES, PALETTE_NODES } from '../components/nodes/index.jsx';
import { useToast, StatusBadge } from '../components/Layout.jsx';
import { resolveTemplate, hasTemplate, sampleContext } from '../lib/expr.js';

let nodeId = 1000;
const uid = () => `node_${++nodeId}`;

function defToFlow(def) {
  if (!def?.steps) return { nodes: [], edges: [] };
  const nodes = def.steps.map(s => ({
    id: s.id,
    type: s.type,
    position: s.position || { x: 100, y: 100 },
    data: { ...s },
  }));
  const edges = [];
  def.steps.forEach(s => {
    if (s.next) edges.push({ id: `e-${s.id}-${s.next}`, source: s.id, target: s.next, markerEnd: { type: MarkerType.ArrowClosed } });
    (s.cases || []).forEach((c, i) => {
      if (c.next) edges.push({ id: `e-${s.id}-case${i}-${c.next}`, source: s.id, target: c.next, label: c.condition?.slice(0, 30) + (c.condition?.length > 30 ? '…' : ''), markerEnd: { type: MarkerType.ArrowClosed } });
    });
    if (s.default && !s.next) edges.push({ id: `e-${s.id}-default`, source: s.id, target: s.default, label: 'default', markerEnd: { type: MarkerType.ArrowClosed } });
  });
  return { nodes, edges };
}

function flowToDef(nodes, edges, trigger) {
  const stepMap = {};
  nodes.forEach(n => { stepMap[n.id] = { ...n.data, id: n.id, type: n.type, position: n.position }; });
  edges.forEach(e => {
    const step = stepMap[e.source];
    if (!step) return;
    if (!e.label || e.label === 'default') {
      if (step.type === 'condition' && !e.label) return;
      if (step.type !== 'condition') step.next = e.target;
    }
  });
  return { trigger: trigger || { type: 'api' }, steps: Object.values(stepMap) };
}

export default function WorkflowDesigner({ workflowId, onBack }) {
  const [wf, setWf]             = useState(null);
  const [nodes, setNodes, onNodesChange] = useNodesState([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState([]);
  const [selected, setSelected] = useState(null);
  const [showYAML, setShowYAML] = useState(false);
  const [saving, setSaving]     = useState(false);
  const [validErrs, setValidErrs] = useState([]);
  const [showRunModal, setShowRunModal] = useState(false);
  const [loadError, setLoadError] = useState(null);
  const [loading, setLoading] = useState(!!workflowId);
  const [runInput, setRunInput] = useState('{\n  \n}');
  const [connectorOpts, setConnectorOpts] = useState([]);
  const [agentOpts, setAgentOpts] = useState([]);
  const [taskSpecOpts, setTaskSpecOpts] = useState([]);
  // Test context for live expression preview: the JSON the author expects as
  // trigger input. Persisted per-workflow in localStorage so it survives reloads.
  const [testInput, setTestInput] = useState('{\n  \n}');
  const canvasRef = useRef(null);
  const { toast } = useToast();

  // Build the preview context from the test input + step stubs for this graph.
  const previewCtx = React.useMemo(() => {
    const def = flowToDef(nodes, edges, wf?.definition?.trigger);
    const ctx = sampleContext(def);
    try { ctx.input = JSON.parse(testInput); } catch { /* keep empty input on bad JSON */ }
    return ctx;
  }, [testInput, nodes, edges, wf]);

  // Load installed connectors + registered agents + AI task specs so node config
  // is driven by what the operator has actually set up (not a hard-coded list).
  useEffect(() => {
    connectorsApi.list()
      .then(r => setConnectorOpts((r.data || []).filter(c => c.installed)))
      .catch(() => setConnectorOpts([]));
    agentsApi.list()
      .then(r => setAgentOpts(r.data || []))
      .catch(() => setAgentOpts([]));
    taskSpecsApi.list()
      .then(r => setTaskSpecOpts(r.data || []))
      .catch(() => setTaskSpecOpts([]));
  }, []);

  const loadWorkflow = useCallback(() => {
    if (!workflowId) {
      setWf({ name: 'Untitled Workflow', status: 'draft', definition: { trigger: { type: 'api' }, steps: [] } });
      setLoading(false);
      return;
    }
    setLoading(true);
    setLoadError(null);
    wfApi.get(workflowId).then(w => {
      setWf(w);
      let def;
      try {
        def = typeof w.definition === 'string' ? JSON.parse(w.definition) : w.definition;
      } catch (parseErr) {
        // Corrupt/invalid definition: recover with an empty canvas rather than crash.
        toast('Workflow definition was invalid — starting from an empty canvas', 'warning');
        def = { trigger: { type: 'api' }, steps: [] };
      }
      const { nodes: n, edges: e } = defToFlow(def || { steps: [] });
      setNodes(n); setEdges(e);
      setLoading(false);
    }).catch(e => {
      setLoadError(e.message || 'Failed to load workflow');
      setLoading(false);
    });
  }, [workflowId]);

  useEffect(() => { loadWorkflow(); }, [loadWorkflow]);

  const onConnect = useCallback((params) => {
    setEdges(es => addEdge({ ...params, markerEnd: { type: MarkerType.ArrowClosed } }, es));
  }, []);

  const onNodeClick = useCallback((_, node) => setSelected(node), []);
  const onPaneClick = useCallback(() => setSelected(null), []);

  const onDragOver = useCallback(e => { e.preventDefault(); e.dataTransfer.dropEffect = 'move'; }, []);

  const onDrop = useCallback(e => {
    e.preventDefault();
    const type = e.dataTransfer.getData('nodeType');
    if (!type) return;
    const bounds = canvasRef.current?.getBoundingClientRect();
    const position = {
      x: e.clientX - (bounds?.left || 0) - 90,
      y: e.clientY - (bounds?.top || 0) - 30,
    };
    const id = uid();
    const newNode = {
      id, type, position,
      data: { id, type, name: type.replace(/_/g, ' ').replace(/\b\w/g, c => c.toUpperCase()), config: {}, inputs: {} },
    };
    setNodes(ns => [...ns, newNode]);
    setSelected(newNode);
  }, []);

  function updateSelectedData(patch) {
    if (!selected) return;
    const updated = { ...selected, data: { ...selected.data, ...patch } };
    setSelected(updated);
    setNodes(ns => ns.map(n => n.id === selected.id ? updated : n));
  }

  async function handleSave() {
    setSaving(true);
    try {
      const def = flowToDef(nodes, edges, wf?.definition?.trigger);
      const body = { name: wf?.name, description: wf?.description, status: wf?.status || 'draft', definition: def, tags: wf?.tags || [] };
      let saved;
      if (workflowId) {
        saved = await wfApi.update(workflowId, body);
      } else if (wf?.id) {
        // Already created once in this session — update instead of duplicating.
        saved = await wfApi.update(wf.id, body);
      } else {
        saved = await wfApi.create(body);
      }
      setWf(saved);
      toast('Workflow saved', 'success');
      return saved;
    } catch (e) {
      toast('Save failed', 'error', e.message);
      return null;
    } finally { setSaving(false); }
  }

  async function handleValidate() {
    try {
      const def = flowToDef(nodes, edges, wf?.definition?.trigger);
      const res = await wfApi.validate(workflowId || 'new', def);
      setValidErrs(res.errors || []);
      if (res.valid) toast('Workflow is valid ✓', 'success');
      else toast(`${res.errors.length} validation error(s)`, 'warning');
    } catch (e) { toast('Validation error', 'error', e.message); }
  }

  async function handleRun() {
    let inputData;
    try { inputData = JSON.parse(runInput); } catch { toast('Invalid JSON input', 'error'); return; }
    const saved = await handleSave();
    const id = saved?.id || workflowId || wf?.id;
    if (!id) { toast('Save the workflow before running', 'warning'); return; }
    try {
      const r = await runsApi.create({ workflow_id: id, input_data: inputData });
      toast(`Run started`, 'success', `ID: ${r.id.slice(0, 8)}…`);
      setShowRunModal(false);
    } catch (e) { toast('Run failed', 'error', e.message); }
  }

  const yamlLike = JSON.stringify(flowToDef(nodes, edges, wf?.definition?.trigger), null, 2);

  // Recovery state: workflow failed to load (network/registry down or bad id).
  if (loadError) {
    return (
      <div className="designer-shell">
        <div className="designer-toolbar">
          <button className="btn btn-ghost btn-sm" onClick={onBack}><ArrowLeft size={13} /> Back</button>
        </div>
        <div className="empty-state" style={{ flex: 1, justifyContent: 'center' }}>
          <AlertTriangle size={40} color="var(--red)" />
          <h3>Couldn't load this workflow</h3>
          <p>{loadError}</p>
          <div style={{ display: 'flex', gap: 8, marginTop: 8 }}>
            <button className="btn btn-secondary" onClick={onBack}>Back to Registry</button>
            <button className="btn btn-primary" onClick={loadWorkflow}>Retry</button>
          </div>
        </div>
      </div>
    );
  }

  if (loading) {
    return (
      <div className="designer-shell">
        <div className="designer-toolbar">
          <button className="btn btn-ghost btn-sm" onClick={onBack}><ArrowLeft size={13} /> Back</button>
        </div>
        <div style={{ flex: 1, display: 'flex', alignItems: 'center', justifyContent: 'center', flexDirection: 'column', gap: 14 }}>
          <div className="spinner spinner-lg" />
          <span style={{ color: 'var(--text-muted)', fontSize: 13 }}>Loading workflow…</span>
        </div>
      </div>
    );
  }

  return (
    <div className="designer-shell">
      {/* Toolbar */}
      <div className="designer-toolbar">
        <button className="btn btn-ghost btn-sm" onClick={onBack}><ArrowLeft size={13} /> Back</button>
        <div className="divider" style={{ width: 1, height: 20, margin: '0 4px' }} />
        <input
          className="input" style={{ width: 240, padding: '5px 10px', fontSize: 14, fontWeight: 600 }}
          value={wf?.name || ''}
          onChange={e => setWf(w => ({ ...w, name: e.target.value }))}
          placeholder="Workflow name…"
        />
        <select className="select" style={{ width: 100, padding: '5px 28px 5px 8px', fontSize: 12 }}
          value={wf?.status || 'draft'} onChange={e => setWf(w => ({ ...w, status: e.target.value }))}>
          <option value="draft">Draft</option>
          <option value="active">Active</option>
          <option value="deprecated">Deprecated</option>
        </select>
        <div style={{ flex: 1 }} />
        {validErrs.length > 0 && (
          <div style={{ display: 'flex', alignItems: 'center', gap: 6, fontSize: 12, color: 'var(--yellow)' }}>
            <AlertTriangle size={13} /> {validErrs.length} error(s)
          </div>
        )}
        <button className="btn btn-ghost btn-sm" onClick={() => setShowYAML(s => !s)}>
          <Code2 size={13} /> {showYAML ? 'Canvas' : 'JSON'}
        </button>
        <button className="btn btn-secondary btn-sm" onClick={handleValidate}><CheckCircle size={13} />Validate</button>
        <button className="btn btn-secondary btn-sm" onClick={handleSave} disabled={saving}>
          {saving ? <span className="spinner-sm" /> : <Save size={13} />} Save
        </button>
        <button className="btn btn-primary btn-sm" onClick={() => setShowRunModal(true)}>
          <Play size={13} /> Run
        </button>
      </div>

      <div className="designer-body">
        {/* Palette */}
        <div className="designer-palette">
          <div className="palette-title">Node Types</div>
          {PALETTE_NODES.map(p => {
            const Icon = p.icon;
            return (
              <div key={p.type} className="palette-node"
                draggable onDragStart={e => e.dataTransfer.setData('nodeType', p.type)}>
                <div className="palette-node-dot" style={{ background: p.color }} />
                <Icon size={13} color={p.color} />
                {p.label}
              </div>
            );
          })}
          <div style={{ marginTop: 16, padding: '0 6px' }}>
            <div style={{ fontSize: 10, color: 'var(--text-muted)', lineHeight: 1.6 }}>
              Drag nodes onto the canvas. Connect by dragging from node handles.
            </div>
          </div>
        </div>

        {/* Canvas or JSON view */}
        {showYAML ? (
          <div style={{ flex: 1, overflow: 'auto', padding: 24 }}>
            <div className="card-label" style={{ marginBottom: 10 }}>Workflow Definition (JSON)</div>
            <div className="code-block">{yamlLike}</div>
          </div>
        ) : (
          <div className="designer-canvas" ref={canvasRef}>
            <ReactFlow
              nodes={nodes.map(n => ({ ...n, selected: selected?.id === n.id }))}
              edges={edges}
              onNodesChange={onNodesChange}
              onEdgesChange={onEdgesChange}
              onConnect={onConnect}
              onNodeClick={onNodeClick}
              onPaneClick={onPaneClick}
              onDrop={onDrop}
              onDragOver={onDragOver}
              nodeTypes={NODE_TYPES}
              fitView
              fitViewOptions={{ padding: 0.2 }}
              defaultEdgeOptions={{ markerEnd: { type: MarkerType.ArrowClosed }, style: { stroke: 'var(--border-active)', strokeWidth: 2 } }}
              edgesFocusable={false}
              deleteKeyCode="Delete"
              style={{ background: 'var(--bg)' }}
            >
              <Background variant={BackgroundVariant.Dots} gap={24} size={1} color="var(--border-dim)" />
              <Controls />
              <MiniMap
                nodeColor={n => {
                  const colors = { trigger: '#f97316', ai_decision: '#7c3aed', human_task: '#3b82f6', condition: '#f59e0b', tool_call: '#14b8a6', agent_call: '#ec4899', end: '#10b981' };
                  return colors[n.type] || '#4a4a68';
                }}
                maskColor="rgba(7,7,14,0.7)"
              />
            </ReactFlow>
            {nodes.length === 0 && (
              <div style={{ position: 'absolute', inset: 0, display: 'flex', flexDirection: 'column', alignItems: 'center', justifyContent: 'center', pointerEvents: 'none', gap: 10 }}>
                <div style={{ fontSize: 48, opacity: 0.15 }}>✦</div>
                <p style={{ color: 'var(--text-muted)', fontSize: 13 }}>Drag nodes from the palette to start designing</p>
              </div>
            )}
          </div>
        )}

        {/* Properties Panel */}
        <div className="designer-props">
          <div className="props-header">
            {selected ? `${selected.type.replace(/_/g, ' ').toUpperCase()} PROPERTIES` : 'PROPERTIES'}
          </div>
          <div className="props-body">
            {!selected ? (
              <div style={{ color: 'var(--text-muted)', fontSize: 12, textAlign: 'center', marginTop: 40 }}>
                Click a node to edit its properties
              </div>
            ) : (
              <NodePropsEditor node={selected} onChange={updateSelectedData} connectorOpts={connectorOpts} agentOpts={agentOpts} taskSpecOpts={taskSpecOpts} previewCtx={previewCtx} testInput={testInput} setTestInput={setTestInput} workflowId={workflowId} />
            )}
          </div>
        </div>
      </div>

      {/* Run modal */}
      {showRunModal && (
        <div className="modal-overlay" onClick={() => setShowRunModal(false)}>
          <div className="modal" onClick={e => e.stopPropagation()}>
            <div className="modal-header">
              <div><div className="modal-title">Run Workflow</div><div className="modal-sub">{wf?.name}</div></div>
              <button className="btn btn-ghost btn-icon btn-sm" onClick={() => setShowRunModal(false)}>✕</button>
            </div>
            <div className="form-group">
              <label className="form-label">Input Payload (JSON)</label>
              <textarea className="textarea" rows={8} value={runInput} onChange={e => setRunInput(e.target.value)} />
            </div>
            <div className="modal-footer">
              <button className="btn btn-secondary" onClick={() => setShowRunModal(false)}>Cancel</button>
              <button className="btn btn-primary" onClick={handleRun}><Play size={13} />Start Run</button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

function NodePropsEditor({ node, onChange, connectorOpts = [], agentOpts = [], taskSpecOpts = [], previewCtx = {}, testInput = '{}', setTestInput, workflowId }) {
  const d = node.data;

  // Built-in fallback list used only if the AI engine is unreachable, so the
  // dropdown is never empty. When reachable, taskSpecOpts drives it dynamically.
  const FALLBACK_SPECS = [
    { id: 'fraud_risk_assessment', name: 'Fraud Risk Assessment' },
    { id: 'credit_risk_assessment', name: 'Credit Risk Assessment' },
    { id: 'content_moderation', name: 'Content Moderation' },
    { id: 'document_classification', name: 'Document Classification' },
    { id: 'sentiment_analysis', name: 'Sentiment Analysis' },
    { id: 'general_decision', name: 'General Decision' },
  ];
  const specs = taskSpecOpts.length ? taskSpecOpts : FALLBACK_SPECS;

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 12 }}>
      <div className="form-group" style={{ marginBottom: 0 }}>
        <label className="form-label">Name</label>
        <input className="input" value={d.name || ''} onChange={e => onChange({ name: e.target.value })} />
      </div>

      <div className="form-group" style={{ marginBottom: 0 }}>
        <label className="form-label">Node ID</label>
        <input className="input" value={d.id || ''} readOnly style={{ opacity: 0.5 }} />
      </div>

      {node.type !== 'trigger' && node.type !== 'end' && (
        <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 12, color: 'var(--text-secondary)', cursor: 'pointer' }}>
          <input type="checkbox" checked={!!d.disabled} onChange={e => onChange({ disabled: e.target.checked })} />
          Disable this node (skipped at runtime — routes straight to Next)
        </label>
      )}
      <div className="form-group" style={{ marginBottom: 0 }}>
        <label className="form-label">Notes</label>
        <textarea className="textarea" rows={2} value={d.notes || ''} placeholder="Optional notes for your team"
          onChange={e => onChange({ notes: e.target.value })} />
      </div>

      {setTestInput && <TestDataPanel testInput={testInput} setTestInput={setTestInput} previewCtx={previewCtx} />}

      {node.type === 'ai_decision' && (
        <>
          <div className="divider" />
          <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.1em' }}>AI Configuration</div>
          <div className="form-group" style={{ marginBottom: 0 }}>
            <label className="form-label">Task Spec</label>
            <select className="select" value={d.config?.task || ''} onChange={e => onChange({ config: { ...(d.config || {}), task: e.target.value } })}>
              <option value="">Select task spec…</option>
              {specs.map(s => <option key={s.id} value={s.id}>{s.name}</option>)}
            </select>
          </div>
          <div className="form-group" style={{ marginBottom: 0 }}>
            <label className="form-label">Confidence Threshold</label>
            <input className="input" type="number" min={0} max={1} step={0.05}
              value={d.config?.confidence_threshold ?? 0.85}
              onChange={e => onChange({ config: { ...(d.config || {}), confidence_threshold: parseFloat(e.target.value) } })} />
          </div>
          <div className="form-group" style={{ marginBottom: 0 }}>
            <label className="form-label">Fallback Node ID</label>
            <input className="input" value={d.config?.fallback || ''} placeholder="node_id (if low confidence)"
              onChange={e => onChange({ config: { ...(d.config || {}), fallback: e.target.value } })} />
          </div>
          <div className="form-group" style={{ marginBottom: 0 }}>
            <label className="form-label">Model Profile</label>
            <select className="select" value={d.config?.model_profile || 'default'}
              onChange={e => onChange({ config: { ...(d.config || {}), model_profile: e.target.value } })}>
              <optgroup label="Anthropic Claude">
                <option value="default">Default (Sonnet 4)</option>
                <option value="high_accuracy">High Accuracy (Opus 4)</option>
                <option value="fast">Fast (Haiku)</option>
              </optgroup>
              <optgroup label="Ollama (local)">
                <option value="ollama_default">Ollama — configured default</option>
                <option value="ollama_fast">Ollama — Llama 3.2 (fast)</option>
                <option value="ollama_large">Ollama — Llama 3.1 70B</option>
              </optgroup>
            </select>
            <div className="form-hint">Anthropic profiles run on Claude; Ollama profiles run on your local instance (set the default model in Settings).</div>
          </div>
          <ToolInputsEditor d={d} onChange={onChange} label="Decision Inputs (data sent to the model)" previewCtx={previewCtx} />
          <AdvancedAIConfig d={d} onChange={onChange} />
        </>
      )}

      {node.type === 'human_task' && (
        <>
          <div className="divider" />
          <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.1em' }}>Task Configuration</div>
          <div className="form-group" style={{ marginBottom: 0 }}>
            <label className="form-label">Task Title</label>
            <input className="input" value={d.config?.title || ''} placeholder="Displayed to reviewer"
              onChange={e => onChange({ config: { ...(d.config || {}), title: e.target.value } })} />
          </div>
          <div className="form-group" style={{ marginBottom: 0 }}>
            <label className="form-label">Description</label>
            <input className="input" value={d.config?.description || ''} placeholder="Short summary (supports {{ templates }})"
              onChange={e => onChange({ config: { ...(d.config || {}), description: e.target.value } })} />
          </div>
          <div className="form-group" style={{ marginBottom: 0 }}>
            <label className="form-label">Reviewer Instructions</label>
            <textarea className="textarea" rows={3} value={d.config?.instructions || ''} placeholder="What should the reviewer check? (shown in the task)"
              onChange={e => onChange({ config: { ...(d.config || {}), instructions: e.target.value } })} />
          </div>
          <div style={{ display: 'flex', gap: 8 }}>
            <div className="form-group" style={{ marginBottom: 0, flex: 1 }}>
              <label className="form-label">SLA (hours)</label>
              <input className="input" type="number" min={1} value={d.config?.due_hours ?? 24}
                onChange={e => onChange({ config: { ...(d.config || {}), due_hours: parseInt(e.target.value) } })} />
            </div>
            <div className="form-group" style={{ marginBottom: 0, flex: 1 }}>
              <label className="form-label">Priority</label>
              <select className="select" value={d.config?.priority || 'NORMAL'}
                onChange={e => onChange({ config: { ...(d.config || {}), priority: e.target.value } })}>
                <option value="LOW">Low</option>
                <option value="NORMAL">Normal</option>
                <option value="HIGH">High</option>
                <option value="URGENT">Urgent</option>
              </select>
            </div>
          </div>
          <div className="form-group" style={{ marginBottom: 0 }}>
            <label className="form-label">Assigned Roles (comma-separated)</label>
            <input className="input" value={(d.config?.assigned_roles || []).join(', ')} placeholder="analyst, manager"
              onChange={e => onChange({ config: { ...(d.config || {}), assigned_roles: e.target.value.split(',').map(s => s.trim()).filter(Boolean) } })} />
          </div>
          <ContextDataEditor d={d} onChange={onChange} previewCtx={previewCtx} />
        </>
      )}

      {node.type === 'condition' && (
        <>
          <div className="divider" />
          <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.1em' }}>Routing Cases</div>
          {(d.cases || []).map((c, i) => (
            <div key={i} style={{ background: 'var(--bg-elevated)', border: '1px solid var(--border)', borderRadius: 8, padding: 10, marginBottom: 6 }}>
              <div className="form-label" style={{ marginBottom: 4 }}>Case {i + 1}</div>
              <input className="input" style={{ marginBottom: 6, fontSize: 11, fontFamily: 'var(--font-mono)' }}
                value={c.condition} placeholder="Expression…"
                onChange={e => { const cases = [...(d.cases || [])]; cases[i] = { ...cases[i], condition: e.target.value }; onChange({ cases }); }} />
              <input className="input" style={{ fontSize: 11 }}
                value={c.next} placeholder="next node id"
                onChange={e => { const cases = [...(d.cases || [])]; cases[i] = { ...cases[i], next: e.target.value }; onChange({ cases }); }} />
            </div>
          ))}
          <button className="btn btn-ghost btn-sm" style={{ width: '100%' }}
            onClick={() => onChange({ cases: [...(d.cases || []), { condition: '', next: '' }] })}>
            + Add Case
          </button>
          <div className="form-group" style={{ marginBottom: 0, marginTop: 8 }}>
            <label className="form-label">Default (fallback)</label>
            <input className="input" value={d.default || ''} placeholder="next node id"
              onChange={e => onChange({ default: e.target.value })} />
          </div>
        </>
      )}

      {node.type === 'loop' && (
        <>
          <div className="divider" />
          <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.1em' }}>Loop</div>
          <div className="form-group" style={{ marginBottom: 0 }}>
            <label className="form-label">Items (list expression)</label>
            <input className="input" value={d.config?.items || ''} placeholder="{{ steps.poll.output.records }}"
              onChange={e => onChange({ config: { ...(d.config || {}), items: e.target.value } })} />
            <ExprPreview value={d.config?.items} ctx={previewCtx} />
          </div>
          <div className="form-group" style={{ marginBottom: 0 }}>
            <label className="form-label">Body Start Node ID</label>
            <input className="input" value={d.config?.body || ''} placeholder="first node id of the loop body"
              onChange={e => onChange({ config: { ...(d.config || {}), body: e.target.value } })} />
            <div className="form-hint">Each item runs this sub-path with <code style={{ fontFamily: 'var(--font-mono)' }}>{'{{ item }}'}</code> and <code style={{ fontFamily: 'var(--font-mono)' }}>{'{{ loop_index }}'}</code> available.</div>
          </div>
          <div style={{ display: 'flex', gap: 8 }}>
            <div className="form-group" style={{ marginBottom: 0, flex: 1 }}>
              <label className="form-label">Item Variable</label>
              <input className="input" value={d.config?.item_var || ''} placeholder="item"
                onChange={e => onChange({ config: { ...(d.config || {}), item_var: e.target.value } })} />
            </div>
            <div className="form-group" style={{ marginBottom: 0, flex: 1 }}>
              <label className="form-label">Max Items</label>
              <input className="input" type="number" min={1} value={d.config?.max_items || 1000}
                onChange={e => onChange({ config: { ...(d.config || {}), max_items: parseInt(e.target.value) } })} />
            </div>
          </div>
        </>
      )}

      {node.type === 'code' && (
        <>
          <div className="divider" />
          <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.1em' }}>Code (Expressions)</div>
          <div className="form-hint" style={{ marginTop: 0 }}>Each output field is an expression: functions (upper, concat, len, if, dateadd…), operators (+ - * / ?? == &gt;), paths.</div>
          <AssignmentsEditor d={d} onChange={onChange} configKey="assignments" valuePlaceholder="expression e.g. concat(input.a,' ',input.b)" previewCtx={previewCtx} asExpr />
        </>
      )}

      {node.type === 'set' && (
        <>
          <div className="divider" />
          <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.1em' }}>Set Fields</div>
          <AssignmentsEditor d={d} onChange={onChange} configKey="fields" valuePlaceholder="value or {{ template }}" previewCtx={previewCtx} />
        </>
      )}

      {node.type === 'filter' && (
        <>
          <div className="divider" />
          <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.1em' }}>Filter</div>
          <div className="form-group" style={{ marginBottom: 0 }}>
            <label className="form-label">Condition</label>
            <input className="input mono" value={d.config?.condition || ''} placeholder="input.score > 80"
              style={{ fontFamily: 'var(--font-mono)' }}
              onChange={e => onChange({ config: { ...(d.config || {}), condition: e.target.value } })} />
            <div className="form-hint">Passes to Next when true. Otherwise routes to the false node (or ends the branch).</div>
          </div>
          <div className="form-group" style={{ marginBottom: 0 }}>
            <label className="form-label">On False → Node ID (optional)</label>
            <input className="input" value={d.config?.on_false || ''} placeholder="node id (blank = drop)"
              onChange={e => onChange({ config: { ...(d.config || {}), on_false: e.target.value } })} />
          </div>
        </>
      )}

      {node.type === 'wait' && (
        <>
          <div className="divider" />
          <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.1em' }}>Wait</div>
          <div className="form-group" style={{ marginBottom: 0 }}>
            <label className="form-label">Mode</label>
            <select className="select" value={d.config?.mode || 'duration'}
              onChange={e => onChange({ config: { ...(d.config || {}), mode: e.target.value } })}>
              <option value="duration">For a duration</option>
              <option value="until">Until a timestamp</option>
            </select>
          </div>
          {(d.config?.mode || 'duration') === 'duration' ? (
            <div style={{ display: 'flex', gap: 8 }}>
              <div className="form-group" style={{ marginBottom: 0, flex: 1 }}>
                <label className="form-label">Amount</label>
                <input className="input" type="number" min={1} value={d.config?.seconds || 60}
                  onChange={e => onChange({ config: { ...(d.config || {}), seconds: parseInt(e.target.value) } })} />
              </div>
              <div className="form-group" style={{ marginBottom: 0, flex: 1 }}>
                <label className="form-label">Unit</label>
                <select className="select" value={d.config?.unit || 'seconds'}
                  onChange={e => onChange({ config: { ...(d.config || {}), unit: e.target.value } })}>
                  <option value="seconds">seconds</option>
                  <option value="minutes">minutes</option>
                  <option value="hours">hours</option>
                  <option value="days">days</option>
                </select>
              </div>
            </div>
          ) : (
            <div className="form-group" style={{ marginBottom: 0 }}>
              <label className="form-label">Until (RFC3339 or expression)</label>
              <input className="input" value={d.config?.until || ''} placeholder="{{ dateadd($now, 3, 'days') }}"
                onChange={e => onChange({ config: { ...(d.config || {}), until: e.target.value } })} />
            </div>
          )}
          <div className="form-hint">The run pauses durably and resumes automatically when the time arrives — survives restarts.</div>
        </>
      )}

      {node.type === 'merge' && (
        <>
          <div className="divider" />
          <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.1em' }}>Merge</div>
          <div className="form-group" style={{ marginBottom: 0 }}>
            <label className="form-label">Source Node IDs (comma-separated)</label>
            <input className="input" value={(d.config?.sources || []).join(', ')} placeholder="nodeA, nodeB"
              onChange={e => onChange({ config: { ...(d.config || {}), sources: e.target.value.split(',').map(s => s.trim()).filter(Boolean) } })} />
          </div>
          <div className="form-group" style={{ marginBottom: 0 }}>
            <label className="form-label">Mode</label>
            <select className="select" value={d.config?.mode || 'by_source'}
              onChange={e => onChange({ config: { ...(d.config || {}), mode: e.target.value } })}>
              <option value="by_source">Keyed by source id</option>
              <option value="combine">Combine into one object</option>
            </select>
          </div>
        </>
      )}

      {node.type === 'transform' && (
        <>
          <div className="divider" />
          <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.1em' }}>Transform</div>
          <ToolInputsEditor d={d} onChange={onChange} label="Output Mapping" previewCtx={previewCtx} />
        </>
      )}

      {node.type === 'end' && (
        <>
          <div className="divider" />
          <div className="form-group" style={{ marginBottom: 0 }}>
            <label className="form-label">Outcome Label</label>
            <select className="select" value={d.outcome || 'COMPLETED'} onChange={e => onChange({ outcome: e.target.value })}>
              <option value="APPROVED">APPROVED</option>
              <option value="REJECTED">REJECTED</option>
              <option value="COMPLETED">COMPLETED</option>
              <option value="ESCALATED">ESCALATED</option>
            </select>
          </div>
        </>
      )}

      {node.type === 'tool_call' && (
        <ToolCallEditor d={d} onChange={onChange} connectorOpts={connectorOpts} previewCtx={previewCtx} />
      )}

      {node.type === 'agent_call' && (
        <>
          <div className="divider" />
          <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.1em' }}>Agent Configuration</div>
          <div className="form-group" style={{ marginBottom: 0 }}>
            <label className="form-label">Agent</label>
            {agentOpts.length > 0 ? (
              <select className="select" value={d.config?.agent_id || ''}
                onChange={e => onChange({ config: { ...(d.config || {}), agent_id: e.target.value } })}>
                <option value="">Select a registered agent…</option>
                {agentOpts.map(a => <option key={a.id} value={a.id}>{a.name}</option>)}
              </select>
            ) : (
              <>
                <input className="input" value={d.config?.agent_id || ''} placeholder="Registered agent id"
                  onChange={e => onChange({ config: { ...(d.config || {}), agent_id: e.target.value } })} />
                <div className="form-hint">No agents registered yet. Add one on the Agents page, then select it here.</div>
              </>
            )}
          </div>
          <div className="form-group" style={{ marginBottom: 0 }}>
            <label className="form-label">On Error → Node ID (optional)</label>
            <input className="input" value={d.config?.on_error || ''} placeholder="fallback node id"
              onChange={e => onChange({ config: { ...(d.config || {}), on_error: e.target.value } })} />
          </div>
          <div style={{ display: 'flex', gap: 8 }}>
            <div className="form-group" style={{ marginBottom: 0, flex: 1 }}>
              <label className="form-label">Timeout (s)</label>
              <input className="input" type="number" min={1} placeholder="30" value={d.config?.timeout_seconds ?? ''}
                onChange={e => onChange({ config: { ...(d.config || {}), timeout_seconds: e.target.value === '' ? undefined : parseInt(e.target.value) } })} />
            </div>
            <div className="form-group" style={{ marginBottom: 0, flex: 1 }}>
              <label className="form-label">Output Path</label>
              <input className="input" value={d.config?.output_path || ''} placeholder="result.data"
                onChange={e => onChange({ config: { ...(d.config || {}), output_path: e.target.value } })} />
            </div>
          </div>
          <ToolInputsEditor d={d} onChange={onChange} label="Agent Inputs" previewCtx={previewCtx} />
        </>
      )}

      {node.type === 'trigger' && (
        <>
          <div className="divider" />
          <TriggerConfigEditor d={d} onChange={onChange} workflowId={workflowId} previewCtx={previewCtx} />
          <TriggerSchemaEditor d={d} onChange={onChange} />
        </>
      )}

      <div className="divider" />
      <div className="form-group" style={{ marginBottom: 0 }}>
        <label className="form-label">Next Node ID</label>
        <input className="input" value={d.next || ''} placeholder="node id (or use edges)"
          onChange={e => onChange({ next: e.target.value })} />
        <div className="form-hint">You can also connect nodes visually using the handles</div>
      </div>

      {['ai_decision', 'tool_call', 'agent_call', 'transform'].includes(node.type) && (
        <ExecutionPolicyEditor d={d} onChange={onChange} nodeType={node.type} />
      )}
    </div>
  );
}

// Per-node reliability controls: retries, backoff, timeout, continue-on-error.
// These map directly to the engine's nodePolicy. Network-bound nodes default to
// 2 retries / 45s timeout server-side; leaving fields blank uses those defaults.
function ExecutionPolicyEditor({ d, onChange, nodeType }) {
  const cfg = d.config || {};
  const set = (k, v) => onChange({ config: { ...cfg, [k]: v } });
  const defaultRetries = ['ai_decision', 'tool_call', 'agent_call'].includes(nodeType) ? 2 : 0;
  const defaultTimeout = ['ai_decision', 'tool_call', 'agent_call'].includes(nodeType) ? 45 : 0;
  return (
    <>
      <div className="divider" />
      <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.1em' }}>Reliability</div>
      <div style={{ display: 'flex', gap: 8 }}>
        <div className="form-group" style={{ marginBottom: 0, flex: 1 }}>
          <label className="form-label">Retries</label>
          <input className="input" type="number" min={0} max={10}
            value={cfg.retries ?? defaultRetries}
            onChange={e => set('retries', parseInt(e.target.value, 10) || 0)} />
        </div>
        <div className="form-group" style={{ marginBottom: 0, flex: 1 }}>
          <label className="form-label">Retry delay (s)</label>
          <input className="input" type="number" min={0} step={0.5}
            value={cfg.retry_delay ?? 2}
            onChange={e => set('retry_delay', parseFloat(e.target.value) || 0)} />
        </div>
      </div>
      <div className="form-group" style={{ marginBottom: 0 }}>
        <label className="form-label">Timeout (s, 0 = none)</label>
        <input className="input" type="number" min={0}
          value={cfg.timeout ?? defaultTimeout}
          onChange={e => set('timeout', parseFloat(e.target.value) || 0)} />
      </div>
      <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 12, color: 'var(--text-secondary)', cursor: 'pointer' }}>
        <input type="checkbox" checked={!!cfg.continue_on_error}
          onChange={e => set('continue_on_error', e.target.checked)} />
        Continue workflow if this node fails
      </label>
      <div className="form-hint">Retries apply to transient failures (network, provider). Backoff is linear per attempt.</div>
    </>
  );
}

// Collapsible advanced AI config: custom system prompt, extra instructions,
// sampling params, and a per-decision route map. All optional — blank = task default.
function AdvancedAIConfig({ d, onChange }) {
  const [open, setOpen] = useState(false);
  const cfg = d.config || {};
  const set = (k, v) => onChange({ config: { ...cfg, [k]: v } });
  const routeMap = cfg.route_map || {};
  const setRoute = (decision, target) => set('route_map', { ...routeMap, [decision]: target });

  return (
    <div style={{ marginTop: 4 }}>
      <div onClick={() => setOpen(o => !o)} style={{ cursor: 'pointer', fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.08em', padding: '4px 0' }}>
        {open ? '▾' : '▸'} Advanced AI
      </div>
      {open && (
        <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
          <div className="form-group" style={{ marginBottom: 0 }}>
            <label className="form-label">Custom System Prompt (overrides task default)</label>
            <textarea className="textarea" rows={3} value={cfg.system_prompt || ''} placeholder="Leave blank to use the built-in task spec prompt"
              onChange={e => set('system_prompt', e.target.value)} />
          </div>
          <div className="form-group" style={{ marginBottom: 0 }}>
            <label className="form-label">Extra Instructions</label>
            <textarea className="textarea" rows={2} value={cfg.instructions || ''} placeholder="Appended to the prompt (supports {{ templates }})"
              onChange={e => set('instructions', e.target.value)} />
          </div>
          <div style={{ display: 'flex', gap: 8 }}>
            <div className="form-group" style={{ marginBottom: 0, flex: 1 }}>
              <label className="form-label">Temperature</label>
              <input className="input" type="number" min={0} max={2} step={0.1} placeholder="default"
                value={cfg.temperature ?? ''} onChange={e => set('temperature', e.target.value === '' ? undefined : parseFloat(e.target.value))} />
            </div>
            <div className="form-group" style={{ marginBottom: 0, flex: 1 }}>
              <label className="form-label">Max Tokens</label>
              <input className="input" type="number" min={1} placeholder="1024"
                value={cfg.max_tokens ?? ''} onChange={e => set('max_tokens', e.target.value === '' ? undefined : parseInt(e.target.value))} />
            </div>
          </div>
          <div className="form-group" style={{ marginBottom: 0 }}>
            <label className="form-label">Route by Decision (node id per outcome)</label>
            {['APPROVE', 'REJECT', 'ESCALATE'].map(dec => (
              <div key={dec} style={{ display: 'flex', gap: 6, marginBottom: 4, alignItems: 'center' }}>
                <span style={{ flex: '0 0 80px', fontSize: 11, fontFamily: 'var(--font-mono)' }}>{dec}</span>
                <input className="input" style={{ flex: 1, fontSize: 11 }} value={routeMap[dec] || ''} placeholder="next node id"
                  onChange={e => setRoute(dec, e.target.value)} />
              </div>
            ))}
            <div className="form-hint">Overrides the default next/fallback when the AI returns that decision.</div>
          </div>
        </div>
      )}
    </div>
  );
}

// AssignmentsEditor binds a key/value map into node.config[configKey] (used by
// Set and Code nodes). For Code, values are expressions; for Set, templates.
function AssignmentsEditor({ d, onChange, configKey, valuePlaceholder, previewCtx, asExpr }) {
  const cfg = d.config || {};
  const obj = cfg[configKey] || {};
  const entries = Object.entries(obj);
  const setObj = (o) => onChange({ config: { ...cfg, [configKey]: o } });
  const rename = (oldK, newK) => { const n = {}; entries.forEach(([k, v]) => { n[k === oldK ? newK : k] = v; }); setObj(n); };
  const setVal = (k, v) => setObj({ ...obj, [k]: v });
  const remove = (k) => { const n = { ...obj }; delete n[k]; setObj(n); };
  const add = () => { let i = 1, key = 'field'; while (obj[key] !== undefined) key = `field${i++}`; setObj({ ...obj, [key]: '' }); };

  return (
    <div className="form-group" style={{ marginBottom: 0 }}>
      {entries.length === 0 && <div style={{ fontSize: 11, color: 'var(--text-muted)', marginBottom: 6 }}>No fields defined</div>}
      {entries.map(([k, v]) => (
        <div key={k} style={{ marginBottom: 6 }}>
          <div style={{ display: 'flex', gap: 6 }}>
            <input className="input" style={{ flex: '0 0 36%', fontSize: 11, fontFamily: 'var(--font-mono)' }} value={k} onChange={e => rename(k, e.target.value)} placeholder="name" />
            <input className="input" style={{ flex: 1, fontSize: 11, fontFamily: 'var(--font-mono)' }} value={typeof v === 'string' ? v : JSON.stringify(v)} onChange={e => setVal(k, e.target.value)} placeholder={valuePlaceholder} />
            <button className="btn btn-ghost btn-icon btn-sm" onClick={() => remove(k)}><X size={12} /></button>
          </div>
          {!asExpr && <ExprPreview value={v} ctx={previewCtx} />}
        </div>
      ))}
      <button className="btn btn-ghost btn-sm" style={{ width: '100%' }} onClick={add}>+ Add Field</button>
    </div>
  );
}

// The trigger node decides HOW a workflow starts. trigger_type swaps the config
// card (manual / webhook / schedule / polling / email). The engine reconciles
// these from the saved workflow, so the node is the single source of truth.
const TRIGGER_TYPES = [
  { value: 'manual',   label: 'Manual', hint: 'Started by an operator clicking Run, or via the API.' },
  { value: 'webhook',  label: 'Webhook', hint: 'An external system POSTs JSON to start a run.' },
  { value: 'schedule', label: 'Schedule', hint: 'Runs automatically on an interval, daily time, or cron.' },
  { value: 'polling',  label: 'Polling', hint: 'Periodically checks a source and fires a run for each new item.' },
  { value: 'email',    label: 'Email', hint: 'Started by an inbound email (configured via your mail provider).' },
];

function TriggerConfigEditor({ d, onChange, workflowId, previewCtx }) {
  const cfg = d.config || {};
  const tt = cfg.trigger_type || 'manual';
  const set = (k, v) => onChange({ config: { ...cfg, [k]: v } });
  const meta = TRIGGER_TYPES.find(t => t.value === tt) || TRIGGER_TYPES[0];
  const base = (typeof window !== 'undefined') ? window.location.origin : 'https://your-host';

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 10 }}>
      <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.1em' }}>Trigger</div>
      <div className="form-group" style={{ marginBottom: 0 }}>
        <label className="form-label">How does this workflow start?</label>
        <select className="select" value={tt} onChange={e => set('trigger_type', e.target.value)}>
          {TRIGGER_TYPES.map(t => <option key={t.value} value={t.value}>{t.label}</option>)}
        </select>
        <div className="form-hint">{meta.hint}</div>
      </div>

      {tt === 'webhook' && (
        <div className="form-group" style={{ marginBottom: 0 }}>
          <label className="form-label">Webhook URL</label>
          <div className="code-block" style={{ fontSize: 11, userSelect: 'all', wordBreak: 'break-all' }}>
            POST {base}/api/v1/hooks/{workflowId || '{workflow_id}'}
          </div>
          <div className="form-hint">Send a JSON body; it becomes <code style={{ fontFamily: 'var(--font-mono)' }}>{'{{ input }}'}</code>. If <code className="mono">WEBHOOK_SECRET</code> is set, sign the body (HMAC-SHA256) in the <code className="mono">X-KNOTT-Signature</code> header. {workflowId ? '' : '(URL appears after the workflow is saved.)'}</div>
        </div>
      )}

      {tt === 'schedule' && (
        <ScheduleTriggerConfig cfg={cfg} set={set} />
      )}

      {tt === 'polling' && (
        <PollingTriggerConfig cfg={cfg} set={set} previewCtx={previewCtx} />
      )}

      {tt === 'email' && (
        <div className="form-hint">
          Email triggers run when a message arrives at your configured inbound address.
          Point your mail provider's inbound-parse webhook at
          <code className="mono" style={{ wordBreak: 'break-all' }}> {base}/api/v1/hooks/{workflowId || '{workflow_id}'}</code>.
          Native IMAP polling is on the roadmap.
        </div>
      )}
    </div>
  );
}

function ScheduleTriggerConfig({ cfg, set }) {
  const kind = cfg.schedule_kind || 'interval';
  return (
    <>
      <div className="form-group" style={{ marginBottom: 0 }}>
        <label className="form-label">Schedule Type</label>
        <div style={{ display: 'flex', gap: 6 }}>
          {['interval', 'daily', 'cron'].map(k => (
            <button key={k} className={`btn btn-sm ${kind === k ? 'btn-primary' : 'btn-secondary'}`}
              style={{ flex: 1, justifyContent: 'center', textTransform: 'capitalize' }}
              onClick={() => set('schedule_kind', k)}>{k}</button>
          ))}
        </div>
      </div>
      {kind === 'interval' && (
        <div className="form-group" style={{ marginBottom: 0 }}>
          <label className="form-label">Run every (seconds)</label>
          <input className="input" type="number" min={5} value={cfg.schedule_expr || '3600'}
            onChange={e => set('schedule_expr', e.target.value)} />
          <div className="form-hint">e.g. 3600 = hourly, 86400 = daily.</div>
        </div>
      )}
      {kind === 'daily' && (
        <div className="form-group" style={{ marginBottom: 0 }}>
          <label className="form-label">Time of day (UTC, HH:MM)</label>
          <input className="input" type="time" value={cfg.schedule_expr || '09:00'}
            onChange={e => set('schedule_expr', e.target.value)} />
        </div>
      )}
      {kind === 'cron' && (
        <div className="form-group" style={{ marginBottom: 0 }}>
          <label className="form-label">Cron (min hour dom mon dow)</label>
          <input className="input mono" value={cfg.schedule_expr || '0 9 * * 1-5'}
            style={{ fontFamily: 'var(--font-mono)' }}
            onChange={e => set('schedule_expr', e.target.value)} />
          <div className="form-hint">e.g. <code>0 9 * * 1-5</code> = 9am Mon–Fri.</div>
        </div>
      )}
      <div className="form-hint">Saving an active workflow registers this schedule automatically (also visible on the Schedules page).</div>
    </>
  );
}

function PollingTriggerConfig({ cfg, set, previewCtx }) {
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState(null);
  const source = cfg.source || 'http';

  async function testPoll() {
    setBusy(true); setResult(null);
    try { setResult(await triggersApi.testPoll(cfg)); }
    catch (e) { setResult({ ok: false, error: e.message }); }
    finally { setBusy(false); }
  }

  return (
    <>
      <div className="form-group" style={{ marginBottom: 0 }}>
        <label className="form-label">Source</label>
        <select className="select" value={source} onChange={e => set('source', e.target.value)}>
          <option value="http">HTTP endpoint</option>
          <option value="connector">Connector (list operation)</option>
        </select>
      </div>
      {source === 'http' ? (
        <>
          <div className="form-group" style={{ marginBottom: 0 }}>
            <label className="form-label">Poll URL</label>
            <input className="input" value={cfg.url || ''} placeholder="https://api.example.com/new-items"
              onChange={e => set('url', e.target.value)} />
          </div>
          <div className="form-group" style={{ marginBottom: 0 }}>
            <label className="form-label">Auth Credential (optional)</label>
            <input className="input" value={cfg.auth_credential || ''} placeholder="MY_API_KEY (bearer)"
              onChange={e => set('auth_credential', e.target.value)} />
          </div>
        </>
      ) : (
        <>
          <div className="form-group" style={{ marginBottom: 0 }}>
            <label className="form-label">Connector ID</label>
            <input className="input" value={cfg.connector_id || ''} placeholder="airtable"
              onChange={e => set('connector_id', e.target.value)} />
          </div>
          <div className="form-group" style={{ marginBottom: 0 }}>
            <label className="form-label">List Action + Fields</label>
            <input className="input" value={cfg.action || ''} placeholder="list_records"
              onChange={e => set('action', e.target.value)} />
            <div className="form-hint">Add the connector's list fields (e.g. base_id, table) below as inputs.</div>
          </div>
        </>
      )}
      <div className="form-group" style={{ marginBottom: 0 }}>
        <label className="form-label">Items Path</label>
        <input className="input" value={cfg.items_path || ''} placeholder="records (path to the array in the response)"
          onChange={e => set('items_path', e.target.value)} />
      </div>
      <div className="form-group" style={{ marginBottom: 0 }}>
        <label className="form-label">Dedup Key</label>
        <input className="input" value={cfg.dedup_key || ''} placeholder="id (field on each item; blank = whole item)"
          onChange={e => set('dedup_key', e.target.value)} />
        <div className="form-hint">Identifies a unique item so each is processed once. Items become <code style={{ fontFamily: 'var(--font-mono)' }}>{'{{ input.item }}'}</code>.</div>
      </div>
      <div style={{ display: 'flex', gap: 8 }}>
        <div className="form-group" style={{ marginBottom: 0, flex: 1 }}>
          <label className="form-label">Interval (seconds)</label>
          <input className="input" type="number" min={15} value={cfg.poll_interval_secs || 300}
            onChange={e => set('poll_interval_secs', parseInt(e.target.value))} />
        </div>
        <div className="form-group" style={{ marginBottom: 0, flex: 1 }}>
          <label className="form-label">Max per poll</label>
          <input className="input" type="number" min={1} value={cfg.max_per_poll || 25}
            onChange={e => set('max_per_poll', parseInt(e.target.value))} />
        </div>
      </div>
      <label style={{ display: 'flex', alignItems: 'center', gap: 8, fontSize: 12, color: 'var(--text-secondary)', cursor: 'pointer' }}>
        <input type="checkbox" checked={!!cfg.fire_on_first} onChange={e => set('fire_on_first', e.target.checked)} />
        Fire for existing items on first poll (default: only new items after activation)
      </label>
      <button className="btn btn-secondary btn-sm" style={{ width: '100%', justifyContent: 'center' }} onClick={testPoll} disabled={busy}>
        {busy ? <span className="spinner-sm" /> : null} Test Poll
      </button>
      {result && (
        <div style={{ fontSize: 11, fontFamily: 'var(--font-mono)', padding: '8px 10px', borderRadius: 6,
          background: 'var(--bg-secondary)', borderLeft: `3px solid ${result.ok ? 'var(--green)' : 'var(--red)'}`,
          color: result.ok ? 'var(--text-secondary)' : 'var(--red)', maxHeight: 200, overflow: 'auto', wordBreak: 'break-word' }}>
          {result.ok
            ? <>✓ {result.count} item(s) found{result.latency_ms != null ? ` (${result.latency_ms}ms)` : ''}<br />dedup keys: {(result.dedup_keys || []).join(', ').slice(0, 200)}</>
            : <>✗ {result.error}</>}
        </div>
      )}
    </>
  );
}

// Editor for a trigger's declared input schema: each field has a type, a
// required flag, and an optional default. Persisted as config.input_schema and
// enforced by the engine at run start.
function TriggerSchemaEditor({ d, onChange }) {
  const schema = (d.config && d.config.input_schema) || {};
  const entries = Object.entries(schema);
  const setSchema = (s) => onChange({ config: { ...(d.config || {}), input_schema: s } });
  const rename = (oldK, newK) => { const n = {}; entries.forEach(([k, v]) => { n[k === oldK ? newK : k] = v; }); setSchema(n); };
  const setSpec = (k, patch) => setSchema({ ...schema, [k]: { ...(schema[k] || {}), ...patch } });
  const remove = (k) => { const n = { ...schema }; delete n[k]; setSchema(n); };
  const add = () => { let i = 1, key = 'field'; while (schema[key] !== undefined) key = `field${i++}`; setSchema({ ...schema, [key]: { type: 'string', required: false } }); };

  return (
    <div className="form-group" style={{ marginBottom: 0, marginTop: 8 }}>
      <label className="form-label">Input Schema</label>
      {entries.length === 0 && <div style={{ fontSize: 11, color: 'var(--text-muted)', marginBottom: 6 }}>No declared fields — any input is accepted.</div>}
      {entries.map(([k, spec]) => (
        <div key={k} style={{ border: '1px solid var(--border)', borderRadius: 6, padding: 8, marginBottom: 6 }}>
          <div style={{ display: 'flex', gap: 6, marginBottom: 6 }}>
            <input className="input" style={{ flex: 1, fontSize: 11, fontFamily: 'var(--font-mono)' }} value={k} onChange={e => rename(k, e.target.value)} placeholder="field name" />
            <select className="select" style={{ flex: '0 0 90px', fontSize: 11 }} value={spec.type || 'string'} onChange={e => setSpec(k, { type: e.target.value })}>
              <option value="string">string</option>
              <option value="number">number</option>
              <option value="boolean">boolean</option>
              <option value="object">object</option>
            </select>
            <button className="btn btn-ghost btn-icon btn-sm" onClick={() => remove(k)}><X size={12} /></button>
          </div>
          <div style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
            <label style={{ display: 'flex', alignItems: 'center', gap: 4, fontSize: 11, color: 'var(--text-secondary)' }}>
              <input type="checkbox" checked={!!spec.required} onChange={e => setSpec(k, { required: e.target.checked })} /> required
            </label>
            <input className="input" style={{ flex: 1, fontSize: 11 }} value={spec.default ?? ''} placeholder="default (optional)"
              onChange={e => setSpec(k, { default: e.target.value === '' ? undefined : e.target.value })} />
          </div>
        </div>
      ))}
      <button className="btn btn-ghost btn-sm" style={{ width: '100%' }} onClick={add}>+ Add Field</button>
    </div>
  );
}

// Key/value editor bound to node.data.context (the data shown to a human reviewer).
function ContextDataEditor({ d, onChange, previewCtx }) {
  const ctxData = d.context || {};
  const entries = Object.entries(ctxData);
  const setKey = (oldK, newK) => { const n = {}; Object.entries(ctxData).forEach(([k, v]) => { n[k === oldK ? newK : k] = v; }); onChange({ context: n }); };
  const setVal = (k, v) => onChange({ context: { ...ctxData, [k]: v } });
  const remove = (k) => { const n = { ...ctxData }; delete n[k]; onChange({ context: n }); };
  const add = () => { let i = 1, key = 'field'; while (ctxData[key] !== undefined) key = `field${i++}`; onChange({ context: { ...ctxData, [key]: '' } }); };

  return (
    <div className="form-group" style={{ marginBottom: 0 }}>
      <label className="form-label">Context Data (shown to reviewer)</label>
      {entries.length === 0 && <div style={{ fontSize: 11, color: 'var(--text-muted)', marginBottom: 6 }}>No context fields — reviewer sees AI recommendation only</div>}
      {entries.map(([k, v]) => (
        <div key={k} style={{ marginBottom: 6 }}>
          <div style={{ display: 'flex', gap: 6 }}>
            <input className="input" style={{ flex: '0 0 38%', fontSize: 11, fontFamily: 'var(--font-mono)' }} value={k} onChange={e => setKey(k, e.target.value)} placeholder="label" />
            <input className="input" style={{ flex: 1, fontSize: 11, fontFamily: 'var(--font-mono)' }} value={typeof v === 'string' ? v : JSON.stringify(v)} onChange={e => setVal(k, e.target.value)} placeholder="value or {{ template }}" />
            <button className="btn btn-ghost btn-icon btn-sm" onClick={() => remove(k)}><X size={12} /></button>
          </div>
          <ExprPreview value={v} ctx={previewCtx} />
        </div>
      ))}
      <button className="btn btn-ghost btn-sm" style={{ width: '100%' }} onClick={add}>+ Add Context Field</button>
    </div>
  );
}

// Editable key/value list bound to node.data.inputs. Values are passed to the
// connector/agent at runtime with {{ template }} resolution against run context.
function ToolInputsEditor({ d, onChange, label = 'Inputs', previewCtx }) {
  const inputs = d.inputs || {};
  const entries = Object.entries(inputs);

  function setKey(oldKey, newKey) {
    const next = {};
    Object.entries(inputs).forEach(([k, v]) => { next[k === oldKey ? newKey : k] = v; });
    onChange({ inputs: next });
  }
  function setVal(key, val) {
    onChange({ inputs: { ...inputs, [key]: val } });
  }
  function remove(key) {
    const next = { ...inputs };
    delete next[key];
    onChange({ inputs: next });
  }
  function add() {
    let i = 1, key = 'key';
    while (inputs[key] !== undefined) { key = `key${i++}`; }
    onChange({ inputs: { ...inputs, [key]: '' } });
  }

  return (
    <div className="form-group" style={{ marginBottom: 0 }}>
      <label className="form-label">{label}</label>
      {entries.length === 0 && (
        <div style={{ fontSize: 11, color: 'var(--text-muted)', marginBottom: 6 }}>No inputs defined</div>
      )}
      {entries.map(([k, v]) => (
        <div key={k} style={{ marginBottom: 6 }}>
          <div style={{ display: 'flex', gap: 6 }}>
            <input className="input" style={{ flex: '0 0 38%', fontSize: 11, fontFamily: 'var(--font-mono)' }}
              value={k} onChange={e => setKey(k, e.target.value)} placeholder="name" />
            <input className="input" style={{ flex: 1, fontSize: 11, fontFamily: 'var(--font-mono)' }}
              value={typeof v === 'string' ? v : JSON.stringify(v)}
              onChange={e => setVal(k, e.target.value)} placeholder="value or {{ template }}" />
            <button className="btn btn-ghost btn-icon btn-sm" onClick={() => remove(k)} title="Remove"><X size={12} /></button>
          </div>
          <ExprPreview value={v} ctx={previewCtx} />
        </div>
      ))}
      <button className="btn btn-ghost btn-sm" style={{ width: '100%' }} onClick={add}>+ Add Input</button>
    </div>
  );
}

// Live resolution chip for a field that may contain {{ templates }}. Shows the
// value the engine would compute given the current Test Data, or flags any
// reference that doesn't resolve. Renders nothing for plain (non-template) values.
function ExprPreview({ value, ctx }) {
  if (!hasTemplate(value)) return null;
  const { value: resolved, ok, missing } = resolveTemplate(value, ctx);
  const text = typeof resolved === 'string' ? resolved : JSON.stringify(resolved);
  return (
    <div style={{
      fontSize: 10, fontFamily: 'var(--font-mono)', marginTop: 3, padding: '3px 8px',
      borderRadius: 4, background: 'var(--bg-secondary)',
      borderLeft: `2px solid ${ok ? 'var(--green)' : 'var(--yellow)'}`,
      color: ok ? 'var(--text-secondary)' : 'var(--yellow)',
      wordBreak: 'break-all',
    }}>
      {ok
        ? <>→ {text === '' ? <em style={{ color: 'var(--text-muted)' }}>(empty)</em> : text}</>
        : <>unresolved: {missing.join(', ')}</>}
    </div>
  );
}

// Collapsible JSON editor for the sample trigger input that drives live expression
// previews. Persisted only in component state (the designer owns it); shows a
// validity indicator so authors know when their JSON is parseable.
function TestDataPanel({ testInput, setTestInput, previewCtx }) {
  const [open, setOpen] = useState(false);
  let valid = true;
  try { JSON.parse(testInput); } catch { valid = false; }
  const stepKeys = Object.keys(previewCtx || {}).filter(k => k.startsWith('steps.'));

  return (
    <div style={{ border: '1px solid var(--border)', borderRadius: 6, padding: '8px 10px', background: 'var(--bg-secondary)' }}>
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', cursor: 'pointer' }}
        onClick={() => setOpen(o => !o)}>
        <span style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.08em' }}>
          Test Data
        </span>
        <span style={{ fontSize: 10, color: valid ? 'var(--green)' : 'var(--yellow)' }}>
          {valid ? '● valid JSON' : '● invalid JSON'} {open ? '▾' : '▸'}
        </span>
      </div>
      {open && (
        <div style={{ marginTop: 8 }}>
          <div className="form-hint" style={{ marginTop: 0, marginBottom: 4 }}>
            Sample <code style={{ fontFamily: 'var(--font-mono)' }}>input</code> for previewing <code style={{ fontFamily: 'var(--font-mono)' }}>{'{{ }}'}</code> expressions. Not saved with the workflow.
          </div>
          <textarea className="textarea" rows={5} value={testInput}
            onChange={e => setTestInput(e.target.value)}
            style={{ fontFamily: 'var(--font-mono)', fontSize: 11 }} />
          {stepKeys.length > 0 && (
            <div className="form-hint" style={{ marginTop: 4 }}>
              Available step refs: {stepKeys.map(k => (
                <code key={k} style={{ fontFamily: 'var(--font-mono)', marginRight: 6 }}>{k}.output</code>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

// Maps a connector to (a) the engine connector key and (b) the input fields the
// operator should fill in. Field values support {{ template }} resolution at runtime.
// Credentials are NEVER entered here — they come from server-side env vars.
const CONNECTOR_SCHEMA = {
  webhook:  { key: 'webhook',  label: 'HTTP / Webhook', fields: [
    { name: 'url', label: 'URL', placeholder: 'https://api.example.com/path', required: true },
    { name: 'method', label: 'Method', placeholder: 'POST', type: 'select', options: ['GET','POST','PUT','PATCH','DELETE'] },
  ], creds: [], http: true },
  slack:    { key: 'slack',    label: 'Slack', fields: [
    { name: 'channel', label: 'Channel', placeholder: '#alerts' },
    { name: 'text', label: 'Message', placeholder: 'Run {{ input.id }} needs review', textarea: true, required: true },
  ], creds: ['SLACK_WEBHOOK_URL (or SLACK_BOT_TOKEN)'] },
  sendgrid: { key: 'sendgrid', label: 'SendGrid Email', fields: [
    { name: 'to', label: 'To', placeholder: 'user@example.com', required: true },
    { name: 'subject', label: 'Subject', placeholder: 'Notification' },
    { name: 'body', label: 'Body', placeholder: 'Message body…', textarea: true },
  ], creds: ['SENDGRID_API_KEY', 'SENDGRID_FROM'] },
  twilio:   { key: 'twilio',   label: 'Twilio SMS', fields: [
    { name: 'to', label: 'To Number', placeholder: '+15551234567', required: true },
    { name: 'body', label: 'Message', placeholder: 'Your code is {{ input.code }}', textarea: true, required: true },
  ], creds: ['TWILIO_ACCOUNT_SID', 'TWILIO_AUTH_TOKEN', 'TWILIO_FROM_NUMBER'] },
  telegram: { key: 'telegram', label: 'Telegram', fields: [
    { name: 'chat_id', label: 'Chat ID', placeholder: '@channel or numeric id', required: true },
    { name: 'text', label: 'Message', placeholder: 'Alert: {{ input.summary }}', textarea: true, required: true },
    { name: 'parse_mode', label: 'Parse Mode', type: 'select', options: ['', 'Markdown', 'HTML'] },
  ], creds: ['TELEGRAM_BOT_TOKEN'] },
  discord:  { key: 'discord',  label: 'Discord', fields: [
    { name: 'content', label: 'Message', placeholder: 'Deploy finished for {{ input.app }}', textarea: true, required: true },
    { name: 'username', label: 'Override Username', placeholder: 'KNOTT Bot' },
  ], creds: ['DISCORD_WEBHOOK_URL'] },
  github:   { key: 'github',   label: 'GitHub', creds: ['GITHUB_TOKEN'], operations: [
    { value: 'create_issue', label: 'Create Issue', fields: [
      { name: 'repo', label: 'Repository', placeholder: 'owner/name', required: true },
      { name: 'title', label: 'Issue Title', placeholder: 'Alert from {{ input.source }}', required: true },
      { name: 'body', label: 'Issue Body', placeholder: 'Details… (supports {{ templates }})', textarea: true },
      { name: 'labels', label: 'Labels', placeholder: 'bug, automated (comma-separated)' },
    ]},
    { value: 'comment_issue', label: 'Comment on Issue', fields: [
      { name: 'repo', label: 'Repository', placeholder: 'owner/name', required: true },
      { name: 'issue_number', label: 'Issue Number', placeholder: '42', required: true },
      { name: 'body', label: 'Comment', placeholder: 'Update: {{ input.note }}', textarea: true, required: true },
    ]},
    { value: 'close_issue', label: 'Close Issue', fields: [
      { name: 'repo', label: 'Repository', placeholder: 'owner/name', required: true },
      { name: 'issue_number', label: 'Issue Number', placeholder: '42', required: true },
    ]},
    { value: 'get_issue', label: 'Get Issue', fields: [
      { name: 'repo', label: 'Repository', placeholder: 'owner/name', required: true },
      { name: 'issue_number', label: 'Issue Number', placeholder: '42', required: true },
    ]},
    { value: 'list_issues', label: 'List Issues', fields: [
      { name: 'repo', label: 'Repository', placeholder: 'owner/name', required: true },
      { name: 'state', label: 'State', type: 'select', options: ['open', 'closed', 'all'] },
    ]},
  ]},
  jira:     { key: 'jira',     label: 'Jira', creds: ['JIRA_EMAIL', 'JIRA_API_TOKEN', 'JIRA_BASE_URL'], operations: [
    { value: 'create_issue', label: 'Create Issue', fields: [
      { name: 'base_url', label: 'Site URL', placeholder: 'https://acme.atlassian.net', required: true },
      { name: 'project_key', label: 'Project Key', placeholder: 'OPS', required: true },
      { name: 'summary', label: 'Summary', placeholder: 'Investigate {{ input.id }}', required: true },
      { name: 'issue_type', label: 'Issue Type', placeholder: 'Task' },
      { name: 'description', label: 'Description', placeholder: 'Details…', textarea: true },
    ]},
    { value: 'comment_issue', label: 'Comment on Issue', fields: [
      { name: 'base_url', label: 'Site URL', placeholder: 'https://acme.atlassian.net', required: true },
      { name: 'issue_key', label: 'Issue Key', placeholder: 'OPS-123', required: true },
      { name: 'body', label: 'Comment', placeholder: '{{ input.note }}', textarea: true, required: true },
    ]},
  ]},
  airtable: { key: 'airtable', label: 'Airtable', creds: ['AIRTABLE_TOKEN'], operations: [
    { value: 'create_record', label: 'Create Record', fields: [
      { name: 'base_id', label: 'Base ID', placeholder: 'appXXXXXXXX', required: true },
      { name: 'table', label: 'Table', placeholder: 'Leads', required: true },
      { name: 'fields', label: 'Fields (JSON)', placeholder: '{ "Name": "{{ input.name }}", "Score": {{ input.score }} }', textarea: true, required: true },
    ]},
    { value: 'update_record', label: 'Update Record', fields: [
      { name: 'base_id', label: 'Base ID', placeholder: 'appXXXXXXXX', required: true },
      { name: 'table', label: 'Table', placeholder: 'Leads', required: true },
      { name: 'record_id', label: 'Record ID', placeholder: 'recXXXXXXXX', required: true },
      { name: 'fields', label: 'Fields (JSON)', placeholder: '{ "Status": "Won" }', textarea: true, required: true },
    ]},
    { value: 'list_records', label: 'List Records', fields: [
      { name: 'base_id', label: 'Base ID', placeholder: 'appXXXXXXXX', required: true },
      { name: 'table', label: 'Table', placeholder: 'Leads', required: true },
      { name: 'max_records', label: 'Max Records', placeholder: '100' },
    ]},
  ]},
  notion:   { key: 'notion',   label: 'Notion', creds: ['NOTION_TOKEN'], operations: [
    { value: 'create_page', label: 'Create Page', fields: [
      { name: 'database_id', label: 'Database ID', placeholder: '32-char database id', required: true },
      { name: 'title', label: 'Page Title', placeholder: '{{ input.title }}', required: true },
      { name: 'title_property', label: 'Title Property', placeholder: 'Name' },
    ]},
    { value: 'query_database', label: 'Query Database', fields: [
      { name: 'database_id', label: 'Database ID', placeholder: '32-char database id', required: true },
      { name: 'filter', label: 'Filter (JSON, optional)', placeholder: '{ "property": "Status", "select": { "equals": "Open" } }', textarea: true },
    ]},
  ]},
  hubspot:  { key: 'hubspot',  label: 'HubSpot CRM', creds: ['HUBSPOT_TOKEN'], operations: [
    { value: 'create_contact', label: 'Create Contact', fields: [
      { name: 'email', label: 'Email', placeholder: '{{ input.email }}', required: true },
      { name: 'firstname', label: 'First Name', placeholder: '{{ input.first }}' },
      { name: 'lastname', label: 'Last Name', placeholder: '{{ input.last }}' },
      { name: 'properties', label: 'Extra Properties (JSON)', placeholder: '{ "company": "Acme" }', textarea: true },
    ]},
    { value: 'create_deal', label: 'Create Deal', fields: [
      { name: 'dealname', label: 'Deal Name', placeholder: '{{ input.name }}', required: true },
      { name: 'amount', label: 'Amount', placeholder: '5000' },
      { name: 'properties', label: 'Extra Properties (JSON)', placeholder: '{ "dealstage": "qualified" }', textarea: true },
    ]},
  ]},
  google_sheets: { key: 'google_sheets', label: 'Google Sheets', creds: ['GOOGLE_CLIENT_ID', 'GOOGLE_CLIENT_SECRET', 'GOOGLE_REFRESH_TOKEN (or GOOGLE_ACCESS_TOKEN)'], operations: [
    { value: 'append_row', label: 'Append Row', fields: [
      { name: 'spreadsheet_id', label: 'Spreadsheet ID', placeholder: 'long sheet id from URL', required: true },
      { name: 'range', label: 'Range', placeholder: 'Sheet1!A1' },
      { name: 'values', label: 'Row Values (JSON array)', placeholder: '["{{ input.name }}", {{ input.score }}]', textarea: true, required: true },
    ]},
    { value: 'read_range', label: 'Read Range', fields: [
      { name: 'spreadsheet_id', label: 'Spreadsheet ID', placeholder: 'long sheet id from URL', required: true },
      { name: 'range', label: 'Range', placeholder: 'Sheet1!A1:C10', required: true },
    ]},
  ]},
  google_calendar: { key: 'google_calendar', label: 'Google Calendar', creds: ['GOOGLE_CLIENT_ID', 'GOOGLE_CLIENT_SECRET', 'GOOGLE_REFRESH_TOKEN'], fields: [
    { name: 'calendar_id', label: 'Calendar ID', placeholder: 'primary' },
    { name: 'summary', label: 'Event Title', placeholder: '{{ input.title }}', required: true },
    { name: 'start', label: 'Start (RFC3339)', placeholder: '2026-07-01T10:00:00Z', required: true },
    { name: 'end', label: 'End (RFC3339)', placeholder: '2026-07-01T10:30:00Z' },
    { name: 'description', label: 'Description', textarea: true },
  ]},
  teams: { key: 'teams', label: 'Microsoft Teams', creds: ['TEAMS_WEBHOOK_URL'], fields: [
    { name: 'title', label: 'Title', placeholder: 'Deployment' },
    { name: 'text', label: 'Message', placeholder: 'Build {{ input.id }} succeeded', textarea: true, required: true },
  ]},
  stripe: { key: 'stripe', label: 'Stripe', creds: ['STRIPE_SECRET_KEY'], operations: [
    { value: 'create_customer', label: 'Create Customer', fields: [
      { name: 'email', label: 'Email', placeholder: '{{ input.email }}' },
      { name: 'name', label: 'Name', placeholder: '{{ input.name }}' },
    ]},
    { value: 'create_charge', label: 'Create Charge', fields: [
      { name: 'amount', label: 'Amount (cents)', placeholder: '2000', required: true },
      { name: 'currency', label: 'Currency', placeholder: 'usd' },
      { name: 'customer', label: 'Customer ID', placeholder: 'cus_...' },
    ]},
  ]},
  database: { key: 'database', label: 'Database (SQL)', creds: ['DATABASE_DSN'], operations: [
    { value: 'query', label: 'Query (SELECT)', fields: [
      { name: 'driver', label: 'Driver', type: 'select', options: ['sqlite', 'postgres', 'mysql'] },
      { name: 'sql', label: 'SQL', placeholder: 'SELECT * FROM leads WHERE score > ?', textarea: true, required: true },
      { name: 'params', label: 'Params (JSON array)', placeholder: '[80]' },
    ]},
    { value: 'exec', label: 'Execute (INSERT/UPDATE/DELETE)', fields: [
      { name: 'driver', label: 'Driver', type: 'select', options: ['sqlite', 'postgres', 'mysql'] },
      { name: 'sql', label: 'SQL', placeholder: 'INSERT INTO leads(name, score) VALUES(?, ?)', textarea: true, required: true },
      { name: 'params', label: 'Params (JSON array)', placeholder: '["{{ input.name }}", {{ input.score }}]' },
    ]},
  ]},

  // ── Broadened connector coverage ──────────────────────────────────────────
  linear:   { key: 'linear',   label: 'Linear', creds: ['LINEAR_API_KEY'], fields: [
    { name: 'team_id', label: 'Team ID', placeholder: 'team UUID', required: true },
    { name: 'title', label: 'Title', placeholder: 'Bug: {{ input.summary }}', required: true },
    { name: 'description', label: 'Description', placeholder: 'Details…', textarea: true },
  ]},
  trello:   { key: 'trello',   label: 'Trello', creds: ['TRELLO_KEY', 'TRELLO_TOKEN'], fields: [
    { name: 'list_id', label: 'List ID', placeholder: 'list id', required: true },
    { name: 'name', label: 'Card Name', placeholder: 'New card {{ input.id }}', required: true },
    { name: 'desc', label: 'Description', placeholder: 'Details…', textarea: true },
  ]},
  asana:    { key: 'asana',    label: 'Asana', creds: ['ASANA_TOKEN'], fields: [
    { name: 'project_id', label: 'Project ID', placeholder: 'project gid' },
    { name: 'name', label: 'Task Name', placeholder: 'Follow up with {{ input.name }}', required: true },
    { name: 'notes', label: 'Notes', placeholder: 'Details…', textarea: true },
  ]},
  clickup:  { key: 'clickup',  label: 'ClickUp', creds: ['CLICKUP_TOKEN'], fields: [
    { name: 'list_id', label: 'List ID', placeholder: 'list id', required: true },
    { name: 'name', label: 'Task Name', placeholder: 'New task', required: true },
    { name: 'description', label: 'Description', placeholder: 'Details…', textarea: true },
  ]},
  pagerduty:{ key: 'pagerduty', label: 'PagerDuty', creds: ['PAGERDUTY_ROUTING_KEY'], fields: [
    { name: 'summary', label: 'Summary', placeholder: 'Incident: {{ input.summary }}', required: true },
    { name: 'severity', label: 'Severity', type: 'select', options: ['critical', 'error', 'warning', 'info'] },
    { name: 'source', label: 'Source', placeholder: 'KNOTT' },
  ]},
  mattermost:{ key: 'mattermost', label: 'Mattermost', creds: ['MATTERMOST_WEBHOOK_URL'], fields: [
    { name: 'text', label: 'Message', placeholder: 'Alert: {{ input.summary }}', textarea: true, required: true },
    { name: 'channel', label: 'Channel (optional)', placeholder: 'town-square' },
  ]},
  zendesk:  { key: 'zendesk',  label: 'Zendesk', creds: ['ZENDESK_EMAIL', 'ZENDESK_API_TOKEN', 'ZENDESK_BASE_URL'], fields: [
    { name: 'base_url', label: 'Site URL', placeholder: 'https://acme.zendesk.com', required: true },
    { name: 'subject', label: 'Subject', placeholder: 'Issue from {{ input.customer }}', required: true },
    { name: 'comment', label: 'Body', placeholder: 'Ticket details…', textarea: true, required: true },
    { name: 'priority', label: 'Priority', type: 'select', options: ['', 'low', 'normal', 'high', 'urgent'] },
  ]},
  shopify:  { key: 'shopify',  label: 'Shopify', creds: ['SHOPIFY_ACCESS_TOKEN', 'SHOPIFY_STORE_URL'], operations: [
    { value: 'list_products', label: 'List Products', fields: [
      { name: 'base_url', label: 'Store URL', placeholder: 'https://store.myshopify.com', required: true },
    ]},
    { value: 'create_customer', label: 'Create Customer', fields: [
      { name: 'base_url', label: 'Store URL', placeholder: 'https://store.myshopify.com', required: true },
      { name: 'email', label: 'Email', placeholder: '{{ input.email }}', required: true },
      { name: 'firstname', label: 'First Name', placeholder: '{{ input.first }}' },
      { name: 'lastname', label: 'Last Name', placeholder: '{{ input.last }}' },
    ]},
  ]},
  mailchimp:{ key: 'mailchimp', label: 'Mailchimp', creds: ['MAILCHIMP_API_KEY'], fields: [
    { name: 'list_id', label: 'Audience ID', placeholder: 'list id', required: true },
    { name: 'email', label: 'Email', placeholder: '{{ input.email }}', required: true },
    { name: 'status', label: 'Status', type: 'select', options: ['subscribed', 'pending'] },
  ]},
  openai:   { key: 'openai',   label: 'OpenAI', creds: ['OPENAI_API_KEY'], fields: [
    { name: 'model', label: 'Model', placeholder: 'gpt-4o-mini' },
    { name: 'system', label: 'System Prompt', placeholder: 'You are a helpful assistant.', textarea: true },
    { name: 'prompt', label: 'Prompt', placeholder: 'Summarize: {{ input.text }}', textarea: true, required: true },
  ]},
  pushover: { key: 'pushover', label: 'Pushover', creds: ['PUSHOVER_TOKEN', 'PUSHOVER_USER'], fields: [
    { name: 'title', label: 'Title', placeholder: 'KNOTT Alert' },
    { name: 'message', label: 'Message', placeholder: '{{ input.summary }}', textarea: true, required: true },
  ]},
  graphql:  { key: 'graphql',  label: 'GraphQL', creds: [], fields: [
    { name: 'url', label: 'Endpoint URL', placeholder: 'https://api.example.com/graphql', required: true },
    { name: 'query', label: 'Query', placeholder: '{ viewer { login } }', textarea: true, required: true },
    { name: 'variables', label: 'Variables (JSON)', placeholder: '{"id": "{{ input.id }}"}' },
    { name: 'auth_token', label: 'Bearer Token (optional)', placeholder: 'secret://MY_TOKEN' },
  ]},
  gitlab:   { key: 'gitlab',   label: 'GitLab', creds: ['GITLAB_TOKEN'], fields: [
    { name: 'project_id', label: 'Project ID', placeholder: '12345', required: true },
    { name: 'title', label: 'Issue Title', placeholder: 'Bug: {{ input.summary }}', required: true },
    { name: 'description', label: 'Description', placeholder: 'Details…', textarea: true },
  ]},
  monday:   { key: 'monday',   label: 'Monday.com', creds: ['MONDAY_TOKEN'], fields: [
    { name: 'board_id', label: 'Board ID', placeholder: '1234567', required: true },
    { name: 'item_name', label: 'Item Name', placeholder: 'New item {{ input.id }}', required: true },
  ]},
  freshdesk:{ key: 'freshdesk', label: 'Freshdesk', creds: ['FRESHDESK_API_KEY', 'FRESHDESK_BASE_URL'], fields: [
    { name: 'base_url', label: 'Site URL', placeholder: 'https://acme.freshdesk.com', required: true },
    { name: 'subject', label: 'Subject', placeholder: 'Issue: {{ input.summary }}', required: true },
    { name: 'description', label: 'Description', placeholder: 'Details…', textarea: true, required: true },
    { name: 'email', label: 'Requester Email', placeholder: '{{ input.email }}', required: true },
  ]},
  intercom: { key: 'intercom', label: 'Intercom', creds: ['INTERCOM_TOKEN'], fields: [
    { name: 'email', label: 'Email', placeholder: '{{ input.email }}', required: true },
    { name: 'name', label: 'Name', placeholder: '{{ input.name }}' },
  ]},
  ms_graph: { key: 'ms_graph', label: 'Microsoft Outlook', creds: ['MS_GRAPH_TOKEN'], fields: [
    { name: 'to', label: 'To', placeholder: '{{ input.email }}', required: true },
    { name: 'subject', label: 'Subject', placeholder: 'Notification', required: true },
    { name: 'body', label: 'Body', placeholder: 'Message…', textarea: true },
  ]},
  whatsapp: { key: 'whatsapp', label: 'WhatsApp', creds: ['WHATSAPP_TOKEN', 'WHATSAPP_PHONE_ID'], fields: [
    { name: 'phone_number_id', label: 'Phone Number ID', placeholder: 'from Meta dashboard' },
    { name: 'to', label: 'To (number)', placeholder: '15551234567', required: true },
    { name: 'text', label: 'Message', placeholder: 'Hi {{ input.name }}', textarea: true, required: true },
  ]},
  coda:     { key: 'coda',     label: 'Coda', creds: ['CODA_TOKEN'], fields: [
    { name: 'doc_id', label: 'Doc ID', placeholder: 'doc id', required: true },
    { name: 'table_id', label: 'Table ID', placeholder: 'grid-xxxx', required: true },
    { name: 'cells', label: 'Cells (JSON)', placeholder: '{"Name":"{{ input.name }}"}', textarea: true, required: true },
  ]},
  close:    { key: 'close',    label: 'Close CRM', creds: ['CLOSE_API_KEY'], fields: [
    { name: 'name', label: 'Lead Name', placeholder: '{{ input.company }}', required: true },
  ]},
  calendly: { key: 'calendly', label: 'Calendly', creds: ['CALENDLY_TOKEN'], fields: [] },
  servicenow:{ key: 'servicenow', label: 'ServiceNow', creds: ['SERVICENOW_USER', 'SERVICENOW_PASSWORD', 'SERVICENOW_BASE_URL'], fields: [
    { name: 'base_url', label: 'Instance URL', placeholder: 'https://acme.service-now.com', required: true },
    { name: 'short_description', label: 'Short Description', placeholder: 'Incident: {{ input.summary }}', required: true },
    { name: 'description', label: 'Description', placeholder: 'Details…', textarea: true },
  ]},
};

// Best-effort mapping from an installed connector record to a schema key.
function connectorKeyFor(c) {
  const n = (c.name || '').toLowerCase();
  if (n.includes('webhook') || n.includes('http')) return 'webhook';
  if (n.includes('slack')) return 'slack';
  if (n.includes('telegram')) return 'telegram';
  if (n.includes('discord')) return 'discord';
  if (n.includes('github')) return 'github';
  if (n.includes('jira')) return 'jira';
  if (n.includes('airtable')) return 'airtable';
  if (n.includes('notion')) return 'notion';
  if (n.includes('hubspot')) return 'hubspot';
  if (n.includes('calendar')) return 'google_calendar';
  if (n.includes('google') || n.includes('sheet')) return 'google_sheets';
  if (n.includes('teams')) return 'teams';
  if (n.includes('stripe')) return 'stripe';
  if (n.includes('database') || n.includes('sql') || n.includes('postgres')) return 'database';
  if (n.includes('sendgrid') || n.includes('email')) return 'sendgrid';
  if (n.includes('twilio') || n.includes('sms')) return 'twilio';
  if (n.includes('linear')) return 'linear';
  if (n.includes('trello')) return 'trello';
  if (n.includes('asana')) return 'asana';
  if (n.includes('clickup')) return 'clickup';
  if (n.includes('pagerduty')) return 'pagerduty';
  if (n.includes('mattermost')) return 'mattermost';
  if (n.includes('zendesk')) return 'zendesk';
  if (n.includes('shopify')) return 'shopify';
  if (n.includes('mailchimp')) return 'mailchimp';
  if (n.includes('openai') || n.includes('gpt')) return 'openai';
  if (n.includes('pushover')) return 'pushover';
  if (n.includes('graphql')) return 'graphql';
  if (n.includes('gitlab')) return 'gitlab';
  if (n.includes('monday')) return 'monday';
  if (n.includes('freshdesk')) return 'freshdesk';
  if (n.includes('intercom')) return 'intercom';
  if (n.includes('outlook') || n.includes('graph')) return 'ms_graph';
  if (n.includes('whatsapp')) return 'whatsapp';
  if (n.includes('coda')) return 'coda';
  if (n.includes('close')) return 'close';
  if (n.includes('calendly')) return 'calendly';
  if (n.includes('servicenow')) return 'servicenow';
  return null;
}

// Resolve the active field list for a connector schema + selected operation.
function fieldsForOperation(schema, action) {
  if (!schema) return [];
  if (schema.operations) {
    const op = schema.operations.find(o => o.value === action) || schema.operations[0];
    return op ? op.fields : [];
  }
  return schema.fields || [];
}

// Generic key/value editor used for query params and headers. Stores into the
// given config key as a flat object. Values support {{ templates }}.
function KVEditor({ label, obj, onSet, previewCtx, valuePlaceholder = 'value or {{ template }}' }) {
  const entries = Object.entries(obj || {});
  const setKey = (oldK, newK) => { const n = {}; entries.forEach(([k, v]) => { n[k === oldK ? newK : k] = v; }); onSet(n); };
  const setVal = (k, v) => onSet({ ...(obj || {}), [k]: v });
  const remove = (k) => { const n = { ...(obj || {}) }; delete n[k]; onSet(n); };
  const add = () => { let i = 1, key = 'key'; while ((obj || {})[key] !== undefined) key = `key${i++}`; onSet({ ...(obj || {}), [key]: '' }); };
  return (
    <div className="form-group" style={{ marginBottom: 0 }}>
      <label className="form-label">{label}</label>
      {entries.map(([k, v]) => (
        <div key={k} style={{ marginBottom: 6 }}>
          <div style={{ display: 'flex', gap: 6 }}>
            <input className="input" style={{ flex: '0 0 38%', fontSize: 11, fontFamily: 'var(--font-mono)' }} value={k} onChange={e => setKey(k, e.target.value)} placeholder="name" />
            <input className="input" style={{ flex: 1, fontSize: 11, fontFamily: 'var(--font-mono)' }} value={typeof v === 'string' ? v : JSON.stringify(v)} onChange={e => setVal(k, e.target.value)} placeholder={valuePlaceholder} />
            <button className="btn btn-ghost btn-icon btn-sm" onClick={() => remove(k)}><X size={12} /></button>
          </div>
          <ExprPreview value={v} ctx={previewCtx} />
        </div>
      ))}
      <button className="btn btn-ghost btn-sm" style={{ width: '100%' }} onClick={add}>+ Add</button>
    </div>
  );
}

// Full HTTP-client configuration surface for the HTTP/Webhook connector: query
// params, headers, auth, body type + body, timeout, and success codes. This is
// what lets the node call any real REST/GraphQL/webhook API.
function HttpAdvancedEditor({ cfg, setField, onChange, previewCtx }) {
  const set = (k, v) => onChange({ config: { ...cfg, [k]: v } });
  const bodyType = cfg.body_type || (cfg.body ? 'json' : 'none');
  const authType = cfg.auth_type || 'none';

  return (
    <div style={{ display: 'flex', flexDirection: 'column', gap: 10, marginTop: 4 }}>
      <KVEditor label="Query Params" obj={cfg.query} onSet={v => set('query', v)} previewCtx={previewCtx} />
      <KVEditor label="Headers" obj={cfg.headers} onSet={v => set('headers', v)} previewCtx={previewCtx} />

      <div className="form-group" style={{ marginBottom: 0 }}>
        <label className="form-label">Authentication</label>
        <select className="select" value={authType} onChange={e => set('auth_type', e.target.value)}>
          <option value="none">None</option>
          <option value="bearer">Bearer Token</option>
          <option value="basic">Basic Auth</option>
          <option value="api_key">API Key Header</option>
        </select>
      </div>
      {authType !== 'none' && (
        <>
          {authType === 'basic' && (
            <div className="form-group" style={{ marginBottom: 0 }}>
              <label className="form-label">Username</label>
              <input className="input" value={cfg.auth_username || ''} onChange={e => set('auth_username', e.target.value)} placeholder="username" />
            </div>
          )}
          {authType === 'api_key' && (
            <div className="form-group" style={{ marginBottom: 0 }}>
              <label className="form-label">Header Name</label>
              <input className="input" value={cfg.auth_header || ''} onChange={e => set('auth_header', e.target.value)} placeholder="X-API-Key" />
            </div>
          )}
          <div className="form-group" style={{ marginBottom: 0 }}>
            <label className="form-label">{authType === 'basic' ? 'Password' : 'Secret'} — credential name</label>
            <input className="input" value={cfg.auth_credential || ''} onChange={e => set('auth_credential', e.target.value)}
              placeholder="e.g. MY_API_KEY (stored in Credentials)" />
            <div className="form-hint">Enter the <em>name</em> of a stored credential / env var — never the secret itself.</div>
          </div>
        </>
      )}

      <div className="form-group" style={{ marginBottom: 0 }}>
        <label className="form-label">Body Type</label>
        <select className="select" value={bodyType} onChange={e => set('body_type', e.target.value)}>
          <option value="none">None</option>
          <option value="json">JSON</option>
          <option value="form">Form (urlencoded)</option>
          <option value="raw">Raw text</option>
        </select>
      </div>
      {bodyType !== 'none' && (
        <div className="form-group" style={{ marginBottom: 0 }}>
          <label className="form-label">Body</label>
          <textarea className="textarea" rows={4} value={cfg.body || ''}
            placeholder={bodyType === 'json' ? '{ "id": "{{ input.id }}" }' : bodyType === 'form' ? '{ "field": "{{ input.x }}" }' : 'raw text {{ input.x }}'}
            onChange={e => set('body', e.target.value)} />
          <ExprPreview value={cfg.body} ctx={previewCtx} />
          <div className="form-hint">JSON/Form expect an object; raw is sent as-is. Templates are resolved before sending.</div>
        </div>
      )}

      <div style={{ display: 'flex', gap: 8 }}>
        <div className="form-group" style={{ marginBottom: 0, flex: 1 }}>
          <label className="form-label">Timeout (s)</label>
          <input className="input" type="number" min={1} placeholder="30" value={cfg.timeout_seconds ?? ''}
            onChange={e => set('timeout_seconds', e.target.value === '' ? undefined : parseInt(e.target.value))} />
        </div>
        <div className="form-group" style={{ marginBottom: 0, flex: 1 }}>
          <label className="form-label">Success Codes</label>
          <input className="input" placeholder="200,201,204" value={(cfg.success_codes || []).join(',')}
            onChange={e => set('success_codes', e.target.value.split(',').map(s => parseInt(s.trim())).filter(n => !isNaN(n)))} />
        </div>
      </div>
    </div>
  );
}

function ToolCallEditor({ d, onChange, connectorOpts, previewCtx }) {
  const cfg = d.config || {};
  // Build the selectable connector list: installed connectors that we can execute,
  // falling back to the full executable set if none are installed yet.
  const installable = (connectorOpts || [])
    .map(c => ({ key: connectorKeyFor(c), name: c.name }))
    .filter(c => c.key);
  const available = installable.length
    ? installable
    : Object.values(CONNECTOR_SCHEMA).map(s => ({ key: s.key, name: s.label }));

  const schema = CONNECTOR_SCHEMA[cfg.connector_id];
  const fields = fieldsForOperation(schema, cfg.action);

  function setField(name, value) {
    onChange({ config: { ...cfg, [name]: value } });
  }

  return (
    <>
      <div className="divider" />
      <div style={{ fontSize: 11, fontWeight: 700, color: 'var(--text-muted)', textTransform: 'uppercase', letterSpacing: '0.1em' }}>Connector</div>
      <div className="form-group" style={{ marginBottom: 0 }}>
        <label className="form-label">Connector</label>
        <select className="select" value={cfg.connector_id || ''}
          onChange={e => {
            const newSchema = CONNECTOR_SCHEMA[e.target.value];
            const firstOp = newSchema && newSchema.operations ? newSchema.operations[0].value : '';
            onChange({ config: { connector_id: e.target.value, action: firstOp } });
          }}>
          <option value="">Select connector…</option>
          {available.map(c => <option key={c.key} value={c.key}>{c.name}</option>)}
        </select>
        {installable.length === 0 && (
          <div className="form-hint">No connectors installed yet — showing built-in connectors. Install connectors on the Connectors page.</div>
        )}
      </div>

      {schema && schema.operations && (
        <div className="form-group" style={{ marginBottom: 0 }}>
          <label className="form-label">Operation</label>
          <select className="select" value={cfg.action || schema.operations[0].value}
            onChange={e => setField('action', e.target.value)}>
            {schema.operations.map(op => <option key={op.value} value={op.value}>{op.label}</option>)}
          </select>
        </div>
      )}

      {fields.map(f => (
        <div key={f.name} className="form-group" style={{ marginBottom: 0 }}>
          <label className="form-label">{f.label}{f.required ? ' *' : ''}</label>
          {f.type === 'select' ? (
            <select className="select" value={cfg[f.name] || f.options[0]} onChange={e => setField(f.name, e.target.value)}>
              {f.options.map(o => <option key={o} value={o}>{o}</option>)}
            </select>
          ) : f.textarea ? (
            <textarea className="textarea" rows={3} value={cfg[f.name] || ''} placeholder={f.placeholder}
              onChange={e => setField(f.name, e.target.value)} />
          ) : (
            <input className="input" value={cfg[f.name] || ''} placeholder={f.placeholder}
              onChange={e => setField(f.name, e.target.value)} />
          )}
          <ExprPreview value={cfg[f.name]} ctx={previewCtx} />
        </div>
      ))}

      {schema && schema.http && <HttpAdvancedEditor cfg={cfg} setField={setField} onChange={onChange} previewCtx={previewCtx} />}

      <div className="form-group" style={{ marginBottom: 0 }}>
        <label className="form-label">Output Path (optional)</label>
        <input className="input" value={cfg.output_path || ''} placeholder="e.g. response.data.0.id"
          onChange={e => setField('output_path', e.target.value)} />
        <div className="form-hint">Extract a sub-value from the response. Downstream: <code style={{ fontFamily: 'var(--font-mono)' }}>{'{{ steps.' + (d.id || 'node') + '.output.value }}'}</code></div>
      </div>

      <div className="form-group" style={{ marginBottom: 0 }}>
        <label className="form-label">On Error → Node ID (optional)</label>
        <input className="input" value={cfg.on_error || ''} placeholder="fallback node id"
          onChange={e => setField('on_error', e.target.value)} />
      </div>

      {schema && schema.creds && schema.creds.length > 0 && (
        <ConnectorCredentialPanel creds={schema.creds} connectorLabel={schema.label} />
      )}
      <ConnectorTester cfg={cfg} previewCtx={previewCtx} />
      <div className="form-hint">
        Field values support templates like <code style={{ fontFamily: 'var(--font-mono)' }}>{'{{ input.email }}'}</code> and <code style={{ fontFamily: 'var(--font-mono)' }}>{'{{ steps.node_id.output.field }}'}</code>. Credentials are stored encrypted in KNOTT, never in the workflow.
      </div>
    </>
  );
}

// Shows live credential readiness for the selected connector and lets the user
// fill missing secrets inline (stored encrypted via the credentials API), so a
// workflow can be wired end-to-end without leaving the builder.
function ConnectorCredentialPanel({ creds, connectorLabel }) {
  const [status, setStatus] = useState(null); // { name: configured }
  const [drafts, setDrafts] = useState({});
  const [saving, setSaving] = useState('');
  const { toast } = useToast();

  // Each schema cred entry may be a hint like "GOOGLE_REFRESH_TOKEN (or GOOGLE_ACCESS_TOKEN)".
  // Extract the primary UPPER_SNAKE token to manage.
  const keyEntries = creds.map(entry => {
    const m = String(entry).match(/[A-Z0-9_]{3,}/g) || [];
    return { entry, primary: m[0] || entry, alts: m };
  });

  async function load() {
    try {
      const r = await credsApi.list();
      const stored = new Set((r.data || []).map(c => c.name));
      const env = r.env_configured || {};
      const s = {};
      keyEntries.forEach(({ alts }) => {
        alts.forEach(k => { s[k] = stored.has(k) || !!env[k]; });
      });
      setStatus(s);
    } catch { setStatus({}); }
  }
  useEffect(() => { load(); /* eslint-disable-next-line */ }, [connectorLabel]);

  async function save(name) {
    const val = (drafts[name] || '').trim();
    if (!val) { toast('Enter a value', 'warning'); return; }
    setSaving(name);
    try {
      await credsApi.set(name, val);
      toast(`${name} saved`, 'success');
      setDrafts(d => ({ ...d, [name]: '' }));
      await load();
    } catch (e) { toast('Save failed', 'error', e.message); }
    finally { setSaving(''); }
  }

  const allReady = status && keyEntries.every(({ alts }) => alts.some(k => status[k]));

  return (
    <div className="card" style={{ marginTop: 8, padding: '12px 14px', borderColor: allReady ? 'var(--green)' : 'var(--amber)' }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 8, marginBottom: 8, fontSize: 12, fontWeight: 650 }}>
        <KeyRound size={13} color={allReady ? 'var(--green)' : 'var(--amber)'} />
        {allReady ? `${connectorLabel} credentials ready` : `${connectorLabel} needs credentials`}
      </div>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 8 }}>
        {keyEntries.map(({ entry, primary, alts }) => {
          const ok = status && alts.some(k => status[k]);
          return (
            <div key={entry} style={{ display: 'flex', gap: 8, alignItems: 'center' }}>
              <div style={{ flex: '0 0 150px', fontSize: 11, fontFamily: 'var(--font-mono)', color: 'var(--text-secondary)' }}>
                {primary}
              </div>
              {ok ? (
                <span className="badge badge-green" style={{ fontSize: 10 }}><Check size={10} /> set</span>
              ) : (
                <>
                  <input className="input" type="password" autoComplete="off" placeholder="paste secret…"
                    value={drafts[primary] || ''} onChange={e => setDrafts(d => ({ ...d, [primary]: e.target.value }))}
                    style={{ flex: 1, fontSize: 11, padding: '4px 8px' }} />
                  <button className="btn btn-secondary btn-sm" disabled={saving === primary}
                    onClick={() => save(primary)}>{saving === primary ? '…' : 'Save'}</button>
                </>
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}

// "Test Connector" — runs the connector live with the current config + sample
// Test Data, showing the result or the exact error, without saving a run.
function ConnectorTester({ cfg, previewCtx }) {
  const [busy, setBusy] = useState(false);
  const [result, setResult] = useState(null);
  if (!cfg.connector_id) return null;

  async function runTest() {
    setBusy(true); setResult(null);
    try {
      const r = await connectorsApi.test({
        connector_id: cfg.connector_id,
        action: cfg.action || '',
        config: cfg,
        sample_input: (previewCtx && previewCtx.input) || {},
      });
      setResult(r);
    } catch (e) {
      setResult({ ok: false, error: e.message });
    } finally { setBusy(false); }
  }

  return (
    <div style={{ marginTop: 6 }}>
      <button className="btn btn-secondary btn-sm" style={{ width: '100%', justifyContent: 'center' }} onClick={runTest} disabled={busy}>
        {busy ? <span className="spinner-sm" /> : <CheckSquare size={13} />} Test Connector
      </button>
      {result && (
        <div style={{
          marginTop: 6, fontSize: 11, fontFamily: 'var(--font-mono)', padding: '8px 10px', borderRadius: 6,
          background: 'var(--bg-secondary)', borderLeft: `3px solid ${result.ok ? 'var(--green)' : 'var(--red)'}`,
          color: result.ok ? 'var(--text-secondary)' : 'var(--red)', maxHeight: 180, overflow: 'auto', wordBreak: 'break-word',
        }}>
          {result.ok
            ? <>✓ Success{result.latency_ms != null ? ` (${result.latency_ms}ms)` : ''}<br />{JSON.stringify(result.output, null, 2)}</>
            : <>✗ {result.error}</>}
        </div>
      )}
      <div className="form-hint">Runs the connector now using the Test Data above. Live call — sends real requests.</div>
    </div>
  );
}
