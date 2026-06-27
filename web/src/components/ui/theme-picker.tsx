import { useState, useEffect } from 'react'
import { THEMES, getTheme, setTheme, injectCommunityThemes } from '@/lib/theme'
import type { ThemeEntry, ThemeKind } from '@/lib/theme'
import { api } from '@/lib/api'

function ThemeGroup({ heading, themes, current, onApply }: {
  heading: string
  themes: ThemeEntry[]
  current: string
  onApply: (id: string) => void
}) {
  if (themes.length === 0) return null
  return (
    <div>
      <p className="text-xs text-slate-500 px-2 py-1 font-semibold uppercase tracking-widest">{heading}</p>
      {themes.map(theme => (
        <button
          key={theme.id}
          onClick={() => onApply(theme.id)}
          className={`w-full flex items-center gap-3 px-2.5 py-1.5 rounded-lg text-left transition-colors ${
            current === theme.id
              ? 'bg-violet-600/20 text-violet-300'
              : 'text-slate-300 hover:bg-slate-800'
          }`}
        >
          <span className="flex gap-0.5 flex-shrink-0">
            {theme.preview.map((c, i) => (
              <span key={i} className="w-3 h-3 rounded-full border border-white/10"
                style={{ backgroundColor: c }} />
            ))}
          </span>
          <span className="flex-1 min-w-0">
            <span className="block text-sm font-medium leading-none mb-0.5">
              {theme.label}
              {theme.isCustom && (
                <span className="ml-1.5 text-[9px] font-semibold px-1 py-px rounded bg-violet-600/20 text-violet-400 align-middle">CUSTOM</span>
              )}
            </span>
            <span className="block text-xs text-slate-500 leading-none">{theme.description}</span>
          </span>
          {current === theme.id && <span className="text-violet-400 text-xs flex-shrink-0">✓</span>}
        </button>
      ))}
    </div>
  )
}

export function ThemePicker() {
  const [current, setCurrent] = useState<string>(getTheme)
  const [open, setOpen] = useState(false)
  const [allThemes, setAllThemes] = useState<ThemeEntry[]>([...THEMES])

  // Load community themes from the API on mount.
  useEffect(() => {
    api.themes.list().then(community => {
      if (community.length === 0) return

      // Inject CSS for community themes.
      const toInject = community
        .filter(t => t.vars && Object.keys(t.vars).length > 0)
        .map(t => ({ id: t.id, vars: t.vars! }))
      injectCommunityThemes(toInject)

      // Merge into the theme list.
      const customEntries: ThemeEntry[] = community.map(t => ({
        id: t.id,
        kind: (t.kind || 'dark') as ThemeKind,
        label: t.label,
        description: 'Custom theme',
        preview: t.preview || [],
        isCustom: true,
        vars: t.vars,
      }))
      setAllThemes([...THEMES, ...customEntries])
    }).catch(() => { /* API not available — use built-in only */ })
  }, [])

  const apply = (id: string) => {
    setTheme(id)
    setCurrent(id)
    setOpen(false)
    // Persist to server so the preference survives cache clears and syncs across browsers
    api.admin.getSettings()
      .then(s => api.admin.saveSettings({ ...s, theme: id }))
      .catch(() => { /* non-critical — localStorage copy is the source of truth */ })
  }

  const currentTheme = allThemes.find(t => t.id === current) || allThemes[0]

  const darkThemes = allThemes.filter(t => t.kind === 'dark' && !t.isCustom)
  const lightThemes = allThemes.filter(t => t.kind === 'light' && !t.isCustom)
  const customDark = allThemes.filter(t => t.kind === 'dark' && t.isCustom)
  const customLight = allThemes.filter(t => t.kind === 'light' && t.isCustom)
  const customThemes = [...customDark, ...customLight]

  return (
    <div className="relative">
      <button
        onClick={() => setOpen(o => !o)}
        className="w-full flex items-center gap-2 px-2 py-1.5 rounded-lg text-slate-500 hover:text-slate-300 hover:bg-slate-800 transition-colors text-xs"
        title="Change theme"
      >
        <span className="flex gap-0.5 flex-shrink-0">
          {currentTheme.preview.map((c, i) => (
            <span key={i} className="w-2.5 h-2.5 rounded-full border border-white/10"
              style={{ backgroundColor: c }} />
          ))}
        </span>
        <span className="flex-1 text-left truncate">{currentTheme.label}</span>
        <span className="opacity-50">{open ? '▲' : '▼'}</span>
      </button>

      {open && (
        <>
          <div className="fixed inset-0 z-40" onClick={() => setOpen(false)} />
          <div className="absolute bottom-full left-0 mb-1 w-56 max-h-80 overflow-y-auto rounded-xl border border-slate-700 bg-slate-900 shadow-2xl z-50 p-1.5 space-y-1">
            <ThemeGroup heading="Dark"   themes={darkThemes}   current={current} onApply={apply} />
            <div className="border-t border-slate-800 my-1" />
            <ThemeGroup heading="Light"  themes={lightThemes}  current={current} onApply={apply} />
            {customThemes.length > 0 && (
              <>
                <div className="border-t border-slate-800 my-1" />
                <ThemeGroup heading="Custom" themes={customThemes} current={current} onApply={apply} />
              </>
            )}
          </div>
        </>
      )}
    </div>
  )
}
