import { useState, useRef, useEffect } from 'react'

// Pastel colour palette — deterministic per tag name so colours are stable
const TAG_COLORS = [
  'bg-violet-900/40 text-violet-300 border-violet-700/50',
  'bg-blue-900/40 text-blue-300 border-blue-700/50',
  'bg-emerald-900/40 text-emerald-300 border-emerald-700/50',
  'bg-amber-900/40 text-amber-300 border-amber-700/50',
  'bg-rose-900/40 text-rose-300 border-rose-700/50',
  'bg-cyan-900/40 text-cyan-300 border-cyan-700/50',
  'bg-fuchsia-900/40 text-fuchsia-300 border-fuchsia-700/50',
  'bg-lime-900/40 text-lime-300 border-lime-700/50',
]

export function tagColour(tag: string): string {
  let hash = 0
  for (let i = 0; i < tag.length; i++) hash = tag.charCodeAt(i) + ((hash << 5) - hash)
  return TAG_COLORS[Math.abs(hash) % TAG_COLORS.length]
}

// A small pill for display only (cards, filter bar, etc.)
export function TagPill({ tag, onRemove }: { tag: string; onRemove?: () => void }) {
  return (
    <span className={`inline-flex items-center gap-1 text-xs font-medium px-2 py-0.5 rounded-full border ${tagColour(tag)}`}>
      {tag}
      {onRemove && (
        <button
          type="button"
          onClick={e => { e.stopPropagation(); onRemove() }}
          className="opacity-60 hover:opacity-100 leading-none"
          aria-label={`Remove ${tag}`}
        >
          ×
        </button>
      )}
    </span>
  )
}

// Interactive tag editor: shows current tags as pills + an inline text input
export function TagInput({
  value,
  onChange,
  suggestions = [],
  placeholder = 'Add tag…',
}: {
  value: string[]
  onChange: (tags: string[]) => void
  suggestions?: string[]  // all known tags from other projects for autocomplete
  placeholder?: string
}) {
  const [input, setInput] = useState('')
  const [focused, setFocused] = useState(false)
  const inputRef = useRef<HTMLInputElement>(null)

  const normalise = (s: string) => s.toLowerCase().trim()

  const addTag = (raw: string) => {
    const tag = normalise(raw)
    if (!tag || value.includes(tag)) { setInput(''); return }
    onChange([...value, tag])
    setInput('')
  }

  const removeTag = (tag: string) => onChange(value.filter(t => t !== tag))

  const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'Enter' || e.key === ',') {
      e.preventDefault()
      addTag(input)
    }
    if (e.key === 'Backspace' && input === '' && value.length > 0) {
      removeTag(value[value.length - 1])
    }
  }

  // Filtered suggestions: matching input, not already added
  const filtered = suggestions.filter(s =>
    s.includes(normalise(input)) && !value.includes(s) && s !== normalise(input)
  ).slice(0, 6)

  return (
    <div className="relative">
      <div
        className="flex flex-wrap gap-1.5 items-center min-h-[38px] px-3 py-2 bg-slate-900 border border-slate-700 rounded-lg cursor-text focus-within:ring-2 focus-within:ring-violet-500"
        onClick={() => inputRef.current?.focus()}
      >
        {value.map(tag => (
          <TagPill key={tag} tag={tag} onRemove={() => removeTag(tag)} />
        ))}
        <input
          ref={inputRef}
          value={input}
          onChange={e => setInput(e.target.value)}
          onKeyDown={handleKeyDown}
          onFocus={() => setFocused(true)}
          onBlur={() => { setTimeout(() => setFocused(false), 150); addTag(input) }}
          placeholder={value.length === 0 ? placeholder : ''}
          className="flex-1 min-w-[80px] bg-transparent text-sm text-white placeholder-slate-500 outline-none"
        />
      </div>

      {/* Autocomplete dropdown */}
      {focused && filtered.length > 0 && (
        <div className="absolute z-10 top-full mt-1 w-full bg-slate-800 border border-slate-700 rounded-lg shadow-lg overflow-hidden">
          {filtered.map(s => (
            <button
              key={s}
              type="button"
              onMouseDown={() => addTag(s)}
              className="w-full text-left px-3 py-2 text-sm text-slate-300 hover:bg-slate-700 flex items-center gap-2"
            >
              <TagPill tag={s} />
            </button>
          ))}
        </div>
      )}

      <p className="text-xs text-slate-600 mt-1">Press Enter or comma to add · Backspace to remove</p>
    </div>
  )
}
