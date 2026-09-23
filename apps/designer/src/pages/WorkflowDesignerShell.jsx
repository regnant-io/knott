// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

import React, { useState, useEffect, useCallback, useRef, useMemo } from 'react';
import {
  ReactFlow, addEdge, useNodesState, useEdgesState,
  Background, Controls, MiniMap, BackgroundVariant, MarkerType,
  useReactFlow, ReactFlowProvider,
} from '@xyflow/react';
import {
  Save, Play, CheckCircle, Code2, ArrowLeft, AlertTriangle, Plus,
  Undo2, Redo2, LayoutGrid, Keyboard, Copy, Trash2, X, Activity, StickyNote,
  Maximize2, Map as MapIcon, Wand2, Settings2, SlidersHorizontal, FileOutput,
} from 'lucide-react';
import {
  workflows as wfApi, runs as runsApi, agents as agentsApi, taskSpecs as taskSpecsApi,
  aiGenerate, info as infoApi,
} from '../lib/api.js';
import { NODE_TYPES } from '../components/nodes/index.jsx';
import { ENTRY_BY_ID, NODE_BY_TYPE, defaultNodeName, entryForNode } from '../designer/nodeCatalog.js';
import NodeCreator from '../designer/NodeCreator.jsx';
import InsertableEdge from '../designer/InsertableEdge.jsx';
import { describeNode } from '../designer/describeNode.js';
import { autoLayout, positionAfter, COLUMN_GAP } from '../designer/autoLayout.js';
import { useGraphHistory } from '../designer/useGraphHistory.js';
import { useRunOverlay } from '../designer/useRunOverlay.js';
import { useConnectors } from '../lib/useConnectors.js';
import { AppIcon } from '../components/AppIcon.jsx';
import { useToast } from '../components/Layout.jsx';
import { sampleContext } from '../lib/expr.js';

let nodeSeq = Date.now() % 100000;
const uid = () => `node_${++nodeSeq}`;

const EDGE_STYLE = {
  type: 'knott',
  markerEnd: { type: MarkerType.ArrowClosed, width: 14, height: 14 },
};
const EDGE_TYPES = { knott: InsertableEdge };

export function isCanvasDoubleClickTarget(target) {
  return !target.closest?.(
    '.react-flow__controls, .react-flow__minimap, .react-flow__node, .react-flow__edge, .kn-edge-tools, .canvas-toolbar, button, input, textarea, select, a',
  );
}

// ─── Definition ⇄ canvas ──────────────────────────────────────────────────────
//
// The graph on screen and the definition on disk hold the same information in
// two shapes. Every edge carries the handle it left from, which is what lets a
// condition's branches and a node's error output survive a save.

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
      const step = { ...stripRuntime(n.data), id: n.id, type: n.type, position: n.position };
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

