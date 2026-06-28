import { useState, useEffect, useCallback, useRef } from 'react'
import { useParams, Link, useNavigate } from 'react-router-dom'
import { api, type Project, type Agent, type Task, type Provider, type ProjectSpend } from '@/lib/api'
import { phoenixWS } from '@/lib/ws'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Modal } from '@/components/ui/modal'
import { Input, Textarea, Label, Select } from '@/components/ui/input'
import { EmptyState } from '@/components/ui/empty'
import { MarkdownOutput } from '@/components/ui/markdown-output'
import { taskStatusVariant, taskStatusLabel, parseOutput, formatCost, timeAgo } from '@/lib/utils'
import { AgentsSection } from '@/components/shared/AgentsSection'
import {
  ScheduleEditor,
} from '@/components/monitor/ScheduleEditor'
import {
  scheduleFromProject,
  schedulePayload,
  scheduleError,
  scheduleSummary,
  type ScheduleValue,
} from '@/components/monitor/schedule'
import { cn } from '@/lib/utils'
import { getErrorMessage } from '@/lib/errors'

// ---- Countdown clock ----

// nextDailyRun returns the timestamp (ms) of the next upcoming daily time given
// sorted "HH:MM" strings, or null if none are configured.
function nextDailyRun(times: string[], now: Date): number | null {
  const valid = times
    .map(t => /^(\d{2}):(\d{2})$/.exec(t.trim()))
    .filter((m): m is RegExpExecArray => m !== null)
    .map(m => ({ h: Number(m[1]), m: Number(m[2]) }))
  if (valid.length === 0) return null
  for (const { h, m } of valid) {
    const cand = new Date(now)
    cand.setHours(h, m, 0, 0)
    if (cand.getTime() > now.getTime()) return cand.getTime()
  }
  // All times today have passed; first time tomorrow.
  const first = valid[0]
  const tomorrow = new Date(now)
  tomorrow.setDate(tomorrow.getDate() + 1)
  tomorrow.setHours(first.h, first.m, 0, 0)
  return tomorrow.getTime()
}

function Countdown({ monitor, tasks }: { monitor: Project; tasks: Task[] }) {
  const [remaining, setRemaining] = useState<number | null>(null)

  useEffect(() => {
    const isDaily = monitor.schedule_kind === 'daily'
    const intervalMs = (monitor.schedule_interval ?? 0) * 1000

    const calc = () => {
      if (isDaily) {
        const next = nextDailyRun(monitor.schedule_times ?? [], new Date())
        setRemaining(next === null ? null : Math.max(0, next - Date.now()))
        return
      }
      const scheduled = tasks
        .filter(t => t.title.startsWith('Scheduled run'))
        .sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime())
      if (!scheduled.length) { setRemaining(null); return }
      const last = new Date(scheduled[0].created_at).getTime()
      const next = last + intervalMs
      setRemaining(Math.max(0, next - Date.now()))
    }

    calc()
    const timer = setInterval(calc, 1000)
    return () => clearInterval(timer)
  }, [monitor, tasks])

  if (remaining === null) return <span className="text-slate-500 text-sm">No runs yet</span>
  if (remaining === 0) return <span className="text-violet-400 text-sm animate-pulse">Firing soon…</span>

  const totalSecs = Math.floor(remaining / 1000)
  const days = Math.floor(totalSecs / 86400)
  const hrs = Math.floor((totalSecs % 86400) / 3600)
  const m = Math.floor((totalSecs % 3600) / 60)
  const s = totalSecs % 60
  let display: string
  if (days > 0) display = `${days}d ${hrs}h`
  else if (hrs > 0) display = `${hrs}h ${m}m`
  else if (m > 0) display = `${m}m ${s}s`
  else display = `${s}s`

  return (
    <span className="text-slate-300 text-sm font-mono">Next run in {display}</span>
  )
}

function ElapsedTimer({ startedAt }: { startedAt: string }) {
  const [elapsed, setElapsed] = useState(0)
  useEffect(() => {
    const origin = new Date(startedAt).getTime()
    const tick = () => setElapsed(Math.floor((Date.now() - origin) / 1000))
    tick()
    const id = setInterval(tick, 1000)
    return () => clearInterval(id)
  }, [startedAt])
  const m = Math.floor(elapsed / 60)
  const s = elapsed % 60
  return <span className="text-xs text-violet-400 font-mono tabular-nums">{m > 0 ? `${m}m ${s}s` : `${s}s`}</span>
}

// ---- Run card ----

