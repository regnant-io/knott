import React from 'react';
import { Handle, Position } from 'reactflow';
import { Zap, Brain, User, GitBranch, Wrench, Bot, Split, CheckCircle, XCircle, Repeat, Code2, Sliders, Filter as FilterIcon, Clock, GitMerge } from 'lucide-react';

function WFNode({ data, selected, typeClass, icon: Icon, typeLabel, color, detail }) {
  const disabled = data.disabled;
  return (
    <div className={`wf-node ${typeClass} ${selected ? 'selected' : ''}`} style={disabled ? { opacity: 0.45 } : undefined}>
      <Handle type="target" position={Position.Left} />
      <div className="wf-node-header">
        <Icon size={11} />
        {typeLabel}{disabled ? ' (off)' : ''}
      </div>
      <div className="wf-node-body">
        <div className="wf-node-name">{data.name || data.id}</div>
        {detail && <div className="wf-node-detail">{detail}</div>}
      </div>
      <Handle type="source" position={Position.Right} />
    </div>
  );
}

export function TriggerNode({ data, selected }) {
  return (
    <div className={`wf-node node-trigger ${selected ? 'selected' : ''}`}>
      <div className="wf-node-header"><Zap size={11} />Trigger</div>
      <div className="wf-node-body">
        <div className="wf-node-name">{data.name || 'API Trigger'}</div>
        <div className="wf-node-detail">{data.config?.type || 'api'}</div>
      </div>
      <Handle type="source" position={Position.Right} />
    </div>
  );
}

export function AIDecisionNode({ data, selected }) {
  return (
    <WFNode data={data} selected={selected}
      typeClass="node-ai_decision" icon={Brain} typeLabel="AI Decision"
      detail={data.config?.task ? `Task: ${data.config.task}` : 'Configure task spec'}
    />
  );
}

export function HumanTaskNode({ data, selected }) {
  return (
    <WFNode data={data} selected={selected}
      typeClass="node-human_task" icon={User} typeLabel="Human Task"
      detail={data.config?.title || 'Awaiting human review'}
    />
  );
}

export function ConditionNode({ data, selected }) {
  const caseCount = data.cases?.length || 0;
  return (
    <WFNode data={data} selected={selected}
      typeClass="node-condition" icon={GitBranch} typeLabel="Condition"
      detail={`${caseCount} case${caseCount !== 1 ? 's' : ''} + default`}
    />
  );
}

export function ToolCallNode({ data, selected }) {
  return (
    <WFNode data={data} selected={selected}
      typeClass="node-tool_call" icon={Wrench} typeLabel="Tool Call"
      detail={data.config?.connector_id ? `→ ${data.config.connector_id}` : 'Configure connector'}
    />
  );
}

export function AgentCallNode({ data, selected }) {
  return (
    <WFNode data={data} selected={selected}
      typeClass="node-agent_call" icon={Bot} typeLabel="Agent Call"
      detail={data.config?.agent_id || 'Select agent'}
    />
  );
}

export function ParallelNode({ data, selected }) {
  return (
    <WFNode data={data} selected={selected}
      typeClass="node-parallel" icon={Split} typeLabel="Parallel"
      detail={`${data.branches?.length || 2} branches`}
    />
  );
}

export function LoopNode({ data, selected }) {
  return (
    <WFNode data={data} selected={selected}
      typeClass="node-loop" icon={Repeat} typeLabel="Loop"
      detail={data.config?.items ? 'for each item' : 'Configure items list'}
    />
  );
}

export function CodeNode({ data, selected }) {
  const n = data.config?.assignments ? Object.keys(data.config.assignments).length : 0;
  return (
    <WFNode data={data} selected={selected}
      typeClass="node-code" icon={Code2} typeLabel="Code"
      detail={n ? `${n} expression${n !== 1 ? 's' : ''}` : 'Compute fields'}
    />
  );
}

export function SetNode({ data, selected }) {
  const n = data.config?.fields ? Object.keys(data.config.fields).length : 0;
  return (
    <WFNode data={data} selected={selected}
      typeClass="node-set" icon={Sliders} typeLabel="Set Fields"
      detail={n ? `${n} field${n !== 1 ? 's' : ''}` : 'Define fields'}
    />
  );
}

