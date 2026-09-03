// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

import React, { useState, useEffect, useCallback, useRef, useMemo } from 'react';
import ReactFlow, {
  addEdge, useNodesState, useEdgesState,
  Background, Controls, MiniMap,
  BackgroundVariant, MarkerType, useReactFlow, ReactFlowProvider,
} from 'reactflow';
import {
  Save, Play, CheckCircle, Code2, ArrowLeft, AlertTriangle, Plus,
  Undo2, Redo2, LayoutGrid, Keyboard, Copy, Trash2, X, Search,
} from 'lucide-react';
import {
  workflows as wfApi, runs as runsApi, connectors as connectorsApi,
  agents as agentsApi, taskSpecs as taskSpecsApi,
} from '../lib/api.js';
import { NODE_TYPES } from '../components/nodes/index.jsx';
import {
  NODE_CATALOG, NODE_BY_TYPE, NODE_GROUPS, defaultNodeName,
} from '../designer/nodeCatalog.js';
import NodePicker from '../designer/NodePicker.jsx';
import { autoLayout, positionAfter } from '../designer/autoLayout.js';
import { useGraphHistory } from '../designer/useGraphHistory.js';
import { useToast } from '../components/Layout.jsx';
import { sampleContext } from '../lib/expr.js';

let nodeSeq = Date.now() % 100000;
const uid = () => `node_${++nodeSeq}`;

const EDGE_STYLE = {
  markerEnd: { type: MarkerType.ArrowClosed, width: 16, height: 16 },
  style: { strokeWidth: 2 },
};

// ─── Definition ⇄ canvas ──────────────────────────────────────────────────────
//
// The graph on screen and the definition on disk hold the same information in
// two shapes. Every edge carries the handle it left from, which is what lets a
// condition's branches and a node's error output survive a save — previously an
// edge drawn from a condition had nowhere to be written back to and was lost.

function defToFlow(def) {
  if (!def?.steps) return { nodes: [], edges: [] };

  const nodes = def.steps.map(s => ({
    id: s.id,
    type: NODE_TYPES[s.type] ? s.type : 'tool_call',
    position: s.position || { x: 120, y: 120 },
    data: { ...s },
  }));

  const edges = [];
  const push = (source, target, handle, label, kind) => {
    if (!target) return;
    edges.push({
      id: `e-${source}-${handle}-${target}`,
      source, target, sourceHandle: handle, label,
      className: kind,
      data: { kind },
      ...EDGE_STYLE,
    });
  };

  def.steps.forEach(s => {
    if (s.type === 'condition') {
      (s.cases || []).forEach((c, i) => push(s.id, c.next, `case-${i}`, undefined, 'branch'));
      push(s.id, s.default, 'default', 'otherwise', 'branch');
    } else if (s.next) {
      push(s.id, s.next, 'main', undefined, 'main');
    }
    const onError = s.config?.on_error;
    if (onError) push(s.id, onError, 'error', 'on error', 'error');
  });

  return { nodes, edges };
}

function flowToDef(nodes, edges, trigger) {
  const steps = nodes
    .filter(n => n.type !== 'note')
    .map(n => {
      const step = { ...n.data, id: n.id, type: n.type, position: n.position };
      // Routing is owned by the edges; clear it so a removed edge really removes
      // the route rather than leaving a stale one behind.
      delete step.next;
      step.config = { ...(step.config || {}) };
      delete step.config.on_error;
      if (n.type === 'condition') {
        step.cases = (step.cases || []).map(c => ({ ...c, next: '' }));
        step.default = '';
      }
      return step;
    });

  const byId = Object.fromEntries(steps.map(s => [s.id, s]));

  for (const e of edges) {
    const step = byId[e.source];
    if (!step || !byId[e.target]) continue;
    const handle = e.sourceHandle || 'main';

    if (handle === 'error') {
      step.config.on_error = e.target;
    } else if (step.type === 'condition') {
      if (handle === 'default') {
        step.default = e.target;
      } else if (handle.startsWith('case-')) {
        const i = Number(handle.slice(5));
        if (!step.cases[i]) step.cases[i] = { condition: '', next: '' };
        step.cases[i].next = e.target;
      }
    } else {
      step.next = e.target;
    }
  }

  // Notes live on the canvas only; keep them out of the executable definition
  // but preserve them so they survive a round trip.
  const notes = nodes.filter(n => n.type === 'note').map(n => ({
    id: n.id, text: n.data.notes || '', position: n.position,
  }));

  const def = { trigger: trigger || { type: 'api' }, steps };
  if (notes.length) def.annotations = notes;
  return def;
}

