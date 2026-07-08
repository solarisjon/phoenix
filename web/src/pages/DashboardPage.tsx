import { useState, useEffect, useCallback, useRef } from 'react'
import { Link } from 'react-router-dom'
import { api, type Project, type Task, type CostsResponse, type Agent, type Team, type Provider } from '@/lib/api'
import { phoenixWS } from '@/lib/ws'
import { Card, CardBody, CardHeader } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Modal } from '@/components/ui/modal'
import { taskStatusVariant, taskStatusLabel, parseOutput, formatCost, timeAgo, getModelInfo } from '@/lib/utils'
import { MarkdownOutput } from '@/components/ui/markdown-output'
import { FollowUpThread } from '@/components/ui/follow-up-thread'
import { EditRetryModal } from '@/components/edit-retry-modal'
import { getErrorMessage } from '@/lib/errors'
import {
  BarChart, Bar, XAxis, YAxis, Tooltip, ResponsiveContainer, CartesianGrid
} from 'recharts'


function StatCard({ label, value, sub, accent, onClick, href }: {
  label: string; value: string; sub?: string; accent?: string
  onClick?: () => void; href?: string
}) {
  const inner = (
    <CardBody className="py-5">
      <p className="text-slate-400 text-xs uppercase tracking-wide mb-1">{label}</p>
      <p className={`text-3xl font-bold ${accent ?? 'text-white'}`}>{value}</p>
      {sub && <p className={`text-xs mt-1 ${onClick || href ? 'text-violet-400' : 'text-slate-500'}`}>{sub}</p>}
    </CardBody>
  )
  if (href) return <Link to={href}><Card className="hover:border-slate-600 transition-colors cursor-pointer">{inner}</Card></Link>
  if (onClick) return <button className="w-full text-left" onClick={onClick}><Card className="hover:border-slate-600 transition-colors cursor-pointer">{inner}</Card></button>
  return <Card>{inner}</Card>
}

// Running task card — shows live streamed content
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
  return <span className="tabular-nums">{m > 0 ? `${m}m ${s}s` : `${s}s`}</span>
}

function DailyTooltip({ active, payload, label }: { active?: boolean; payload?: { value: number; name: string; color: string }[]; label?: string }) {
  if (!active || !payload?.length) return null
  return (
    <div className="bg-slate-900 border border-slate-700 rounded px-3 py-2 text-xs">
      <p className="text-slate-400 mb-1">{label}</p>
      {payload.map(p => (
        <p key={p.name} style={{ color: p.color }}>
          {p.name === 'cost_usd' ? fmt(p.value) : `${fmtTokens(p.value)} tok`}
        </p>
      ))}
    </div>
  )
}

const DAILY_TOOLTIP_CONTENT = <DailyTooltip />

