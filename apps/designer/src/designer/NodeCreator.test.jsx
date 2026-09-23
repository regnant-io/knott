// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0
import React from 'react';
import { render, screen, fireEvent, cleanup, within } from '@testing-library/react';
import { describe, it, expect, vi, afterEach } from 'vitest';
import NodeCreator from './NodeCreator.jsx';
import { entryForNode, searchNodes, defaultNodeName } from './nodeCatalog.js';
import { describeNode } from './describeNode.js';
import { monogram, readableOn } from '../components/AppIcon.jsx';

afterEach(cleanup);

const CONNECTORS = [
  {
    slug: 'slack', name: 'Slack', category: 'Communication', description: 'Post messages', credentials_ready: true,
    credentials: [{ name: 'SLACK_BOT_TOKEN' }],
    actions: [{ id: '', label: 'Send message', fields: [{ name: 'text', label: 'Message', required: true }] }],
  },
  {
    slug: 'pipedrive', name: 'Pipedrive', category: 'CRM', description: 'Deals and people', credentials_ready: false,
    credentials: [{ name: 'PIPEDRIVE_API_TOKEN' }],
    actions: [
      { id: 'create_deal', label: 'Create deal', fields: [{ name: 'title', label: 'Title', required: true, default: 'New deal' }] },
      { id: 'list_deals', label: 'List deals', fields: [] },
    ],
  },
];

function setup(props = {}) {
  const onPick = vi.fn();
  const onClose = vi.fn();
  render(
    <NodeCreator open context={{ from: 'start', handle: 'main' }} connectors={CONNECTORS} hasTrigger
      onPick={onPick} onClose={onClose} canvasRef={{ current: null }} onPlace={vi.fn()} onDragging={vi.fn()} {...props} />,
  );
  const search = screen.getByRole('textbox', { name: /search steps/i });
  return { onPick, onClose, search };
}

const type = (el, value) => fireEvent.change(el, { target: { value } });

describe('NodeCreator', () => {
  it('suggests next steps and offers categories to browse', () => {
    setup();
    expect(screen.getByText('What happens next?')).toBeInTheDocument();
    expect(screen.getByText('AI Prompt')).toBeInTheDocument();
    expect(screen.getByText('Apps & APIs')).toBeInTheDocument();
    // A trigger never follows another step.
    expect(screen.queryByText('Triggers')).not.toBeInTheDocument();
  });

  it('finds an app action by what it does', () => {
    const { search, onPick } = setup();
    type(search, 'create deal');
    const row = screen.getByText('Pipedrive: Create deal');
    fireEvent.keyDown(search, { key: 'Enter' });
    expect(onPick).toHaveBeenCalledTimes(1);
    const pick = onPick.mock.calls[0][0];
    expect(pick.connector.slug).toBe('pipedrive');
    expect(pick.action.id).toBe('create_deal');
    expect(row).toBeTruthy();
  });

  it('drills from an app into its actions', () => {
    const { search, onPick } = setup();
    fireEvent.click(screen.getByText('Apps & APIs'));
    expect(screen.getByText('Built in')).toBeInTheDocument();
    fireEvent.click(screen.getByText('Pipedrive'));
    expect(screen.getByText('List deals')).toBeInTheDocument();
    // ← goes back up to the app list.
    fireEvent.keyDown(search, { key: 'ArrowLeft' });
    expect(screen.getByText('Pipedrive')).toBeInTheDocument();
    expect(onPick).not.toHaveBeenCalled();
  });

  it('picks a single-action app in one step', () => {
    const { search, onPick } = setup();
    type(search, 'slack');
    fireEvent.keyDown(search, { key: 'Enter' });
    expect(onPick.mock.calls[0][0].connector.slug).toBe('slack');
  });

  it('opens on triggers for an empty workflow', () => {
    setup({ hasTrigger: false, context: {} });
    expect(screen.getByText('Triggers')).toBeInTheDocument();
    expect(screen.getByText('Webhook')).toBeInTheDocument();
    expect(screen.getByText('Schedule')).toBeInTheDocument();
  });

  it('closes on Escape and says when nothing matches', () => {
    const { search, onClose } = setup();
    type(search, 'zzzz-nothing');
    expect(screen.getByText(/Nothing matches/)).toBeInTheDocument();
    fireEvent.keyDown(search, { key: 'Escape' });
    expect(onClose).toHaveBeenCalled();
  });
});

describe('catalogue', () => {
  it('describes a configured node by its preset', () => {
    expect(entryForNode('list', { config: { operation: 'dedupe' } }).label).toBe('Remove duplicates');
    expect(entryForNode('trigger', { config: { trigger_type: 'schedule' } }).label).toBe('Schedule');
    expect(entryForNode('tool_call', { config: { connector_id: 'webhook' } }).label).toBe('HTTP Request');
  });

  it('finds steps by intent', () => {
    expect(searchNodes('delay')[0].id).toBe('wait');
    expect(searchNodes('dedupe')[0].id).toBe('list.dedupe');
    expect(searchNodes('if')[0].id).toBe('condition');
  });

  it('names new steps without colliding', () => {
    expect(defaultNodeName('Sort', ['Sort', 'Sort 2'])).toBe('Sort 3');
  });
});

describe('describeNode', () => {
  it('labels an app step with its app and action', () => {
    const d = describeNode('tool_call', { config: { connector_id: 'pipedrive', action: 'list_deals' } }, CONNECTORS);
    expect(d.kind).toBe('Pipedrive');
    expect(d.detail).toBe('List deals');
    expect(d.warning).toBe('Credentials needed');
  });

  it('flags what an unfinished step still needs', () => {
    expect(describeNode('llm', { config: {} }).incomplete).toBe(true);
    expect(describeNode('tool_call', { config: {} }).detail).toBe('Choose an app');
    expect(describeNode('trigger', { config: { trigger_type: 'schedule', schedule_kind: 'interval', schedule_expr: '3600' } }).detail).toBe('Every 1 hour');
  });
});

describe('AppIcon', () => {
  it('makes readable monograms', () => {
    expect(monogram('Google Chat')).toBe('GC');
    expect(monogram('Salesforce')).toBe('Sa');
    expect(monogram('Kit (ConvertKit)')).toBe('Ki');
    expect(readableOn('#FFDE00')).toBe('#111');
    expect(readableOn('#0052CC')).toBe('#fff');
  });
});

// within is used to keep the import meaningful when rows gain nested markup.
void within;
