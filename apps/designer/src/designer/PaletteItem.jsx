// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0
import React, { useRef, useState } from 'react';
import { createPortal } from 'react-dom';

/**
 * A row that adds a step when chosen, or places it where it is dropped when
 * dragged onto the canvas.
 *
 * Pointer capture keeps the drag alive across React Flow panes, nodes and
 * overlays. Unlike HTML drag-and-drop it also works with pen and touch and in
 * the desktop app's embedded web view.
 *
 * `spec.key` (falling back to `spec.type`) is what the callbacks receive.
 */
export default function PaletteItem({
  spec, canvasRef, onPlace, onAdd, onDragging,
  className = 'palette-node', children, title, active, onHover, rowRef,
}) {
  const gesture = useRef(null);
  const [preview, setPreview] = useState(null);
  const Icon = spec.icon;
  const key = spec.key ?? spec.type;
  function finish(e, cancelled = false) {
    const drag = gesture.current;
    if (!drag || drag.id !== e.pointerId) return;
    gesture.current = null;
    setPreview(null);
    onDragging(false);
    if (e.currentTarget.hasPointerCapture?.(e.pointerId)) e.currentTarget.releasePointerCapture(e.pointerId);
    if (cancelled) return;
    if (!drag.moved) {
      // A plain click adds the step in the default place.
      if (e.pointerType !== undefined && e.button === 0) onAdd(key);
      return;
    }
    const bounds = canvasRef.current?.getBoundingClientRect();
    if (bounds && e.clientX >= bounds.left && e.clientX <= bounds.right && e.clientY >= bounds.top && e.clientY <= bounds.bottom) {
      onPlace(key, { x: e.clientX, y: e.clientY });
    }
  }
  return <>
    <button type="button" ref={rowRef} className={`${className}${active ? ' active' : ''}`}
      title={title ?? `${spec.summary || spec.label}. Drag onto the canvas or press Enter to add.`}
      aria-label={`Add ${spec.label}`} draggable={false} data-active={active ? 'true' : undefined}
      onMouseEnter={onHover}
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
      onClick={e => { if (e.detail === 0) onAdd(key); }}>
      {children ?? <><Icon size={14} style={{ color: spec.color }} />{spec.label}</>}
    </button>
    {preview && createPortal(
      <div className="palette-drag-preview" style={{ left: preview.x + 14, top: preview.y + 14 }} aria-hidden="true">
        {Icon ? <Icon size={16} /> : null}{spec.label}
      </div>, document.body)}
  </>;
}
