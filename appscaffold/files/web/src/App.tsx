import { AppShell } from './foundation/AppShell.js';
import { NotesPage } from './features/notes/Notes.js';

// The shell mechanics come from the registry; everything visible here is the application's own.
// The top bar is the page title; pages render no heading of their own.
export function App() {
  return (
    <AppShell
      className="app-layout"
      topBar={
        <header className="topbar">
          <h1>Notes</h1>
        </header>
      }
      nav={
        <nav className="bottom-nav" aria-label="Main navigation">
          <a href="/" aria-current="page">Notes</a>
        </nav>
      }
      mainProps={{ id: 'main-content', tabIndex: -1, className: 'no-pan-x' }}
    >
      <NotesPage />
    </AppShell>
  );
}
