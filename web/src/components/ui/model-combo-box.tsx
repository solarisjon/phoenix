/**
 * ModelComboBox — searchable model picker.
 *
 * Two fetch modes (mutually exclusive):
 *   providerId   — GET /api/providers/:id/models  (saved provider, agent page)
 *   directFetch  — calls the provider API directly from the browser
 *                  (used during provider creation before the record is saved)
 *
 * Always renders the combo-box UI — no "not supported" text-only fallback.
 * If models can't be fetched the list is empty and the user can still type freely.
 */

import { useCallback, useEffect, useRef, useState } from 'react'
import { api } from '@/lib/api'
import { getErrorMessage } from '@/lib/errors'

export interface OllamaDirectConfig { kind: 'ollama'; baseUrl: string }
export interface LLMDirectConfig    { kind: 'llm';    endpoint: string; authHeader: string }
export type DirectFetchConfig = OllamaDirectConfig | LLMDirectConfig

interface Props {
  providerId?:   string              // saved provider — uses backend /models proxy
  directFetch?:  DirectFetchConfig   // live config — fetches provider API directly
  value:         string
  onChange:      (v: string) => void
  placeholder?:  string
  allowEmpty?:   boolean             // adds "(use provider default)" at top of list
}

// ---- direct-fetch helpers (browser → provider API) ----

async function ollamaModels(baseUrl: string): Promise<string[]> {
  const r = await fetch(baseUrl.replace(/\/$/, '') + '/api/tags')
  if (!r.ok) throw new Error(`Ollama ${r.status}`)
  const d = await r.json()
  return (d.models ?? []).map((m: { name: string }) => m.name)
}

async function llmModels(endpoint: string, authHeader: string): Promise<string[]> {
  let base = endpoint
  for (const s of ['/v1/chat/completions', '/chat/completions', '/v1']) {
    if (base.endsWith(s)) { base = base.slice(0, -s.length); break }
  }
  const r = await fetch(base.replace(/\/$/, '') + '/v1/models', {
    headers: authHeader ? { Authorization: authHeader } : {},
  })
  if (!r.ok) throw new Error(`Server ${r.status}`)
  const d = await r.json()
  return (d.data ?? []).map((m: { id: string }) => m.id).filter(Boolean)
}

// ---- component ----

