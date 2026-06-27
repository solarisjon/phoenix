import { describe, it, expect, vi, beforeEach } from 'vitest'
import { getTheme, setTheme, initTheme, THEMES, injectCommunityThemes } from '../lib/theme'

// jsdom provides window.localStorage, but Node.js 22 may shadow the global.
// Provide a simple in-memory storage implementation for all localStorage tests.
const storageMock = (() => {
  let store: Record<string, string> = {}
  return {
    getItem: (key: string) => store[key] ?? null,
    setItem: (key: string, val: string) => { store[key] = val },
    removeItem: (key: string) => { delete store[key] },
    clear: () => { store = {} },
  }
})()

vi.stubGlobal('localStorage', storageMock)

describe('THEMES', () => {
  it('contains both dark and light themes', () => {
    const kinds = new Set(THEMES.map((t) => t.kind))
    expect(kinds).toContain('dark')
    expect(kinds).toContain('light')
  })

  it('each theme has required fields', () => {
    for (const theme of THEMES) {
      expect(theme.id).toBeTruthy()
      expect(theme.label).toBeTruthy()
      expect(theme.preview).toHaveLength(3)
    }
  })
})

describe('getTheme / setTheme', () => {
  beforeEach(() => {
    localStorage.clear()
    document.documentElement.removeAttribute('data-theme')
  })

  it('returns default theme when nothing stored', () => {
    expect(getTheme()).toBe('dracula')
  })

  it('returns stored theme', () => {
    localStorage.setItem('phoenix-theme', 'monokai')
    expect(getTheme()).toBe('monokai')
  })

  it('setTheme writes to localStorage and sets data-theme', () => {
    setTheme('solarized')
    expect(localStorage.getItem('phoenix-theme')).toBe('solarized')
    expect(document.documentElement.getAttribute('data-theme')).toBe('solarized')
  })
})

describe('initTheme', () => {
  it('applies the stored theme on init', () => {
    localStorage.setItem('phoenix-theme', 'afterglow')
    initTheme()
    expect(document.documentElement.getAttribute('data-theme')).toBe('afterglow')
  })
})

describe('injectCommunityThemes', () => {
  it('injects a style block for community themes', () => {
    injectCommunityThemes([{ id: 'my-theme', vars: { 'color-bg': '#111' } }])
    const style = document.getElementById('phoenix-community-themes')
    expect(style).not.toBeNull()
    expect(style?.textContent).toContain('[data-theme="my-theme"]')
    expect(style?.textContent).toContain('--color-bg: #111')
  })

  it('removes old style block when re-injecting', () => {
    injectCommunityThemes([{ id: 'a', vars: { 'x': '1' } }])
    injectCommunityThemes([{ id: 'b', vars: { 'y': '2' } }])
    const blocks = document.querySelectorAll('#phoenix-community-themes')
    expect(blocks).toHaveLength(1)
    expect(blocks[0].textContent).toContain('"b"')
    expect(blocks[0].textContent).not.toContain('"a"')
  })

  it('removes style block when given empty array', () => {
    injectCommunityThemes([{ id: 'x', vars: {} }])
    injectCommunityThemes([])
    expect(document.getElementById('phoenix-community-themes')).toBeNull()
  })
})