function RunningTaskCard({ task, queuePos, agents, projects, providers, onCancel, onOpen }: { task: Task; queuePos?: number; agents: Agent[]; projects: Project[]; providers: Provider[]; onCancel: () => void; onOpen: () => void }) {
  const [stream, setStream] = useState('')
  const [cancelling, setCancelling] = useState(false)
  const [forceResetting, setForceResetting] = useState(false)
  const [cancelError, setCancelError] = useState<string | null>(null)
  const [hidden, setHidden] = useState(false)
  const agent = agents.find(a => a.id === task.agent_id)
  const project = projects.find(p => p.id === task.project_id)
  const modelInfo = getModelInfo(agent, providers)
  const scrollRef = useRef<HTMLPreElement>(null)
  const isQueued = task.status === 'queued'

  useEffect(() => {
    const unsub = phoenixWS.on((ev) => {
      if (ev.type === 'task.output_stream') {
        const p = ev.payload
        if (p.task_id === task.id) setStream(prev => prev + p.chunk)
      }
    })
    return unsub
  }, [task.id])

  // Auto-scroll to bottom as new chunks arrive
  useEffect(() => {
    const el = scrollRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [stream])

  const handleCancel = async () => {
    setCancelling(true)
    setCancelError(null)
    // Optimistically hide the card — the goroutine will update the DB
    // asynchronously, so this avoids the "still running" confusion.
    setHidden(true)
    try {
      await api.tasks.cancel(task.id)
      onCancel()
    } catch (error: unknown) {
      // Cancel failed — show the card again with an error + force-reset option.
      setHidden(false)
      setCancelError(getErrorMessage(error, 'Cancel failed'))
    } finally {
      setCancelling(false)
    }
  }

  const handleForceReset = async () => {
    setForceResetting(true)
    setCancelError(null)
    try {
      await api.tasks.forceReset(task.id)
      onCancel()
    } catch (error: unknown) {
      setCancelError(getErrorMessage(error, 'Force reset failed'))
      setForceResetting(false)
    }
  }

  if (hidden) return null

  const preview = stream || parseOutput(task.output)

  return (
    <div className="cursor-pointer" onClick={onOpen}>
    <Card className={`${isQueued ? 'border-slate-700/50' : 'border-violet-900/40'} hover:border-violet-700/60 transition-colors`}>
      <CardBody className="py-3 px-4">
        <div className="flex items-start justify-between gap-3 mb-2">
          <div className="flex-1 min-w-0">
            <div className="flex items-center gap-2 mb-0.5">
              {isQueued
                ? <span className="text-xs text-slate-500 font-mono w-5 flex-shrink-0">#{queuePos}</span>
                : <span className="w-1.5 h-1.5 rounded-full bg-violet-500 animate-pulse flex-shrink-0" />
              }
              <p className="text-sm font-medium text-white truncate">{task.title}</p>
            </div>
            <div className="flex items-center gap-1.5 flex-wrap mt-0.5">
              {task.task_type === 'orchestration' && (
                <span className="text-[10px] font-medium text-violet-400 bg-violet-900/30 border border-violet-800/40 rounded px-1.5 py-0.5 leading-none">⚡ Orchestrator</span>
              )}
              {task.task_type === 'subtask' && (
                <span className="text-[10px] font-medium text-sky-400 bg-sky-900/30 border border-sky-800/40 rounded px-1.5 py-0.5 leading-none">↳ Subtask</span>
              )}
              <span className="text-xs text-slate-500">
                {project ? <Link to={`/projects/${project.id}`} className="text-violet-400 hover:underline" onClick={e => e.stopPropagation()}>{project.name}</Link> : ''}
                {project && agent ? ' · ' : ''}
                {agent?.name ?? ''}
                {modelInfo && <> · <span className="text-slate-600">{modelInfo.providerName}</span> · <span className="text-slate-600">{modelInfo.model}</span></>}
                {task.started_at && !isQueued && (
                  <> · <ElapsedTimer startedAt={task.started_at} /></>
                )}
                {isQueued && ' · waiting in queue'}
              </span>
            </div>
          </div>
          <div className="flex items-center gap-2">
            <Badge variant={isQueued ? 'muted' : 'info'}>{isQueued ? 'Queued' : 'Running'}</Badge>
            <button
              onClick={e => { e.stopPropagation(); handleCancel() }}
              disabled={cancelling || forceResetting}
              className="text-xs text-slate-500 hover:text-red-400 disabled:opacity-50 transition-colors"
              title={isQueued ? 'Remove from queue' : 'Cancel task'}
            >
              {cancelling ? '…' : '✕'}
            </button>
          </div>
        </div>
        {!isQueued && preview ? (
          <pre ref={scrollRef} className="text-xs text-slate-400 font-mono bg-slate-950 rounded p-2 max-h-48 overflow-y-auto whitespace-pre-wrap" onClick={e => e.stopPropagation()}>
            {preview}
          </pre>
        ) : isQueued && task.description ? (
          <div className="text-xs text-slate-500 bg-slate-900 rounded p-2 mt-1 leading-relaxed line-clamp-2">
            {task.description}
          </div>
        ) : null}
        <div className="flex items-center justify-between mt-1.5">
          <p className="text-xs text-slate-600">{timeAgo(task.created_at)}</p>
          {cancelError ? (
            <div className="flex items-center gap-2" onClick={e => e.stopPropagation()}>
              <span className="text-xs text-red-400">{cancelError}</span>
              <button
                onClick={handleForceReset}
                disabled={forceResetting}
                className="text-xs bg-red-900/40 hover:bg-red-900/60 text-red-300 px-2 py-0.5 rounded transition-colors disabled:opacity-50"
                title="Force-reset this task to failed immediately, killing any subprocess"
              >
                {forceResetting ? 'Forcing…' : '⚡ Force Reset'}
              </button>
            </div>
          ) : (
            <span className="text-xs text-slate-600 italic">click for details</span>
          )}
        </div>
      </CardBody>
    </Card>
    </div>
  )
}

function TaskDetailModal({ task, agents, projects, onRetry, onClose }: {
  task: Task
  agents: Agent[]
  projects: Project[]
  onRetry: () => void
  onClose: () => void
}) {
  const agent = agents.find(a => a.id === task.agent_id)
  const project = projects.find(p => p.id === task.project_id)
  const [stream, setStream] = useState('')
  const scrollRef = useRef<HTMLDivElement>(null)
  const isLive = task.status === 'running' || task.status === 'queued'
  const output = stream || parseOutput(task.output)
  const [retrying, setRetrying] = useState(false)
  const [cancelling, setCancelling] = useState(false)
  const [bumping, setBumping] = useState(false)
  const [forceResetting, setForceResetting] = useState(false)
  const [editRetrying, setEditRetrying] = useState(false)

  // Stream live output when the modal is open for a running task
  useEffect(() => {
    if (!isLive) return
    const unsub = phoenixWS.on((ev) => {
      if (ev.type === 'task.output_stream') {
        const p = ev.payload
        if (p.task_id === task.id) setStream(prev => prev + p.chunk)
      }
    })
    return unsub
  }, [task.id, isLive])

  // Auto-scroll output as new chunks arrive
  useEffect(() => {
    const el = scrollRef.current
    if (el) el.scrollTop = el.scrollHeight
  }, [stream])

  const retry = async () => {
    setRetrying(true)
    try { await api.tasks.retry(task.id); onRetry() } finally { setRetrying(false) }
  }

  const cancel = async () => {
    setCancelling(true)
    try { await api.tasks.cancel(task.id); onRetry() } finally { setCancelling(false) }
  }

  const forceReset = async () => {
    setForceResetting(true)
    try { await api.tasks.forceReset(task.id); onRetry() } finally { setForceResetting(false) }
  }

  const bump = async () => {
    setBumping(true)
    try { await api.tasks.bump(task.id); onRetry() } finally { setBumping(false) }
  }

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-2 gap-3 text-sm">
        <div>
          <p className="text-slate-500 text-xs mb-0.5">Project</p>
          <Link to={`/projects/${task.project_id}`} className="text-violet-400 hover:underline" onClick={onClose}>
            {project?.name ?? 'Unknown'}
          </Link>
        </div>
        <div>
          <p className="text-slate-500 text-xs mb-0.5">Agent</p>
          <p className="text-white">{agent?.name ?? 'Unknown'}</p>
        </div>
        <div>
          <p className="text-slate-500 text-xs mb-0.5">Status</p>
          <div className="flex items-center gap-2">
            <Badge variant={taskStatusVariant(task.status)}>{taskStatusLabel(task.status)}</Badge>
            {task.priority > 0 && (
              <span className="text-xs bg-amber-900/50 text-amber-300 border border-amber-700/50 rounded px-1.5 py-0.5">
                P+{task.priority}
              </span>
            )}
          </div>
        </div>
        <div>
          <p className="text-slate-500 text-xs mb-0.5">Created</p>
          <p className="text-slate-300">{timeAgo(task.created_at)}</p>
        </div>
        {task.cost_usd > 0 && (
          <div>
            <p className="text-slate-500 text-xs mb-0.5">Cost</p>
            <p className="text-slate-300">{formatCost(task.cost_usd)}</p>
          </div>
        )}
        {(task.tokens_in > 0 || task.tokens_out > 0) && (
          <div>
            <p className="text-slate-500 text-xs mb-0.5">Tokens</p>
            <p className="text-slate-300 text-xs font-mono">↑{task.tokens_in.toLocaleString()} ↓{task.tokens_out.toLocaleString()}</p>
          </div>
        )}
      </div>
      {task.description && (
        <div>
          <p className="text-slate-500 text-xs mb-1">Description</p>
          <pre className="text-xs text-slate-300 whitespace-pre-wrap font-mono bg-slate-800 rounded-lg p-3 max-h-32 overflow-y-auto">{task.description}</pre>
        </div>
      )}
      <div>
        <div className="flex items-center gap-2 mb-1">
          <p className="text-slate-500 text-xs">Output</p>
          {isLive && (
            <span className="flex items-center gap-1 text-xs text-violet-400">
              <span className="w-1.5 h-1.5 rounded-full bg-violet-500 animate-pulse" />
              live
            </span>
          )}
        </div>
        <div ref={scrollRef} className="bg-slate-950 border border-slate-800 rounded-lg p-3 max-h-96 overflow-y-auto">
          {output ? (
            isLive
              ? <pre className="text-xs text-slate-300 font-mono whitespace-pre-wrap">{output}</pre>
              : <MarkdownOutput content={output} />
          ) : (
            <span className="text-xs text-slate-500">{isLive ? 'Waiting for output…' : '(no output)'}</span>
          )}
        </div>
      </div>
      <div className="flex gap-2 justify-end flex-wrap">
        <Link to={`/projects/${task.project_id}`} onClick={onClose}>
          <Button variant="secondary" size="sm">View Project →</Button>
        </Link>
        {task.status === 'queued' && (
          <Button size="sm" variant="secondary" onClick={bump} disabled={bumping}>
            {bumping ? 'Bumping…' : '⬆ Bump'}
          </Button>
        )}
        {(task.status === 'running' || task.status === 'queued') && (
          <>
            <Button size="sm" variant="secondary" onClick={cancel} disabled={cancelling || forceResetting}>
              {cancelling ? 'Cancelling…' : '✕ Cancel'}
            </Button>
            <button
              onClick={forceReset}
              disabled={forceResetting || cancelling}
              className="text-xs bg-red-900/40 hover:bg-red-900/60 text-red-300 px-3 py-1.5 rounded transition-colors disabled:opacity-50"
              title="Force-reset immediately: kills subprocess and marks task failed. Use if regular cancel has no effect."
            >
              {forceResetting ? 'Forcing…' : '⚡ Force Reset'}
            </button>
          </>
        )}
        {task.status === 'failed' && (
          <>
            <Button size="sm" variant="secondary" onClick={() => setEditRetrying(true)}>✎ Edit & Retry</Button>
            <Button size="sm" onClick={retry} disabled={retrying}>{retrying ? 'Retrying…' : '↺ Retry'}</Button>
          </>
        )}
      </div>
      <FollowUpThread task={task} agents={agents} onSent={onRetry} />
      {editRetrying && (
        <EditRetryModal
          task={task}
          onDone={() => { setEditRetrying(false); onRetry() }}
          onClose={() => setEditRetrying(false)}
        />
      )}
    </div>
  )
}

