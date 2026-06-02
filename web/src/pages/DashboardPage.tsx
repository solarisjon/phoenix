import { useState, useEffect, useCallback, useRef } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { api, type Project, type Task, type CostsResponse, type Agent, type Team } from '@/lib/api'
import { phoenixWS } from '@/lib/ws'
import { Card, CardBody, CardHeader } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Modal } from '@/components/ui/modal'
import { taskStatusVariant, taskStatusLabel, parseOutput, formatCost, timeAgo } from '@/lib/utils'
import { MarkdownOutput } from '@/components/ui/markdown-output'
import { FollowUpThread } from '@/components/ui/follow-up-thread'


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

function RunningTaskCard({ task, queuePos, agents, projects, onCancel }: { task: Task; queuePos?: number; agents: Agent[]; projects: Project[]; onCancel: () => void }) {
  const [stream, setStream] = useState('')
  const [cancelling, setCancelling] = useState(false)
  const agent = agents.find(a => a.id === task.agent_id)
  const project = projects.find(p => p.id === task.project_id)
  const scrollRef = useRef<HTMLPreElement>(null)
  const isQueued = task.status === 'queued'

  useEffect(() => {
    const unsub = phoenixWS.on((ev) => {
      if (ev.type === 'task.output_stream') {
        const p = ev.payload as any
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
    try { await api.tasks.cancel(task.id); onCancel() } finally { setCancelling(false) }
  }

  const preview = stream || parseOutput(task.output)

  return (
    <Card className={isQueued ? 'border-slate-700/50' : 'border-violet-900/40'}>
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
            <p className="text-xs text-slate-500">
              {project ? <Link to={`/projects/${project.id}`} className="text-violet-400 hover:underline">{project.name}</Link> : ''}
              {project && agent ? ' · ' : ''}
              {agent?.name ?? ''}
              {task.started_at && !isQueued && (
                <> · <ElapsedTimer startedAt={task.started_at} /></>
              )}
              {isQueued && ' · waiting in queue'}
            </p>
          </div>
          <div className="flex items-center gap-2">
            <Badge variant={isQueued ? 'muted' : 'info'}>{isQueued ? 'Queued' : 'Running'}</Badge>
            <button
              onClick={handleCancel}
              disabled={cancelling}
              className="text-xs text-slate-500 hover:text-red-400 disabled:opacity-50 transition-colors"
              title={isQueued ? 'Remove from queue' : 'Cancel task'}
            >
              {cancelling ? '…' : '✕'}
            </button>
          </div>
        </div>
        {!isQueued && preview ? (
          <pre ref={scrollRef} className="text-xs text-slate-400 font-mono bg-slate-950 rounded p-2 max-h-48 overflow-y-auto whitespace-pre-wrap">
            {preview}
          </pre>
        ) : isQueued && task.description ? (
          <div className="text-xs text-slate-500 bg-slate-900 rounded p-2 mt-1 leading-relaxed line-clamp-2">
            {task.description}
          </div>
        ) : null}
        <p className="text-xs text-slate-600 mt-1.5">{timeAgo(task.created_at)}</p>
      </CardBody>
    </Card>
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
  const output = parseOutput(task.output)
  const [retrying, setRetrying] = useState(false)
  const [cancelling, setCancelling] = useState(false)

  const retry = async () => {
    setRetrying(true)
    try { await api.tasks.retry(task.id); onRetry() } finally { setRetrying(false) }
  }

  const cancel = async () => {
    setCancelling(true)
    try { await api.tasks.cancel(task.id); onRetry() } finally { setCancelling(false) }
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
          <Badge variant={taskStatusVariant(task.status)}>{taskStatusLabel(task.status)}</Badge>
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
        <p className="text-slate-500 text-xs mb-1">Output</p>
        <div className="bg-slate-950 border border-slate-800 rounded-lg p-3 max-h-64 overflow-y-auto">
          {output ? <MarkdownOutput content={output} /> : <span className="text-xs text-slate-500">(no output yet)</span>}
        </div>
      </div>
      <div className="flex gap-2 justify-end">
        <Link to={`/projects/${task.project_id}`} onClick={onClose}>
          <Button variant="secondary" size="sm">View Project →</Button>
        </Link>
        {(task.status === 'running' || task.status === 'queued') && (
          <Button size="sm" variant="secondary" onClick={cancel} disabled={cancelling}>
            {cancelling ? 'Cancelling…' : '✕ Cancel'}
          </Button>
        )}
        {task.status === 'failed' && (
          <Button size="sm" onClick={retry} disabled={retrying}>{retrying ? 'Retrying…' : '↺ Retry'}</Button>
        )}
      </div>
      <FollowUpThread task={task} agents={agents} onSent={onRetry} />
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

function CostSection({ costs }: { costs: CostsResponse }) {
  const hasSpend = costs.total_cost_usd > 0
  // by_agent now uses INNER JOIN so only agents with tasks are returned.
  // Sorted by cost desc, then task count desc — so $0 coding agents appear after paid ones.
  const agentRows = costs.by_agent
  const projectsWithCost = costs.by_project.filter(p => p.total_cost_usd > 0)

  // Only show daily breakdown if there are multiple days
  const dailyRows = (costs.by_day ?? []).filter(d => d.cost_usd > 0)
  const showDaily = dailyRows.length > 1

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-sm font-medium text-slate-400 uppercase tracking-wide">Activity & Cost</h2>
        <span className="text-xs text-slate-500">{costs.total_tasks} total tasks</span>
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

      <div className="grid grid-cols-2 gap-4">
          {/* By agent — includes $0-cost coding agents; shows task count alongside cost */}
          <Card>
            <CardBody>
              <p className="text-xs text-slate-500 uppercase tracking-wide mb-3">Activity by agent</p>
              {agentRows.length === 0 ? (
                <p className="text-xs text-slate-600">No agents have run tasks yet.</p>
              ) : (
                <table className="w-full text-sm">
                  <thead>
                    <tr>
                      <th className="pb-2 text-left text-xs text-slate-600 font-normal">Agent</th>
                      <th className="pb-2 text-right text-xs text-slate-600 font-normal">Tasks</th>
                      <th className="pb-2 text-right text-xs text-slate-600 font-normal">Cost</th>
                    </tr>
                  </thead>
                  <tbody>
                    {agentRows.map(a => (
                      <tr key={a.id} className="border-b border-slate-800 last:border-0">
                        <td className="py-2 text-slate-300">{a.name}</td>
                        <td className="py-2 text-right text-slate-400">{a.task_count}</td>
                        <td className="py-2 text-right font-mono text-white">{a.total_cost_usd > 0 ? fmt(a.total_cost_usd) : <span className="text-slate-600">—</span>}</td>
                      </tr>
                    ))}
                  </tbody>
                  {hasSpend && (
                    <tfoot>
                      <tr>
                        <td className="pt-3 text-xs text-slate-500" colSpan={2}>Total</td>
                        <td className="pt-3 text-right font-mono text-violet-400 font-semibold">{fmt(costs.total_cost_usd)}</td>
                      </tr>
                    </tfoot>
                  )}
                </table>
              )}
            </CardBody>
          </Card>

          {/* By project/monitor */}
          <Card>
            <CardBody>
              <p className="text-xs text-slate-500 uppercase tracking-wide mb-3">Cost by project</p>
              {projectsWithCost.length === 0 ? (
                <p className="text-xs text-slate-600">No per-project cost data.</p>
              ) : (
                <table className="w-full text-sm">
                  <tbody>
                    {projectsWithCost.map(p => (
                      <tr key={p.id} className="border-b border-slate-800 last:border-0">
                        <td className="py-2 text-slate-300">{p.name}</td>
                        <td className="py-2 text-right font-mono text-white">{fmt(p.total_cost_usd)}</td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              )}

              {/* Daily breakdown — only when we have multiple days */}
              {showDaily && (
                <>
                  <p className="text-xs text-slate-500 uppercase tracking-wide mt-4 mb-2">Daily spend</p>
                  <table className="w-full text-sm">
                    <tbody>
                      {dailyRows.map(d => (
                        <tr key={d.date} className="border-b border-slate-800 last:border-0">
                          <td className="py-1.5 text-slate-400 font-mono text-xs">{d.date}</td>
                          <td className="py-1.5 text-right font-mono text-white text-xs">{fmt(d.cost_usd)}</td>
                        </tr>
                      ))}
                    </tbody>
                  </table>
                </>
              )}
            </CardBody>
          </Card>
        </div>
    </div>
  )
}

export function DashboardPage() {
  const navigate = useNavigate()
  const [projects, setProjects] = useState<Project[]>([])
  const [agents, setAgents] = useState<Agent[]>([])
  const [teams, setTeams] = useState<Team[]>([])
  const [recentTasks, setRecentTasks] = useState<Task[]>([])
  const [runningTasks, setRunningTasks] = useState<Task[]>([])
  const [attentionCount, setAttentionCount] = useState(0)
  const [costs, setCosts] = useState<CostsResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [selectedTask, setSelectedTask] = useState<Task | null>(null)

  const load = useCallback(async () => {
    try {
      const [projs, agts, tms] = await Promise.all([api.projects.list(), api.agents.list(), api.teams.list()])
      setProjects(projs)
      setAgents(agts)
      setTeams(tms)

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
    const unsub = phoenixWS.on((ev) => {
      if (ev.type === 'task.status_changed') load()
    })
    return unsub
  }, [load])

  

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
                  <RunningTaskCard key={t.id} task={t} agents={agents} projects={projects} onCancel={load} />
                )),
                ...queued.map((t, i) => (
                  <RunningTaskCard key={t.id} task={t} queuePos={i + 1} agents={agents} projects={projects} onCancel={load} />
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
