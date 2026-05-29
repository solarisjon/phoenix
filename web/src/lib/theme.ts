// Theme management for Phoenix.
// Themes are applied by setting data-theme on <html>; CSS variables do the rest.

export const THEMES = [
  { id: 'dark',     label: 'Dark',     description: 'Default — slate & violet',  preview: ['#020817', '#7c3aed', '#0f172a'] },
  { id: 'midnight', label: 'Midnight', description: 'Deep black & blue',          preview: ['#000000', '#2563eb', '#0d0d14'] },
  { id: 'forest',   label: 'Forest',   description: 'Dark green & emerald',       preview: ['#030d07', '#059669', '#071a0e'] },
  { id: 'ember',    label: 'Ember',    description: 'Warm dark & orange',         preview: ['#0c0500', '#ea580c', '#1a0e00'] },
  { id: 'light',    label: 'Light',    description: 'Light mode',                 preview: ['#f8fafc', '#7c3aed', '#ffffff'] },
] as const

export type ThemeID = typeof THEMES[number]['id']

const STORAGE_KEY = 'phoenix-theme'
const DEFAULT: ThemeID = 'dark'

export function getTheme(): ThemeID {
  try {
    const stored = localStorage.getItem(STORAGE_KEY)
    if (stored && THEMES.some(t => t.id === stored)) return stored as ThemeID
  } catch { /* SSR / private mode */ }
  return DEFAULT
}

export function setTheme(id: ThemeID) {
  document.documentElement.setAttribute('data-theme', id)
  try { localStorage.setItem(STORAGE_KEY, id) } catch { /* ignore */ }
}

export function initTheme() {
  setTheme(getTheme())
}