export function ModelComboBox({
  providerId, directFetch, value, onChange, placeholder, allowEmpty,
}: Props) {
  const [models,     setModels]     = useState<string[]>([])
  const [loading,    setLoading]    = useState(false)
  const [fetchErr,   setFetchErr]   = useState('')
  const [open,       setOpen]       = useState(false)
  const [filter,     setFilter]     = useState('')
  const inputRef = useRef<HTMLInputElement>(null)
  const wrapRef  = useRef<HTMLDivElement>(null)
  const directFetchKind = directFetch?.kind
  const directFetchBaseUrl = directFetchKind === 'ollama' ? directFetch.baseUrl : ''
  const directFetchEndpoint = directFetchKind === 'llm' ? directFetch.endpoint : ''
  const directFetchAuthHeader = directFetchKind === 'llm' ? directFetch.authHeader : ''

  // ---- fetch logic ----
  const fetchModels = useCallback(async () => {
    setLoading(true); setFetchErr('')
    try {
      let names: string[] = []
      if (providerId) {
        const r = await api.providers.listModels(providerId)
        names = r.supported ? (r.models ?? []) : []
      } else if (directFetchKind === 'ollama' && directFetchBaseUrl) {
        names = await ollamaModels(directFetchBaseUrl)
      } else if (directFetchKind === 'llm' && directFetchEndpoint) {
        names = await llmModels(directFetchEndpoint, directFetchAuthHeader)
      }
      setModels(names)
    } catch (error: unknown) {
      setFetchErr(getErrorMessage(error, 'Could not fetch models'))
    } finally {
      setLoading(false)
    }
  }, [directFetchAuthHeader, directFetchBaseUrl, directFetchEndpoint, directFetchKind, providerId])

  useEffect(() => {
    const hasTarget =
      !!providerId ||
      (directFetchKind === 'ollama' && !!directFetchBaseUrl) ||
      (directFetchKind === 'llm' && !!directFetchEndpoint)
    if (!hasTarget) return
    const timer = window.setTimeout(() => {
      void fetchModels()
    }, 0)
    return () => window.clearTimeout(timer)
  }, [directFetchBaseUrl, directFetchEndpoint, directFetchKind, fetchModels, providerId])

  // Close on outside click
  useEffect(() => {
    const h = (e: MouseEvent) => {
      if (wrapRef.current && !wrapRef.current.contains(e.target as Node)) {
        setOpen(false); setFilter('')
      }
    }
    document.addEventListener('mousedown', h)
    return () => document.removeEventListener('mousedown', h)
  }, [])

  const availableModels = providerId || directFetch ? models : []
  const filtered = filter
    ? availableModels.filter(m => m.toLowerCase().includes(filter.toLowerCase()))
    : availableModels

  const canFetch = !!(providerId || directFetch)

  return (
    <div ref={wrapRef} className="relative">

      {/* Input row */}
      <div
        className="flex items-center w-full bg-slate-900 border border-slate-700 rounded-lg px-3 py-2 gap-2 cursor-text focus-within:border-violet-500 transition-colors"
        onClick={() => { setOpen(true); setFilter(value); inputRef.current?.focus() }}
      >
        <input
          ref={inputRef}
          type="text"
          value={open ? filter : (value || '')}
          onChange={e => { setFilter(e.target.value); onChange(e.target.value); setOpen(true) }}
          onFocus={() => { setOpen(true); setFilter(value) }}
          onKeyDown={e => {
            if (e.key === 'Escape') { setOpen(false); setFilter('') }
            if (e.key === 'Enter' && filtered.length > 0) {
              onChange(filtered[0]); setOpen(false); setFilter('')
            }
          }}
          placeholder={
            loading ? 'Fetching models…' :
            placeholder ?? 'Select or type a model name'
          }
          className="flex-1 bg-transparent text-sm text-slate-200 placeholder-slate-500 outline-none min-w-0"
        />

        {/* Right-side controls */}
        {loading ? (
          <span className="text-xs text-slate-500 animate-pulse flex-shrink-0">loading…</span>
        ) : availableModels.length > 0 ? (
          <button type="button"
            onClick={e => { e.stopPropagation(); setOpen(o => !o); setFilter('') }}
            className="text-slate-500 hover:text-slate-300 flex-shrink-0 text-xs px-1"
          >
            {open ? '▲' : '▼'}
          </button>
        ) : canFetch ? (
          <button type="button"
            onClick={e => { e.stopPropagation(); fetchModels() }}
            title="Fetch available models"
            className="text-xs text-violet-400 hover:text-violet-300 flex-shrink-0 px-1"
          >
            ↻ fetch
          </button>
        ) : null}
      </div>

      {/* Fetch error */}
      {fetchErr && !loading && (
        <p className="text-xs text-amber-500 mt-1">
          {fetchErr} — type a model name above, or{' '}
          <button type="button" onClick={fetchModels}
            className="underline hover:text-amber-400">retry</button>.
        </p>
      )}

      {/* Dropdown */}
      {open && filtered.length > 0 && (
        <div className="absolute z-50 mt-1 w-full bg-slate-900 border border-slate-700 rounded-lg shadow-xl overflow-hidden">
          {allowEmpty && (
            <button type="button"
              onClick={() => { onChange(''); setOpen(false); setFilter('') }}
              className="w-full text-left px-3 py-2 text-xs text-slate-500 hover:bg-slate-800 border-b border-slate-800"
            >
              (use provider default)
            </button>
          )}
          <div className="max-h-60 overflow-y-auto">
            {filtered.map(m => (
              <button key={m} type="button"
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
            <p className="text-xs text-slate-600">
              {availableModels.length} model{availableModels.length !== 1 ? 's' : ''} · type to filter · Enter selects first
            </p>
          </div>
        </div>
      )}

      {/* No match hint */}
      {open && filter && filtered.length === 0 && availableModels.length > 0 && (
        <div className="absolute z-50 mt-1 w-full bg-slate-900 border border-slate-700 rounded-lg shadow-xl px-3 py-2">
          <p className="text-xs text-slate-500">
            No match — keep typing to use <span className="text-slate-300">"{filter}"</span> as a custom model name.
          </p>
        </div>
      )}
    </div>
  )
}
