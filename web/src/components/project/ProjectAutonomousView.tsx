import { useState, useEffect } from 'react'
import { api } from '../../lib/api'
import type { Task, Agent, Project } from '../../lib/api'
import { MarkdownOutput } from '../ui/markdown-output'

interface Props {
  project: Project
  tasks: Task[]
  agents: Agent[]
  onUpdate: () => void
  onTaskClick: (task: Task) => void
  onNewTask: () => void
}

function relativeTime(dateStr: string | null): string {
  if (!dateStr) return ''
  const diff = Date.now() - new Date(dateStr).getTime()
  const m = Math.floor(diff / 60000)
  if (m < 1) return 'just now'
  if (m < 60) return `${m}m ago`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h}h ago`
  return `${Math.floor(h / 24)}d ago`
}

function parseOutputText(output: string): string {
  if (!output || output === '{}') return ''
  try {
    const parsed = JSON.parse(output)
    if (parsed.text) return parsed.text
    if (parsed.error) return `Error: ${parsed.error}`
    return ''
  } catch {
    return output
  }
}

function nextScheduledCountdown(tasks: Task[], project: Project): string | null {
  if (!project.schedule_interval) return null
  const scheduledTasks = tasks
    .filter(t => t.title.startsWith('Scheduled run') && t.status === 'completed')
    .sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime())
  if (!scheduledTasks.length) return null

  const last = new Date(scheduledTasks[0].created_at).getTime()
  const nextFire = last + project.schedule_interval * 1000
  const remaining = nextFire - Date.now()
  if (remaining <= 0) return 'firing soon'
  const m = Math.floor(remaining / 60000)
  const s = Math.floor((remaining % 60000) / 1000)
  return m > 0 ? `${m}m ${s}s` : `${s}s`
}

export function ProjectAutonomousView({ project, tasks, agents, onUpdate, onTaskClick, onNewTask }: Props) {
  const agentNames = agents.map(a => a.name).join(', ')

  const [dismissingId, setDismissingId] = useState<string | null>(null)
  const [countdown, setCountdown] = useState<string | null>(
    nextScheduledCountdown(tasks, project)
  )

  // Refresh countdown every 10s and auto-refresh data every 30s
  useEffect(() => {
    const tick = setInterval(() => {
      setCountdown(nextScheduledCountdown(tasks, project))
    }, 10_000)
    const dataRefresh = setInterval(onUpdate, 30_000)
    return () => { clearInterval(tick); clearInterval(dataRefresh) }
  }, [project, tasks, onUpdate])

  // Stats
  const isScheduled = (t: Task) => t.title.startsWith('Scheduled run')
  const done = tasks.filter(t => t.status === 'completed' && !isScheduled(t) && !t.dismissed).length
  const active = tasks.filter(t => (t.status === 'running' || t.status === 'queued') && !t.dismissed).length
  const stuck = tasks.filter(t => t.status === 'failed' && !t.dismissed).length
  const sessions24h = tasks.filter(t => {
    if (!isScheduled(t) || t.status !== 'completed') return false
    const age = Date.now() - new Date(t.created_at).getTime()
    return age < 24 * 60 * 60 * 1000
  }).length

  const total = done + active + stuck
  const progressPct = total > 0 ? Math.round((done / total) * 100) : 0

  // Stuck tasks (for attention panel)
  const stuckTasks = tasks.filter(t => t.status === 'failed' && !t.dismissed)

  // Last scheduled session
  const lastSession = tasks
    .filter(t => isScheduled(t) && t.status === 'completed')
    .sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime())[0]
  const lastSessionText = lastSession ? parseOutputText(lastSession.output) : null
  const lastSessionPreview = lastSessionText?.split('\n').slice(0, 3).join('\n') ?? ''

  // Activity feed — last 10 non-running tasks
  const activityFeed = [...tasks]
    .filter(t => !t.dismissed && t.status !== 'queued')
    .sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime())
    .slice(0, 10)

  async function handleDismiss(taskId: string) {
    setDismissingId(taskId)
    try {
      await api.tasks.dismiss(taskId)
      onUpdate()
    } finally {
      setDismissingId(null)
    }
  }

  return (
    <div className="space-y-5">
      {/* Header */}
      <div className="flex items-start justify-between">
        <div>
          <h1 className="text-2xl font-bold text-stone-900">{project.name}</h1>
          <p className="text-sm text-stone-400 mt-1">{agentNames}</p>
        </div>
        <div className="text-right flex flex-col items-end gap-1">
          <span className="bg-green-50 border border-green-200 text-green-700 text-xs rounded-full px-3 py-1 font-medium">
            ● live
          </span>
          {countdown && (
            <span className="text-xs text-stone-400">next in {countdown}</span>
          )}
          <button
            onClick={onNewTask}
            className="text-xs text-stone-400 hover:text-indigo-600 transition-colors mt-1"
          >
            + manual task
          </button>
        </div>
      </div>

      {/* Progress bar */}
      <div className="bg-stone-200 rounded-full h-1.5 overflow-hidden">
        <div
          className="h-full bg-green-500 rounded-full transition-all duration-500"
          style={{ width: `${progressPct}%` }}
        />
      </div>

      {/* Stats row */}
      <div className="grid grid-cols-4 gap-3">
        <div className="bg-white border border-stone-200 rounded-xl p-4 text-center shadow-sm">
          <div className="text-2xl font-bold text-green-600">{done}</div>
          <div className="text-xs text-stone-400 uppercase tracking-wide mt-1">Done</div>
        </div>
        <div className="bg-white border border-stone-200 rounded-xl p-4 text-center shadow-sm">
          <div className="text-2xl font-bold text-orange-500">{active}</div>
          <div className="text-xs text-stone-400 uppercase tracking-wide mt-1">Active</div>
        </div>
        <div className="bg-white border border-stone-200 rounded-xl p-4 text-center shadow-sm">
          <div className={`text-2xl font-bold ${stuck > 0 ? 'text-red-600' : 'text-stone-300'}`}>{stuck}</div>
          <div className="text-xs text-stone-400 uppercase tracking-wide mt-1">Stuck</div>
        </div>
        <div className="bg-white border border-stone-200 rounded-xl p-4 text-center shadow-sm">
          <div className="text-2xl font-bold text-indigo-500">{sessions24h}</div>
          <div className="text-xs text-stone-400 uppercase tracking-wide mt-1">Sessions (24h)</div>
        </div>
      </div>

      {/* Needs attention panel */}
      {stuckTasks.length > 0 && (
        <div className="bg-red-50 border border-red-200 rounded-xl p-4 space-y-2">
          <div className="text-xs font-semibold text-red-600 uppercase tracking-wide mb-3">
            ⚠ Needs your attention
          </div>
          {stuckTasks.map(t => (
            <div key={t.id} className="flex items-start justify-between gap-3">
              <div>
                <button
                  className="text-sm text-stone-800 hover:text-indigo-700 text-left font-medium"
                  onClick={() => onTaskClick(t)}
                >
                  {t.title}
                </button>
                <div className="text-xs text-stone-500 mt-0.5">{relativeTime(t.created_at)}</div>
              </div>
              <button
                onClick={() => handleDismiss(t.id)}
                disabled={dismissingId === t.id}
                className="text-xs text-stone-400 hover:text-stone-600 shrink-0 disabled:opacity-40"
              >
                {dismissingId === t.id ? '...' : 'Dismiss'}
              </button>
            </div>
          ))}
        </div>
      )}

      {/* Two-column body */}
      <div className="grid grid-cols-2 gap-4">
        {/* Last session */}
        <div>
          <div className="text-xs font-semibold text-stone-400 uppercase tracking-wide mb-2">Last session</div>
          {lastSession ? (
            <div className="bg-white border border-stone-200 rounded-xl p-4 shadow-sm">
              <div className="text-xs text-stone-400 mb-2">
                {lastSession.title} · {relativeTime(lastSession.created_at)}
              </div>
              {lastSessionPreview ? (
                <>
                  <div className="line-clamp-6 overflow-hidden">
                    <MarkdownOutput content={lastSessionPreview} compact />
                  </div>
                  <button
                    onClick={() => onTaskClick(lastSession)}
                    className="text-xs text-indigo-600 hover:text-indigo-800 mt-2 block"
                  >
                    View full output →
                  </button>
                </>
              ) : (
                <p className="text-sm text-stone-400 italic">No output recorded</p>
              )}
            </div>
          ) : (
            <div className="bg-white border border-stone-200 rounded-xl p-4 shadow-sm text-sm text-stone-400 italic">
              No sessions yet — first scheduled run will fire soon.
            </div>
          )}
        </div>

        {/* Activity feed */}
        <div>
          <div className="text-xs font-semibold text-stone-400 uppercase tracking-wide mb-2">Activity</div>
          <div className="space-y-2">
            {activityFeed.length === 0 ? (
              <div className="bg-white border border-stone-200 rounded-xl p-4 text-sm text-stone-400 italic shadow-sm">
                No activity yet
              </div>
            ) : (
              activityFeed.map(t => (
                <button
                  key={t.id}
                  onClick={() => onTaskClick(t)}
                  className="w-full bg-white border border-stone-200 rounded-xl px-4 py-3 shadow-sm hover:shadow-md transition-shadow text-left"
                >
                  <div className="flex items-center gap-2">
                    <span className={
                      t.status === 'completed' ? 'text-green-500' :
                      t.status === 'failed' ? 'text-red-500' :
                      t.status === 'running' ? 'text-violet-500' : 'text-stone-400'
                    }>
                      {t.status === 'completed' ? '✓' : t.status === 'failed' ? '✗' : t.status === 'running' ? '●' : '○'}
                    </span>
                    <span className="text-sm text-stone-700 truncate flex-1">{t.title}</span>
                    <span className="text-xs text-stone-400 shrink-0">{relativeTime(t.created_at)}</span>
                  </div>
                </button>
              ))
            )}
          </div>
        </div>
      </div>
    </div>
  )
}
