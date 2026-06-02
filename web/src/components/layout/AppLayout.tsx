import { useState, useEffect, useCallback } from 'react'
import { useLocation } from 'react-router-dom'
import { Sidebar } from './Sidebar'
import { TopBar } from './TopBar'
import { api } from '@/lib/api'
import { phoenixWS } from '@/lib/ws'
import { QuickTaskButton } from '@/components/ui/quick-task'

const titles: Record<string, string> = {
  '/': 'Dashboard',
  '/inbox': 'Inbox',
  '/projects': 'Projects',
  '/monitors': 'Monitors',
  '/tasks': 'Tasks',
  '/teams': 'Teams',
  '/settings': 'Settings',
  '/feed': 'Event Log',
}

export function AppLayout({ children }: { children: React.ReactNode }) {
  const location = useLocation()
  const [inboxCount, setInboxCount] = useState(0)
  const [activeCount, setActiveCount] = useState(0)

  const title = Object.entries(titles).find(([path]) =>
    path === '/' ? location.pathname === '/' : location.pathname.startsWith(path)
  )?.[1] ?? 'Phoenix'

  const refreshInbox = useCallback(async () => {
    try {
      const [items, drafts] = await Promise.all([
        api.inbox.list(),
        api.agentDrafts.list(),
      ])
      setInboxCount(items.length + drafts.length)
    } catch { /* ignore */ }
  }, [])

  const refreshRunning = useCallback(async () => {
    try {
      const tasks = await api.tasks.listRunning()
      setActiveCount(tasks.length)
    } catch { /* ignore */ }
  }, [])

  useEffect(() => {
    refreshInbox()
    refreshRunning()
    phoenixWS.connect()
    const unsub = phoenixWS.on((ev) => {
      if (
        ev.type === 'inbox.new_item' ||
        ev.type === 'task.status_changed' ||
        ev.type === 'agent_draft.created'
      ) {
        refreshInbox()
      }
      if (ev.type === 'task.status_changed') {
        refreshRunning()
      }
    })
    // Periodic re-sync in case WS missed an event (dismissed tasks, reconnects)
    const poll = setInterval(() => { refreshInbox(); refreshRunning() }, 30_000)
    return () => { unsub(); clearInterval(poll) }
  }, [refreshInbox, refreshRunning])

  return (
    <div className="flex h-screen bg-slate-950 text-slate-100 overflow-hidden">
      <Sidebar inboxCount={inboxCount} activeCount={activeCount} />
      <main className="flex-1 flex flex-col overflow-hidden">
        <TopBar inboxCount={inboxCount} title={title} />
        <div className="flex-1 overflow-auto p-6">
          {children}
        </div>
      </main>
      <QuickTaskButton />
    </div>
  )
}