const STATUS_BORDER: Record<string, string> = {
  success: 'border-l-emerald-500',
  warning: 'border-l-amber-500',
  danger: 'border-l-red-500',
  info: 'border-l-violet-500',
  muted: 'border-l-slate-700',
  default: 'border-l-slate-700',
}

function RunCard({ task, agent }: { task: Task; agent?: Agent }) {
  const [expanded, setExpanded] = useState(task.status === 'running')
  const [stream, setStream] = useState('')
  const streamRef = useRef<HTMLDivElement>(null)
  const variant = taskStatusVariant(task.status)

  useEffect(() => {
    if (task.status !== 'running') return
    return phoenixWS.on((ev) => {
      if (ev.type === 'task.output_stream') {
        const p = ev.payload
        if (p.task_id === task.id) {
          setStream(prev => prev + p.chunk)
          setTimeout(() => {
            const el = streamRef.current
            if (el) el.scrollTop = el.scrollHeight
          }, 0)
        }
      }
    })
  }, [task.id, task.status])

  const output = stream || parseOutput(task.output)

  const duration = task.started_at && task.completed_at
    ? Math.round((new Date(task.completed_at).getTime() - new Date(task.started_at).getTime()) / 1000)
    : null

  return (
    <div className={cn(
      'bg-slate-900 border border-slate-800 border-l-2 rounded-xl overflow-hidden',
      STATUS_BORDER[variant] ?? 'border-l-slate-700'
    )}>
      <div className="flex items-center gap-4 px-4 py-3">
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-3 flex-wrap">
            {task.health_signal && (
              <span className={
                task.health_signal === 'all_clear' ? 'text-xs font-medium px-2 py-0.5 rounded-full bg-emerald-900/40 text-emerald-400 border border-emerald-800' :
                task.health_signal === 'needs_attention' ? 'text-xs font-medium px-2 py-0.5 rounded-full bg-amber-900/40 text-amber-400 border border-amber-800' :
                'text-xs font-medium px-2 py-0.5 rounded-full bg-red-900/40 text-red-400 border border-red-800'
              }>
                {task.health_signal === 'all_clear' ? '✓ All clear' :
                 task.health_signal === 'needs_attention' ? '⚠ Needs attention' :
                 '✗ Failed'}
              </span>
            )}
            <Badge variant={variant}>{taskStatusLabel(task.status)}</Badge>
            <span className="text-sm text-slate-300">{task.title}</span>
          </div>
          <div className="flex items-center gap-3 mt-1 text-xs text-slate-500">
            <span>{timeAgo(task.created_at)}</span>
            {task.status === 'running' && task.started_at && (
              <><span className="w-1.5 h-1.5 rounded-full bg-violet-500 animate-pulse" /><ElapsedTimer startedAt={task.started_at} /></>
            )}
            {duration !== null && <span>{duration}s</span>}
            {task.cost_usd > 0 && <span>{formatCost(task.cost_usd)}</span>}
            {agent && <span>{agent.name}</span>}
          </div>
        </div>
        {(output || task.status !== 'running') && (
          <button
            onClick={() => setExpanded(e => !e)}
            className="text-xs text-slate-500 hover:text-slate-300 transition-colors shrink-0"
          >
            {expanded ? 'Hide ▲' : 'Show output ▼'}
          </button>
        )}
      </div>
      {/* Running: always show live content below header */}
      {task.status === 'running' && (
        <div className="border-t border-slate-800 px-4 py-3 bg-slate-950">
          {stream ? (
            <div ref={streamRef} className="max-h-64 overflow-y-auto">
              <MarkdownOutput content={stream} />
            </div>
          ) : task.description ? (
            <p className="text-xs text-slate-400 leading-relaxed">{task.description}</p>
          ) : (
            <span className="text-xs text-violet-400 animate-pulse">Waiting for first response…</span>
          )}
        </div>
      )}
      {/* Non-running: expand/collapse output */}
      {task.status !== 'running' && expanded && output && (
        <div ref={streamRef} className="border-t border-slate-800 px-4 py-3 bg-slate-950 max-h-96 overflow-y-auto">
          <MarkdownOutput content={output} />
        </div>
      )}
    </div>
  )
}

// ---- Page ----

