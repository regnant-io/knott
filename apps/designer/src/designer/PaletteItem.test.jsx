// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0
import React from 'react';
import { render, screen, fireEvent, cleanup } from '@testing-library/react';
import { describe, it, expect, vi, beforeAll, afterEach } from 'vitest';
import PaletteItem from './PaletteItem.jsx';

beforeAll(() => {
  class TestPointerEvent extends MouseEvent {
    constructor(type, init = {}) { super(type, init); Object.defineProperty(this, 'pointerId', { value: init.pointerId || 1 }); }
  }
  window.PointerEvent = TestPointerEvent;
  HTMLElement.prototype.setPointerCapture = vi.fn();
  HTMLElement.prototype.hasPointerCapture = () => true;
  HTMLElement.prototype.releasePointerCapture = vi.fn();
});
afterEach(cleanup);
function setup() {
  const onPlace = vi.fn(), onAdd = vi.fn(), onDragging = vi.fn();
  render(<PaletteItem spec={{ type: 'condition', label: 'Condition', icon: () => null }}
    canvasRef={{ current: { getBoundingClientRect: () => ({ left: 200, top: 100, right: 800, bottom: 600 }) } }}
    onPlace={onPlace} onAdd={onAdd} onDragging={onDragging} />);
  return { button: screen.getByRole('button', { name: 'Add Condition' }), onPlace, onAdd, onDragging };
}
describe('Palette pointer placement', () => {
  it('places a node at the release position exactly once', () => {
    const { button, onPlace, onDragging } = setup();
    fireEvent.pointerDown(button, { pointerId: 1, button: 0, clientX: 60, clientY: 120 });
    fireEvent.pointerMove(button, { pointerId: 1, clientX: 350, clientY: 230 });
    expect(onDragging).toHaveBeenCalledWith(true);
    fireEvent.pointerUp(button, { pointerId: 1, clientX: 400, clientY: 250 });
    fireEvent.lostPointerCapture(button, { pointerId: 1 });
    expect(onPlace).toHaveBeenCalledTimes(1);
    expect(onPlace).toHaveBeenCalledWith('condition', { x: 400, y: 250 });
    expect(onDragging).toHaveBeenLastCalledWith(false);
  });
  it('ignores releases outside the canvas and cancelled gestures', () => {
    const { button, onPlace } = setup();
    fireEvent.pointerDown(button, { button: 0, clientX: 60, clientY: 120 });
    fireEvent.pointerMove(button, { clientX: 850, clientY: 230 });
    fireEvent.pointerUp(button, { clientX: 850, clientY: 230 });
    fireEvent.pointerDown(button, { button: 0, clientX: 60, clientY: 120 });
    fireEvent.pointerMove(button, { clientX: 350, clientY: 230 });
    fireEvent.pointerCancel(button, { clientX: 350, clientY: 230 });
    expect(onPlace).not.toHaveBeenCalled();
  });
  it('does not add a node for a click or tiny pointer movement', () => {
    const { button, onPlace } = setup();
    fireEvent.pointerDown(button, { button: 0, clientX: 60, clientY: 120 });
    fireEvent.pointerMove(button, { clientX: 61, clientY: 121 });
    fireEvent.pointerUp(button, { clientX: 61, clientY: 121 });
    expect(onPlace).not.toHaveBeenCalled();
  });
  it('allows keyboard activation without a drag', () => {
    const { button, onAdd } = setup();
    fireEvent.click(button, { detail: 0 });
    expect(onAdd).toHaveBeenCalledWith('condition');
  });
});
