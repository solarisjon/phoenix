# Phoenix — Agent Context

**READ FIRST:** `docs/superpowers/specs/CONTEXT.md`

This file is the authoritative quick-load context document for any coding agent resuming work on this project. It covers:

- What Phoenix is and the tech stack
- Current repo layout (actual, not design-doc)
- Complete data model including all post-migration fields
- All API routes
- Provider adapter kinds and stream formats
- Build & deploy commands
- Prioritised TODO list with next steps
- Patterns to follow for adding features
- Gotchas learned during development

**Todo list:** `jons-todo-list` (annotated, [FIXED]/[TODO] markers)  
**Design spec:** `docs/superpowers/specs/2026-05-28-phoenix-design.md`  
**Implementation plan:** `docs/superpowers/specs/2026-05-28-phoenix-implementation-plan.md`

## Quick orientation

```bash
go build ./... && go test ./...         # verify everything compiles and tests pass
curl http://localhost:8080/api/agents   # check server is running
sqlite3 ~/.local/share/phoenix/phoenix.db ".tables"  # inspect DB
```
