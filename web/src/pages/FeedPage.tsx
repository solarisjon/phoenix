import { useEffect, useRef, useState } from 'react'
import { phoenixWS } from '@/lib/ws'
import type { WSEvent } from '@/lib/ws'
import { cn } from '@/lib/utils'

interface FeedEntry {
  id: number
  at: Date
  icon: string
  title: string
  detail: string
  kind: 'task' | 'agent' | 'inbox' | 'monitor' | 'system'
  cost?: number
}

let seq = 0

function eventToEntry(ev: WSEvent): FeedEntry | null {
  const at = new Date()
  const id = ++seq

  switch (ev.type) {
    case 'task.status_changed': {
      const { payload } = ev
      const status = payload.status
      const icons: Record<string, string> = {
        pending: '○',
        running: '◉',
        done: '✓',
        failed: '✗',
        cancelled: '⊘',
        waiting_for_human: '⏸',
      }
      const icon = icons[status] ?? '•'
      const taskId = payload.task_id.slice(0, 8)
      const cost = payload.cost_usd
      return {
        id, at, icon, kind: 'task',
        title: `Task ${taskId} → ${status}`,
        detail: payload.project_id ? `project ${payload.project_id.slice(0, 8)}` : '',
        cost,
      }
    }

    case 'task.output_stream': {
      const { payload } = ev
      const taskId = payload.task_id.slice(0, 8)
      const chunk = payload.chunk.slice(0, 80)
      return {
        id, at, icon: '›', kind: 'task',
        title: `Task ${taskId} output`,
        detail: chunk,
      }
    }

    case 'agent.status_changed': {
      const { payload } = ev
      const status = payload.status
      return {
        id, at, icon: status === 'busy' ? '◉' : '○', kind: 'agent',
        title: `Agent status → ${status}`,
        detail: payload.agent_id.slice(0, 8),
      }
    }

    case 'inbox.new_item': {
      const { payload } = ev
      return {
        id, at, icon: '⊡', kind: 'inbox',
        title: `Inbox: ${payload.title || 'new item'}`,
        detail: `agent ${payload.agent_id.slice(0, 8)}`,
      }
    }

    case 'agent_draft.created': {
      const { payload } = ev
      return {
        id, at, icon: '✦', kind: 'agent',
        title: 'Agent draft created',
        detail: payload.name,
      }
    }

    case 'memo.created': {
      const { payload } = ev
      return {
        id, at, icon: '📝', kind: 'system',
        title: 'Memo created',
        detail: payload.title,
      }
    }

    case 'budget.exceeded': {
      const { payload } = ev
      return {
        id, at, icon: '⚠', kind: 'system',
        title: 'Project budget exceeded',
        detail: `${payload.project_id.slice(0, 8)} · $${payload.spent_usd.toFixed(2)} / $${payload.budget_usd.toFixed(2)}`,
      }
    }

    case 'connected': {
      return {
        id, at, icon: '⚡', kind: 'system',
        title: 'Connected to Phoenix',
        detail: '',
      }
    }

  }

  const unknownEvent = ev as { type: string; payload?: unknown }
  return {
    id, at, icon: '•', kind: 'system',
    title: String(unknownEvent.type),
    detail: JSON.stringify(unknownEvent.payload ?? '').slice(0, 80),
  }
}

const kindColour: Record<string, string> = {
  task: 'text-violet-400',
  agent: 'text-sky-400',
  inbox: 'text-amber-400',
  monitor: 'text-emerald-400',
  system: 'text-slate-500',
}

function fmt(d: Date) {
  return d.toLocaleTimeString([], { hour: '2-digit', minute: '2-digit', second: '2-digit' })
}

const MAX_ENTRIES = 500

