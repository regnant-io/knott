// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

import React from 'react';
import ReactDOM from 'react-dom/client';
import App from './App.jsx';
import { ErrorBoundary } from './components/ErrorBoundary.jsx';
import DesktopChrome, { isWindowsDesktop } from './components/DesktopChrome.jsx';
import { DialogProvider } from './components/KnottDialog.jsx';
import './index.css';
import './styles/designer.css';
import './styles/console.css';
import './styles/desktop.css';

// In the desktop app, links to the outside world (API docs, provider consoles)
// belong in the user's browser, not in a second app window.
document.addEventListener('click', e => {
  const open = window.runtime?.BrowserOpenURL;
  const a = e.target.closest?.('a[href]');
  if (!open || !a) return;
  const url = new URL(a.href, window.location.href);
  if (url.origin === window.location.origin || !/^https?:$/.test(url.protocol)) return;
  e.preventDefault();
  open(url.href);
});

ReactDOM.createRoot(document.getElementById('root')).render(
  <React.StrictMode>
    <DialogProvider>
      <ErrorBoundary>
        {isWindowsDesktop() ? <DesktopChrome><App /></DesktopChrome> : <App />}
      </ErrorBoundary>
    </DialogProvider>
  </React.StrictMode>
);
