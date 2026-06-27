import { useState, useEffect, useCallback, useRef } from 'react'
import { api, type Memo, type Task } from '@/lib/api'
import { phoenixWS } from '@/lib/ws'
import { MarkdownOutput } from '@/components/ui/markdown-output'
import { timeAgo } from '@/lib/utils'
import { getErrorMessage } from '@/lib/errors'

// ---- Filter tabs ----
type Filter = 'all' | 'unread' | 'flagged'

const FILTERS: { id: Filter; label: string }[] = [
  { id: 'all',     label: 'All'     },
  { id: 'unread',  label: 'Unread'  },
  { id: 'flagged', label: 'Flagged' },
]

// ---- PromptContinue — inline follow-up input for a memo ----
function PromptContinue({ memo }: { memo: Memo }) {
  const [text, setText] = useState('')
  const [sending, setSending] = useState(false)
  const [error, setError] = useState('')
  const [latestFollowUp, setLatestFollowUp] = useState<Task | null>(null)
  const pollRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  // Fetch the most-recent follow-up task for this memo's source task.
  const loadLatest = useCallback(async () => {
    if (!memo.task_id || !memo.project_id) return
    try {
      const all = await api.tasks.list(memo.project_id)
      const followUps = all
        .filter(t => t.follow_up_of === memo.task_id)
        .sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime())
      setLatestFollowUp(followUps[0] ?? null)
    } catch { /* ignore */ }
  }, [memo.task_id, memo.project_id])

  // Poll while latest follow-up is still in-flight.
  useEffect(() => {
    if (!latestFollowUp) return
    if (latestFollowUp.status === 'running' || latestFollowUp.status === 'queued' || latestFollowUp.status === 'pending') {
      pollRef.current = setTimeout(loadLatest, 2500)
    }
    return () => { if (pollRef.current) clearTimeout(pollRef.current) }
  }, [latestFollowUp, loadLatest])

  // Refresh when a WS task-status event arrives.
  useEffect(() => {
    return phoenixWS.on(ev => {
      if (ev.type === 'task.status_changed') loadLatest()
    })
  }, [loadLatest])

  // Load on mount.
  useEffect(() => {
    const timer = window.setTimeout(() => {
      void loadLatest()
    }, 0)
    return () => window.clearTimeout(timer)
  }, [loadLatest])

  const send = async () => {
    if (!text.trim() || !memo.task_id) return
    setError('')
    setSending(true)
    try {
      await api.tasks.followUp(memo.task_id, text.trim(), memo.agent_id || undefined)
      setText('')
      await loadLatest()
    } catch (error: unknown) {
      setError(getErrorMessage(error, 'Failed to send'))
    } finally {
      setSending(false)
    }
  }

  // No task link → no prompt-continue possible.
  if (!memo.task_id) return null

  const inFlight = latestFollowUp &&
    (latestFollowUp.status === 'running' || latestFollowUp.status === 'queued' || latestFollowUp.status === 'pending')

  return (
    <div className="mt-4 border-t border-slate-800 pt-3 space-y-2">
      <p className="text-xs font-medium text-slate-500 uppercase tracking-wide">Continue with an agent</p>

      {/* Latest follow-up status bubble */}
      {latestFollowUp && (
        <div className={`rounded-xl border px-3 py-2 text-xs ${
          inFlight
            ? 'bg-slate-800 border-slate-700 text-slate-400'
            : latestFollowUp.status === 'completed'
              ? 'bg-emerald-950/30 border-emerald-800/40 text-emerald-300'
              : 'bg-red-950/30 border-red-800/40 text-red-400'
        }`}>
          {inFlight ? (
            <span className="flex items-center gap-1.5">
              <span className="w-1.5 h-1.5 rounded-full bg-violet-400 animate-pulse inline-block" />
              {latestFollowUp.status === 'running' ? 'Agent is working…' : 'Queued…'}
            </span>
          ) : latestFollowUp.status === 'completed' ? (
            <span>✓ Completed · <a
              href={`/projects/${latestFollowUp.project_id}`}
              className="underline hover:text-emerald-200"
            >View in project ↗</a></span>
          ) : (
            <span>✕ Follow-up failed</span>
          )}
        </div>
      )}

      {/* Input */}
      {!inFlight && (
        <div className="space-y-1">
          <div className="flex gap-2 items-end bg-slate-800 border border-slate-700 rounded-xl px-3 py-2 focus-within:border-violet-600 transition-colors">
            <textarea
              value={text}
              onChange={e => setText(e.target.value)}
              onKeyDown={e => {
                if (e.key === 'Enter' && !e.shiftKey) { e.preventDefault(); send() }
              }}
              placeholder="Ask the agent to refine or continue this memo… (Enter to send)"
              disabled={sending}
              rows={2}
              className="flex-1 bg-transparent text-sm text-slate-200 placeholder-slate-500 outline-none resize-none"
            />
            <button
              onClick={send}
              disabled={sending || !text.trim()}
              className="flex-shrink-0 bg-violet-600 hover:bg-violet-500 disabled:opacity-40 disabled:cursor-not-allowed text-white rounded-lg text-xs px-3 py-1.5 transition-colors font-medium"
            >
              {sending ? '…' : '↵ Send'}
            </button>
          </div>
          {error && <p className="text-xs text-red-400">{error}</p>}
          <p className="text-xs text-slate-600">Shift+Enter for a new line · Enter to send</p>
        </div>
      )}
    </div>
  )
}