export default function FeedPage() {
  const [entries, setEntries] = useState<FeedEntry[]>([])
  const [paused, setPaused] = useState(false)
  const [filter, setFilter] = useState<string>('all')
  const [hideStream, setHideStream] = useState(true)
  const bottomRef = useRef<HTMLDivElement>(null)
  const pausedRef = useRef(paused)

  useEffect(() => {
    pausedRef.current = paused
  }, [paused])

  useEffect(() => {
    return phoenixWS.on((ev) => {
      if (pausedRef.current) return
      if (hideStream && ev.type === 'task.output_stream') return
      const entry = eventToEntry(ev)
      if (!entry) return
      setEntries(prev => {
        const next = [...prev, entry]
        return next.length > MAX_ENTRIES ? next.slice(next.length - MAX_ENTRIES) : next
      })
    })
  }, [hideStream])

  // Auto-scroll when not paused
  useEffect(() => {
    if (!paused) {
      bottomRef.current?.scrollIntoView({ behavior: 'smooth' })
    }
  }, [entries, paused])

  const visible = filter === 'all'
    ? entries
    : entries.filter(e => e.kind === filter)

  return (
    <div className="flex flex-col h-full">
      {/* Header */}
      <div className="flex items-center justify-between px-6 py-4 border-b border-slate-800 flex-shrink-0">
        <div>
          <h1 className="text-lg font-semibold text-white">Feed</h1>
          <p className="text-xs text-slate-500 mt-0.5">Live event stream — visibility only, no action needed</p>
        </div>
        <div className="flex items-center gap-3">
          {/* Filter tabs */}
          {(['all', 'task', 'agent', 'inbox', 'system'] as const).map(k => (
            <button
              key={k}
              onClick={() => setFilter(k)}
              className={cn(
                'text-xs px-2.5 py-1 rounded-full transition-colors',
                filter === k
                  ? 'bg-violet-600/30 text-violet-300'
                  : 'text-slate-500 hover:text-slate-300 hover:bg-slate-800'
              )}
            >
              {k}
            </button>
          ))}
          <div className="w-px h-4 bg-slate-700" />
          <button
            onClick={() => setHideStream(h => !h)}
            className={cn(
              'text-xs px-2.5 py-1 rounded-full transition-colors',
              hideStream ? 'text-slate-500 hover:text-slate-300' : 'text-amber-400'
            )}
            title="Toggle streaming output chunks"
          >
            {hideStream ? 'show stream' : 'hide stream'}
          </button>
          <button
            onClick={() => setPaused(p => !p)}
            className={cn(
              'text-xs px-3 py-1.5 rounded-lg transition-colors font-medium',
              paused
                ? 'bg-amber-600/30 text-amber-300 hover:bg-amber-600/40'
                : 'bg-slate-800 text-slate-400 hover:text-white'
            )}
          >
            {paused ? '▶ Resume' : '⏸ Pause'}
          </button>
          <button
            onClick={() => setEntries([])}
            className="text-xs px-3 py-1.5 rounded-lg bg-slate-800 text-slate-500 hover:text-slate-300 transition-colors"
          >
            Clear
          </button>
        </div>
      </div>

      {/* Feed list */}
      <div className="flex-1 overflow-y-auto px-6 py-3 space-y-0.5 font-mono text-xs">
        {visible.length === 0 && (
          <div className="flex flex-col items-center justify-center h-full text-slate-600 gap-2">
            <span className="text-3xl">⚡</span>
            <p>Waiting for events…</p>
            <p className="text-slate-700">Events appear here as agents work, tasks run, and monitors fire.</p>
          </div>
        )}
        {visible.map(entry => (
          <div
            key={entry.id}
            className="flex items-start gap-3 py-1 border-b border-slate-800/40 hover:bg-slate-800/20 px-1 rounded"
          >
            <span className="text-slate-600 w-20 flex-shrink-0 pt-0.5">{fmt(entry.at)}</span>
            <span className={cn('flex-shrink-0 w-4 text-center', kindColour[entry.kind])}>{entry.icon}</span>
            <span className="text-slate-300 flex-1 truncate">{entry.title}</span>
            {entry.detail && (
              <span className="text-slate-600 truncate max-w-xs">{entry.detail}</span>
            )}
            {entry.cost !== undefined && entry.cost > 0 && (
              <span className="text-emerald-600 flex-shrink-0">${entry.cost.toFixed(4)}</span>
            )}
          </div>
        ))}
        <div ref={bottomRef} />
      </div>

      {/* Status bar */}
      <div className="px-6 py-2 border-t border-slate-800 flex items-center gap-4 text-xs text-slate-600 flex-shrink-0">
        <span>{visible.length} event{visible.length !== 1 ? 's' : ''}{filter !== 'all' ? ` (${filter})` : ''}</span>
        {paused && <span className="text-amber-500">⏸ Paused — new events buffered</span>}
        {entries.length >= MAX_ENTRIES && <span className="text-slate-700">Showing last {MAX_ENTRIES}</span>}
      </div>
    </div>
  )
}
