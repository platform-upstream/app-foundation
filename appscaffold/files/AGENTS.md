# __APP_NAME__

<One paragraph: what the app does, its hostname, who uses it, UI language.>

- General UI rules (shell, page titles, look, layout shift): shared memory `ui`
  (`~/.agents/memory/ui.md`). App-specific look here: palette, fonts, language.
- App structure: the target backend and frontend specifications in the workspace
  `docs/app-platform-migration-plan.md`. Go: thin handlers in `internal/api`, logic in
  `internal/app` or domain packages, SQL only in `internal/store`, each outside service behind one
  small interface. Frontend in `web/src`: `components/ui` (primitives), `features/<x>/`, `lib/`;
  imports point downward only.
- Review with the `review` skill: plans before the owner sees them, app and UI diffs before push.
- Pushes to `main` deploy through the shared `ci-deliver` pipeline; the Dockerfile runs every
  check.
