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
  '/briefing': 'Briefing',
  '/projects': 'Projects',
  '/monitors': 'Monitors',
  '/tasks': 'Tasks',
  '/teams': 'Teams',
  '/plugins': 'Plugins',
  '/settings': 'Settings',
  '/help': 'Help',
  '/feed': 'Event Log',
}

export function AppLayout({ children, onLogout, userName }: { children: React.ReactNode; onLogout?: () => void; userName?: string }) {
  const location = useLocation()
  const [inboxCount, setInboxCount] = useState(0)
  const [activeCount, setActiveCount] = useState(0)
  const [memoCount, setMemoCount] = useState(0)

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

  const refreshMemos = useCallback(async () => {
    try {
      const { count } = await api.memos.count()
      setMemoCount(count)
    } catch { /* ignore */ }
  }, [])

  useEffect(() => {
    const initialLoad = window.setTimeout(() => {
      void refreshInbox()
      void refreshRunning()
      void refreshMemos()
    }, 0)
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
      if (ev.type === 'memo.created') {
        refreshMemos()
      }
    })
    // Periodic re-sync in case WS missed an event (dismissed tasks, reconnects)
    const poll = setInterval(() => { refreshInbox(); refreshRunning(); refreshMemos() }, 30_000)
    return () => { unsub(); clearInterval(poll); clearTimeout(initialLoad) }
  }, [refreshInbox, refreshRunning, refreshMemos])

  return (
    <div className="flex h-screen bg-slate-950 text-slate-100 overflow-hidden">
      <Sidebar inboxCount={inboxCount} activeCount={activeCount} memoCount={memoCount} />
      <main className="flex-1 flex flex-col overflow-hidden">
        <TopBar inboxCount={inboxCount} title={title} onLogout={onLogout} userName={userName} />
        <div className="flex-1 overflow-auto p-6">
          {children}
        </div>
      </main>
      <QuickTaskButton />
    </div>
  )
}
