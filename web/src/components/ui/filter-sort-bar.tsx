import { TagPill, tagColour } from './tag-input'

export type SortKey = 'name-asc' | 'name-desc' | 'created-asc' | 'created-desc' | 'tag'

export interface FilterSortState {
  search: string
  activeTags: string[]
  sort: SortKey
}

interface Props {
  state: FilterSortState
  onChange: (s: FilterSortState) => void
  allTags: string[]     // union of tags across all shown projects
  total: number         // total before filter
  filtered: number      // count after filter
}

export function FilterSortBar({ state, onChange, allTags, total, filtered }: Props) {
  const set = (patch: Partial<FilterSortState>) => onChange({ ...state, ...patch })

  const toggleTag = (tag: string) => {
    const next = state.activeTags.includes(tag)
      ? state.activeTags.filter(t => t !== tag)
      : [...state.activeTags, tag]
    set({ activeTags: next })
  }

  const hasFilter = state.search.trim() !== '' || state.activeTags.length > 0

  return (
    <div className="space-y-3">
      {/* Search + sort row */}
      <div className="flex gap-3 items-center">
        {/* Search */}
        <div className="relative flex-1">
          <span className="absolute left-3 top-1/2 -translate-y-1/2 text-slate-500 text-sm select-none">⌕</span>
          <input
            type="search"
            value={state.search}
            onChange={e => set({ search: e.target.value })}
            placeholder="Search projects…"
            className="w-full bg-slate-900 border border-slate-700 rounded-lg pl-8 pr-3 py-2 text-sm text-white placeholder-slate-500 focus:outline-none focus:ring-2 focus:ring-violet-500"
          />
        </div>

        {/* Sort */}
        <select
          value={state.sort}
          onChange={e => set({ sort: e.target.value as SortKey })}
          className="bg-slate-900 border border-slate-700 rounded-lg px-3 py-2 text-sm text-slate-300 focus:outline-none focus:ring-2 focus:ring-violet-500 shrink-0"
        >
          <option value="created-desc">Newest first</option>
          <option value="created-asc">Oldest first</option>
          <option value="name-asc">Name A → Z</option>
          <option value="name-desc">Name Z → A</option>
          <option value="tag">Group by tag</option>
        </select>

        {/* Clear */}
        {hasFilter && (
          <button
            onClick={() => set({ search: '', activeTags: [] })}
            className="text-xs text-slate-500 hover:text-white transition-colors shrink-0"
          >
            Clear
          </button>
        )}
      </div>

      {/* Tag filter pills */}
      {allTags.length > 0 && (
        <div className="flex flex-wrap gap-2 items-center">
          <span className="text-xs text-slate-600 shrink-0">Filter by tag:</span>
          {allTags.map(tag => {
            const active = state.activeTags.includes(tag)
            return (
              <button
                key={tag}
                onClick={() => toggleTag(tag)}
                className={`inline-flex items-center gap-1 text-xs font-medium px-2.5 py-1 rounded-full border transition-all ${
                  active
                    ? tagColour(tag) + ' ring-2 ring-offset-1 ring-offset-slate-950 ring-violet-500'
                    : 'border-slate-700 text-slate-400 hover:border-slate-500 hover:text-white bg-slate-900'
                }`}
              >
                {tag}
              </button>
            )
          })}
        </div>
      )}

      {/* Result count — only shown when filtering */}
      {hasFilter && (
        <p className="text-xs text-slate-500">
          Showing {filtered} of {total}
        </p>
      )}
    </div>
  )
}

// ---- Utility: apply filter + sort to a project list ----

export interface Taggable {
  name: string
  tags: string[] | null   // API may return null for rows that predate the tags migration
  created_at: string
}

/** Normalise tags to a safe array regardless of what the API returned */
function tags(p: Taggable): string[] {
  return p.tags ?? []
}

export function applyFilterSort<T extends Taggable>(
  items: T[],
  state: FilterSortState,
): T[] {
  let result = items

  // Text search
  const q = state.search.trim().toLowerCase()
  if (q) {
    result = result.filter(p =>
      p.name.toLowerCase().includes(q) ||
      tags(p).some(t => t.includes(q))
    )
  }

  // Tag filter — project must have ALL active tags
  if (state.activeTags.length > 0) {
    result = result.filter(p =>
      state.activeTags.every(t => tags(p).includes(t))
    )
  }

  // Sort
  const sorted = [...result]
  switch (state.sort) {
    case 'name-asc':
      sorted.sort((a, b) => a.name.localeCompare(b.name))
      break
    case 'name-desc':
      sorted.sort((a, b) => b.name.localeCompare(a.name))
      break
    case 'created-asc':
      sorted.sort((a, b) => a.created_at.localeCompare(b.created_at))
      break
    case 'created-desc':
      sorted.sort((a, b) => b.created_at.localeCompare(a.created_at))
      break
    case 'tag':
      // Group by first tag alphabetically; untagged go last
      sorted.sort((a, b) => {
        const ta = [...tags(a)].sort()[0] ?? '\uffff'
        const tb = [...tags(b)].sort()[0] ?? '\uffff'
        if (ta !== tb) return ta.localeCompare(tb)
        return a.name.localeCompare(b.name)
      })
      break
  }
  return sorted
}

// Returns sorted, deduped list of all tags across items
export function collectAllTags(items: Taggable[]): string[] {
  const set = new Set<string>()
  items.forEach(p => p.tags?.forEach(t => set.add(t)))
  return [...set].sort()
}
