export type SortKey = 'name-asc' | 'name-desc' | 'created-asc' | 'created-desc' | 'tag'

export interface FilterSortState {
  search: string
  activeTags: string[]
  sort: SortKey
}

export interface Taggable {
  name: string
  tags: string[] | null
  created_at: string
}

function tags(p: Taggable): string[] {
  return p.tags ?? []
}

export function applyFilterSort<T extends Taggable>(
  items: T[],
  state: FilterSortState,
): T[] {
  let result = items
  const q = state.search.trim().toLowerCase()

  if (q) {
    result = result.filter(p =>
      p.name.toLowerCase().includes(q) ||
      tags(p).some(t => t.includes(q)),
    )
  }

  if (state.activeTags.length > 0) {
    result = result.filter(p =>
      state.activeTags.every(t => tags(p).includes(t)),
    )
  }

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

export function collectAllTags(items: Taggable[]): string[] {
  const set = new Set<string>()
  items.forEach(p => p.tags?.forEach(t => set.add(t)))
  return [...set].sort()
}
