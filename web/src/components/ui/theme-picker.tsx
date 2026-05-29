import { useState } from 'react'
import { THEMES, getTheme, setTheme, type ThemeID } from '@/lib/theme'

export function ThemePicker() {
  const [current, setCurrent] = useState<ThemeID>(getTheme)
  const [open, setOpen] = useState(false)

  const apply = (id: ThemeID) => {
    setTheme(id)
    setCurrent(id)
    setOpen(false)
  }

  const currentTheme = THEMES.find(t => t.id === current)!

  return (
    <div className="relative">
      <button
        onClick={() => setOpen(o => !o)}
        className="w-full flex items-center gap-2 px-2 py-1.5 rounded-lg text-slate-500 hover:text-slate-300 hover:bg-slate-800 transition-colors text-xs"
        title="Change theme"
      >
        {/* Mini colour preview */}
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
          {/* Backdrop */}
          <div className="fixed inset-0 z-40" onClick={() => setOpen(false)} />
          {/* Popup — above the button */}
          <div className="absolute bottom-full left-0 mb-1 w-52 rounded-xl border border-slate-700 bg-slate-900 shadow-2xl z-50 p-1.5 space-y-0.5">
            <p className="text-xs text-slate-500 px-2 py-1 font-medium uppercase tracking-wide">Theme</p>
            {THEMES.map(theme => (
              <button
                key={theme.id}
                onClick={() => apply(theme.id)}
                className={`w-full flex items-center gap-3 px-2.5 py-2 rounded-lg text-left transition-colors ${
                  current === theme.id
                    ? 'bg-violet-600/20 text-violet-300'
                    : 'text-slate-300 hover:bg-slate-800'
                }`}
              >
                {/* Colour swatches */}
                <span className="flex gap-0.5 flex-shrink-0">
                  {theme.preview.map((c, i) => (
                    <span key={i} className="w-3 h-3 rounded-full border border-white/10"
                      style={{ backgroundColor: c }} />
                  ))}
                </span>
                <span className="flex-1 min-w-0">
                  <span className="block text-sm font-medium leading-none mb-0.5">{theme.label}</span>
                  <span className="block text-xs text-slate-500 leading-none">{theme.description}</span>
                </span>
                {current === theme.id && <span className="text-violet-400 text-xs flex-shrink-0">✓</span>}
              </button>
            ))}
          </div>
        </>
      )}
    </div>
  )
}
