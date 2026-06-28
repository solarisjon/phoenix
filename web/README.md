# Phoenix — Frontend

React + TypeScript + Vite frontend for Phoenix. Built output is embedded into the Go binary via `embed.FS` — users do not need Node.js to run Phoenix.

## Development

```bash
# Install dependencies (once)
npm install

# Start dev server with hot reload (proxies API to :8080)
npm run dev
# → http://localhost:5173

# Production build (output → web/dist/)
npm run build

# Type check without building
npm run tsc
```

The dev server proxies all `/api/*` and `/ws` requests to `http://localhost:8080`, so you need a running Phoenix backend (`go run ./cmd/phoenix/` or `./phoenix`).

## Stack

- **React 19** + **TypeScript**
- **Vite** for bundling
- **Tailwind CSS** for styling
- **Radix UI** primitives for accessible components
- **react-router-dom** for client-side routing

## Key files

```
src/
  lib/api.ts          # typed API client (all endpoints)
  lib/ws.ts           # WebSocket client
  lib/theme.ts        # 15 built-in themes + community theme loader
  components/
    layout/           # AppLayout, Sidebar — nav, badges, WS lifecycle
    ui/               # shared primitives (buttons, modals, markdown output)
    project/          # task thread cards, human project view
    edit-retry-modal.tsx  # retry-with-edit: pre-fill prompt before re-run
  pages/              # one file per page/route
```

## Building for production

`make build` (from the repo root) runs `npm run build` then compiles the Go binary with the built frontend embedded. You do not need to run `npm run build` separately.