export function MonitorDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const [monitor, setMonitor] = useState<Project | null>(null)
  const [spend, setSpend] = useState<ProjectSpend | null>(null)
  const [agents, setAgents] = useState<Agent[]>([])
  const [allAgents, setAllAgents] = useState<Agent[]>([])
  const [tasks, setTasks] = useState<Task[]>([])
  const [loading, setLoading] = useState(true)
  const [linkedProject, setLinkedProject] = useState<Project | null>(null)
  const [linkedTasks, setLinkedTasks] = useState<Task[]>([])
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [triggering, setTriggering] = useState(false)
  const [triggerError, setTriggerError] = useState('')
  const [showRunModal, setShowRunModal] = useState(false)
  const [runExtraPrompt, setRunExtraPrompt] = useState('')
  const [runAiExpanding, setRunAiExpanding] = useState(false)
  const [runAiError, setRunAiError] = useState('')
  const [runShowAI, setRunShowAI] = useState(false)
  const [runAiHint, setRunAiHint] = useState('')
  const [runAiProviderID, setRunAiProviderID] = useState('')
  const [showEdit, setShowEdit] = useState(false)
  const [editName, setEditName] = useState('')
  const [editDesc, setEditDesc] = useState('')
  const [editSchedule, setEditSchedule] = useState<ScheduleValue>({ kind: 'interval', intervalSeconds: 0, times: [], catchUp: false })
  const [editMonitorModel, setEditMonitorModel] = useState('')
  const [editWorkingDir, setEditWorkingDir] = useState('')
  const [editBudgetUSD, setEditBudgetUSD] = useState('')
  const [editBudgetPeriod, setEditBudgetPeriod] = useState<'day'|'week'|'month'|'total'>('total')
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState('')
  const [providers, setProviders] = useState<Provider[]>([])
  const [showAI, setShowAI] = useState(false)
  const [aiHint, setAiHint] = useState('')
  const [aiProviderID, setAiProviderID] = useState('')
  const [aiGenerating, setAiGenerating] = useState(false)
  const [aiError, setAiError] = useState('')

  const load = useCallback(async () => {
    if (!id) return
    try {
      const [proj, agts, allAgts, tsks, provs] = await Promise.all([
        api.projects.get(id),
        api.projects.listAgents(id),
        api.agents.list(),
        api.tasks.list(id),
        api.providers.list(),
      ])
      setMonitor(proj)
      setAgents(agts)
      setAllAgents(allAgts)
      setTasks(tsks)
      setProviders(provs)
      // Load spend separately — non-fatal if it fails
      if (proj?.budget_usd > 0) {
        api.projects.getSpend(id).then(setSpend).catch(() => {})
      }
      // Load linked project + its active remediation tasks
      if (proj?.linked_project_id) {
        const [lp, lts] = await Promise.all([
          api.projects.get(proj.linked_project_id).catch(() => null),
          api.tasks.list(proj.linked_project_id).catch(() => [] as Task[]),
        ])
        setLinkedProject(lp)
        setLinkedTasks(lts.filter(t =>
          t.source.startsWith('heartbeat:') &&
          (t.status === 'running' || t.status === 'queued')
        ))
      } else {
        setLinkedProject(null)
        setLinkedTasks([])
      }
      setAiProviderID(p => p || provs.find(p => p.type === 'llm')?.id || provs[0]?.id || '')
      setRunAiProviderID(p => p || provs.find(p => p.type === 'llm')?.id || provs[0]?.id || '')
    } finally {
      setLoading(false)
    }
  }, [id])

  useEffect(() => {
    const timer = window.setTimeout(() => {
      void load()
    }, 0)
    return () => window.clearTimeout(timer)
  }, [load])

  useEffect(() => {
    const unsub = phoenixWS.on((ev) => {
      if (ev.type === 'task.status_changed') load()
    })
    return unsub
  }, [load])

  const primaryAgent = agents[0] ?? null

  const hasSchedule = monitor
    ? (monitor.schedule_kind === 'daily'
        ? (monitor.schedule_times?.length ?? 0) > 0
        : !!monitor.schedule_interval)
    : false

  const runNow = async () => {
    if (!primaryAgent || !id) return
    setTriggering(true)
    setTriggerError('')
    try {
      await api.tasks.create({
        project_id: id,
        agent_id: primaryAgent.id,
        title: `Manual run — ${new Date().toLocaleString()}`,
        description: monitor?.description ?? '',
      })
      load()
    } catch (e: unknown) {
      setTriggerError(e instanceof Error ? e.message : 'Failed to trigger run')
    } finally {
      setTriggering(false)
    }
  }

  const openRunModal = () => {
    setRunExtraPrompt('')
    setRunAiHint('')
    setRunShowAI(false)
    setRunAiError('')
    setTriggerError('')
    setShowRunModal(true)
  }

  const expandWithAI = async () => {
    setRunAiExpanding(true)
    setRunAiError('')
    try {
      const title = `One-off run: ${monitor?.name ?? ''}`
      const result = await api.tasks.generateDescription(title, runAiHint, runAiProviderID)
      setRunExtraPrompt(result.description)
      setRunShowAI(false)
      setRunAiHint('')
    } catch (error: unknown) {
      setRunAiError(getErrorMessage(error, 'Generation failed'))
    } finally {
      setRunAiExpanding(false)
    }
  }

  const runWithExtraPrompt = async () => {
    if (!primaryAgent || !id) return
    setTriggering(true)
    setTriggerError('')
    try {
      const base = monitor?.description ?? ''
      const extra = runExtraPrompt.trim()
      const combined = extra
        ? base
          ? `${base}\n\n## Additional instructions for this run\n${extra}`
          : extra
        : base
      await api.tasks.create({
        project_id: id,
        agent_id: primaryAgent.id,
        title: `Manual run — ${new Date().toLocaleString()}`,
        description: combined,
        source: extra ? 'Human one-off with extra prompt' : undefined,
      })
      setShowRunModal(false)
      load()
    } catch (error: unknown) {
      setTriggerError(getErrorMessage(error, 'Failed to trigger run'))
    } finally {
      setTriggering(false)
    }
  }

  const openEdit = () => {
    if (!monitor) return
    setEditName(monitor.name)
    setEditDesc(monitor.description ?? '')
    setEditMonitorModel(monitor.monitor_model ?? '')
    setEditWorkingDir(monitor.working_dir ?? '')
    setEditBudgetUSD(monitor.budget_usd > 0 ? String(monitor.budget_usd) : '')
    setEditBudgetPeriod((monitor.budget_period as 'day'|'week'|'month'|'total') || 'total')
    setEditSchedule(scheduleFromProject(monitor))
    setSaveError('')
    setShowAI(false)
    setAiHint('')
    setAiError('')
    setShowEdit(true)
  }

  const saveEdit = async () => {
    if (!id || !monitor) return
    if (!editName.trim()) { setSaveError('Name is required'); return }
    const schedErr = scheduleError(editSchedule)
    if (schedErr) { setSaveError(schedErr); return }
    setSaving(true)
    setSaveError('')
    try {
      await api.projects.update(id, {
        name: editName.trim(),
        description: editDesc,
        working_dir: editWorkingDir.trim(),
        monitor_model: editMonitorModel.trim(),
        budget_usd: editBudgetUSD.trim() ? parseFloat(editBudgetUSD) : 0,
        budget_period: editBudgetPeriod,
        kind: 'monitor',
        ...schedulePayload(editSchedule),
      })
      setShowEdit(false)
      load()
    } catch (e: unknown) {
      setSaveError(e instanceof Error ? e.message : 'Failed to save')
    } finally {
      setSaving(false)
    }
  }

  const generateDescription = async () => {
    setAiGenerating(true)
    setAiError('')
    try {
      const result = await api.projects.generateDescription(editName || monitor?.name || '', aiHint, aiProviderID)
      setEditDesc(result.description)
      setShowAI(false)
      setAiHint('')
    } catch (e: unknown) {
      setAiError(e instanceof Error ? e.message : 'Generation failed')
    } finally {
      setAiGenerating(false)
    }
  }

  const deleteMonitor = async () => {
    if (!id) return
    setDeleting(true)
    try {
      await api.projects.delete(id)
      navigate('/monitors')
    } catch { setDeleting(false) }
  }

  const sortedTasks = [...tasks].sort(
    (a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime()
  )
  const agentById = Object.fromEntries(agents.map(a => [a.id, a]))

  if (loading) return <div className="text-slate-500 text-sm">Loading…</div>
  if (!monitor) return <div className="text-slate-500 text-sm">Monitor not found.</div>

  return (
    <div className="space-y-6">
      {/* Breadcrumb */}
      <div className="flex items-center gap-2 text-sm">
        <Link to="/monitors" className="text-slate-500 hover:text-white transition-colors">Monitors</Link>
        <span className="text-slate-700">/</span>
        <span className="text-white">{monitor.name}</span>
      </div>

      {/* Header */}
      <div className="bg-slate-900 border border-slate-800 rounded-xl p-5 space-y-4">
        <div className="flex items-start justify-between gap-4 flex-wrap">
          <div>
            <div className="flex items-center gap-3 mb-1">
              <span className="text-xl">⟳</span>
              <h1 className="text-xl font-bold text-white">{monitor.name}</h1>
              <Badge variant={monitor.status === 'active' ? 'success' : 'muted'}>{monitor.status}</Badge>
            </div>
            {monitor.description && (
              <p className="text-slate-400 text-sm ml-8">{monitor.description}</p>
            )}
          </div>
          <div className="flex gap-2 shrink-0">
            <div className="flex rounded-lg overflow-hidden border border-slate-700">
              <Button
                variant="secondary"
                size="sm"
                onClick={runNow}
                disabled={triggering || !primaryAgent}
                className="rounded-none border-0 border-r border-slate-700"
              >
                {triggering ? 'Triggering…' : '▶ Run now'}
              </Button>
              <button
                title="Run now with extra prompt…"
                onClick={openRunModal}
                disabled={triggering || !primaryAgent}
                className="px-2 py-1 bg-slate-800 hover:bg-slate-700 text-slate-400 hover:text-violet-300 transition-colors disabled:opacity-40 disabled:cursor-not-allowed text-xs"
              >
                ✦
              </button>
            </div>
            <Button variant="secondary" size="sm" onClick={openEdit}>Edit</Button>
            <Button variant="danger" size="sm" onClick={() => setShowDeleteConfirm(true)}>Delete</Button>
          </div>
        </div>

        {/* Schedule + agent info */}
        <div className="flex items-center gap-6 ml-8 flex-wrap">
          <div>
            <p className="text-xs text-slate-500 mb-0.5">Schedule</p>
            {hasSchedule
              ? <p className="text-sm text-violet-400 font-medium">⟳ {scheduleSummary(monitor)}</p>
              : <p className="text-sm text-slate-500">Manual only</p>
            }
          </div>
          {primaryAgent && (
            <div>
              <p className="text-xs text-slate-500 mb-0.5">Agent</p>
              <p className="text-sm text-slate-300">{primaryAgent.name}</p>
            </div>
          )}
          {monitor.working_dir && (
            <div>
              <p className="text-xs text-slate-500 mb-0.5">Working dir</p>
              <p className="text-xs text-slate-400 font-mono">{monitor.working_dir}</p>
            </div>
          )}
          {monitor.monitor_model && (
            <div>
              <p className="text-xs text-slate-500 mb-0.5">Monitor model</p>
              <p className="text-xs text-slate-400 font-mono">{monitor.monitor_model}</p>
            </div>
          )}
          {monitor.budget_usd > 0 && (
            <div>
              <p className="text-xs text-slate-500 mb-1">
                Budget ({monitor.budget_period})
              </p>
              {spend ? (() => {
                const pct = Math.min(100, (spend.spent_usd / spend.budget_usd) * 100)
                const exceeded = spend.spent_usd >= spend.budget_usd
                return (
                  <div className="space-y-1">
                    <div className="w-full bg-slate-800 rounded-full h-1.5">
                      <div
                        className={`h-1.5 rounded-full transition-all ${exceeded ? 'bg-red-500' : pct > 80 ? 'bg-amber-400' : 'bg-emerald-500'}`}
                        style={{ width: `${pct}%` }}
                      />
                    </div>
                    <p className={`text-xs ${exceeded ? 'text-red-400 font-medium' : 'text-slate-400'}`}>
                      {exceeded ? '⚠ ' : ''}{formatCost(spend.spent_usd)} / {formatCost(spend.budget_usd)}
                      {exceeded && ' — budget exceeded'}
                    </p>
                  </div>
                )
              })() : (
                <p className="text-xs text-slate-400">limit: {formatCost(monitor.budget_usd)}</p>
              )}
            </div>
          )}
          {hasSchedule && primaryAgent && (
            <div>
              <p className="text-xs text-slate-500 mb-0.5">Next run</p>
              <Countdown monitor={monitor} tasks={tasks} />
            </div>
          )}
        </div>

        {/* Heartbeat state */}
        {(monitor.heartbeat_last_signal || monitor.heartbeat_consecutive_bad > 0) && (
          <div className="ml-8 flex items-center gap-4 flex-wrap">
            <div>
              <p className="text-xs text-slate-500 mb-0.5">Last signal</p>
              <span className={cn(
                'text-xs font-medium px-2 py-0.5 rounded-full border',
                monitor.heartbeat_last_signal === 'all_clear'
                  ? 'bg-emerald-900/40 text-emerald-400 border-emerald-800'
                  : monitor.heartbeat_last_signal === 'needs_attention'
                  ? 'bg-amber-900/40 text-amber-400 border-amber-800'
                  : 'bg-red-900/40 text-red-400 border-red-800'
              )}>
                {monitor.heartbeat_last_signal === 'all_clear' ? '✓ All clear'
                  : monitor.heartbeat_last_signal === 'needs_attention' ? '⚠ Needs attention'
                  : monitor.heartbeat_last_signal === 'failed' ? '✗ Failed'
                  : monitor.heartbeat_last_signal}
              </span>
            </div>
            {monitor.heartbeat_consecutive_bad > 0 && (
              <div>
                <p className="text-xs text-slate-500 mb-0.5">Consecutive bad</p>
                <span className={cn(
                  'text-xs font-medium px-2 py-0.5 rounded-full border',
                  monitor.heartbeat_escalate_after > 0 &&
                  monitor.heartbeat_consecutive_bad >= monitor.heartbeat_escalate_after
                    ? 'bg-red-900/40 text-red-400 border-red-800'
                    : 'bg-amber-900/40 text-amber-400 border-amber-800'
                )}>
                  {monitor.heartbeat_consecutive_bad}
                  {monitor.heartbeat_escalate_after > 0 && ` / ${monitor.heartbeat_escalate_after}`}
                </span>
              </div>
            )}
            {(monitor.heartbeat_on_attention || monitor.heartbeat_on_failed) && (
              <div>
                <p className="text-xs text-slate-500 mb-0.5">Reactions</p>
                <div className="flex gap-1 flex-wrap">
                  {monitor.heartbeat_on_attention && (
                    <span className="text-xs text-slate-400 bg-slate-800 px-2 py-0.5 rounded border border-slate-700">
                      attention → {monitor.heartbeat_on_attention}
                    </span>
                  )}
                  {monitor.heartbeat_on_failed && (
                    <span className="text-xs text-slate-400 bg-slate-800 px-2 py-0.5 rounded border border-slate-700">
                      failed → {monitor.heartbeat_on_failed}
                    </span>
                  )}
                </div>
              </div>
            )}
          </div>
        )}

        {/* Remediation bridge */}
        {linkedProject && (
          <div className="ml-8 rounded-lg border border-slate-700 bg-slate-800/50 px-4 py-3 flex items-center justify-between gap-4">
            <div className="flex items-center gap-3 min-w-0">
              <span className="text-slate-500 text-sm shrink-0">Remediation →</span>
              <Link
                to={`/projects/${linkedProject.id}`}
                className="text-violet-400 hover:text-violet-300 transition-colors text-sm font-medium truncate"
              >
                {linkedProject.name}
              </Link>
              {linkedTasks.length > 0 && (
                <span className="flex items-center gap-1.5 text-xs text-amber-400 bg-amber-900/30 border border-amber-800 px-2 py-0.5 rounded-full shrink-0">
                  <span className="w-1.5 h-1.5 rounded-full bg-amber-400 animate-pulse" />
                  {linkedTasks.length === 1 ? 'Remediation in progress' : `${linkedTasks.length} remediation tasks active`}
                </span>
              )}
            </div>
            <Link
              to={`/projects/${linkedProject.id}`}
              className="text-xs text-slate-500 hover:text-slate-300 transition-colors shrink-0"
            >
              View →
            </Link>
          </div>
        )}

        {triggerError && (
          <p className="text-xs text-red-400 ml-8">{triggerError}</p>
        )}

        {/* Agents — inline, same pattern as list page */}
        <div className="border-t border-slate-800 pt-4">
          <p className="text-xs text-slate-500 uppercase tracking-wide mb-2">Agents</p>
          <AgentsSection
            assigned={agents}
            allAgents={allAgents}
            showHeartbeat
            onAdd={async (agentId) => { await api.projects.assignAgent(id!, agentId); load() }}
            onRemove={async (agentId) => { await api.projects.removeAgent(id!, agentId); load() }}
          />
        </div>
      </div>

      {/* Run log */}
      <div>
        <h2 className="text-sm font-semibold text-slate-400 uppercase tracking-wide mb-3">
          Run log <span className="font-normal text-slate-600 normal-case ml-1">({sortedTasks.length} runs)</span>
        </h2>

        {sortedTasks.length === 0 ? (
          <EmptyState
            icon="⟳"
            title="No runs yet"
            description={hasSchedule
              ? `Waiting for the first scheduled run. ${scheduleSummary(monitor)}.`
              : primaryAgent
                ? 'Click "Run now" to trigger the first run manually.'
                : 'Assign an agent then click "Run now" to trigger a run.'
            }
          />
        ) : (
          <div className="space-y-2">
            {sortedTasks.map(t => (
              <RunCard key={t.id} task={t} agent={agentById[t.agent_id]} />
            ))}
          </div>
        )}
      </div>

      {/* Edit modal */}
      {showEdit && (
        <Modal title="Edit Monitor" onClose={() => setShowEdit(false)} className="max-w-2xl">
          <div className="space-y-4">
            <div>
              <Label htmlFor="edit-name">Name</Label>
              <Input id="edit-name" value={editName} onChange={e => setEditName(e.target.value)} />
            </div>
            <div>
              <div className="flex items-center justify-between mb-1">
                <Label htmlFor="edit-desc">Description</Label>
                {providers.length > 0 && (
                  <button
                    type="button"
                    onClick={() => { setShowAI(v => !v); setAiError('') }}
                    className="text-xs text-violet-400 hover:text-violet-300 transition-colors flex items-center gap-1"
                  >
                    ✦ {showAI ? 'Hide AI assist' : 'Generate with AI'}
                  </button>
                )}
              </div>
              {showAI && (
                <div className="mb-3 rounded-lg border border-violet-800/50 bg-violet-950/30 p-3 space-y-3">
                  <p className="text-xs text-slate-400">Describe what you want this monitor to do and AI will write the description for you.</p>
                  {providers.length > 1 && (
                    <div>
                      <Label htmlFor="ai-provider">Generate using</Label>
                      <Select id="ai-provider" value={aiProviderID} onChange={e => setAiProviderID(e.target.value)}>
                        {providers.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
                      </Select>
                    </div>
                  )}
                  <div>
                    <Label htmlFor="ai-hint">Additional context <span className="text-slate-500 font-normal">(optional)</span></Label>
                    <Textarea
                      id="ai-hint"
                      value={aiHint}
                      onChange={e => setAiHint(e.target.value)}
                      rows={2}
                      placeholder="e.g. Check disk usage on prod servers and alert if above 80%"
                    />
                  </div>
                  {aiError && <p className="text-xs text-red-400">{aiError}</p>}
                  <div className="flex justify-end">
                    <Button onClick={generateDescription} disabled={aiGenerating}>
                      {aiGenerating ? 'Generating…' : '✦ Generate'}
                    </Button>
                  </div>
                </div>
              )}
              <Textarea
                id="edit-desc"
                value={editDesc}
                onChange={e => setEditDesc(e.target.value)}
                rows={5}
                placeholder="Describe what this monitor should check and report on each run…"
              />
            </div>
            <div>
              <ScheduleEditor value={editSchedule} onChange={setEditSchedule} idPrefix="edit" />
            </div>
            <div>
              <Label htmlFor="edit-monitor-model">
                Monitor model <span className="text-slate-500 font-normal">(optional — cheap model for cost savings)</span>
              </Label>
              <Input
                id="edit-monitor-model"
                value={editMonitorModel}
                onChange={e => setEditMonitorModel(e.target.value)}
                placeholder="e.g. claude-haiku-3-5, gpt-4o-mini (leave blank for agent default)"
              />
            </div>
            <div className="grid grid-cols-2 gap-3">
              <div>
                <Label htmlFor="edit-budget">Budget limit (USD) <span className="text-slate-500 font-normal">(optional)</span></Label>
                <Input
                  id="edit-budget"
                  type="number"
                  min="0"
                  step="0.01"
                  value={editBudgetUSD}
                  onChange={e => setEditBudgetUSD(e.target.value)}
                  placeholder="e.g. 5.00"
                />
              </div>
              <div>
                <Label htmlFor="edit-budget-period">Period</Label>
                <Select id="edit-budget-period" value={editBudgetPeriod} onChange={e => setEditBudgetPeriod(e.target.value as 'day'|'week'|'month'|'total')}>
                  <option value="total">Total (all time)</option>
                  <option value="day">Day (calendar day)</option>
                  <option value="week">Week (rolling 7 days)</option>
                  <option value="month">Month (calendar month)</option>
                </Select>
              </div>
            </div>
            <div>
              <Label htmlFor="edit-wdir">Working Directory <span className="text-slate-500 font-normal">(optional)</span></Label>
              <Input id="edit-wdir" value={editWorkingDir} onChange={e => setEditWorkingDir(e.target.value)} placeholder="/path/to/project" />
            </div>
            {saveError && <p className="text-sm text-red-400">{saveError}</p>}
            <div className="flex gap-3 justify-end pt-2">
              <Button variant="secondary" onClick={() => setShowEdit(false)}>Cancel</Button>
              <Button onClick={saveEdit} disabled={saving}>{saving ? 'Saving…' : 'Save'}</Button>
            </div>
          </div>
        </Modal>
      )}

      {/* Run with extra prompt modal */}
      {showRunModal && (
        <Modal title="Run now with extra prompt" onClose={() => setShowRunModal(false)} className="max-w-2xl">
          <div className="space-y-4">
            <p className="text-sm text-slate-400">
              Add special instructions for this one-off run. They'll be appended to the monitor's
              base description so the agent has full context plus your extra guidance.
            </p>

            {/* Base description (read-only preview) */}
            {monitor?.description && (
              <div className="rounded-lg border border-slate-700 bg-slate-950 p-3">
                <p className="text-xs text-slate-500 uppercase tracking-wide mb-1">Base monitor instructions</p>
                <p className="text-xs text-slate-400 leading-relaxed line-clamp-4">{monitor.description}</p>
              </div>
            )}

            {/* Extra prompt field */}
            <div>
              <div className="flex items-center justify-between mb-1">
                <Label htmlFor="run-extra-prompt">Extra instructions for this run</Label>
                {providers.length > 0 && (
                  <button
                    type="button"
                    onClick={() => { setRunShowAI(v => !v); setRunAiError('') }}
                    className="text-xs text-violet-400 hover:text-violet-300 transition-colors flex items-center gap-1"
                  >
                    ✦ {runShowAI ? 'Hide AI assist' : 'Generate with AI'}
                  </button>
                )}
              </div>

              {runShowAI && (
                <div className="mb-3 rounded-lg border border-violet-800/50 bg-violet-950/30 p-3 space-y-3">
                  <p className="text-xs text-slate-400">
                    Describe what's special about this run in a few words — AI will expand it into detailed instructions.
                  </p>
                  {providers.length > 1 && (
                    <div>
                      <Label htmlFor="run-ai-provider">Generate using</Label>
                      <Select
                        id="run-ai-provider"
                        value={runAiProviderID}
                        onChange={e => setRunAiProviderID(e.target.value)}
                      >
                        {providers.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
                      </Select>
                    </div>
                  )}
                  <div>
                    <Label htmlFor="run-ai-hint">Hint <span className="text-slate-500 font-normal">(what's different about this run?)</span></Label>
                    <Textarea
                      id="run-ai-hint"
                      value={runAiHint}
                      onChange={e => setRunAiHint(e.target.value)}
                      rows={2}
                      placeholder="e.g. Focus on the /var/log directory only, ignore /tmp"
                    />
                  </div>
                  {runAiError && <p className="text-xs text-red-400">{runAiError}</p>}
                  <div className="flex justify-end">
                    <Button onClick={expandWithAI} disabled={runAiExpanding || !runAiHint.trim()}>
                      {runAiExpanding ? 'Generating…' : '✦ Generate'}
                    </Button>
                  </div>
                </div>
              )}

              <Textarea
                id="run-extra-prompt"
                value={runExtraPrompt}
                onChange={e => setRunExtraPrompt(e.target.value)}
                rows={5}
                placeholder="e.g. Pay special attention to error logs from the last 2 hours. Flag any OOM events."
              />
              <p className="text-xs text-slate-600 mt-1">
                Leave blank to run with the standard monitor instructions only.
              </p>
            </div>

            {triggerError && <p className="text-sm text-red-400">{triggerError}</p>}

            <div className="flex gap-3 justify-end pt-2">
              <Button variant="secondary" onClick={() => setShowRunModal(false)}>Cancel</Button>
              <Button onClick={runWithExtraPrompt} disabled={triggering || !primaryAgent}>
                {triggering ? 'Triggering…' : '▶ Run now'}
              </Button>
            </div>
          </div>
        </Modal>
      )}

      {/* Delete confirmation */}
      {showDeleteConfirm && (
        <Modal title="Delete Monitor" onClose={() => setShowDeleteConfirm(false)}>
          <div className="space-y-4">
            <p className="text-slate-300 text-sm">
              Delete <span className="text-white font-semibold">{monitor.name}</span>?
              This removes all {tasks.length} run record{tasks.length !== 1 ? 's' : ''} permanently.
            </p>
            <div className="flex gap-3 justify-end">
              <Button variant="secondary" onClick={() => setShowDeleteConfirm(false)}>Cancel</Button>
              <Button
                className="bg-red-600 hover:bg-red-700 text-white"
                onClick={deleteMonitor}
                disabled={deleting}
              >
                {deleting ? 'Deleting…' : 'Delete Monitor'}
              </Button>
            </div>
          </div>
        </Modal>
      )}
    </div>
  )
}
