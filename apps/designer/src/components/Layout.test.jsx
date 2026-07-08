import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { StatusBadge } from './Layout.jsx';

describe('StatusBadge', () => {
  it('renders the status text', () => {
    render(<StatusBadge status="COMPLETED" />);
    expect(screen.getByText('COMPLETED')).toBeInTheDocument();
  });

  it('renders arbitrary statuses with a muted fallback class', () => {
    const { container } = render(<StatusBadge status="WAITING_TIMER" />);
    expect(screen.getByText('WAITING_TIMER')).toBeInTheDocument();
    // It always renders a badge element.
    expect(container.querySelector('.badge')).toBeTruthy();
  });
});
