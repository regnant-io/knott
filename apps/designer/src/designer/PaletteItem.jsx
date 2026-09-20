// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0
import React, { useRef, useState } from 'react';
import { createPortal } from 'react-dom';

// Pointer capture keeps the drag alive across React Flow panes, nodes and overlays.
// Unlike HTML drag-and-drop, this also works with pen/touch and embedded browsers.
export default function PaletteItem({ spec, canvasRef, onPlace, onAdd, onDragging }) {
  const gesture = useRef(null);
  const [preview, setPreview] = useState(null);
  const Icon = spec.icon;
  function finish(e, cancelled = false) {
    const drag = gesture.current;
    if (!drag || drag.id !== e.pointerId) return;
    gesture.current = null;
    setPreview(null);
    onDragging(false);
    if (e.currentTarget.hasPointerCapture?.(e.pointerId)) e.currentTarget.releasePointerCapture(e.pointerId);
    if (cancelled || !drag.moved) return;
    const bounds = canvasRef.current?.getBoundingClientRect();
    if (bounds && e.clientX >= bounds.left && e.clientX <= bounds.right && e.clientY >= bounds.top && e.clientY <= bounds.bottom) {
      onPlace(spec.type, { x: e.clientX, y: e.clientY });
    }
  }
  return <>
    <button type="button" className="palette-node" title={`${spec.summary}. Drag onto the canvas or press Enter to add.`}
      aria-label={`Add ${spec.label}`} draggable={false}
      onPointerDown={e => {
        if (e.button !== 0) return;
        gesture.current = { id: e.pointerId, x: e.clientX, y: e.clientY, moved: false };
        e.currentTarget.setPointerCapture(e.pointerId);
      }}
      onPointerMove={e => {
        const drag = gesture.current;
        if (!drag || drag.id !== e.pointerId) return;
        if (!drag.moved && Math.hypot(e.clientX - drag.x, e.clientY - drag.y) < 5) return;
        drag.moved = true;
        onDragging(true);
        setPreview({ x: e.clientX, y: e.clientY });
      }}
      onPointerUp={e => finish(e)} onPointerCancel={e => finish(e, true)} onLostPointerCapture={e => finish(e, true)}
      onKeyDown={e => { if (e.key === 'Escape') { gesture.current = null; setPreview(null); onDragging(false); } }}
      onClick={e => { if (e.detail === 0) onAdd(spec.type); }} onDoubleClick={() => onAdd(spec.type)}>
      <Icon size={14} style={{ color: spec.color }} />{spec.label}
    </button>
    {preview && createPortal(<div className="palette-drag-preview" style={{ left: preview.x + 14, top: preview.y + 14 }} aria-hidden="true"><Icon size={16} />{spec.label}</div>, document.body)}
  </>;
}
