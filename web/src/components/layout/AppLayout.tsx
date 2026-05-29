import { useState, useEffect, useCallback } from 'react'
import { useLocation } from 'react-router-dom'
import { Sidebar } from './Sidebar'
import { TopBar } from './TopBar'
import { api } from '@/lib/api'
import { phoenixWS } from '@/lib/ws'

const titles: Record<string, string> = {
  '/': 'Dashboard',
  '/inbox': 'Inbox',
  '/projects': 'Projects',
  '/tasks': 'Tasks',
  '/teams': 'Teams',
  '/settings': 'Settings',
}

export function AppLayout({ children }: { children: React.ReactNode }) {
  const location = useLocation()
  const [inboxCount, setInboxCount] = useState(0)

  const title = Object.entries(titles).find(([path]) =>
    path === '/' ? location.pathname === '/' : location.pathname.startsWith(path)
  )?.[1] ?? 'Phoenix'

  const refreshInbox = useCallback(async () => {
    try {
      const items = await api.inbox.list()
      setInboxCount(items.length)
    } catch { /* ignore */ }
  }, [])

  useEffect(() => {
    refreshInbox()
    phoenixWS.connect()
    const unsub = phoenixWS.on((ev) => {
      if (ev.type === 'inbox.new_item' || ev.type === 'task.status_changed') {
        refreshInbox()
      }
    })
    return unsub
  }, [refreshInbox])

  return (
    <div className="flex h-screen bg-slate-950 text-slate-100 overflow-hidden">
      <Sidebar inboxCount={inboxCount} />
      <main className="flex-1 flex flex-col overflow-hidden">
        <TopBar inboxCount={inboxCount} title={title} />
        <div className="flex-1 overflow-auto p-6">
          {children}
        </div>
      </main>
    </div>
  )
}
