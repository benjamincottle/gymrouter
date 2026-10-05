# Gym Router

- Product decisions and their history: `docs/spec.md` (decision log at the end).
- **UI work follows `docs/DESIGN.md`**: tokens, components, layout and UX conventions (time formats, destructive
  actions, errors, focus, copy). Read it before touching `web/`. Use its tokens and components rather than raw
  values or one-off styles; if something isn't covered, add the rule to DESIGN.md first.
- Frontend: Preact + Vite in `web/` (`npm run dev` proxies the API to 127.0.0.1:18080); styles in `web/src/style.css`,
  shared components in `web/src/views/ui.tsx`.
- The repo is public: no personal addresses, coordinates, home-side lines or secrets in tracked files.
