import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import './index.css'
import App from './App.tsx'
import { initTheme, setTheme } from '@/lib/theme'
import { api } from '@/lib/api'

// Apply saved theme before first render to avoid flash
initTheme()

// Sync from server in the background: if localStorage has no theme yet,
// adopt the server-persisted value on first load.
api.admin.getSettings().then(s => {
  if (s.theme && !localStorage.getItem('phoenix-theme')) {
    setTheme(s.theme)
  }
}).catch(() => { /* server unreachable — stay with local value */ })

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <App />
  </StrictMode>,
)
