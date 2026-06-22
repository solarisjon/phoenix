// Theme management for Phoenix.
// Themes are applied by setting data-theme on <html>; CSS variables do the rest.
// Community themes are loaded from the API and injected as <style> blocks.

export type ThemeKind = 'dark' | 'light'

export interface ThemeEntry {
  id: string
  kind: ThemeKind
  label: string
  description: string
  preview: string[]
  isCustom?: boolean
  vars?: Record<string, string>
}

export const THEMES: readonly ThemeEntry[] = [
  // ── Dark ──────────────────────────────────────────────────────────────────
  { id: 'dracula',         kind: 'dark',  label: 'Dracula',         description: 'Classic dark & purple',    preview: ['#1e1f28', '#bd93f9', '#282a36'] },
  { id: 'monokai',         kind: 'dark',  label: 'Monokai',         description: 'Warm charcoal & lavender', preview: ['#2d2a2e', '#ab9df2', '#403e41'] },
  { id: 'solarized',       kind: 'dark',  label: 'Solarized Dark',  description: 'Precision dark teal',      preview: ['#002b36', '#268bd2', '#073642'] },
  { id: 'mirage',          kind: 'dark',  label: 'Mirage',          description: 'Ayu dark blue-gray',       preview: ['#1a1f29', '#6dcbfa', '#212733'] },
  { id: 'afterglow',       kind: 'dark',  label: 'Afterglow',       description: 'Subtle neutral & slate',   preview: ['#181818', '#6c99ba', '#202020'] },
  { id: 'tomorrow-night',  kind: 'dark',  label: 'Tomorrow Night',  description: 'Clean dark & steel blue',  preview: ['#1d1f21', '#80a1bd', '#282a2e'] },
  // ── Light ─────────────────────────────────────────────────────────────────
  { id: 'solarized-light', kind: 'light', label: 'Solarized Light', description: 'Warm cream & blue',        preview: ['#fdf6e3', '#268bd2', '#eee8d5'] },
  { id: 'ayu-light',       kind: 'light', label: 'Ayu Light',       description: 'Clean minimal & teal',     preview: ['#fafafa', '#41a6d9', '#ffffff'] },
  { id: 'atom-light',      kind: 'light', label: 'Atom One Light',  description: 'Fresh white & blue',       preview: ['#f8f8f8', '#2f5af3', '#ffffff'] },
  { id: 'tomorrow',        kind: 'light', label: 'Tomorrow',        description: 'Pure white classic',       preview: ['#ffffff', '#4170ae', '#fafafa'] },
  { id: 'paper',           kind: 'light', label: 'Paper',           description: 'Soft gray & teal',         preview: ['#eeeeee', '#0087af', '#ffffff'] },
  { id: 'nord-light',      kind: 'light', label: 'Nord Light',      description: 'Arctic frost & blue',      preview: ['#eceff4', '#5e81ac', '#e5e9f0'] },
] as const

const STORAGE_KEY = 'phoenix-theme'
const DEFAULT = 'dracula'

export function getTheme(): string {
  try {
    const stored = localStorage.getItem(STORAGE_KEY)
    if (stored) return stored
  } catch { /* SSR / private mode */ }
  return DEFAULT
}

export function setTheme(id: string) {
  document.documentElement.setAttribute('data-theme', id)
  try { localStorage.setItem(STORAGE_KEY, id) } catch { /* ignore */ }
}

export function initTheme() {
  setTheme(getTheme())
}

/**
 * Inject community themes as a <style> block so CSS variables are available
 * when the user selects a custom theme via data-theme attribute.
 */
export function injectCommunityThemes(themes: { id: string; vars: Record<string, string> }[]) {
  const existing = document.getElementById('phoenix-community-themes')
  if (existing) existing.remove()

  if (themes.length === 0) return

  const style = document.createElement('style')
  style.id = 'phoenix-community-themes'
  style.textContent = themes.map(t =>
    `[data-theme="${t.id}"] {\n` +
    Object.entries(t.vars).map(([k, v]) => `  --${k}: ${v};`).join('\n') +
    '\n}'
  ).join('\n\n')
  document.head.appendChild(style)
}
