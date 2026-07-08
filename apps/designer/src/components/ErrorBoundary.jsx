import React from 'react';
import { AlertTriangle } from 'lucide-react';

// Catches render-time errors anywhere in the tree and shows a recovery screen
// instead of a blank page. Keeps a client demo from ever showing a white screen.
export class ErrorBoundary extends React.Component {
  constructor(props) {
    super(props);
    this.state = { error: null };
  }

  static getDerivedStateFromError(error) {
    return { error };
  }

  componentDidCatch(error, info) {
    // eslint-disable-next-line no-console
    console.error('[KW Sagittarii] UI error:', error, info);
  }

  handleReload = () => {
    this.setState({ error: null });
    window.location.reload();
  };

  render() {
    if (this.state.error) {
      return (
        <div style={{
          height: '100vh', width: '100%', display: 'flex', flexDirection: 'column',
          alignItems: 'center', justifyContent: 'center', gap: 14, padding: 24,
          background: 'var(--bg-primary)', color: 'var(--text-primary)', textAlign: 'center',
        }}>
          <AlertTriangle size={44} color="var(--error)" />
          <div style={{ fontSize: 18, fontWeight: 700 }}>Something went wrong</div>
          <div style={{ fontSize: 13, color: 'var(--text-tertiary)', maxWidth: 460 }}>
            The interface hit an unexpected error. Your data is safe — reloading usually resolves it.
          </div>
          <pre style={{
            fontSize: 11, fontFamily: 'var(--font-mono)', color: 'var(--text-muted)',
            background: 'var(--bg-secondary)', border: '1px solid var(--border-primary)',
            borderRadius: 4, padding: '8px 12px', maxWidth: 460, overflow: 'auto', maxHeight: 120,
          }}>{String(this.state.error?.message || this.state.error)}</pre>
          <button className="btn btn-primary" onClick={this.handleReload}>Reload App</button>
        </div>
      );
    }
    return this.props.children;
  }
}