// ---- Inline markdown file viewer for artifact memos ----
function MdFileViewer({ path }: { path: string }) {
  const [open, setOpen] = useState(false)
  const [content, setContent] = useState<string | null>(null)
  const [truncated, setTruncated] = useState(false)
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')

  const load = async () => {
    if (content !== null) { setOpen(true); return }
    setLoading(true)
    setError('')
    try {
      const result = await api.memos.getFileContent(path)
      setContent(result.content)
      setTruncated(result.truncated)
      setOpen(true)
    } catch (err: unknown) {
      setError(getErrorMessage(err, 'Could not read file'))
    } finally {
      setLoading(false)
    }
  }

  const filename = path.split('/').pop() ?? path

  return (
    <div className="mt-3">
      <button
        onClick={() => open ? setOpen(false) : load()}
        disabled={loading}
        className="inline-flex items-center gap-1.5 text-xs text-violet-400 hover:text-violet-300 transition-colors disabled:opacity-50"
      >
        <span>{open ? '▾' : '▸'}</span>
        <span className="font-mono">{filename}</span>
        {loading && <span className="text-slate-500">Loading…</span>}
      </button>
      {error && <p className="text-xs text-red-400 mt-1">{error}</p>}
      {open && content !== null && (
        <div className="mt-2 border border-slate-700 rounded-lg bg-slate-950 overflow-auto max-h-[32rem]">
          <div className="px-4 py-3">
            <MarkdownOutput content={content} />
          </div>
          {truncated && (
            <p className="text-xs text-slate-500 px-4 py-2 border-t border-slate-800">
              File truncated at 512 KB — open in an editor to view the full file.
            </p>
          )}
        </div>
      )}
    </div>
  )
}

