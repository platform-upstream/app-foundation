import { AppShell } from './foundation/AppShell.js';

// The shell mechanics come from the registry; everything visible here is the application's own.
export function App() {
  return (
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
      mainProps={{ id: 'main-content', tabIndex: -1 }}
    >
      <div className="page">
        <p className="eyebrow">Reference shell</p>
        <p>
          Give this application its own identity. The foundation supplies the shell, lifecycle and
          data seams; product language, information architecture, colour and interaction design
          belong to this application.
        </p>
      </div>
    </AppShell>
  );
}