// ─── Shell ────────────────────────────────────────────────────────────────────

export default function WorkflowDesigner(props) {
  return (
    <ReactFlowProvider>
      <DesignerCanvas {...props} />
    </ReactFlowProvider>
  );
}

function DesignerCanvas({ workflowId, onBack, NodePropsEditor }) {
  const [wf, setWf] = useState(null);
  const [nodes, setNodes, onNodesChange] = useNodesState([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState([]);
  const [selectedId, setSelectedId] = useState(null);
  const [showJSON, setShowJSON] = useState(false);
  const [saving, setSaving] = useState(false);
  const [dirty, setDirty] = useState(false);
  const [validErrs, setValidErrs] = useState([]);
  const [showRunModal, setShowRunModal] = useState(false);
  const [showShortcuts, setShowShortcuts] = useState(false);
  const [loadError, setLoadError] = useState(null);
  const [loading, setLoading] = useState(!!workflowId);
  const [runInput, setRunInput] = useState('{\n  \n}');
  const [connectorOpts, setConnectorOpts] = useState([]);
  const [agentOpts, setAgentOpts] = useState([]);
  const [taskSpecOpts, setTaskSpecOpts] = useState([]);
  const [testInput, setTestInput] = useState('{\n  \n}');
  const [picker, setPicker] = useState(null); // { from, handle } | { at: {x,y} } | null
  const [paletteQuery, setPaletteQuery] = useState('');

  const canvasRef = useRef(null);
  const clipboard = useRef(null);
  const { toast } = useToast();
  const { screenToFlowPosition, fitView } = useReactFlow();

  const selected = useMemo(
    () => nodes.find(n => n.id === selectedId) || null,
    [nodes, selectedId],
  );

  // ── Undo / redo ────────────────────────────────────────────────────────────
  const snapshot = useCallback(
    () => JSON.stringify({
      nodes: nodes.map(({ id, type, position, data }) => ({ id, type, position, data: stripRuntime(data) })),
      edges,
    }),
    [nodes, edges],
  );
  const restore = useCallback(raw => {
    const { nodes: n, edges: e } = JSON.parse(raw);
    setNodes(n);
    setEdges(e);
    setDirty(true);
  }, [setNodes, setEdges]);
  const history = useGraphHistory(snapshot, restore);

  /** Record a restore point, then mutate. Every structural change goes through this. */
  const change = useCallback(fn => {
    history.commit();
    setDirty(true);
    fn();
  }, [history]);

  // ── Loading ────────────────────────────────────────────────────────────────
  useEffect(() => {
    connectorsApi.list()
      .then(r => setConnectorOpts((r.data || []).filter(c => c.installed)))
      .catch(() => setConnectorOpts([]));
    agentsApi.list().then(r => setAgentOpts(r.data || [])).catch(() => setAgentOpts([]));
    taskSpecsApi.list().then(r => setTaskSpecOpts(r.data || [])).catch(() => setTaskSpecOpts([]));
  }, []);

  const loadWorkflow = useCallback(() => {
    if (!workflowId) {
      setWf({ name: 'Untitled Workflow', status: 'draft', definition: { trigger: { type: 'api' }, steps: [] } });
      setNodes([]);
      setEdges([]);
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
      } catch {
        toast('That workflow’s definition could not be read — starting from an empty canvas', 'warning');
        def = { trigger: { type: 'api' }, steps: [] };
      }
      const { nodes: n, edges: e } = defToFlow(def || { steps: [] });
      const notes = (def?.annotations || []).map(a => ({
        id: a.id || uid(), type: 'note', position: a.position || { x: 40, y: 40 },
        data: { id: a.id, type: 'note', notes: a.text },
      }));
      setNodes([...n, ...notes]);
      setEdges(e);
      history.reset();
      setDirty(false);
      setLoading(false);
    }).catch(e => {
      setLoadError(e.message || 'Could not load this workflow');
      setLoading(false);
    });
  }, [workflowId]); // eslint-disable-line react-hooks/exhaustive-deps

  useEffect(() => { loadWorkflow(); }, [loadWorkflow]);

  // ── Building ───────────────────────────────────────────────────────────────

  const addNode = useCallback((type, { from, handle, position } = {}) => {
    const spec = NODE_BY_TYPE[type];
    if (spec?.unique && nodes.some(n => n.type === type)) {
      toast(`A workflow has one ${spec.label.toLowerCase()}`, 'warning');
      return null;
    }

    const source = from ? nodes.find(n => n.id === from) : null;
    const at = position || positionAfter(source, nodes);
    const id = uid();
    const names = nodes.map(n => n.data?.name).filter(Boolean);
    const node = {
      id, type, position: at,
      data: {
        id, type,
        name: defaultNodeName(type, names),
        config: { ...(spec?.defaults?.config || {}) },
        inputs: {},
        ...(spec?.defaults || {}),
      },
    };
    if (type === 'condition') node.data.cases = [{ condition: '', next: '' }];

    change(() => {
      setNodes(ns => [...ns, node]);
      if (from) {
        setEdges(es => connect(es, {
          source: from, target: id, sourceHandle: handle || 'main',
        }));
      }
    });
    setSelectedId(id);
    return id;
  }, [nodes, change, setNodes, setEdges, toast]);

  /** Add an edge, replacing any existing edge from the same output. */
  function connect(existing, params) {
    const handle = params.sourceHandle || 'main';
    const kind = handle === 'error' ? 'error' : handle === 'main' ? 'main' : 'branch';
    const kept = existing.filter(
      e => !(e.source === params.source && (e.sourceHandle || 'main') === handle),
    );
    return addEdge({
      ...params,
      sourceHandle: handle,
      id: `e-${params.source}-${handle}-${params.target}`,
      className: kind,
      data: { kind },
      label: kind === 'error' ? 'on error' : undefined,
      ...EDGE_STYLE,
    }, kept);
  }

  const onConnect = useCallback(params => {
    if (params.source === params.target) {
      toast('A step cannot connect to itself', 'warning');
      return;
    }
    change(() => setEdges(es => connect(es, params)));
  }, [change, setEdges, toast]);

  const openPickerFor = useCallback((from, handle) => setPicker({ from, handle }), []);

  // The + buttons live inside node components, so the handler travels in data.
  const decorated = useMemo(() => nodes.map(n => ({
    ...n,
    selected: n.id === selectedId,
    data: { ...n.data, __onAppend: openPickerFor },
  })), [nodes, selectedId, openPickerFor]);

  const onDrop = useCallback(e => {
    e.preventDefault();
    const type = e.dataTransfer.getData('nodeType');
    if (!type) return;
    const position = screenToFlowPosition({ x: e.clientX, y: e.clientY });
    addNode(type, { position: { x: position.x - 90, y: position.y - 30 } });
  }, [addNode, screenToFlowPosition]);

  const onPaneDoubleClick = useCallback(e => {
    const position = screenToFlowPosition({ x: e.clientX, y: e.clientY });
    setPicker({ at: position });
  }, [screenToFlowPosition]);

  const deleteSelection = useCallback(() => {
    const nodeIds = new Set(nodes.filter(n => n.selected || n.id === selectedId).map(n => n.id));
    const edgeIds = new Set(edges.filter(e => e.selected).map(e => e.id));
    if (!nodeIds.size && !edgeIds.size) return;
    change(() => {
      setNodes(ns => ns.filter(n => !nodeIds.has(n.id)));
      setEdges(es => es.filter(e =>
        !edgeIds.has(e.id) && !nodeIds.has(e.source) && !nodeIds.has(e.target)));
    });
    setSelectedId(null);
  }, [nodes, edges, selectedId, change, setNodes, setEdges]);

  const duplicateSelection = useCallback(() => {
    const picked = nodes.filter(n => n.selected || n.id === selectedId);
    if (!picked.length) return;
    const idMap = {};
    const copies = picked.map(n => {
      const id = uid();
      idMap[n.id] = id;
      return {
        ...n, id, selected: false,
        position: { x: n.position.x + 40, y: n.position.y + 60 },
        data: { ...stripRuntime(n.data), id, name: `${n.data.name || n.type} copy` },
      };
    });
    // Carry over the edges that live entirely inside the selection.
    const inner = edges
      .filter(e => idMap[e.source] && idMap[e.target])
      .map(e => ({
        ...e,
        id: `e-${idMap[e.source]}-${e.sourceHandle || 'main'}-${idMap[e.target]}`,
        source: idMap[e.source], target: idMap[e.target],
      }));
    change(() => {
      setNodes(ns => [...ns, ...copies]);
      setEdges(es => [...es, ...inner]);
    });
    setSelectedId(copies[0].id);
  }, [nodes, edges, selectedId, change, setNodes, setEdges]);

  const copySelection = useCallback(() => {
    const picked = nodes.filter(n => n.selected || n.id === selectedId);
    if (!picked.length) return;
    const ids = new Set(picked.map(n => n.id));
    clipboard.current = {
      nodes: picked.map(n => ({ ...n, data: stripRuntime(n.data) })),
      edges: edges.filter(e => ids.has(e.source) && ids.has(e.target)),
    };
    toast(`${picked.length} step${picked.length === 1 ? '' : 's'} copied`, 'info');
  }, [nodes, edges, selectedId, toast]);

  const paste = useCallback(() => {
    const buf = clipboard.current;
    if (!buf?.nodes?.length) return;
    const idMap = {};
    const copies = buf.nodes.map(n => {
      const id = uid();
      idMap[n.id] = id;
      return {
        ...n, id, selected: false,
        position: { x: n.position.x + 60, y: n.position.y + 60 },
        data: { ...n.data, id },
      };
    });
    const inner = buf.edges.map(e => ({
      ...e,
      id: `e-${idMap[e.source]}-${e.sourceHandle || 'main'}-${idMap[e.target]}`,
      source: idMap[e.source], target: idMap[e.target],
    }));
    change(() => {
      setNodes(ns => [...ns, ...copies]);
      setEdges(es => [...es, ...inner]);
    });
    setSelectedId(copies[0].id);
  }, [change, setNodes, setEdges]);

  const tidy = useCallback(() => {
    if (!nodes.length) return;
    const positions = autoLayout(nodes, edges, { isAnnotation: n => n.type === 'note' });
    change(() => setNodes(ns => ns.map(n =>
      positions[n.id] ? { ...n, position: positions[n.id] } : n)));
    requestAnimationFrame(() => fitView({ padding: 0.2, duration: 300 }));
  }, [nodes, edges, change, setNodes, fitView]);

  // ── Keyboard ───────────────────────────────────────────────────────────────
  useEffect(() => {
    function onKey(e) {
      const target = e.target;
      const typing = target && (
        target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' ||
        target.tagName === 'SELECT' || target.isContentEditable);
      const mod = e.metaKey || e.ctrlKey;

      if (e.key === 'Escape') {
        setPicker(null);
        setShowShortcuts(false);
        return;
      }
      if (typing) return;

      if (e.key === 'Tab' && !mod) {
        e.preventDefault();
        setPicker(selectedId ? { from: selectedId, handle: 'main' } : { at: null });
        return;
      }
      if (e.key === '?' ) { setShowShortcuts(s => !s); return; }

      if (mod && e.key.toLowerCase() === 'z') {
        e.preventDefault();
        (e.shiftKey ? history.redo : history.undo)();
        return;
      }
      if (mod && e.key.toLowerCase() === 'y') { e.preventDefault(); history.redo(); return; }
      if (mod && e.key.toLowerCase() === 'd') { e.preventDefault(); duplicateSelection(); return; }
      if (mod && e.key.toLowerCase() === 'c') { copySelection(); return; }
      if (mod && e.key.toLowerCase() === 'v') { paste(); return; }
      if (mod && e.shiftKey && e.key.toLowerCase() === 'l') { e.preventDefault(); tidy(); return; }
      if (e.key === 'Delete' || e.key === 'Backspace') { e.preventDefault(); deleteSelection(); }
    }
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [selectedId, history, duplicateSelection, copySelection, paste, tidy, deleteSelection]);

  // Warn before losing unsaved work on a browser navigation.
  useEffect(() => {
    if (!dirty) return;
    const warn = e => { e.preventDefault(); e.returnValue = ''; };
    window.addEventListener('beforeunload', warn);
    return () => window.removeEventListener('beforeunload', warn);
  }, [dirty]);

  // ── Persistence ────────────────────────────────────────────────────────────
  const previewCtx = useMemo(() => {
    const def = flowToDef(nodes, edges, wf?.definition?.trigger);
    const ctx = sampleContext(def);
    try { ctx.input = JSON.parse(testInput); } catch { /* leave input empty */ }
    return ctx;
  }, [testInput, nodes, edges, wf]);

  function updateSelectedData(patch) {
    if (!selected) return;
    setDirty(true);
    setNodes(ns => ns.map(n => n.id === selected.id ? { ...n, data: { ...n.data, ...patch } } : n));
  }

  async function handleSave() {
    setSaving(true);
    try {
      const def = flowToDef(nodes, edges, wf?.definition?.trigger);
      const body = {
        name: wf?.name || 'Untitled Workflow',
        description: wf?.description,
        status: wf?.status || 'draft',
        definition: def,
        tags: wf?.tags || [],
      };
      const id = workflowId || wf?.id;
      const saved = id ? await wfApi.update(id, body) : await wfApi.create(body);
      setWf(saved);
      setDirty(false);
      toast('Workflow saved', 'success');
      return saved;
    } catch (e) {
      toast('Could not save the workflow', 'error', e.message);
      return null;
    } finally {
      setSaving(false);
    }
  }

  async function handleValidate() {
    try {
      const def = flowToDef(nodes, edges, wf?.definition?.trigger);
      const res = await wfApi.validate(workflowId || 'new', def);
      setValidErrs(res.errors || []);
      if (res.valid) toast('This workflow is valid', 'success');
      else toast(`${res.errors.length} problem${res.errors.length === 1 ? '' : 's'} to fix`, 'warning');
    } catch (e) {
      toast('Could not validate', 'error', e.message);
    }
  }

  async function handleRun() {
    let inputData;
    try { inputData = JSON.parse(runInput); } catch { toast('That input is not valid JSON', 'error'); return; }
    const saved = await handleSave();
    const id = saved?.id || workflowId || wf?.id;
    if (!id) { toast('Save the workflow before running it', 'warning'); return; }
    try {
      const r = await runsApi.create({ workflow_id: id, input_data: inputData });
      toast('Run started', 'success', `ID ${r.id.slice(0, 8)}…`);
      setShowRunModal(false);
    } catch (e) {
      toast('Could not start the run', 'error', e.message);
    }
  }

  function handleBack() {
    if (dirty && !confirm('This workflow has unsaved changes. Leave anyway?')) return;
    onBack();
  }

  // ── Render ─────────────────────────────────────────────────────────────────
  if (loadError) {
    return (
      <div className="designer-shell">
        <div className="designer-toolbar">
          <button className="btn btn-ghost btn-sm" onClick={onBack}><ArrowLeft size={13} /> Back</button>
        </div>
        <div className="empty-state" style={{ flex: 1, justifyContent: 'center' }}>
          <AlertTriangle size={40} color="var(--red)" />
          <h3>Couldn’t load this workflow</h3>
          <p>{loadError}</p>
          <div style={{ display: 'flex', gap: 8, marginTop: 8 }}>
            <button className="btn btn-secondary" onClick={onBack}>Back to the registry</button>
            <button className="btn btn-primary" onClick={loadWorkflow}>Try again</button>
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
        <div style={{ flex: 1, display: 'grid', placeItems: 'center', gap: 14 }}>
          <div className="spinner spinner-lg" />
        </div>
      </div>
    );
  }

  const paletteGroups = NODE_GROUPS
    .map(g => [g, NODE_CATALOG.filter(n => n.group === g &&
      (!paletteQuery || matches(n, paletteQuery)))])
    .filter(([, items]) => items.length);

  return (
    <div className="designer-shell">
      <div className="designer-toolbar">
        <button className="btn btn-ghost btn-sm" onClick={handleBack}><ArrowLeft size={13} /> Back</button>
        <span className="toolbar-sep" />
        <input
          className="input toolbar-name"
          value={wf?.name || ''}
          onChange={e => { setWf(w => ({ ...w, name: e.target.value })); setDirty(true); }}
          placeholder="Name this workflow"
          aria-label="Workflow name"
        />
        <select
          className="select toolbar-status"
          value={wf?.status || 'draft'}
          onChange={e => { setWf(w => ({ ...w, status: e.target.value })); setDirty(true); }}
          aria-label="Workflow status"
        >
          <option value="draft">Draft</option>
          <option value="active">Active</option>
          <option value="deprecated">Deprecated</option>
        </select>

        <span className="toolbar-sep" />
        <button className="btn btn-ghost btn-icon btn-sm" onClick={history.undo}
          disabled={!history.canUndo} title="Undo (Ctrl+Z)" aria-label="Undo">
          <Undo2 size={14} />
        </button>
        <button className="btn btn-ghost btn-icon btn-sm" onClick={history.redo}
          disabled={!history.canRedo} title="Redo (Ctrl+Shift+Z)" aria-label="Redo">
          <Redo2 size={14} />
        </button>
        <button className="btn btn-ghost btn-icon btn-sm" onClick={tidy}
          title="Tidy the layout (Ctrl+Shift+L)" aria-label="Tidy layout">
          <LayoutGrid size={14} />
        </button>

        <div style={{ flex: 1 }} />

        {dirty && <span className="toolbar-dirty">Unsaved</span>}
        {validErrs.length > 0 && (
          <button className="toolbar-errors" onClick={() => setValidErrs([])} title={validErrs.join('\n')}>
            <AlertTriangle size={13} /> {validErrs.length} problem{validErrs.length === 1 ? '' : 's'}
          </button>
        )}
        <button className="btn btn-ghost btn-icon btn-sm" onClick={() => setShowShortcuts(true)}
          title="Keyboard shortcuts (?)" aria-label="Keyboard shortcuts">
          <Keyboard size={14} />
        </button>
        <button className="btn btn-ghost btn-sm" onClick={() => setShowJSON(v => !v)}>
          <Code2 size={13} /> {showJSON ? 'Canvas' : 'JSON'}
        </button>
        <button className="btn btn-secondary btn-sm" onClick={handleValidate}>
          <CheckCircle size={13} /> Validate
        </button>
        <button className="btn btn-secondary btn-sm" onClick={handleSave} disabled={saving}>
          {saving ? <span className="spinner-sm" /> : <Save size={13} />} Save
        </button>
        <button className="btn btn-primary btn-sm" onClick={() => setShowRunModal(true)}>
          <Play size={13} /> Run
        </button>
      </div>

      <div className="designer-body">
        <div className="designer-palette">
          <div className="palette-search">
            <Search size={12} />
            <input
              value={paletteQuery}
              onChange={e => setPaletteQuery(e.target.value)}
              placeholder="Find a step"
              aria-label="Search step types"
            />
          </div>
          {paletteGroups.map(([group, items]) => (
            <div key={group} className="palette-group">
              <div className="palette-title">{group}</div>
              {items.map(p => {
                const Icon = p.icon;
                return (
                  <div
                    key={p.type}
                    className="palette-node"
                    draggable
                    title={p.summary}
                    onDragStart={e => e.dataTransfer.setData('nodeType', p.type)}
                    onDoubleClick={() => addNode(p.type)}
                  >
                    <Icon size={13} style={{ color: p.color }} />
                    {p.label}
                  </div>
                );
              })}
            </div>
          ))}
          <p className="palette-hint">
            Drag onto the canvas, or press <kbd>Tab</kbd> to search for a step.
          </p>
        </div>

        {showJSON ? (
          <div className="designer-json">
            <div className="card-label">Workflow definition</div>
            <pre className="code-block">
              {JSON.stringify(flowToDef(nodes, edges, wf?.definition?.trigger), null, 2)}
            </pre>
          </div>
        ) : (
          <div className="designer-canvas" ref={canvasRef}>
            <ReactFlow
              nodes={decorated}
              edges={edges}
              onNodesChange={onNodesChange}
              onEdgesChange={onEdgesChange}
              onConnect={onConnect}
              onNodeClick={(_, n) => setSelectedId(n.id)}
              onNodeDragStop={() => setDirty(true)}
              onPaneClick={() => setSelectedId(null)}
              onDoubleClick={onPaneDoubleClick}
              onDrop={onDrop}
              onDragOver={e => { e.preventDefault(); e.dataTransfer.dropEffect = 'move'; }}
              nodeTypes={NODE_TYPES}
              fitView
              fitViewOptions={{ padding: 0.25 }}
              defaultEdgeOptions={EDGE_STYLE}
              connectionRadius={30}
              deleteKeyCode={null}
              proOptions={{ hideAttribution: true }}
            >
              <Background variant={BackgroundVariant.Dots} gap={22} size={1} color="var(--border-dim)" />
              <Controls showInteractive={false} />
              <MiniMap
                pannable
                zoomable
                nodeColor={n => NODE_BY_TYPE[n.type]?.color || 'var(--text-muted)'}
                maskColor="var(--minimap-mask)"
              />
            </ReactFlow>

            {nodes.length === 0 && (
              <div className="canvas-empty">
                <h3>Start with a trigger</h3>
                <p>Every workflow begins with something that sets it off — a webhook,
                   a schedule, or a manual run.</p>
                <button className="btn btn-primary" onClick={() => addNode('trigger', { position: { x: 140, y: 180 } })}>
                  <Plus size={14} /> Add a trigger
                </button>
                <p className="canvas-empty-hint">
                  or press <kbd>Tab</kbd> to search, or double-click anywhere on the canvas
                </p>
              </div>
            )}

            {selected && (
              <div className="canvas-actions">
                <button className="btn btn-ghost btn-sm" onClick={duplicateSelection} title="Duplicate (Ctrl+D)">
                  <Copy size={13} /> Duplicate
                </button>
                <button className="btn btn-ghost btn-sm" onClick={deleteSelection} title="Delete (Del)">
                  <Trash2 size={13} /> Delete
                </button>
              </div>
            )}
          </div>
        )}

        <div className="designer-props">
          <div className="props-header">
            {selected
              ? (NODE_BY_TYPE[selected.type]?.label || selected.type).toUpperCase()
              : 'PROPERTIES'}
            {selected && (
              <button className="btn btn-ghost btn-icon btn-sm" onClick={() => setSelectedId(null)} aria-label="Close">
                <X size={13} />
              </button>
            )}
          </div>
          <div className="props-body">
            {!selected ? (
              <div className="props-placeholder">
                <p>Select a step to configure it.</p>
                <p>Click the <strong>+</strong> on a step to add the next one, or press <kbd>Tab</kbd> to search.</p>
              </div>
            ) : (
              <NodePropsEditor
                node={selected}
                onChange={updateSelectedData}
                connectorOpts={connectorOpts}
                agentOpts={agentOpts}
                taskSpecOpts={taskSpecOpts}
                previewCtx={previewCtx}
                testInput={testInput}
                setTestInput={setTestInput}
                workflowId={workflowId}
                nodes={nodes}
              />
            )}
          </div>
        </div>
      </div>

      <NodePicker
        open={!!picker}
        exclude={new Set(nodes.some(n => n.type === 'trigger') ? ['trigger'] : [])}
        title={picker?.from ? 'Add the next step' : 'Add a step'}
        subtitle={picker?.handle === 'error'
          ? 'What should happen when this step fails?'
          : 'What should this step do?'}
        onClose={() => setPicker(null)}
        onPick={type => {
          addNode(type, picker?.from
            ? { from: picker.from, handle: picker.handle }
            : { position: picker?.at || undefined });
          setPicker(null);
        }}
      />

      {showShortcuts && <ShortcutSheet onClose={() => setShowShortcuts(false)} />}

      {showRunModal && (
        <div className="modal-overlay" onClick={() => setShowRunModal(false)}>
          <div className="modal" onClick={e => e.stopPropagation()}>
            <div className="modal-header">
              <div>
                <div className="modal-title">Run this workflow</div>
                <div className="modal-sub">{wf?.name}</div>
              </div>
              <button className="btn btn-ghost btn-icon btn-sm" onClick={() => setShowRunModal(false)} aria-label="Close">
                <X size={14} />
              </button>
            </div>
            <div className="form-group">
              <label className="form-label">Input payload (JSON)</label>
              <textarea className="textarea" rows={8} value={runInput}
                onChange={e => setRunInput(e.target.value)} />
              <div className="form-hint">This becomes <code>input</code> in every expression.</div>
            </div>
            <div className="modal-footer">
              <button className="btn btn-secondary" onClick={() => setShowRunModal(false)}>Cancel</button>
              <button className="btn btn-primary" onClick={handleRun}><Play size={13} /> Start run</button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
}

function matches(n, q) {
  const s = q.toLowerCase();
  return n.label.toLowerCase().includes(s) ||
    n.keywords.some(k => k.includes(s)) ||
    n.summary.toLowerCase().includes(s);
}

/** Strip the handlers and run overlays the designer injects into node data. */
function stripRuntime(data) {
  const { __onAppend, __run, ...rest } = data || {};
  return rest;
}

const SHORTCUTS = [
  ['Tab', 'Search for a step to add'],
  ['Double-click canvas', 'Add a step where you clicked'],
  ['Ctrl / ⌘ + Z', 'Undo'],
  ['Ctrl / ⌘ + Shift + Z', 'Redo'],
  ['Ctrl / ⌘ + D', 'Duplicate the selection'],
  ['Ctrl / ⌘ + C, V', 'Copy and paste steps'],
  ['Ctrl / ⌘ + Shift + L', 'Tidy the layout'],
  ['Delete', 'Remove the selected step or connection'],
  ['?', 'Show this list'],
];

function ShortcutSheet({ onClose }) {
  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal" style={{ maxWidth: 460 }} onClick={e => e.stopPropagation()}>
        <div className="modal-header">
          <div className="modal-title">Keyboard shortcuts</div>
          <button className="btn btn-ghost btn-icon btn-sm" onClick={onClose} aria-label="Close">
            <X size={14} />
          </button>
        </div>
        <div className="shortcut-list">
          {SHORTCUTS.map(([keys, what]) => (
            <div key={keys} className="shortcut-row">
              <kbd>{keys}</kbd>
              <span>{what}</span>
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

export { defToFlow, flowToDef };
