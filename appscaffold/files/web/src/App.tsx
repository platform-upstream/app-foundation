import { AppShell } from './foundation/AppShell.js';
import { ToastRegion } from './foundation/ToastRegion.js';
import { NotesPage } from './features/notes/Notes.js';

// The shell mechanics come from the registry; everything visible here is the application's own.
// The top bar is the page title; pages render no heading of their own.
export function App() {
  return (
    <>
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
        // no-pan-x (touch.css) here and on html and body: the page never pans or swipes back sideways.
        mainProps={{ id: 'main-content', tabIndex: -1, className: 'no-pan-x' }}
      >
        <NotesPage />
      </AppShell>
      {/* toast() from anywhere lands here, above the nav; the look is .toast in the app's CSS. */}
      <ToastRegion toastClassName="toast" label="Notifications" />
    </>
  );
}
