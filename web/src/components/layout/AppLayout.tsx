import { useState, useEffect, useCallback, useRef } from 'react'
import { useLocation, useNavigate } from 'react-router-dom'
import { Sidebar } from './Sidebar'
import { TopBar } from './TopBar'
import { api, type Provider } from '@/lib/api'
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
  const navigate = useNavigate()
  const [inboxCount, setInboxCount] = useState(0)
  const [activeCount, setActiveCount] = useState(0)
  const [memoCount, setMemoCount] = useState(0)
  const [unhealthyProviders, setUnhealthyProviders] = useState<Provider[]>([])
  const [toastDismissed, setToastDismissed] = useState(false)
  // Track which provider IDs were already known-unhealthy so we only re-surface the toast for new failures
  const knownUnhealthyIds = useRef<Set<string>>(new Set())

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

  const refreshProviders = useCallback(async () => {
    try {
      const providers = await api.providers.list()
      const unhealthy = providers.filter(p => p.health_status === 'error')
      setUnhealthyProviders(unhealthy)
      // Re-surface the toast if any new provider just became unhealthy
      const newIds = unhealthy.map(p => p.id)
      const hasNew = newIds.some(id => !knownUnhealthyIds.current.has(id))
      if (hasNew && unhealthy.length > 0) {
        setToastDismissed(false)
      }
      knownUnhealthyIds.current = new Set(newIds)
    } catch { /* ignore */ }
  }, [])

  useEffect(() => {
    const initialLoad = window.setTimeout(() => {
      void refreshInbox()
      void refreshRunning()
      void refreshMemos()
      void refreshProviders()
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
    const providerPoll = setInterval(() => { refreshProviders() }, 60_000)
    return () => { unsub(); clearInterval(poll); clearInterval(providerPoll); clearTimeout(initialLoad) }
  }, [refreshInbox, refreshRunning, refreshMemos, refreshProviders])

  return (
    <div className="flex h-screen bg-slate-950 text-slate-100 overflow-hidden">
      <Sidebar inboxCount={inboxCount} activeCount={activeCount} memoCount={memoCount} unhealthyProviderCount={unhealthyProviders.length} />
      <main className="flex-1 flex flex-col overflow-hidden">
        <TopBar inboxCount={inboxCount} title={title} onLogout={onLogout} userName={userName} />
        <div className="flex-1 overflow-auto p-6">
          {children}
        </div>
      </main>
      <QuickTaskButton />

      {/* Provider health alert toast */}
      {unhealthyProviders.length > 0 && !toastDismissed && (
        <div className="fixed bottom-6 right-6 z-50 max-w-sm w-full">
          <div className="bg-slate-900 border border-red-700/60 rounded-xl shadow-2xl overflow-hidden">
            <div className="flex items-start gap-3 px-4 py-3">
              <span className="mt-0.5 flex-shrink-0">
                <span className="relative flex h-3 w-3">
                  <span className="animate-ping absolute inline-flex h-full w-full rounded-full bg-red-500 opacity-75" />
                  <span className="relative inline-flex rounded-full h-3 w-3 bg-red-500" />
                </span>
              </span>
              <div className="flex-1 min-w-0">
                <p className="text-sm font-semibold text-red-400">
                  {unhealthyProviders.length === 1
                    ? `Provider "${unhealthyProviders[0].name}" is unhealthy`
                    : `${unhealthyProviders.length} providers are unhealthy`}
                </p>
                <p className="text-xs text-slate-400 mt-0.5 truncate">
                  {unhealthyProviders[0].health_error || 'Check credentials / connectivity'}
                </p>
                {unhealthyProviders.length > 1 && (
                  <p className="text-xs text-slate-500 mt-0.5">
                    {unhealthyProviders.map(p => p.name).join(', ')}
                  </p>
                )}
              </div>
              <button
                onClick={() => setToastDismissed(true)}
                className="flex-shrink-0 text-slate-500 hover:text-slate-300 transition-colors ml-1"
                aria-label="Dismiss"
              >
                ✕
              </button>
            </div>
            <div className="border-t border-slate-800 px-4 py-2 flex justify-end">
              <button
                onClick={() => { navigate('/settings?tab=providers'); setToastDismissed(true) }}
                className="text-xs text-red-400 hover:text-red-300 font-medium transition-colors"
              >
                Go to Providers →
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  )
}