// ---- Single memo row ----
function MemoRow({ memo, onAction }: {
  memo: Memo
  onAction: () => void
}) {
  const [expanded, setExpanded] = useState(false)
  const [busy, setBusy] = useState(false)

  const act = async (fn: () => Promise<unknown>) => {
    setBusy(true)
    try { await fn() } finally { setBusy(false); onAction() }
  }

  const markRead   = () => act(() => api.memos.updateStatus(memo.id, 'read'))
  const markUnread = () => act(() => api.memos.updateStatus(memo.id, 'unread'))
  const flag       = () => act(() => api.memos.updateStatus(memo.id, 'flagged'))
  const unflag     = () => act(() => api.memos.updateStatus(memo.id, 'read'))
  const archive    = () => act(() => api.memos.updateStatus(memo.id, 'archived'))
  const remove     = () => act(() => api.memos.delete(memo.id))

  const isUnread  = memo.status === 'unread'
  const isFlagged = memo.status === 'flagged'
  const isHigh    = memo.priority === 'high'

  const handleToggle = () => {
    const opening = !expanded
    setExpanded(opening)
    if (opening && isUnread) markRead()
  }

  return (
    <div className={`border-b border-slate-800 last:border-b-0 transition-colors ${
      expanded ? 'bg-slate-900/60' : 'hover:bg-slate-900/40'
    }`}>
      {/* Collapsed row — single dense line */}
      <button
        className="w-full flex items-center gap-3 px-4 py-2.5 text-left cursor-pointer"
        onClick={handleToggle}
      >
        {/* Status dot / priority indicator */}
        <span className="shrink-0 w-4 flex items-center justify-center">
          {isHigh
            ? <span className="text-amber-400 text-xs" title="High priority">▲</span>
            : isFlagged
              ? <span className="text-violet-400 text-xs" title="Flagged">⚑</span>
              : isUnread
                ? <span className="w-1.5 h-1.5 rounded-full bg-violet-500 inline-block" title="Unread" />
                : <span className="w-1.5 h-1.5 rounded-full bg-slate-700 inline-block" title="Read" />
          }
        </span>

        {/* Title */}
        <span className={`flex-1 min-w-0 text-sm truncate ${
          isUnread || isFlagged ? 'text-white font-medium' : 'text-slate-400'
        }`}>
          {memo.title}
        </span>

        {/* Meta — project · agent · time */}
        <span className="shrink-0 hidden sm:flex items-center gap-2 text-xs text-slate-600">
          {memo.project_name && (
            <span className="truncate max-w-[140px]">{memo.project_name}</span>
          )}
          <span>·</span>
          <span>{timeAgo(memo.created_at)}</span>
        </span>

        {/* Chevron */}
        <span className="shrink-0 text-slate-600 text-xs ml-1">
          {expanded ? '▲' : '▼'}
        </span>
      </button>

      {/* Expanded body */}
      {expanded && (
        <div className="px-4 pb-4">
          {/* Secondary meta row */}
          <div className="flex flex-wrap items-center gap-x-3 gap-y-0.5 mb-3 text-xs text-slate-500">
            {memo.project_name && <span>⊞ {memo.project_name}</span>}
            <span>{memo.agent_name}</span>
            <span>·</span>
            <span>{timeAgo(memo.created_at)}</span>
            {isHigh && (
              <span className="text-amber-400 font-medium">▲ High priority</span>
            )}
          </div>

          {/* Body */}
          <div className="border-l-2 border-slate-700 pl-4">
            <MarkdownOutput content={memo.body} />
          </div>

          {/* Inline .md artifact viewer */}
          {memo.artifact_path && (
            <div className="border-l-2 border-violet-800/50 pl-4 mt-3">
              <p className="text-xs text-slate-500 mb-1">📄 Markdown file</p>
              <MdFileViewer path={memo.artifact_path} />
            </div>
          )}

          {/* Actions */}
          <div className="flex flex-wrap gap-2 mt-4">
            {isUnread && (
              <ActionBtn onClick={markRead} disabled={busy}>Mark read</ActionBtn>
            )}
            {!isUnread && !isFlagged && (
              <ActionBtn onClick={markUnread} disabled={busy}>Mark unread</ActionBtn>
            )}
            {isFlagged
              ? <ActionBtn onClick={unflag} disabled={busy}>Unflag</ActionBtn>
              : <ActionBtn onClick={flag} disabled={busy} accent="violet">⚑ Flag</ActionBtn>
            }
            <ActionBtn onClick={archive} disabled={busy}>Archive</ActionBtn>
            <ActionBtn onClick={remove} disabled={busy} accent="red">Delete</ActionBtn>
          </div>

          {/* Prompt-continue — refine the memo with an agent */}
          <PromptContinue memo={memo} />
        </div>
      )}
    </div>
  )
}

function ActionBtn({ onClick, disabled, accent, children }: {
  onClick: () => void
  disabled?: boolean
  accent?: 'violet' | 'red'
  children: React.ReactNode
}) {
  const cls = accent === 'red'
    ? 'border-red-700/40 text-red-400 hover:bg-red-900/20'
    : accent === 'violet'
      ? 'border-violet-600/40 text-violet-400 hover:bg-violet-900/20'
      : 'border-slate-700 text-slate-400 hover:text-white hover:border-slate-600'
  return (
    <button
      onClick={onClick}
      disabled={disabled}
      className={`px-3 py-1 text-xs rounded-lg border transition-colors disabled:opacity-40 ${cls}`}
    >
      {children}
    </button>
  )
}