export function FilterNode({ data, selected }) {
  return (
    <WFNode data={data} selected={selected}
      typeClass="node-filter" icon={FilterIcon} typeLabel="Filter"
      detail={data.config?.condition || 'Set a condition'}
    />
  );
}

export function WaitNode({ data, selected }) {
  const mode = data.config?.mode || 'duration';
  const detail = mode === 'until' ? `until ${data.config?.until || '…'}` : `${data.config?.seconds || '?'} ${data.config?.unit || 'seconds'}`;
  return (
    <WFNode data={data} selected={selected}
      typeClass="node-wait" icon={Clock} typeLabel="Wait"
      detail={detail}
    />
  );
}

export function MergeNode({ data, selected }) {
  const n = data.config?.sources?.length || 0;
  return (
    <WFNode data={data} selected={selected}
      typeClass="node-merge" icon={GitMerge} typeLabel="Merge"
      detail={n ? `${n} source${n !== 1 ? 's' : ''}` : 'Select sources'}
    />
  );
}

export function TransformNode({ data, selected }) {
  return (
    <WFNode data={data} selected={selected}
      typeClass="node-transform" icon={Sliders} typeLabel="Transform"
      detail="Map outputs"
    />
  );
}

export function EndNode({ data, selected }) {
  const isApproved = data.outcome === 'APPROVED';
  const isRejected = data.outcome === 'REJECTED';
  const Icon = isApproved ? CheckCircle : isRejected ? XCircle : CheckCircle;
  return (
    <div className={`wf-node node-end ${selected ? 'selected' : ''}`}
         style={isRejected ? { '--green': 'var(--red)', '--green-dim': 'var(--red-dim)' } : {}}>
      <Handle type="target" position={Position.Left} />
      <div className="wf-node-header"><Icon size={11} />End</div>
      <div className="wf-node-body">
        <div className="wf-node-name">{data.name || 'End'}</div>
        {data.outcome && (
          <div className="wf-node-detail">
            <span className={`badge badge-${isApproved ? 'green' : isRejected ? 'red' : 'muted'}`} style={{ fontSize: 10, padding: '1px 6px' }}>
              {data.outcome}
            </span>
          </div>
        )}
      </div>
    </div>
  );
}

export const NODE_TYPES = {
  trigger:    TriggerNode,
  ai_decision: AIDecisionNode,
  human_task: HumanTaskNode,
  condition:  ConditionNode,
  tool_call:  ToolCallNode,
  agent_call: AgentCallNode,
  parallel:   ParallelNode,
  loop:       LoopNode,
  code:       CodeNode,
  set:        SetNode,
  filter:     FilterNode,
  wait:       WaitNode,
  merge:      MergeNode,
  transform:  TransformNode,
  end:        EndNode,
};

export const PALETTE_NODES = [
  { type: 'trigger',    label: 'Trigger',     color: 'var(--amber)',  icon: Zap },
  { type: 'ai_decision',label: 'AI Decision', color: 'var(--violet)', icon: Brain },
  { type: 'human_task', label: 'Human Task',  color: 'var(--blue)',   icon: User },
  { type: 'condition',  label: 'Condition',   color: 'var(--yellow)', icon: GitBranch },
  { type: 'filter',     label: 'Filter',      color: 'var(--yellow)', icon: FilterIcon },
  { type: 'loop',       label: 'Loop',        color: 'var(--indigo)', icon: Repeat },
  { type: 'set',        label: 'Set Fields',  color: 'var(--teal)',   icon: Sliders },
  { type: 'code',       label: 'Code',        color: 'var(--teal)',   icon: Code2 },
  { type: 'tool_call',  label: 'Tool Call',   color: 'var(--teal)',   icon: Wrench },
  { type: 'agent_call', label: 'Agent Call',  color: 'var(--pink)',   icon: Bot },
  { type: 'wait',       label: 'Wait',        color: 'var(--blue)',   icon: Clock },
  { type: 'parallel',   label: 'Parallel',    color: 'var(--indigo)', icon: Split },
  { type: 'merge',      label: 'Merge',       color: 'var(--indigo)', icon: GitMerge },
  { type: 'end',        label: 'End',         color: 'var(--green)',  icon: CheckCircle },
];
