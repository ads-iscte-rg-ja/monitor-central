# Frontend

Go serves this tree directly; no package manager or build step is required.

- `templates/`: page bodies defining `{{define "main"}}`.
- `partials/`: shared `base`, navigation, and alert-form blocks.
- `pages/`: complete static documents served with `PageBox.ServePage`.
- `js/`: vanilla JavaScript served at `/js/*`.
- `public/`: CSS and other flat assets served at `/public/*`.

`internal/templating/pages` parses/clones templates at startup and renders them through Go's contextual HTML escaping. Add a template and a route/handler calling `RenderPage`. Use `CurrentPath` for navigation and `isAuthenticated` for the shell. Restart the Go process after template changes.

The head loads Tailwind's browser package from `public/` (ADR-0016): the same pinned
version that used to come from a CDN, `@tailwindcss/browser@4.1.18` (`dist/index.global.js`, MIT, licence in
`public/tailwindcss-browser-LICENSE.txt`), checked against the npm registry integrity
`sha512-cnAUsj2TXGslPmDEnM469b4MAFT/ifxjt+Bla/2vBTBzp/PIPo6pjKxRrd8x+S84/S1bAtEIlzbJaaJxgZUV8Q==`
(SHA-256 of the file: `02d70b1ae97aab2e87be23869b2bb5ad4ed0a3b63911c02612a028ee9e473a7b`). To upgrade, replace the
file with the same check and rename it with the new version. The Content-Security-Policy allows scripts only
from this origin and no inline scripts: keep JavaScript in `js/`, never in `<script>` blocks or `on…`
attributes. Custom CSS supplies reusable form/dialog/chart styles. Charts are SVG rendered by `dashboard.js`; no Chart.js or other frontend dependency is present.

Render tests execute every page and check escaping/static-asset boundaries. Check JavaScript syntax with `node --check js/app.js` and `node --check js/dashboard.js` when Node is available for development; Node is not needed to run the application.