// ---- Page ----

export function BriefingPage() {
  const [memos, setMemos] = useState<Memo[]>([])
  const [filter, setFilter] = useState<Filter>('all')
  const [loading, setLoading] = useState(true)

  const load = useCallback(async () => {
    try {
      const status = filter === 'all' ? undefined : filter
      const data = await api.memos.list(status)
      setMemos(data)
    } finally {
      setLoading(false)
    }
  }, [filter])

  useEffect(() => {
    const timer = window.setTimeout(() => {
      setLoading(true)
      void load()
    }, 0)
    return () => window.clearTimeout(timer)
  }, [load])

  useEffect(() => {
    return phoenixWS.on(ev => {
      if (ev.type === 'memo.created') load()
    })
  }, [load])

  const unreadCount  = memos.filter(m => m.status === 'unread').length
  const flaggedCount = memos.filter(m => m.status === 'flagged').length

  const archiveAll = async () => {
    if (!confirm('Archive all visible memos?')) return
    await Promise.all(memos.map(m => api.memos.updateStatus(m.id, 'archived')))
    load()
  }

  return (
    <div className="space-y-5 max-w-3xl">
      {/* Header */}
      <div className="flex items-start justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold text-white">Briefing</h1>
          <p className="text-slate-400 text-sm mt-1">
            Key findings and actions surfaced by your agents.
          </p>
        </div>
        {memos.length > 0 && (
          <button
            onClick={archiveAll}
            className="text-xs text-slate-500 hover:text-slate-300 transition-colors mt-1 shrink-0"
          >
            Archive all
          </button>
        )}
      </div>

      {/* Summary badges */}
      {(unreadCount > 0 || flaggedCount > 0) && (
        <div className="flex gap-3">
          {unreadCount > 0 && (
            <span className="inline-flex items-center gap-1.5 bg-violet-900/30 border border-violet-700/40 text-violet-300 text-xs font-medium px-3 py-1 rounded-full">
              <span className="w-1.5 h-1.5 rounded-full bg-violet-400 inline-block" />
              {unreadCount} unread
            </span>
          )}
          {flaggedCount > 0 && (
            <span className="inline-flex items-center gap-1.5 bg-amber-900/20 border border-amber-700/40 text-amber-300 text-xs font-medium px-3 py-1 rounded-full">
              ⚑ {flaggedCount} flagged
            </span>
          )}
        </div>
      )}

      {/* Filter tabs */}
      <div className="flex gap-1 border-b border-slate-800">
        {FILTERS.map(f => (
          <button
            key={f.id}
            onClick={() => setFilter(f.id)}
            className={`px-4 py-2 text-sm font-medium rounded-t-lg transition-colors -mb-px border-b-2 ${
              filter === f.id
                ? 'text-violet-300 border-violet-500 bg-violet-900/10'
                : 'text-slate-400 border-transparent hover:text-white hover:border-slate-600'
            }`}
          >
            {f.label}
          </button>
        ))}
      </div>

      {/* Content */}
      {loading ? (
        <div className="text-slate-500 text-sm">Loading…</div>
      ) : memos.length === 0 ? (
        <div className="text-center py-16">
          <div className="text-4xl mb-3">📋</div>
          <p className="text-slate-400 font-medium">
            {filter === 'all' ? 'No memos yet' : `No ${filter} memos`}
          </p>
          <p className="text-slate-600 text-sm mt-1 max-w-sm mx-auto">
            {filter === 'all'
              ? "When an agent completes work with important findings, they'll post a memo here. You can also pin notes manually from any completed task."
              : `Switch to "All" to see other memos.`}
          </p>
        </div>
      ) : (
        <div className="bg-slate-900 border border-slate-800 rounded-xl overflow-hidden">
          {memos.map(m => (
            <MemoRow key={m.id} memo={m} onAction={load} />
          ))}
        </div>
      )}
    </div>
  )
}
