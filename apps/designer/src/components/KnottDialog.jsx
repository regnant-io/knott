import React, { createContext, useCallback, useContext, useEffect, useRef, useState } from 'react';
import { AlertTriangle, Info, X } from 'lucide-react';

const DialogContext = createContext(null);

export function DialogProvider({ children }) {
  const [request, setRequest] = useState(null);
  const queue = useRef([]);
  const dialog = useRef(null);

  const enqueue = useCallback((options) => new Promise(resolve => {
    queue.current.push({ ...options, resolve });
    setRequest(current => current || queue.current.shift());
  }), []);

  const confirm = useCallback((message, options = {}) => enqueue({
    title: options.title || 'Please confirm', message,
    action: options.action || 'Confirm', destructive: !!options.destructive,
    confirm: true,
  }), [enqueue]);
  const alert = useCallback((message, options = {}) => enqueue({
    title: options.title || 'KNOTT', message, action: 'OK', confirm: false,
  }), [enqueue]);

  useEffect(() => {
    if (request && dialog.current && !dialog.current.open) dialog.current.showModal();
  }, [request]);

  const finish = useCallback((accepted) => {
    if (!request) return;
    dialog.current?.close();
    request.resolve(accepted);
    setRequest(queue.current.shift() || null);
  }, [request]);

  return <DialogContext.Provider value={{ confirm, alert }}>
    {children}
    {request && <dialog ref={dialog} className="knott-dialog" onCancel={event => {
      event.preventDefault(); finish(false);
    }}>
      <div className="knott-dialog-heading">
        <span className={`knott-dialog-icon ${request.destructive ? 'destructive' : ''}`}>
          {request.destructive ? <AlertTriangle size={19} /> : <Info size={19} />}
        </span>
        <h2>{request.title}</h2>
        <button className="knott-dialog-x" aria-label="Close dialog" onClick={() => finish(false)}><X size={18} /></button>
      </div>
      <p>{request.message}</p>
      <div className="knott-dialog-actions">
        {request.confirm && <button className="btn btn-ghost" onClick={() => finish(false)}>Cancel</button>}
        <button autoFocus className={`btn ${request.destructive ? 'btn-danger' : 'btn-primary'}`} onClick={() => finish(true)}>{request.action}</button>
      </div>
    </dialog>}
  </DialogContext.Provider>;
}

export function useKnottDialog() {
  const context = useContext(DialogContext);
  if (!context) throw new Error('KnottDialog requires DialogProvider');
  return context;
}
