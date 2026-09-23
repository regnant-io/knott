import React, { useEffect, useState } from 'react';
import { ChevronDown, Maximize2, Minus, Square, X } from 'lucide-react';
import { useKnottDialog } from './KnottDialog.jsx';
import { KnottMark } from './Brand.jsx';

const docsURL = 'https://github.com/regnant-io/knott#readme';
const issueURL = 'https://github.com/regnant-io/knott/issues/new/choose';

export function isWindowsDesktop() {
  return navigator.platform?.startsWith('Win') && !!window.runtime?.WindowMinimise;
}

export default function DesktopChrome({ children }) {
  const { alert } = useKnottDialog();
  const [active, setActive] = useState(null);
  const [maximised, setMaximised] = useState(false);
  const desktop = window.go?.main?.Desktop;
  const runtime = window.runtime;

  useEffect(() => {
    const close = event => {
      if (!event.target.closest?.('.desktop-menu')) setActive(null);
    };
    const key = event => {
      if (event.key === 'Escape') setActive(null);
      if (event.key === 'F11') {
        event.preventDefault();
        runtime.WindowIsFullscreen().then(full => full ? runtime.WindowUnfullscreen() : runtime.WindowFullscreen());
      }
      if (event.ctrlKey && event.key.toLowerCase() === 'r') {
        event.preventDefault(); runtime.WindowReloadApp();
      }
      if (event.ctrlKey && event.key.toLowerCase() === 'b') {
        event.preventDefault(); desktop?.OpenInBrowser?.();
      }
    };
    window.addEventListener('pointerdown', close);
    window.addEventListener('keydown', key);
    return () => { window.removeEventListener('pointerdown', close); window.removeEventListener('keydown', key); };
  }, [desktop, runtime]);

  const run = async action => {
    setActive(null);
    try { await action(); }
    catch (error) { await alert(String(error), { title: 'KNOTT could not complete the action' }); }
  };
  const toggleMaximise = async () => {
    runtime.WindowToggleMaximise();
    setMaximised(await runtime.WindowIsMaximised());
  };
  const menus = [
    { label: 'File', items: [
      ['Open in Browser', () => desktop.OpenInBrowser()],
      ['Show Data Folder', () => desktop.ShowDataFolder()],
      ['Show Logs', () => desktop.ShowLogs()],
    ] },
    { label: 'View', items: [
      ['Reload', () => runtime.WindowReloadApp()],
      ['Toggle Full Screen', async () => (await runtime.WindowIsFullscreen()) ? runtime.WindowUnfullscreen() : runtime.WindowFullscreen()],
    ] },
    { label: 'Help', items: [
      ['Documentation', () => runtime.BrowserOpenURL(docsURL)],
      ['Report an Issue', () => runtime.BrowserOpenURL(issueURL)],
      ['Test Desktop Notification', () => desktop.Notify('KNOTT notifications are working', 'Review tasks and engine alerts will appear here.')],
      ['About KNOTT', async () => alert(await desktop.About(), { title: 'About KNOTT' })],
    ] },
  ];

  return <div className="desktop-shell">
    <header className="desktop-titlebar" aria-label="KNOTT window controls">
      <div className="desktop-identity desktop-drag" onDoubleClick={toggleMaximise}>
        <KnottMark size={21} strokeWidth={2.5} /><strong>KNOTT</strong>
      </div>
      <nav className="desktop-menus" aria-label="Desktop menu">
        {menus.map(menu => <div className="desktop-menu" key={menu.label}>
          <button aria-haspopup="menu" aria-expanded={active === menu.label}
            className={active === menu.label ? 'is-active' : ''}
            onClick={() => setActive(active === menu.label ? null : menu.label)}>
            {menu.label}<ChevronDown size={12} />
          </button>
          {active === menu.label && <div role="menu" className="desktop-menu-popover">
            {menu.items.map(([label, action]) => <button role="menuitem" key={label} onClick={() => run(action)}>{label}</button>)}
          </div>}
        </div>)}
      </nav>
      <div className="desktop-drag desktop-title-space" onDoubleClick={toggleMaximise} />
      <div className="desktop-window-controls">
        <button aria-label="Minimize" title="Minimize" onClick={() => runtime.WindowMinimise()}><Minus size={16} /></button>
        <button aria-label={maximised ? 'Restore' : 'Maximize'} title={maximised ? 'Restore' : 'Maximize'} onClick={toggleMaximise}>{maximised ? <Square size={13} /> : <Maximize2 size={14} />}</button>
        <button className="desktop-close" aria-label="Close to system tray" title="Close to system tray" onClick={() => runtime.Quit()}><X size={17} /></button>
      </div>
    </header>
    <div className="desktop-content">{children}</div>
  </div>;
}
