# Frontend

Go serves this tree directly; no package manager or build step is required.

- `templates/`: page bodies defining `{{define "main"}}`.
- `partials/`: shared `base`, navigation, and alert-form blocks.
- `pages/`: complete static documents served with `PageBox.ServePage`.
- `js/`: vanilla JavaScript served at `/js/*`.
- `public/`: CSS and other flat assets served at `/public/*`.

`internal/templating/pages` parses/clones templates at startup and renders them through Go's contextual HTML escaping. Add a template and a route/handler calling `RenderPage`. Use `CurrentPath` for navigation and `isAuthenticated` for the shell. Restart the Go process after template changes.

The head loads Tailwind's browser package from its pinned CDN. Custom CSS supplies reusable form/dialog/chart styles. Charts are SVG rendered by `dashboard.js`; no Chart.js or other frontend dependency is present.

Render tests execute every page and check escaping/static-asset boundaries. Check JavaScript syntax with `node --check js/app.js` and `node --check js/dashboard.js` when Node is available for development; Node is not needed to run the application.