// ---- Cost charts ----

const STATUS_COLORS: Record<string, string> = {
  completed: '#10b981',
  failed: '#ef4444',
  running: '#8b5cf6',
  queued: '#6366f1',
  awaiting_approval: '#f59e0b',
  pending: '#64748b',
}

function fmt(usd: number): string {
  if (usd === 0) return '$0'
  if (usd < 0.001) return `$${usd.toFixed(5)}`
  if (usd < 0.01) return `$${usd.toFixed(4)}`
  if (usd < 1) return `$${usd.toFixed(3)}`
  return `$${usd.toFixed(2)}`
}

function fmtTokens(n: number): string {
  if (n === 0) return '0'
  if (n >= 1_000_000) return `${(n / 1_000_000).toFixed(1)}M`
  if (n >= 1_000) return `${(n / 1_000).toFixed(1)}k`
  return String(n)
}

function CostSection({ costs }: { costs: CostsResponse }) {
  const hasSpend = costs.total_cost_usd > 0
  const agentRows = costs.by_agent
  const projectsWithCost = costs.by_project.filter(p => p.total_cost_usd > 0)
  const providerRows = costs.by_provider ?? []
  const modelRows = costs.by_model ?? []

  // Daily chart: show all days (token or cost activity)
  const dailyRows = (costs.by_day ?? [])
  const showDaily = dailyRows.length > 0

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-sm font-medium text-slate-400 uppercase tracking-wide">Activity & Cost</h2>
        <span className="text-xs text-slate-500">{costs.total_tasks} total tasks</span>
      </div>

      {/* Token + cost summary cards */}
      <div className="grid grid-cols-3 gap-3">
        <Card>
          <CardBody className="py-3">
            <p className="text-xs text-slate-500 uppercase tracking-wide mb-1">Total spend</p>
            <p className="text-xl font-mono font-semibold text-violet-400">{fmt(costs.total_cost_usd)}</p>
          </CardBody>
        </Card>
        <Card>
          <CardBody className="py-3">
            <p className="text-xs text-slate-500 uppercase tracking-wide mb-1">Tokens in</p>
            <p className="text-xl font-mono font-semibold text-sky-400">{fmtTokens(costs.total_tokens_in ?? 0)}</p>
            <p className="text-xs text-slate-600 mt-0.5">prompt / context</p>
          </CardBody>
        </Card>
        <Card>
          <CardBody className="py-3">
            <p className="text-xs text-slate-500 uppercase tracking-wide mb-1">Tokens out</p>
            <p className="text-xl font-mono font-semibold text-emerald-400">{fmtTokens(costs.total_tokens_out ?? 0)}</p>
            <p className="text-xs text-slate-600 mt-0.5">completion / output</p>
          </CardBody>
        </Card>
      </div>

      {/* Task status breakdown */}
      {costs.by_status && costs.by_status.length > 0 && (
        <Card>
          <CardBody className="py-3">
            <div className="flex gap-4 flex-wrap mb-3">
              {costs.by_status.map(s => (
                <div key={s.status} className="flex items-center gap-1.5">
                  <span className="w-2 h-2 rounded-full flex-shrink-0" style={{ backgroundColor: STATUS_COLORS[s.status] ?? '#64748b' }} />
                  <span className="text-xs text-slate-400 capitalize">{s.status.replace(/_/g, ' ')}</span>
                  <span className="text-xs text-white font-medium">{s.count}</span>
                </div>
              ))}
            </div>
            <div className="flex h-1.5 rounded-full overflow-hidden gap-px">
              {costs.by_status.map(s => (
                <div
                  key={s.status}
                  style={{ width: `${(s.count / costs.total_tasks) * 100}%`, backgroundColor: STATUS_COLORS[s.status] ?? '#64748b' }}
                  title={`${s.status}: ${s.count}`}
                />
              ))}
            </div>
          </CardBody>
        </Card>
      )}

      {/* Daily spend chart */}
      {showDaily && (
        <Card>
          <CardBody>
            <p className="text-xs text-slate-500 uppercase tracking-wide mb-3">Daily activity (30 days)</p>
            <ResponsiveContainer width="100%" height={140}>
              <BarChart data={dailyRows} margin={{ top: 4, right: 4, left: -20, bottom: 0 }}>
                <CartesianGrid strokeDasharray="3 3" stroke="#1e293b" vertical={false} />
                <XAxis dataKey="date" tick={{ fontSize: 9, fill: '#64748b' }} tickFormatter={d => d.slice(5)} />
                <YAxis tick={{ fontSize: 9, fill: '#64748b' }} />
                <Tooltip content={DAILY_TOOLTIP_CONTENT} />
                {hasSpend
                  ? <Bar dataKey="cost_usd" fill="#8b5cf6" radius={[2,2,0,0]} name="cost_usd" />
                  : <Bar dataKey="tokens_in" fill="#0ea5e9" radius={[2,2,0,0]} name="tokens_in" stackId="tok" />
                }
                {!hasSpend && <Bar dataKey="tokens_out" fill="#10b981" radius={[2,2,0,0]} name="tokens_out" stackId="tok" />}
              </BarChart>
            </ResponsiveContainer>
            {!hasSpend && (
              <div className="flex gap-4 mt-2">
                <span className="flex items-center gap-1 text-xs text-slate-500"><span className="w-2 h-2 rounded-sm bg-sky-500 inline-block" /> tokens in</span>
                <span className="flex items-center gap-1 text-xs text-slate-500"><span className="w-2 h-2 rounded-sm bg-emerald-500 inline-block" /> tokens out</span>
              </div>
            )}
          </CardBody>
        </Card>
      )}

      <div className="grid grid-cols-2 gap-4">
        {/* By provider */}
        {providerRows.length > 0 && (
          <Card>
            <CardBody>
              <p className="text-xs text-slate-500 uppercase tracking-wide mb-3">By provider</p>
              <table className="w-full text-sm">
                <thead>
                  <tr>
                    <th className="pb-2 text-left text-xs text-slate-600 font-normal">Provider</th>
                    <th className="pb-2 text-right text-xs text-slate-600 font-normal">Tok↑</th>
                    <th className="pb-2 text-right text-xs text-slate-600 font-normal">Tok↓</th>
                    <th className="pb-2 text-right text-xs text-slate-600 font-normal">Cost</th>
                  </tr>
                </thead>
                <tbody>
                  {providerRows.map(p => (
                    <tr key={p.label} className="border-b border-slate-800 last:border-0">
                      <td className="py-2 text-slate-300">{p.label}</td>
                      <td className="py-2 text-right font-mono text-sky-400 text-xs">{fmtTokens(p.tokens_in)}</td>
                      <td className="py-2 text-right font-mono text-emerald-400 text-xs">{fmtTokens(p.tokens_out)}</td>
                      <td className="py-2 text-right font-mono text-white">{p.total_cost_usd > 0 ? fmt(p.total_cost_usd) : <span className="text-slate-600">—</span>}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </CardBody>
          </Card>
        )}

        {/* By model */}
        {modelRows.length > 0 && (
          <Card>
            <CardBody>
              <p className="text-xs text-slate-500 uppercase tracking-wide mb-3">By model</p>
              <table className="w-full text-sm">
                <thead>
                  <tr>
                    <th className="pb-2 text-left text-xs text-slate-600 font-normal">Model</th>
                    <th className="pb-2 text-right text-xs text-slate-600 font-normal">Tasks</th>
                    <th className="pb-2 text-right text-xs text-slate-600 font-normal">Tok↑</th>
                    <th className="pb-2 text-right text-xs text-slate-600 font-normal">Tok↓</th>
                    <th className="pb-2 text-right text-xs text-slate-600 font-normal">Cost</th>
                  </tr>
                </thead>
                <tbody>
                  {modelRows.map(m => (
                    <tr key={m.label} className="border-b border-slate-800 last:border-0">
                      <td className="py-2 text-slate-300 text-xs font-mono">{m.label}</td>
                      <td className="py-2 text-right text-slate-400 text-xs">{m.task_count}</td>
                      <td className="py-2 text-right font-mono text-sky-400 text-xs">{fmtTokens(m.tokens_in)}</td>
                      <td className="py-2 text-right font-mono text-emerald-400 text-xs">{fmtTokens(m.tokens_out)}</td>
                      <td className="py-2 text-right font-mono text-white text-xs">{m.total_cost_usd > 0 ? fmt(m.total_cost_usd) : <span className="text-slate-600">—</span>}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </CardBody>
          </Card>
        )}

        {/* By agent */}
        <Card>
          <CardBody>
            <p className="text-xs text-slate-500 uppercase tracking-wide mb-3">By agent</p>
            {agentRows.length === 0 ? (
              <p className="text-xs text-slate-600">No agents have run tasks yet.</p>
            ) : (
              <table className="w-full text-sm">
                <thead>
                  <tr>
                    <th className="pb-2 text-left text-xs text-slate-600 font-normal">Agent</th>
                    <th className="pb-2 text-right text-xs text-slate-600 font-normal">Tasks</th>
                    <th className="pb-2 text-right text-xs text-slate-600 font-normal">Tok↑+↓</th>
                    <th className="pb-2 text-right text-xs text-slate-600 font-normal">Cost</th>
                  </tr>
                </thead>
                <tbody>
                  {agentRows.map(a => (
                    <tr key={a.id} className="border-b border-slate-800 last:border-0">
                      <td className="py-2 text-slate-300">{a.name}</td>
                      <td className="py-2 text-right text-slate-400">{a.task_count}</td>
                      <td className="py-2 text-right font-mono text-xs text-slate-400">
                        {(a.tokens_in + a.tokens_out) > 0
                          ? <span><span className="text-sky-400">{fmtTokens(a.tokens_in)}</span><span className="text-slate-600">/</span><span className="text-emerald-400">{fmtTokens(a.tokens_out)}</span></span>
                          : <span className="text-slate-600">—</span>}
                      </td>
                      <td className="py-2 text-right font-mono text-white">{a.total_cost_usd > 0 ? fmt(a.total_cost_usd) : <span className="text-slate-600">—</span>}</td>
                    </tr>
                  ))}
                </tbody>
                {hasSpend && (
                  <tfoot>
                    <tr>
                      <td className="pt-3 text-xs text-slate-500" colSpan={3}>Total</td>
                      <td className="pt-3 text-right font-mono text-violet-400 font-semibold">{fmt(costs.total_cost_usd)}</td>
                    </tr>
                  </tfoot>
                )}
              </table>
            )}
          </CardBody>
        </Card>

        {/* By project */}
        {projectsWithCost.length > 0 && (
          <Card>
            <CardBody>
              <p className="text-xs text-slate-500 uppercase tracking-wide mb-3">By project</p>
              <table className="w-full text-sm">
                <thead>
                  <tr>
                    <th className="pb-2 text-left text-xs text-slate-600 font-normal">Project</th>
                    <th className="pb-2 text-right text-xs text-slate-600 font-normal">Tasks</th>
                    <th className="pb-2 text-right text-xs text-slate-600 font-normal">Cost</th>
                  </tr>
                </thead>
                <tbody>
                  {projectsWithCost.map(p => (
                    <tr key={p.id} className="border-b border-slate-800 last:border-0">
                      <td className="py-2 text-slate-300">{p.name}</td>
                      <td className="py-2 text-right text-slate-400 text-xs">{p.task_count}</td>
                      <td className="py-2 text-right font-mono text-white">{fmt(p.total_cost_usd)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </CardBody>
          </Card>
        )}
      </div>
    </div>
  )
}

function GettingStartedCard({ providers, agents, projects }: {
  providers: Provider[]
  agents: Agent[]
  projects: Project[]
}) {
  const hasProvider = providers.length > 0
  const hasAgent = agents.length > 0
  const hasProject = projects.filter(p => p.status === 'active').length > 0

  if (hasProvider && hasAgent && hasProject) return null

  const steps = [
    {
      done: hasProvider,
      label: 'Add a provider',
      description: 'Connect an LLM endpoint, Ollama, or coding agent',
      href: '/settings?tab=providers',
      enabled: true,
    },
    {
      done: hasAgent,
      label: 'Create an agent',
      description: 'Define behaviour and assign a provider',
      href: '/settings?tab=agents',
      enabled: hasProvider,
    },
    {
      done: hasProject,
      label: 'Create a project',
      description: 'Give your agent something to work on',
      href: '/projects',
      enabled: hasAgent,
    },
  ]

  return (
    <Card className="border-violet-900/50 bg-violet-950/10">
      <CardBody>
        <p className="text-xs font-medium text-violet-400 uppercase tracking-wide mb-3">Get started</p>
        <div className="space-y-1">
          {steps.map((step, i) => {
            const row = (
              <div className={`flex items-center gap-3 px-3 py-2.5 rounded-lg transition-colors ${
                step.done
                  ? 'opacity-50'
                  : step.enabled
                  ? 'hover:bg-slate-800/60 cursor-pointer'
                  : 'opacity-30 cursor-not-allowed'
              }`}>
                <div className={`w-5 h-5 rounded-full flex items-center justify-center flex-shrink-0 text-xs font-bold ${
                  step.done
                    ? 'bg-emerald-500/20 text-emerald-400'
                    : step.enabled
                    ? 'bg-violet-500/20 text-violet-400'
                    : 'bg-slate-700 text-slate-500'
                }`}>
                  {step.done ? '✓' : i + 1}
                </div>
                <div className="flex-1 min-w-0">
                  <p className={`text-sm font-medium ${step.done ? 'line-through text-slate-500' : 'text-white'}`}>
                    {step.label}
                  </p>
                  <p className="text-xs text-slate-500">{step.description}</p>
                </div>
                {!step.done && step.enabled && (
                  <span className="text-slate-500 text-xs">→</span>
                )}
              </div>
            )
            return step.enabled && !step.done
              ? <Link key={i} to={step.href}>{row}</Link>
              : <div key={i}>{row}</div>
          })}
        </div>
      </CardBody>
    </Card>
  )
}

export function DashboardPage() {
  const [projects, setProjects] = useState<Project[]>([])
  const [agents, setAgents] = useState<Agent[]>([])
  const [providers, setProviders] = useState<Provider[]>([])
  const [teams, setTeams] = useState<Team[]>([])
  const [recentTasks, setRecentTasks] = useState<Task[]>([])
  const [runningTasks, setRunningTasks] = useState<Task[]>([])
  const [attentionCount, setAttentionCount] = useState(0)
  const [costs, setCosts] = useState<CostsResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [selectedTask, setSelectedTask] = useState<Task | null>(null)

  const loadCosts = useCallback(async () => {
    const c = await api.stats.costs().catch(() => null)
    if (c) setCosts(c)
  }, [])

  const load = useCallback(async () => {
    try {
      const [projs, agts, tms, provs] = await Promise.all([api.projects.list(), api.agents.list(), api.teams.list(), api.providers.list()])
      setProjects(projs)
      setAgents(agts)
      setTeams(tms)
      setProviders(provs)

      const [taskLists, running, attention, c] = await Promise.all([
        Promise.all(projs.map(p => api.tasks.list(p.id).catch(() => [] as Task[]))),
        api.tasks.listRunning().catch(() => [] as Task[]),
        api.inbox.listAttention().catch(() => [] as Task[]),
        api.stats.costs().catch(() => null),
      ])

      const all = taskLists.flat().sort((a, b) =>
        new Date(b.created_at).getTime() - new Date(a.created_at).getTime()
      ).slice(0, 15)
      setRecentTasks(all)
      setRunningTasks(running)
      setAttentionCount(attention.length)
      setCosts(c)
    } finally { setLoading(false) }
  }, [])

  useEffect(() => {
    load()

    // Re-fetch costs whenever a task finishes (status change event).
    const unsub = phoenixWS.on((ev) => {
      if (ev.type === 'task.status_changed') load()
    })

    // Also refresh costs on a 60-second timer so numbers stay current
    // even if no WebSocket events fire (e.g. all tasks already completed).
    const timer = setInterval(loadCosts, 60_000)

    // And refresh immediately when the user returns to this tab.
    const handleVisibility = () => { if (document.visibilityState === 'visible') loadCosts() }
    document.addEventListener('visibilitychange', handleVisibility)

    return () => {
      unsub()
      clearInterval(timer)
      document.removeEventListener('visibilitychange', handleVisibility)
    }
  }, [load, loadCosts])

  

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-white">Dashboard</h1>
        <p className="text-slate-400 text-sm mt-1">Your agent orchestration control center</p>
      </div>

      {/* Stats */}
      <div className="grid grid-cols-4 gap-4">
        <StatCard label="Active Projects" value={String(projects.filter(p => p.status === 'active').length)} />
        <StatCard
          label="Tasks Running"
          value={String(runningTasks.filter(t => t.status === 'running').length)}
          sub={runningTasks.filter(t => t.status === 'queued').length > 0
            ? `${runningTasks.filter(t => t.status === 'queued').length} queued`
            : undefined}
        />
        <StatCard
          label="Needs Attention"
          value={String(attentionCount)}
          sub={attentionCount > 0 ? 'Go to inbox →' : undefined}
          accent={attentionCount > 0 ? 'text-amber-400' : undefined}
          href={attentionCount > 0 ? '/inbox' : undefined}
        />
        <StatCard
          label="Total Cost"
          value={costs ? formatCost(costs.total_cost_usd) : '—'}
          sub={costs && costs.total_tasks > 0 ? `across ${costs.total_tasks} tasks` : undefined}
        />
      </div>

      {/* Getting started checklist — hidden once all steps are complete */}
      {!loading && <GettingStartedCard providers={providers} agents={agents} projects={projects} />}

      {/* Running & queued tasks — always visible when active */}
      {runningTasks.length > 0 && (
        <div>
          <div className="flex items-center justify-between mb-3">
            <h2 className="text-sm font-semibold text-slate-300 uppercase tracking-wide flex items-center gap-2">
              <span className="w-2 h-2 rounded-full bg-violet-500 animate-pulse" />
              Active Tasks ({runningTasks.length})
            </h2>
          </div>
          <div className="grid grid-cols-2 gap-3">
            {(() => {
              const running = runningTasks.filter(t => t.status === 'running')
              const queued = runningTasks.filter(t => t.status === 'queued')
              return [
                ...running.map(t => (
                  <RunningTaskCard key={t.id} task={t} agents={agents} projects={projects} providers={providers} onCancel={load} onOpen={() => setSelectedTask(t)} />
                )),
                ...queued.map((t, i) => (
                  <RunningTaskCard key={t.id} task={t} queuePos={i + 1} agents={agents} projects={projects} providers={providers} onCancel={load} onOpen={() => setSelectedTask(t)} />
                )),
              ]
            })()}
          </div>
        </div>
      )}

      {/* Cost charts */}
      {costs && <CostSection costs={costs} />}

      <div className="grid grid-cols-3 gap-6">
        {/* Teams quick-view */}
        <div className="col-span-1 space-y-3">
          <div className="flex items-center justify-between">
            <h2 className="text-sm font-medium text-slate-400 uppercase tracking-wide">Teams</h2>
            <Link to="/teams" className="text-xs text-violet-400 hover:text-violet-300">View all →</Link>
          </div>
          {loading ? (
            <p className="text-slate-500 text-sm">Loading…</p>
          ) : teams.length === 0 ? (
            <Card>
              <CardBody className="py-8 text-center">
                <p className="text-slate-500 text-sm mb-2">No teams yet</p>
                <div className="flex flex-col gap-2 items-center">
                  <Link to="/teams" className="text-violet-400 text-xs hover:underline">Create a team →</Link>
                  <Link to="/teams" className="text-slate-500 text-xs hover:underline">or import a bundle</Link>
                </div>
              </CardBody>
            </Card>
          ) : teams.slice(0, 5).map(team => {
            const memberIds = new Set((team.agents ?? []).map(a => a.id))
            const activeTasks = recentTasks.filter(t => memberIds.has(t.agent_id) && (t.status === 'running' || t.status === 'queued'))
            return (
              <Link key={team.id} to={`/teams/${team.id}`}>
                <Card className="hover:border-slate-700 transition-colors cursor-pointer">
                  <CardBody className="py-3">
                    <div className="flex items-start justify-between">
                      <p className="text-sm font-medium text-white truncate">{team.name}</p>
                      {activeTasks.length > 0 && (
                        <span className="flex items-center gap-1 text-xs text-violet-400 flex-shrink-0 ml-2">
                          <span className="w-1.5 h-1.5 rounded-full bg-violet-500 animate-pulse" />
                          {activeTasks.length}
                        </span>
                      )}
                    </div>
                    <p className="text-xs text-slate-600 mt-0.5">
                      {team.agents?.length ?? 0} member{(team.agents?.length ?? 0) !== 1 ? 's' : ''}
                    </p>
                  </CardBody>
                </Card>
              </Link>
            )
          })}
        </div>

        {/* Recent Tasks */}
        <div className="col-span-2 space-y-3">
          <div className="flex items-center justify-between">
            <h2 className="text-sm font-medium text-slate-400 uppercase tracking-wide">Recent Activity</h2>
            <Link to="/tasks" className="text-xs text-violet-400 hover:text-violet-300">All tasks →</Link>
          </div>
          {loading ? (
            <p className="text-slate-500 text-sm">Loading…</p>
          ) : recentTasks.length === 0 ? (
            <Card>
              <CardBody className="py-12 text-center">
                <div className="text-3xl mb-3">✦</div>
                <p className="text-white font-medium mb-2">Ready to orchestrate</p>
                <p className="text-slate-400 text-sm mb-4">Import a team bundle or create a team, then start a project.</p>
                <div className="flex gap-3 justify-center">
                  <Link to="/teams" className="bg-violet-600 hover:bg-violet-500 text-white text-sm font-medium px-4 py-2 rounded-lg transition-colors">
                    Go to Teams
                  </Link>
                  <Link to="/settings" className="bg-slate-800 hover:bg-slate-700 text-white text-sm font-medium px-4 py-2 rounded-lg transition-colors">
                    Settings
                  </Link>
                </div>
              </CardBody>
            </Card>
          ) : (
            <Card>
              <CardHeader>
                <div className="flex items-center justify-between">
                  <p className="text-sm font-medium text-slate-300">Task Activity</p>
                  <Link to="/tasks" className="text-xs text-violet-400 hover:text-violet-300">View all →</Link>
                </div>
              </CardHeader>
              <div className="divide-y divide-slate-800">
                {recentTasks.map(t => (
                  <button
                    key={t.id}
                    className="w-full px-5 py-3 flex items-center gap-3 hover:bg-slate-800/50 transition-colors text-left"
                    onClick={() => setSelectedTask(t)}
                  >
                    <div className={`w-1.5 h-1.5 rounded-full flex-shrink-0 ${
                      t.status === 'running' ? 'bg-violet-500 animate-pulse' :
                      t.status === 'completed' ? 'bg-emerald-500' :
                      t.status === 'failed' ? 'bg-red-500' :
                      t.status === 'awaiting_approval' ? 'bg-amber-500' : 'bg-slate-600'
                    }`} />
                    <div className="flex-1 min-w-0">
                      <p className="text-sm text-white truncate">{t.title}</p>
                      <p className="text-xs text-slate-600 truncate">
                        {projects.find(p => p.id === t.project_id)?.name ?? ''}
                      </p>
                    </div>
                    <Badge variant={taskStatusVariant(t.status)}>{taskStatusLabel(t.status)}</Badge>
                    {t.cost_usd > 0 && <span className="text-xs text-slate-500 flex-shrink-0">{formatCost(t.cost_usd)}</span>}
                    <span className="text-xs text-slate-600 flex-shrink-0">{timeAgo(t.created_at)}</span>
                  </button>
                ))}
              </div>
            </Card>
          )}
        </div>
      </div>

      {/* Task detail modal */}
      {selectedTask && (
        <Modal title={selectedTask.title} onClose={() => setSelectedTask(null)} className="max-w-2xl">
          <TaskDetailModal
            task={selectedTask}
            agents={agents}
            projects={projects}
            onRetry={() => { setSelectedTask(null); load() }}
            onClose={() => setSelectedTask(null)}
          />
        </Modal>
      )}
    </div>
  )
}
