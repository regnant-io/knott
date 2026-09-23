// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

import React from 'react';
import { BaseEdge, EdgeLabelRenderer, getBezierPath } from '@xyflow/react';
import { Plus, X } from 'lucide-react';

/**
 * A connection with its own controls.
 *
 * Hovering a connection shows a + to insert a step between the two ends — the
 * most common edit to an existing workflow, which previously meant deleting
 * the edge, adding a node and drawing two new edges — and an × to remove it.
 * Branch and error connections carry their label on the line.
 */
export default function InsertableEdge({
  id, sourceX, sourceY, targetX, targetY, sourcePosition, targetPosition,
  markerEnd, style, data, label, selected,
}) {
  const [path, labelX, labelY] = getBezierPath({
    sourceX, sourceY, sourcePosition, targetX, targetY, targetPosition, curvature: 0.35,
  });
  const kind = data?.kind || 'main';
  return (
    <>
      <BaseEdge id={id} path={path} markerEnd={markerEnd} style={style}
        className={`kn-edge kind-${kind}${selected ? ' selected' : ''}`} interactionWidth={24} />
      <EdgeLabelRenderer>
        <div
          className={`kn-edge-tools kind-${kind}${selected ? ' show' : ''}`}
          style={{ transform: `translate(-50%, -50%) translate(${labelX}px, ${labelY}px)` }}
        >
          {label && <span className="kn-edge-label">{label}</span>}
          <span className="kn-edge-buttons nodrag nopan">
            <button type="button" title="Insert a step here" aria-label="Insert a step here"
              onClick={e => { e.stopPropagation(); data?.onInsert?.(id); }}>
              <Plus size={12} />
            </button>
            <button type="button" title="Remove this connection" aria-label="Remove this connection"
              onClick={e => { e.stopPropagation(); data?.onRemove?.(id); }}>
              <X size={12} />
            </button>
          </span>
        </div>
      </EdgeLabelRenderer>
    </>
  );
}
