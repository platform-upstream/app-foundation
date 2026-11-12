// The src/App.tsx of the fixture mechanics_e2e.mjs drives: a page on the shell with a sheet over a
// sheet, toasts with undo and pull to refresh. The look below is a stand-in for an app's own.
import { useRef, useState, type ReactNode } from 'react';
import { AppShell } from './foundation/AppShell.js';
import { useSheet, useSheetDrag } from './foundation/useSheet.js';
import { toast, configureToasts } from './foundation/toast.js';
import { ToastRegion } from './foundation/ToastRegion.js';
import { usePullToRefresh } from './foundation/usePullToRefresh.js';

const LOOK = `
.demo-sheet { background: #fff; border-radius: 20px 20px 0 0; }
.demo-handle { display: flex; align-items: center; gap: 8px; padding: 12px 16px; }
.demo-sheet::backdrop { background: rgb(0 0 0 / .3); }
.demo-toast { background: #172033; color: #fff; padding: 10px 16px; border-radius: 12px; margin-bottom: 12px; display: flex; gap: 12px; }
.band { display: flex; overflow-x: auto; gap: 8px; } .band span { flex: none; width: 120px; height: 60px; background: #ddd; }
.ptr { overflow: hidden; text-align: center; }
`;

configureToasts({ undoLabel: 'Rückgängig' });
declare global {
  interface Window {
    refreshed: number;
    undone: number;
  }
}
window.refreshed = 0;
window.undone = 0;

function Sheet({ open, onClose, title, children }: { open: boolean; onClose: () => void; title: string; children: ReactNode }) {
  const ref = useRef<HTMLDialogElement>(null);
  useSheet(ref, open, onClose);
  useSheetDrag(ref, { onClose, below: 700 });
  return (
    <dialog ref={ref} className="demo-sheet" aria-labelledby={`${title}-t`} data-testid={title}>
      <header data-sheet-handle className="demo-handle">
        <span className="grabber" />
        <h2 id={`${title}-t`}>{title}</h2>
        <button type="button" onClick={onClose}>Schliessen</button>
      </header>
      {open && children}
    </dialog>
  );
}

export function App() {
  const main = useRef<HTMLElement>(null);
  const [open, setOpen] = useState(false);
  const [inner, setInner] = useState(false);
  const { pull, ready, busy } = usePullToRefresh(main, async () => {
    window.refreshed++;
    await new Promise((r) => setTimeout(r, 100));
  });
  return (
    <>
      <style>{LOOK}</style>
      <AppShell
        className="app-layout"
        topBar={
          <header className="topbar">
            <h1>Overview</h1>
          </header>
        }
        nav={
          <nav className="bottom-nav" aria-label="Main navigation">
            <a href="/" aria-current="page">Overview</a>
          </nav>
        }
        mainProps={{ id: 'main-content', tabIndex: -1, ref: main }}
      >
        {(pull > 0 || busy) && (
          <div className="ptr" role="status" data-ready={ready || undefined} style={{ height: pull }}>
            {busy ? 'Aktualisiere…' : ready ? 'Loslassen' : 'Ziehen'}
          </div>
        )}
        <div className="page">
          <button type="button" id="open" onClick={() => setOpen(true)}>Sheet öffnen</button>
          <button type="button" id="toast" onClick={() => toast('Gelöscht', { undo: () => window.undone++ })}>Toast</button>
          <div className="band" id="band">
            {Array.from({ length: 12 }, (_, i) => <span key={i}>Karte {i + 1}</span>)}
          </div>
          {Array.from({ length: 60 }, (_, i) => <p key={i}>Zeile {i + 1}</p>)}
        </div>
      </AppShell>
      <Sheet open={open} onClose={() => setOpen(false)} title="Erstes">
        <input id="field" placeholder="Feld" />
        <button type="button" id="inner" onClick={() => setInner(true)}>Zweites öffnen</button>
        <button type="button" id="toast-in-sheet" onClick={() => toast('Im Sheet', { undo: () => window.undone++ })}>Toast im Sheet</button>
        {Array.from({ length: 4 }, (_, i) => <p key={i}>Inhalt {i + 1}</p>)}
      </Sheet>
      <Sheet open={inner} onClose={() => setInner(false)} title="Zweites">
        <p>Oben</p>
      </Sheet>
      <ToastRegion toastClassName="demo-toast" label="Meldungen" />
    </>
  );
}