function DesignerCanvas({ workflowId, onBack, NodePropsEditor, theme }) {
  const [wf, setWf] = useState(null);
  const [nodes, setNodes, onNodesChange] = useNodesState([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState([]);
  const [selectedId, setSelectedId] = useState(null);
  const [inspectorTab, setInspectorTab] = useState('setup');
  const [showJSON, setShowJSON] = useState(false);
  const [saving, setSaving] = useState(false);
  const [dirty, setDirty] = useState(false);
  const [savedAt, setSavedAt] = useState(null);
  const [findings, setFindings] = useState(null);
  const [runOpen, setRunOpen] = useState(false);
  const [showShortcuts, setShowShortcuts] = useState(false);
  const [showMinimap, setShowMinimap] = useState(() => localStorage.getItem('knott-minimap') !== 'off');
  const [loadError, setLoadError] = useState(null);
  const [loading, setLoading] = useState(!!workflowId);
  const [runInput, setRunInput] = useState('{\n  \n}');
  const [agentOpts, setAgentOpts] = useState([]);
  const [taskSpecOpts, setTaskSpecOpts] = useState([]);
  const [testInput, setTestInput] = useState('{\n  \n}');
  const [creator, setCreator] = useState(null); // { from, handle } | { insert } | { at } | null
  const [dragging, setDragging] = useState(false);
  const [publicURL, setPublicURL] = useState('');

  const overlay = useRunOverlay(workflowId || wf?.id);
  const { connectors } = useConnectors();

  const canvasRef = useRef(null);
  const clipboard = useRef(null);
  const dragStart = useRef(null);
  const { toast } = useToast();
  const { screenToFlowPosition, fitView } = useReactFlow();

  const selected = useMemo(() => nodes.find(n => n.id === selectedId) || null, [nodes, selectedId]);
  const hasTrigger = nodes.some(n => n.type === 'trigger');

  // ── Undo / redo ────────────────────────────────────────────────────────────
  const snapshot = useCallback(
    () => JSON.stringify({
      nodes: nodes.map(({ id, type, position, data }) => ({ id, type, position, data: stripRuntime(data) })),
      edges: edges.map(({ id, source, target, sourceHandle, label, className, data }) =>
        ({ id, source, target, sourceHandle, label, className, data: { kind: data?.kind } })),
    }),
    [nodes, edges],
  );
  const restore = useCallback(raw => {
    const { nodes: n, edges: e } = JSON.parse(raw);
    setNodes(n);
    setEdges(e.map(x => ({ ...x, ...EDGE_STYLE })));
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
    agentsApi.list().then(r => setAgentOpts(r.data || [])).catch(() => setAgentOpts([]));
    taskSpecsApi.list().then(r => setTaskSpecOpts(r.data || [])).catch(() => setTaskSpecOpts([]));
    infoApi.get().then(r => setPublicURL(r.public_url || '')).catch(() => {});
  }, []);

  const loadWorkflow = useCallback(() => {
    if (!workflowId) {
      setWf({ name: 'Untitled workflow', status: 'draft', definition: { trigger: { type: 'api' }, steps: [] } });
      setNodes([]);
      setEdges([]);
      setLoading(false);
      return;
    }
    setLoading(true);
    setLoadError(null);
    wfApi.get(workflowId).then(w => {
      setWf(w);
      applyDefinition(parseDefinition(w.definition, toast));
      setSavedAt(w.updated_at ? new Date(w.updated_at) : null);
      history.reset();
      setDirty(false);
      setLoading(false);
      try { setRunInput(localStorage.getItem(`knott-run-input:${workflowId}`) || '{\n  \n}'); } catch { /* ignore */ }
    }).catch(e => {
      setLoadError(e.message || 'Could not load this workflow');
      setLoading(false);
    });
  }, [workflowId]); // eslint-disable-line react-hooks/exhaustive-deps

  function applyDefinition(def) {
    const { nodes: n, edges: e } = defToFlow(def || { steps: [] });
    const notes = (def?.annotations || []).map(a => ({
      id: a.id || uid(), type: 'note', position: a.position || { x: 40, y: 40 },
      data: { id: a.id, type: 'note', notes: a.text },
    }));
    // Workflows laid out for narrower cards (or generated without positions)
    // would open with steps stacked on each other; lay those out once.
    if (overlapping(n)) {
      const positions = autoLayout(n, e, { isAnnotation: x => x.type === 'note' });
      n.forEach(x => { if (positions[x.id]) x.position = positions[x.id]; });
    }
    setNodes([...n, ...notes]);
    setEdges(e);
    requestAnimationFrame(() => fitView({ padding: 0.25, maxZoom: 1.1 }));
  }

  useEffect(() => { loadWorkflow(); }, [loadWorkflow]);

  // ── Building ───────────────────────────────────────────────────────────────

  /** Add a step described by a creator pick, wired according to its context. */
  const addFromPick = useCallback((pick, ctx = {}, dropAt = null) => {
    const entry = pick.entry;
    if (entry.unique && nodes.some(n => n.type === entry.type)) {
      toast(`A workflow has one ${entry.label.toLowerCase()} — change its type in the inspector`, 'warning');
      return null;
    }
    const id = uid();
    const names = nodes.map(n => n.data?.name).filter(Boolean);
    const preset = structuredClone(entry.preset || {});
    const data = {
      id, type: entry.type,
      name: defaultNodeName(pick.connector ? `${pick.connector.name}: ${pick.action?.label || 'Action'}` : entry.label, names),
      inputs: {},
      ...preset,
      config: { ...(preset.config || {}) },
    };
    if (pick.connector) {
      data.config.connector_id = pick.connector.slug;
      if (pick.action?.id) data.config.action = pick.action.id;
      for (const f of pick.action?.fields || []) {
        if (f.default !== undefined && data.config[f.name] === undefined) data.config[f.name] = f.default;
      }
    }
    if (entry.type === 'note') data.notes = '';

    let position;
    let rewire = null;
    const source = ctx.from ? nodes.find(n => n.id === ctx.from) : null;
    if (dropAt) {
      position = dropAt;
    } else if (ctx.insert) {
      const edge = edges.find(e => e.id === ctx.insert);
      const target = edge && nodes.find(n => n.id === edge.target);
      if (edge && target) {
        position = { ...target.position };
        rewire = edge;
      }
    }
    if (!position && ctx.at) position = ctx.at;
    if (!position) position = source ? positionAfter(source, nodes) : freeSpot(nodes, screenToFlowPosition, canvasRef);

    const node = { id, type: entry.type, position, data };

    change(() => {
      if (rewire) {
        // Make room: everything downstream of the insertion point shifts right.
        const downstream = reachable(rewire.target, edges);
        setNodes(ns => [...ns.map(n => downstream.has(n.id)
          ? { ...n, position: { x: n.position.x + COLUMN_GAP, y: n.position.y } } : n), node]);
        setEdges(es => {
          let next = es.filter(e => e.id !== rewire.id);
          next = connect(next, { source: rewire.source, sourceHandle: rewire.sourceHandle, target: id });
          if (!['end', 'stop_error', 'note'].includes(entry.type)) {
            next = connect(next, { source: id, sourceHandle: 'main', target: rewire.target });
          }
          return next;
        });
      } else {
        setNodes(ns => [...ns, node]);
        if (ctx.from) setEdges(es => connect(es, { source: ctx.from, target: id, sourceHandle: ctx.handle || 'main' }));
      }
    });
    setSelectedId(entry.type === 'note' ? null : id);
    setInspectorTab('setup');
    return id;
  }, [nodes, edges, change, setNodes, setEdges, toast, screenToFlowPosition]);

  /** Add an edge, replacing any existing edge from the same output. */
  function connect(existing, params) {
    const handle = params.sourceHandle || 'main';
    const kind = handle === 'error' ? 'error' : handle === 'main' ? 'main' : 'branch';
    const kept = existing.filter(e => !(e.source === params.source && (e.sourceHandle || 'main') === handle));
    return addEdge({
      ...params,
      sourceHandle: handle,
      id: `e-${params.source}-${handle}-${params.target}`,
      className: kind,
      data: { kind },
      label: kind === 'error' ? 'on error' : handle === 'default' ? 'otherwise' : undefined,
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

  const openCreator = useCallback(ctx => { setCreator(ctx || {}); }, []);
  const openCreatorFor = useCallback((from, handle) => openCreator({ from, handle }), [openCreator]);
  const removeEdge = useCallback(id => change(() => setEdges(es => es.filter(e => e.id !== id))), [change, setEdges]);
  const insertOnEdge = useCallback(id => openCreator({ insert: id }), [openCreator]);

  // Presentation and handlers travel in data; stripped again on save.
  const errorWired = useMemo(() => new Set(edges.filter(e => e.sourceHandle === 'error').map(e => e.source)), [edges]);
  const decorated = useMemo(() => nodes.map(n => ({
    ...n,
    selected: n.id === selectedId || n.selected,
    data: {
      ...n.data,
      __onAppend: openCreatorFor,
      __errorWired: errorWired.has(n.id),
      __run: overlay.byNode?.[n.id],
      __meta: n.type === 'note' ? undefined : describeNode(n.type, n.data, connectors),
    },
  })), [nodes, selectedId, openCreatorFor, overlay.byNode, connectors, errorWired]);

  const decoratedEdges = useMemo(() => edges.map(e => {
    const state = overlay.byNode?.[e.source];
    const ran = state && state.status === 'done' && overlay.byNode?.[e.target];
    return {
      ...e,
      ...EDGE_STYLE,
      animated: !!(state && state.status === 'running'),
      className: `${e.className || ''}${ran ? ' ran' : ''}`,
      data: { ...e.data, onInsert: insertOnEdge, onRemove: removeEdge },
    };
  }), [edges, insertOnEdge, removeEdge, overlay.byNode]);

  const onPaneDoubleClick = useCallback(e => {
    // ReactFlow's generic double-click event also bubbles from its zoom controls.
    // Only canvas/pane double-clicks are an add-step gesture.
    if (!isCanvasDoubleClickTarget(e.target)) return;
    const position = screenToFlowPosition({ x: e.clientX, y: e.clientY });
    openCreator({ at: { x: position.x - 130, y: position.y - 40 } });
  }, [screenToFlowPosition, openCreator]);

  const deleteSelection = useCallback(() => {
    const nodeIds = new Set(nodes.filter(n => n.selected || n.id === selectedId).map(n => n.id));
    const edgeIds = new Set(edges.filter(e => e.selected).map(e => e.id));
    if (!nodeIds.size && !edgeIds.size) return;
    change(() => {
      // Deleting a step in the middle of a chain reconnects its neighbours, so
      // removing one step does not silently cut the workflow in two.
      setEdges(es => {
        let next = es.filter(e => !edgeIds.has(e.id));
        for (const id of nodeIds) {
          const incoming = next.filter(e => e.target === id && !nodeIds.has(e.source));
          const outgoing = next.filter(e => e.source === id && (e.sourceHandle || 'main') === 'main' && !nodeIds.has(e.target));
          next = next.filter(e => e.source !== id && e.target !== id);
          if (incoming.length === 1 && outgoing.length === 1) {
            next = connect(next, { source: incoming[0].source, sourceHandle: incoming[0].sourceHandle, target: outgoing[0].target });
          }
        }
        return next;
      });
      setNodes(ns => ns.filter(n => !nodeIds.has(n.id)));
    });
    setSelectedId(null);
  }, [nodes, edges, selectedId, change, setNodes, setEdges]);

  const duplicateSelection = useCallback(() => {
    const picked = nodes.filter(n => n.selected || n.id === selectedId);
    if (!picked.length) return;
    if (picked.some(n => n.type === 'trigger')) { toast('A workflow has one trigger', 'warning'); return; }
    const idMap = {};
    const copies = picked.map(n => {
      const id = uid();
      idMap[n.id] = id;
      return {
        ...n, id, selected: false,
        position: { x: n.position.x + 40, y: n.position.y + 80 },
        data: { ...stripRuntime(n.data), id, name: `${n.data.name || n.type} copy` },
      };
    });
    const inner = edges.filter(e => idMap[e.source] && idMap[e.target]).map(e => ({
      ...e, id: `e-${idMap[e.source]}-${e.sourceHandle || 'main'}-${idMap[e.target]}`,
      source: idMap[e.source], target: idMap[e.target],
    }));
    change(() => { setNodes(ns => [...ns, ...copies]); setEdges(es => [...es, ...inner]); });
    setSelectedId(copies[0].id);
  }, [nodes, edges, selectedId, change, setNodes, setEdges, toast]);

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
    const copies = buf.nodes.filter(n => n.type !== 'trigger' || !hasTrigger).map(n => {
      const id = uid();
      idMap[n.id] = id;
      return { ...n, id, selected: false, position: { x: n.position.x + 60, y: n.position.y + 60 }, data: { ...n.data, id } };
    });
    const inner = buf.edges.filter(e => idMap[e.source] && idMap[e.target]).map(e => ({
      ...e, id: `e-${idMap[e.source]}-${e.sourceHandle || 'main'}-${idMap[e.target]}`,
      source: idMap[e.source], target: idMap[e.target],
    }));
    change(() => { setNodes(ns => [...ns, ...copies]); setEdges(es => [...es, ...inner]); });
    if (copies[0]) setSelectedId(copies[0].id);
  }, [change, setNodes, setEdges, hasTrigger]);

  const tidy = useCallback(() => {
    if (!nodes.length) return;
    const positions = autoLayout(nodes, edges, { isAnnotation: n => n.type === 'note' });
    change(() => setNodes(ns => ns.map(n => positions[n.id] ? { ...n, position: positions[n.id] } : n)));
    requestAnimationFrame(() => fitView({ padding: 0.25, duration: 300, maxZoom: 1.1 }));
  }, [nodes, edges, change, setNodes, fitView]);

  const addNote = useCallback(() => {
    const at = freeSpot(nodes, screenToFlowPosition, canvasRef);
    addFromPick({ entry: ENTRY_BY_ID.note }, {}, { x: at.x, y: at.y - 140 });
  }, [nodes, screenToFlowPosition, addFromPick]);

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

  const handleSave = useCallback(async ({ quiet = false } = {}) => {
    setSaving(true);
    try {
      const def = flowToDef(nodes, edges, wf?.definition?.trigger);
      const body = {
        name: wf?.name || 'Untitled workflow',
        description: wf?.description,
        status: wf?.status || 'draft',
        definition: def,
        tags: wf?.tags || [],
      };
      const id = workflowId || wf?.id;
      const saved = id ? await wfApi.update(id, body) : await wfApi.create(body);
      setWf(saved);
      setDirty(false);
      setSavedAt(new Date());
      if (!quiet) toast('Workflow saved', 'success');
      return saved;
    } catch (e) {
      toast('Could not save the workflow', 'error', e.message);
      return null;
    } finally {
      setSaving(false);
    }
  }, [nodes, edges, wf, workflowId, toast]);

  async function handleValidate() {
    try {
      const def = flowToDef(nodes, edges, wf?.definition?.trigger);
      const res = await wfApi.validate(workflowId || wf?.id || 'new', def);
      const errors = res.errors || [];
      const warnings = res.warnings || [];
      setFindings({ errors, warnings });
      if (!errors.length && !warnings.length) toast('This workflow is ready to run', 'success');
    } catch (e) {
      toast('Could not validate', 'error', e.message);
    }
  }

  async function handleRun() {
    let inputData;
    try { inputData = JSON.parse(runInput || '{}'); } catch { toast('That input is not valid JSON', 'error'); return; }
    const saved = await handleSave({ quiet: true });
    const id = saved?.id || workflowId || wf?.id;
    if (!id) return;
    try { localStorage.setItem(`knott-run-input:${id}`, runInput); } catch { /* ignore */ }
    try {
      const r = await runsApi.create({ workflow_id: id, input_data: inputData });
      toast('Run started', 'success', 'Follow it on the canvas');
      setRunOpen(false);
      overlay.watch(r.id);
    } catch (e) {
      toast('Could not start the run', 'error', e.message);
    }
  }

  async function toggleActive() {
    const next = wf?.status === 'active' ? 'draft' : 'active';
    setWf(w => ({ ...w, status: next }));
    setDirty(true);
    toast(next === 'active' ? 'Workflow will be active once saved' : 'Workflow will be a draft once saved', 'info');
  }

  function handleBack() {
    if (dirty && !confirm('This workflow has unsaved changes. Leave anyway?')) return;
    onBack();
  }

  // ── Keyboard ───────────────────────────────────────────────────────────────
  useEffect(() => {
    function onKey(e) {
      const target = e.target;
      const typing = target && (target.tagName === 'INPUT' || target.tagName === 'TEXTAREA' ||
        target.tagName === 'SELECT' || target.isContentEditable);
      const mod = e.metaKey || e.ctrlKey;

      if (mod && e.key.toLowerCase() === 's') { e.preventDefault(); handleSave(); return; }
      if (e.key === 'Escape') {
        if (creator) setCreator(null);
        else if (runOpen) setRunOpen(false);
        else if (showShortcuts) setShowShortcuts(false);
        else setSelectedId(null);
        return;
      }
      if (typing) return;

      if (e.key === 'Tab' && !mod) {
        e.preventDefault();
        openCreator(selectedId ? { from: selectedId, handle: 'main' } : {});
        return;
      }
      if (e.key === '?') { setShowShortcuts(s => !s); return; }
      if (mod && e.key.toLowerCase() === 'z') { e.preventDefault(); (e.shiftKey ? history.redo : history.undo)(); return; }
      if (mod && e.key.toLowerCase() === 'y') { e.preventDefault(); history.redo(); return; }
      if (mod && e.key.toLowerCase() === 'd') { e.preventDefault(); duplicateSelection(); return; }
      if (mod && e.key.toLowerCase() === 'c') { copySelection(); return; }
      if (mod && e.key.toLowerCase() === 'v') { paste(); return; }
      if (mod && e.key === 'Enter') { e.preventDefault(); setRunOpen(true); return; }
      if (mod && e.shiftKey && e.key.toLowerCase() === 'l') { e.preventDefault(); tidy(); return; }
      if (e.key === '1' && e.shiftKey) { fitView({ padding: 0.25, duration: 250, maxZoom: 1.1 }); return; }
      if (e.key === 'Delete' || e.key === 'Backspace') { e.preventDefault(); deleteSelection(); }
    }
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [selectedId, history, duplicateSelection, copySelection, paste, tidy, deleteSelection, creator, runOpen, showShortcuts, openCreator, handleSave, fitView]);

  // Warn before losing unsaved work on a navigation.
  useEffect(() => {
    if (!dirty) return;
    const warn = e => { e.preventDefault(); e.returnValue = ''; };
    window.addEventListener('beforeunload', warn);
    return () => window.removeEventListener('beforeunload', warn);
  }, [dirty]);

  // ── Render ─────────────────────────────────────────────────────────────────
  if (loadError || loading) {
    return (
      <div className="studio">
        <header className="studio-bar">
          <button className="btn btn-ghost btn-sm" onClick={onBack}><ArrowLeft size={14} /> Workflows</button>
        </header>
        <div className="studio-state">
          {loadError ? (
            <>
              <AlertTriangle size={36} color="var(--red)" />
              <h3>Couldn’t load this workflow</h3>
              <p>{loadError}</p>
              <div style={{ display: 'flex', gap: 8 }}>
                <button className="btn btn-secondary" onClick={onBack}>Back to workflows</button>
                <button className="btn btn-primary" onClick={loadWorkflow}>Try again</button>
              </div>
            </>
          ) : <div className="spinner spinner-lg" />}
        </div>
      </div>
    );
  }

  const status = wf?.status || 'draft';
  const inspectorOpen = !!selected && !creator && selected.type !== 'note';
  const errorCount = findings?.errors?.length || 0;
  const warnCount = findings?.warnings?.length || 0;

  return (
    <div className="studio">
      <header className="studio-bar">
        <button className="btn btn-ghost btn-sm" onClick={handleBack} title="Back to workflows"><ArrowLeft size={14} /></button>
        <input
          className="studio-name"
          value={wf?.name || ''}
          onChange={e => { setWf(w => ({ ...w, name: e.target.value })); setDirty(true); }}
          placeholder="Name this workflow"
          aria-label="Workflow name"
        />
        <SaveState dirty={dirty} saving={saving} savedAt={savedAt} />

        <div className="studio-bar-center">
          <div className="seg" role="tablist" aria-label="View">
            <button role="tab" aria-selected={!showJSON} className={!showJSON ? 'on' : ''} onClick={() => setShowJSON(false)}>Canvas</button>
            <button role="tab" aria-selected={showJSON} className={showJSON ? 'on' : ''} onClick={() => setShowJSON(true)}><Code2 size={13} /> JSON</button>
          </div>
        </div>

        <div className="studio-bar-right">
          <button className="icon-btn" onClick={history.undo} disabled={!history.canUndo} title="Undo (Ctrl+Z)" aria-label="Undo"><Undo2 size={16} /></button>
          <button className="icon-btn" onClick={history.redo} disabled={!history.canRedo} title="Redo (Ctrl+Shift+Z)" aria-label="Redo"><Redo2 size={16} /></button>
          <span className="bar-sep" />
          <button className={`icon-btn ${overlay.active ? 'on' : ''}`}
            onClick={async () => {
              if (overlay.active) { overlay.stop(); return; }
              if (!(await overlay.watchLatest())) toast('This workflow has not run yet', 'info');
            }}
            title={overlay.active ? 'Hide the run' : 'Show the latest run on the canvas'} aria-label="Show the latest run">
            <Activity size={16} />
          </button>
          <button className={`btn btn-sm btn-ghost ${errorCount ? 'has-errors' : warnCount ? 'has-warnings' : ''}`} onClick={handleValidate}>
            {errorCount ? <AlertTriangle size={14} /> : <CheckCircle size={14} />}
            {errorCount ? `${errorCount} to fix` : warnCount ? `${warnCount} to check` : 'Validate'}
          </button>
          <label className={`kn-switch ${status === 'active' ? 'on' : ''}`} title="Active workflows run from their trigger">
            <input type="checkbox" checked={status === 'active'} onChange={toggleActive} aria-label="Active" />
            <span className="kn-switch-track"><span className="kn-switch-thumb" /></span>
            <span className="kn-switch-label">{status === 'active' ? 'Active' : status === 'deprecated' ? 'Deprecated' : 'Draft'}</span>
          </label>
          <button className="btn btn-secondary btn-sm" onClick={() => handleSave()} disabled={saving} title="Save (Ctrl+S)">
            {saving ? <span className="spinner-sm" /> : <Save size={14} />} Save
          </button>
          <button className="btn btn-primary btn-sm" onClick={() => setRunOpen(o => !o)} title="Test run (Ctrl+Enter)">
            <Play size={14} /> Test run
          </button>
        </div>
      </header>

      <div className="studio-body">
        {showJSON ? (
          <div className="studio-json">
            <pre className="code-block">{JSON.stringify(flowToDef(nodes, edges, wf?.definition?.trigger), null, 2)}</pre>
          </div>
        ) : (
          <div className={`studio-canvas ${dragging ? 'accepting-drop' : ''} ${inspectorOpen || creator ? 'panel-open' : ''}`} ref={canvasRef}>
            <ReactFlow
              nodes={decorated}
              edges={decoratedEdges}
              onNodesChange={onNodesChange}
              onEdgesChange={onEdgesChange}
              onConnect={onConnect}
              onNodeClick={(_, n) => { setSelectedId(n.id); setCreator(null); }}
              onNodeDoubleClick={(_, n) => { setSelectedId(n.id); setInspectorTab('setup'); }}
              onNodeDragStart={() => { dragStart.current = snapshot(); }}
              onNodeDragStop={() => {
                const before = dragStart.current;
                dragStart.current = null;
                if (before && before !== snapshot()) { history.record(before); setDirty(true); }
              }}
              onPaneClick={() => { setSelectedId(null); setCreator(null); }}
              onDoubleClick={onPaneDoubleClick}
              nodeTypes={NODE_TYPES}
              edgeTypes={EDGE_TYPES}
              defaultEdgeOptions={EDGE_STYLE}
              colorMode={theme === 'dark' ? 'dark' : theme === 'light' ? 'light' : 'system'}
              fitView
              fitViewOptions={{ padding: 0.25, maxZoom: 1.1 }}
              minZoom={0.2}
              maxZoom={2}
              zoomOnDoubleClick={false}
              connectionRadius={36}
              deleteKeyCode={null}
              snapToGrid
              snapGrid={[10, 10]}
              proOptions={{ hideAttribution: true }}
            >
              <Background variant={BackgroundVariant.Dots} gap={24} size={1.2} color="var(--canvas-dot)" />
              <Controls showInteractive={false} position="bottom-left" onDoubleClick={e => e.stopPropagation()} />
              {showMinimap && (
                <MiniMap pannable zoomable position="bottom-right"
                  nodeColor={n => entryForNode(n.type, n.data)?.color || 'var(--text-muted)'}
                  maskColor="var(--minimap-mask)" />
              )}
            </ReactFlow>

            <div className="canvas-toolbar" role="toolbar" aria-label="Canvas tools">
              <button className="tool primary" onClick={() => openCreator(selectedId ? { from: selectedId, handle: 'main' } : {})} title="Add a step (Tab)" aria-label="Add a step"><Plus size={18} /></button>
              <button className="tool" onClick={addNote} title="Add a sticky note" aria-label="Add a sticky note"><StickyNote size={16} /></button>
              <button className="tool" onClick={tidy} title="Tidy up (Ctrl+Shift+L)" aria-label="Tidy up"><LayoutGrid size={16} /></button>
              <button className="tool" onClick={() => fitView({ padding: 0.25, duration: 250, maxZoom: 1.1 })} title="Fit to view (Shift+1)" aria-label="Fit to view"><Maximize2 size={16} /></button>
              <button className={`tool ${showMinimap ? 'on' : ''}`} onClick={() => setShowMinimap(v => { localStorage.setItem('knott-minimap', v ? 'off' : 'on'); return !v; })} title="Minimap" aria-label="Toggle minimap"><MapIcon size={16} /></button>
              <button className="tool" onClick={() => setShowShortcuts(true)} title="Keyboard shortcuts (?)" aria-label="Keyboard shortcuts"><Keyboard size={16} /></button>
            </div>

            {overlay.active && <RunStrip overlay={overlay} nodes={nodes} onSelect={id => { setSelectedId(id); setInspectorTab('output'); }} />}
            {findings && (errorCount > 0 || warnCount > 0) && <FindingsPanel findings={findings} onClose={() => setFindings(null)} />}

            {nodes.length === 0 && (
              <EmptyCanvas
                onStart={() => openCreator({})}
                onGenerated={def => { change(() => applyDefinition(def)); }}
                onName={name => setWf(w => ({ ...w, name: w?.name && w.name !== 'Untitled workflow' ? w.name : name }))}
              />
            )}

            {runOpen && (
              <RunPopover value={runInput} onChange={setRunInput} onRun={handleRun} onClose={() => setRunOpen(false)}
                schema={nodes.find(n => n.type === 'trigger')?.data?.config?.input_schema} />
            )}

            <NodeCreator
              open={!!creator}
              context={creator}
              connectors={connectors}
              hasTrigger={hasTrigger}
              canvasRef={canvasRef}
              onDragging={setDragging}
              onClose={() => setCreator(null)}
              onPick={pick => { const ctx = creator; setCreator(null); addFromPick(pick, ctx); }}
              onPlace={(row, point) => {
                const p = screenToFlowPosition(point);
                const pick = row.connector ? { entry: ENTRY_BY_ID.tool_call, connector: row.connector, action: row.action || row.connector.actions?.[0] } : { entry: row.entry };
                setCreator(null);
                addFromPick(pick, {}, { x: p.x - 130, y: p.y - 40 });
              }}
            />

            {inspectorOpen && (
              <Inspector
                node={selected}
                tab={inspectorTab}
                setTab={setInspectorTab}
                onClose={() => setSelectedId(null)}
                onChange={updateSelectedData}
                onDelete={deleteSelection}
                onDuplicate={duplicateSelection}
                run={overlay.byNode?.[selected.id]}
                runActive={overlay.active}
                connectors={connectors}
              >
                <NodePropsEditor
                  section={inspectorTab}
                  node={selected}
                  onChange={updateSelectedData}
                  connectors={connectors}
                  agentOpts={agentOpts}
                  taskSpecOpts={taskSpecOpts}
                  previewCtx={previewCtx}
                  testInput={testInput}
                  setTestInput={setTestInput}
                  workflowId={workflowId || wf?.id}
                  publicURL={publicURL}
                  nodes={nodes}
                />
              </Inspector>
            )}
          </div>
        )}
      </div>

      {showShortcuts && <ShortcutSheet onClose={() => setShowShortcuts(false)} />}
    </div>
  );
}

// ─── Pieces ────────────────────────────────────────────────────────────────────

function SaveState({ dirty, saving, savedAt }) {
  const [, tick] = useState(0);
  useEffect(() => { const t = setInterval(() => tick(x => x + 1), 30000); return () => clearInterval(t); }, []);
  if (saving) return <span className="save-state">Saving…</span>;
  if (dirty) return <span className="save-state dirty">Unsaved changes</span>;
  if (savedAt) return <span className="save-state">Saved {relative(savedAt)}</span>;
  return <span className="save-state">Not saved yet</span>;
}

function relative(date) {
  const s = Math.round((Date.now() - date.getTime()) / 1000);
  if (s < 45) return 'just now';
  if (s < 3600) return `${Math.round(s / 60)} min ago`;
  if (s < 86400) return `${Math.round(s / 3600)} h ago`;
  return date.toLocaleDateString();
}

/**
 * The inspector: one panel, three tabs. Setup holds what the step does,
 * Settings how it behaves (reliability, error handling, notes), Output what
 * it produced in the run being shown.
 */
function Inspector({ node, tab, setTab, onClose, onDelete, onDuplicate, run, runActive, connectors, children }) {
  const meta = describeNode(node.type, node.data, connectors);
  const entry = meta.entry;
  const Icon = entry.icon;
  const tabs = [
    { id: 'setup', label: 'Setup', icon: SlidersHorizontal },
    { id: 'settings', label: 'Settings', icon: Settings2 },
    { id: 'output', label: 'Output', icon: FileOutput, dot: run?.status },
  ];
  return (
    <aside className="inspector" aria-label="Step inspector">
      <div className="inspector-head" style={{ '--c': entry.color }}>
        <span className="inspector-icon">
          {meta.connector ? <AppIcon connector={meta.connector} size={32} /> : <Icon size={18} />}
        </span>
        <div className="inspector-titles">
          <div className="inspector-kind">{meta.kind}</div>
          <div className="inspector-name">{node.data?.name || node.id}</div>
        </div>
        <button className="icon-btn" onClick={onDuplicate} title="Duplicate (Ctrl+D)" aria-label="Duplicate"><Copy size={15} /></button>
        <button className="icon-btn danger" onClick={onDelete} title="Delete (Del)" aria-label="Delete"><Trash2 size={15} /></button>
        <button className="icon-btn" onClick={onClose} title="Close (Esc)" aria-label="Close"><X size={16} /></button>
      </div>
      <div className="inspector-tabs" role="tablist">
        {tabs.map(t => (
          <button key={t.id} role="tab" aria-selected={tab === t.id} className={tab === t.id ? 'on' : ''} onClick={() => setTab(t.id)}>
            <t.icon size={13} /> {t.label}{t.dot && <span className={`tab-dot ${t.dot}`} />}
          </button>
        ))}
      </div>
      <div className="inspector-body">
        {tab === 'output' ? <OutputView run={run} runActive={runActive} /> : children}
      </div>
    </aside>
  );
}

function OutputView({ run, runActive }) {
  if (!runActive) {
    return (
      <div className="inspector-empty">
        <FileOutput size={28} />
        <p>Run the workflow, or show its latest run, to see what this step produced.</p>
      </div>
    );
  }
  if (!run) return <div className="inspector-empty"><p>This step did not run.</p></div>;
  return (
    <div className="output-view">
      <div className={`output-status ${run.status}`}>
        {run.status === 'failed' ? 'Failed' : run.status === 'running' ? (run.retrying ? `Retrying — ${run.retrying}` : 'Running…') : run.status === 'waiting' ? 'Waiting' : 'Completed'}
        {run.at && <span className="muted"> · {new Date(run.at).toLocaleTimeString()}</span>}
      </div>
      {run.error && <pre className="code-block error">{run.error}</pre>}
      {run.routedTo && <p className="form-hint">The failure was routed to <code>{run.routedTo}</code>.</p>}
      {run.output && Object.keys(run.output).length > 0 && (
        <>
          <div className="form-label">Output</div>
          <pre className="code-block">{JSON.stringify(run.output, null, 2)}</pre>
        </>
      )}
    </div>
  );
}

function RunPopover({ value, onChange, onRun, onClose, schema }) {
  let valid = true;
  try { JSON.parse(value || '{}'); } catch { valid = false; }
  function fromSchema() {
    const sample = {};
    for (const [k, spec] of Object.entries(schema || {})) {
      sample[k] = spec?.default ?? (spec?.type === 'number' ? 0 : spec?.type === 'boolean' ? false : spec?.type === 'object' ? {} : '');
    }
    onChange(JSON.stringify(sample, null, 2));
  }
  return (
    <div className="run-popover" role="dialog" aria-label="Test run">
      <div className="run-popover-head">
        <strong>Test run</strong>
        <button className="icon-btn" onClick={onClose} aria-label="Close"><X size={15} /></button>
      </div>
      <label className="form-label">Input (JSON)</label>
      <textarea className="textarea mono" rows={9} value={value} onChange={e => onChange(e.target.value)} spellCheck={false} autoFocus />
      <div className="run-popover-foot">
        <span className={`form-hint ${valid ? '' : 'error'}`}>{valid ? <>Available to every step as <code>input</code>.</> : 'Not valid JSON'}</span>
        {schema && Object.keys(schema).length > 0 && <button className="btn btn-ghost btn-sm" onClick={fromSchema}>Fill from schema</button>}
        <button className="btn btn-primary btn-sm" onClick={onRun} disabled={!valid}><Play size={13} /> Save & run</button>
      </div>
    </div>
  );
}

/**
 * The empty canvas offers the two ways to begin: pick a trigger, or describe
 * the automation and let the AI draft it.
 */
function EmptyCanvas({ onStart, onGenerated, onName }) {
  const [prompt, setPrompt] = useState('');
  const [busy, setBusy] = useState(false);
  const [note, setNote] = useState('');
  const { toast } = useToast();
  async function generate() {
    if (!prompt.trim()) return;
    setBusy(true);
    setNote('');
    try {
      const r = await aiGenerate.workflow(prompt.trim());
      onGenerated(r.workflow);
      if (r.workflow?.name) onName(r.workflow.name);
      if (r.generator === 'template') {
        toast('Drafted from a template', 'info', r.fallback_reason ? `The AI model failed: ${r.fallback_reason}` : 'Connect an AI model in Settings for a draft tailored to your description.');
      } else {
        toast(`Drafted by ${r.model_id}`, 'success', (r.warnings || []).length ? `${r.warnings.length} thing(s) to check` : 'Review each step before running it.');
      }
    } catch (e) {
      setNote(e.message);
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="canvas-empty">
      <h2>Build your workflow</h2>
      <p>Every workflow starts with a trigger — a webhook, a schedule, or the Run button.</p>
      <button className="btn btn-primary" onClick={onStart}><Plus size={15} /> Add the first step</button>
      <div className="or"><span>or describe it</span></div>
      <div className="generate">
        <textarea className="textarea" rows={3} value={prompt} onChange={e => setPrompt(e.target.value)}
          placeholder="e.g. When a Typeform response arrives, summarise it with AI and post it to Slack. Escalate complaints to a person."
          onKeyDown={e => { if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') generate(); }} />
        <button className="btn btn-secondary" onClick={generate} disabled={busy || !prompt.trim()}>
          {busy ? <span className="spinner-sm" /> : <Wand2 size={14} />} Draft with AI
        </button>
        {note && <div className="form-hint error">{note}</div>}
      </div>
      <p className="canvas-empty-hint">Tip: press <kbd>Tab</kbd> anywhere, or double-click the canvas, to add a step.</p>
    </div>
  );
}

/**
 * What validation found, listed rather than counted. Errors block; warnings do
 * not, and neither prevents saving a half-finished draft.
 */
function FindingsPanel({ findings, onClose }) {
  const { errors, warnings } = findings;
  return (
    <div className="findings">
      <div className="findings-head">
        <strong>
          {errors.length > 0
            ? `${errors.length} thing${errors.length === 1 ? '' : 's'} to fix`
            : `${warnings.length} thing${warnings.length === 1 ? '' : 's'} worth checking`}
        </strong>
        <button className="icon-btn" onClick={onClose} aria-label="Dismiss"><X size={14} /></button>
      </div>
      <ul className="findings-list">
        {errors.map((e, i) => <li key={`e${i}`} className="finding is-error"><AlertTriangle size={12} /><span>{e}</span></li>)}
        {warnings.map((w, i) => <li key={`w${i}`} className="finding is-warning"><AlertTriangle size={12} /><span>{w}</span></li>)}
      </ul>
      {errors.length === 0 && <p className="findings-note">None of these stop the workflow running.</p>}
    </div>
  );
}

/** A summary of the run being shown. */
function RunStrip({ overlay, nodes, onSelect }) {
  const run = overlay.run;
  const states = overlay.byNode || {};
  const failed = Object.entries(states).find(([, s]) => s.status === 'failed');
  const failedName = failed && (nodes.find(n => n.id === failed[0])?.data?.name || failed[0]);
  const done = Object.values(states).filter(s => s.status === 'done').length;
  return (
    <div className={`run-strip ${run?.status ? `is-${run.status.toLowerCase()}` : ''}`}>
      <span className="run-strip-dot" />
      <span className="run-strip-status">{run?.status?.replace('_', ' ') || 'Loading…'}</span>
      <span className="run-strip-meta">
        {done} step{done === 1 ? '' : 's'} completed
        {failed && <> · failed at <button className="link" onClick={() => onSelect(failed[0])}>{failedName}</button></>}
      </span>
      {run?.id && <code className="run-strip-id">{run.id.slice(0, 8)}</code>}
      <button className="icon-btn" onClick={overlay.stop} aria-label="Hide this run"><X size={13} /></button>
    </div>
  );
}

const SHORTCUTS = [
  ['Tab', 'Add a step (after the selected one)'],
  ['Double-click canvas', 'Add a step there'],
  ['Ctrl / ⌘ + S', 'Save'],
  ['Ctrl / ⌘ + Enter', 'Test run'],
  ['Ctrl / ⌘ + Z', 'Undo'],
  ['Ctrl / ⌘ + Shift + Z', 'Redo'],
  ['Ctrl / ⌘ + D', 'Duplicate the selection'],
  ['Ctrl / ⌘ + C, V', 'Copy and paste steps'],
  ['Ctrl / ⌘ + Shift + L', 'Tidy the layout'],
  ['Shift + 1', 'Fit everything in view'],
  ['Delete', 'Remove the selection (neighbours are reconnected)'],
  ['Esc', 'Close the panel or deselect'],
  ['?', 'Show this list'],
];

function ShortcutSheet({ onClose }) {
  return (
    <div className="modal-overlay" onClick={onClose}>
      <div className="modal" style={{ maxWidth: 480 }} onClick={e => e.stopPropagation()}>
        <div className="modal-header">
          <div className="modal-title">Keyboard shortcuts</div>
          <button className="icon-btn" onClick={onClose} aria-label="Close"><X size={15} /></button>
        </div>
        <div className="shortcut-list">
          {SHORTCUTS.map(([keys, what]) => (
            <div key={keys} className="shortcut-row"><kbd>{keys}</kbd><span>{what}</span></div>
          ))}
        </div>
      </div>
    </div>
  );
}

// ─── Helpers ───────────────────────────────────────────────────────────────────

function parseDefinition(raw, toast) {
  try {
    return typeof raw === 'string' ? JSON.parse(raw) : raw;
  } catch {
    toast?.('That workflow’s definition could not be read — starting from an empty canvas', 'warning');
    return { trigger: { type: 'api' }, steps: [] };
  }
}

/** Whether any two cards would sit on top of each other. */
function overlapping(nodes) {
  const W = 270, H = 96;
  for (let i = 0; i < nodes.length; i++) {
    for (let j = i + 1; j < nodes.length; j++) {
      const a = nodes[i].position, b = nodes[j].position;
      if (Math.abs(a.x - b.x) < W && Math.abs(a.y - b.y) < H) return true;
    }
  }
  return false;
}

/** Ids reachable from start by following edges, start included. */
function reachable(start, edges) {
  const seen = new Set([start]);
  const queue = [start];
  while (queue.length) {
    const id = queue.shift();
    for (const e of edges) {
      if (e.source === id && !seen.has(e.target)) { seen.add(e.target); queue.push(e.target); }
    }
  }
  return seen;
}

/** A spot near the centre of the visible canvas not already taken. */
function freeSpot(nodes, screenToFlowPosition, canvasRef) {
  const r = canvasRef.current?.getBoundingClientRect();
  const center = r ? screenToFlowPosition({ x: r.left + r.width * 0.4, y: r.top + r.height * 0.45 }) : { x: 200, y: 200 };
  const p = { x: Math.round(center.x / 10) * 10, y: Math.round(center.y / 10) * 10 };
  while (nodes.some(n => Math.abs(n.position.x - p.x) < 200 && Math.abs(n.position.y - p.y) < 90)) p.y += 110;
  return p;
}

/** Strip the handlers and presentation the designer injects into node data. */
function stripRuntime(data) {
  const { __onAppend, __run, __meta, ...rest } = data || {};
  return rest;
}

export { defToFlow, flowToDef, stripRuntime, NODE_BY_TYPE };
