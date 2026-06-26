# Working Directory: Stat Check + Create Button

**Date:** 2026-06-26  
**Status:** Approved

---

## Problem

The New Project form (and Edit Project form) has a plain text input for Working Directory. If the user types a path that doesn't exist yet, they have no way to create it from the UI — they must leave Phoenix, create the directory in a terminal, then come back. There is also no feedback about whether the path exists at all.

---

## Goal

1. After the user finishes typing a working directory path, silently check whether it exists on disk.
2. Show a clear presence indicator next to the field.
3. If the path doesn't exist, offer a **[Create]** button that creates it with one click.

---

## Scope

- Applies to both form surfaces:
  - `ProjectsWorkspace.tsx` — `NewProjectForm` (inline right-pane panel, the primary entry point)
  - `ProjectsPage.tsx` — `ProjectForm` (legacy modal on `/projects` page)
- No change to the project save path — directory creation is an explicit user action, not automatic on submit.

---

## Backend API

Two new endpoints in a new file `internal/api/fs.go`, registered in the main router.

### `GET /api/fs/stat?path=<url-encoded-path>`

Checks whether a filesystem path exists.

- **Request:** query param `path` (must be non-empty, must be absolute)
- **Response 200:**
  ```json
  { "exists": true, "is_dir": true }
  ```
- **Response 400:** `{ "error": "path must be absolute" }` if path is relative or empty
- **Implementation:** `os.Stat(path)` — read-only, no side effects

### `POST /api/fs/mkdir`

Creates a directory (and all parents) at the given path.

- **Request body:**
  ```json
  { "path": "/absolute/path/to/dir" }
  ```
- **Response 200:**
  ```json
  { "created": true }
  ```
  `created: false` if the directory already existed (idempotent).
- **Response 400:** OS error message (e.g. "permission denied") or validation error
- **Implementation:** `os.MkdirAll(path, 0755)`

Both handlers are registered alongside the existing routes in `internal/api/server.go`.

---

## Frontend

### State additions (per form)

```ts
type DirStatus = 'unknown' | 'exists' | 'missing' | 'not_dir' | 'creating' | 'error'

const [dirStatus, setDirStatus] = useState<DirStatus>('unknown')
const [dirStatusMsg, setDirStatusMsg] = useState<string>('')
```

### Trigger: on blur + 600ms debounce

- `onChange`: reset `dirStatus` to `'unknown'` immediately (stale indicator cleared)
- `onBlur`: if field is non-empty, call `GET /api/fs/stat?path=...`
- Debounce (600ms on keystroke pause) also triggers stat if user stops typing without blurring

### Stat result → `dirStatus`

| `os.Stat` result | `exists` | `is_dir` | `dirStatus` |
|---|---|---|---|
| path found, is directory | true | true | `'exists'` |
| path found, not a directory | true | false | `'not_dir'` |
| path not found | false | — | `'missing'` |
| backend error | — | — | `'error'` + message |

### Indicator UI (below or beside the input)

| `dirStatus` | Visual |
|---|---|
| `'unknown'` | nothing |
| `'exists'` | green check icon + "Directory exists" |
| `'missing'` | orange dot + "Does not exist" + **[Create]** button |
| `'not_dir'` | red dot + "Not a directory" |
| `'creating'` | spinner + "Creating…" |
| `'error'` | red dot + error message |

### Create button behavior

1. User clicks **[Create]**
2. `dirStatus` → `'creating'`; button disabled
3. `POST /api/fs/mkdir` with `{ path: workingDir.trim() }`
4. On success: `dirStatus` → `'exists'`; indicator flips to green check + "Created"
5. On error: `dirStatus` → `'error'`; message shown in red

---

## Edge Cases

| Scenario | Behavior |
|---|---|
| Empty path | Stat skipped; no indicator |
| Relative path | Backend returns 400 "path must be absolute"; shown as red error |
| Permission denied on mkdir | Backend returns 400 with OS message; shown as red error |
| Path is a file, not a dir | `'not_dir'` indicator; no Create button |
| User edits field after stat | `dirStatus` resets to `'unknown'` on next keystroke |
| Concurrent Create clicks | Button disabled while `'creating'` is in flight |

---

## Files Changed

| File | Change |
|---|---|
| `internal/api/fs.go` | New file — `statHandler` and `mkdirHandler` |
| `internal/api/server.go` | Register `/api/fs/stat` and `/api/fs/mkdir` routes |
| `web/src/pages/ProjectsWorkspace.tsx` | Add `dirStatus` state + indicator UI to `NewProjectForm` |
| `web/src/pages/ProjectsPage.tsx` | Same additions to `ProjectForm` |
| `web/src/lib/api.ts` | Add `fs.stat(path)` and `fs.mkdir(path)` client methods |

---

## Non-Goals

- No native OS directory picker (browser security restrictions prevent it for arbitrary paths)
- No path autocomplete / suggestions
- No automatic directory creation on project save (explicit user action only)
- No recursive delete or any destructive filesystem operation
