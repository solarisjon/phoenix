/**
 * ModelComboBox — a searchable model picker that fetches available models
 * from GET /api/providers/:id/models and falls back to a free-text input
 * when the provider doesn't support model listing (crush, opencode, etc.).
 *
 * Usage:
 *   <ModelComboBox
 *     providerId={selectedProviderId}
 *     value={model}
 *     onChange={setModel}
 *     placeholder="e.g. claude-sonnet-4-5"
 *   />
 */

import { useState, useEffect, useRef } from 'react'
import { api } from '@/lib/api'

interface Props {
  providerId: string
  value: string
  onChange: (v: string) => void
  placeholder?: string
  /** Label shown above the field — omit to render without label */
  label?: string
  /** When true, an empty value means "use provider default" */
  allowEmpty?: boolean
}

export function ModelComboBox({ providerId, value, onChange, placeholder, label, allowEmpty }: Props) {
  const [models, setModels] = useState<string[]>([])
  const [supported, setSupported] = useState(false)
  const [loading, setLoading] = useState(false)
  const [open, setOpen] = useState(false)
  const [filter, setFilter] = useState('')
  const inputRef = useRef<HTMLInputElement>(null)
  const dropRef = useRef<HTMLDivElement>(null)

  // Fetch models whenever the provider changes
  useEffect(() => {
    if (!providerId) { setModels([]); setSupported(false); return }
    setLoading(true)
    api.providers.listModels(providerId)
      .then(r => {
        setSupported(r.supported)
        setModels(r.models ?? [])
      })
      .catch(() => { setSupported(false); setModels([]) })
      .finally(() => setLoading(false))
  }, [providerId])

  // Close dropdown on outside click
  useEffect(() => {
    const handler = (e: MouseEvent) => {
      if (dropRef.current && !dropRef.current.contains(e.target as Node)) {
        setOpen(false)
        setFilter('')
      }
    }
    document.addEventListener('mousedown', handler)
    return () => document.removeEventListener('mousedown', handler)
  }, [])

  const filtered = filter
    ? models.filter(m => m.toLowerCase().includes(filter.toLowerCase()))
    : models

  // Not supported — just render a plain text input
  if (!loading && !supported) {
    return (
      <div>
        {label && <label className="block text-sm font-medium text-slate-300 mb-1">{label}</label>}
        <input
          type="text"
          value={value}
          onChange={e => onChange(e.target.value)}
          placeholder={placeholder ?? 'Model name'}
          className="w-full bg-slate-900 border border-slate-700 rounded-lg px-3 py-2 text-sm text-slate-200 placeholder-slate-500 focus:outline-none focus:border-violet-500 transition-colors"
        />
        <p className="text-xs text-slate-600 mt-1">Enter the model name manually — this provider doesn't support model listing.</p>
      </div>
    )
  }

  const displayValue = value || ''

  return (
    <div ref={dropRef} className="relative">
      {label && <label className="block text-sm font-medium text-slate-300 mb-1">{label}</label>}

      {/* Trigger input */}
      <div
        className="flex items-center w-full bg-slate-900 border border-slate-700 rounded-lg px-3 py-2 gap-2 cursor-text focus-within:border-violet-500 transition-colors"
        onClick={() => { setOpen(true); setFilter(value); inputRef.current?.focus() }}
      >
        <input
          ref={inputRef}
          type="text"
          value={open ? filter : displayValue}
          onChange={e => { setFilter(e.target.value); onChange(e.target.value); setOpen(true) }}
          onFocus={() => { setOpen(true); setFilter(value) }}
          onKeyDown={e => {
            if (e.key === 'Escape') { setOpen(false); setFilter('') }
            if (e.key === 'Enter' && filtered.length > 0) {
              onChange(filtered[0])
              setOpen(false)
              setFilter('')
            }
          }}
          placeholder={loading ? 'Loading models…' : (placeholder ?? 'Select or type model name')}
          className="flex-1 bg-transparent text-sm text-slate-200 placeholder-slate-500 outline-none min-w-0"
        />
        {loading ? (
          <span className="text-xs text-slate-500 flex-shrink-0">⟳</span>
        ) : models.length > 0 ? (
          <button
            type="button"
            onClick={e => { e.stopPropagation(); setOpen(o => !o); setFilter('') }}
            className="text-slate-500 hover:text-slate-300 flex-shrink-0 text-xs"
          >
            {open ? '▲' : '▼'}
          </button>
        ) : null}
      </div>

      {/* Dropdown */}
      {open && filtered.length > 0 && (
        <div className="absolute z-50 mt-1 w-full bg-slate-900 border border-slate-700 rounded-lg shadow-xl overflow-hidden">
          {allowEmpty && (
            <button
              type="button"
              onClick={() => { onChange(''); setOpen(false); setFilter('') }}
              className="w-full text-left px-3 py-2 text-xs text-slate-500 hover:bg-slate-800 border-b border-slate-800"
            >
              (use provider default)
            </button>
          )}
          <div className="max-h-56 overflow-y-auto">
            {filtered.map(m => (
              <button
                key={m}
                type="button"
                onClick={() => { onChange(m); setOpen(false); setFilter('') }}
                className={`w-full text-left px-3 py-2 text-sm hover:bg-slate-800 transition-colors ${
                  m === value ? 'text-violet-300 bg-violet-900/20' : 'text-slate-200'
                }`}
              >
                {m}
              </button>
            ))}
          </div>
          <div className="px-3 py-1.5 border-t border-slate-800">
            <p className="text-xs text-slate-600">{models.length} model{models.length !== 1 ? 's' : ''} available · type to filter</p>
          </div>
        </div>
      )}

      {/* No matches hint */}
      {open && filter && filtered.length === 0 && models.length > 0 && (
        <div className="absolute z-50 mt-1 w-full bg-slate-900 border border-slate-700 rounded-lg shadow-xl px-3 py-2">
          <p className="text-xs text-slate-500">No match — press Enter or keep typing to use <span className="text-slate-300">{filter}</span> as a custom model name.</p>
        </div>
      )}
    </div>
  )
}
