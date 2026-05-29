import { useState, useEffect, useCallback } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { api, type Project, type Task, type CostsResponse, type Agent } from '@/lib/api'
import { phoenixWS } from '@/lib/ws'
import { Card, CardBody, CardHeader } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Modal } from '@/components/ui/modal'
import { taskStatusVariant, taskStatusLabel, parseOutput, formatCost, timeAgo } from '@/lib/utils'

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
function RunningTaskCard({ task, agents, projects }: { task: Task; agents: Agent[]; projects: Project[] }) {
  const [stream, setStream] = useState('')
  const agent = agents.find(a => a.id === task.agent_id)
  const project = projects.find(p => p.id === task.project_id)

  useEffect(() => {
    const unsub = phoenixWS.on((ev) => {
      if (ev.type === 'task.output_stream') {
        const p = ev.payload as any
        if (p.task_id === task.id) setStream(prev => prev + p.chunk)
      }
    })
    return unsub
  }, [task.id])

  const preview = stream || parseOutput(task.output)

  return (
    <Card className="border-violet-900/40">
      <CardBody className="py-3 px-4">
        <div className="flex items-start justify-between gap-3 mb-2">
          <div className="flex-1 min-w-0">
            <div className="flex items-center gap-2 mb-0.5">
              <span className="w-1.5 h-1.5 rounded-full bg-violet-500 animate-pulse flex-shrink-0" />
              <p className="text-sm font-medium text-white truncate">{task.title}</p>
            </div>
            <p className="text-xs text-slate-500">
              {project ? <Link to={`/projects/${project.id}`} className="text-violet-400 hover:underline">{project.name}</Link> : ''}
              {project && agent ? ' · ' : ''}
              {agent?.name ?? ''}
            </p>
          </div>
          <Badge variant="info">{task.status}</Badge>
        </div>
        {preview && (
          <pre className="text-xs text-slate-400 font-mono bg-slate-950 rounded p-2 max-h-20 overflow-hidden whitespace-pre-wrap line-clamp-4">
            {preview}
          </pre>
        )}
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

  const retry = async () => {
    setRetrying(true)
    try { await api.tasks.retry(task.id); onRetry() } finally { setRetrying(false) }
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
      </div>
      {task.description && (
        <div>
          <p className="text-slate-500 text-xs mb-1">Description</p>
          <pre className="text-xs text-slate-300 whitespace-pre-wrap font-mono bg-slate-800 rounded-lg p-3 max-h-32 overflow-y-auto">{task.description}</pre>
        </div>
      )}
      <div>
        <p className="text-slate-500 text-xs mb-1">Output</p>
        <pre className="text-xs text-slate-300 whitespace-pre-wrap font-mono bg-slate-950 rounded-lg p-3 max-h-64 overflow-y-auto">{output || '(no output yet)'}</pre>
      </div>
      <div className="flex gap-2 justify-end">
        <Link to={`/projects/${task.project_id}`} onClick={onClose}>
          <Button variant="secondary" size="sm">View Project →</Button>
        </Link>
        {task.status === 'failed' && (
          <Button size="sm" onClick={retry} disabled={retrying}>{retrying ? 'Retrying…' : '↺ Retry'}</Button>
        )}
      </div>
    </div>
  )
}

export function DashboardPage() {
  const navigate = useNavigate()
  const [projects, setProjects] = useState<Project[]>([])
  const [agents, setAgents] = useState<Agent[]>([])
  const [recentTasks, setRecentTasks] = useState<Task[]>([])
  const [runningTasks, setRunningTasks] = useState<Task[]>([])
  const [attentionCount, setAttentionCount] = useState(0)
  const [costs, setCosts] = useState<CostsResponse | null>(null)
  const [loading, setLoading] = useState(true)
  const [selectedTask, setSelectedTask] = useState<Task | null>(null)
  const [showRunning, setShowRunning] = useState(false)

  const load = useCallback(async () => {
    try {
      const [projs, agts] = await Promise.all([api.projects.list(), api.agents.list()])
      setProjects(projs)
      setAgents(agts)

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

  const runningCount = runningTasks.length

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
          value={String(runningCount)}
          sub={runningCount > 0 ? 'View live →' : undefined}
          onClick={runningCount > 0 ? () => setShowRunning(true) : undefined}
        />
        <StatCard
          label="Needs Attention"
          value={String(attentionCount)}
          sub={attentionCount > 0 ? 'Go to inbox →' : undefined}
          accent={attentionCount > 0 ? 'text-amber-400' : undefined}
          href={attentionCount > 0 ? '/inbox' : undefined}
        />
        <StatCard label="Total Cost" value={costs ? formatCost(costs.total_cost_usd) : '—'} />
      </div>

      {/* Running tasks panel */}
      {showRunning && runningTasks.length > 0 && (
        <div>
          <div className="flex items-center justify-between mb-3">
            <h2 className="text-sm font-semibold text-slate-300 uppercase tracking-wide flex items-center gap-2">
              <span className="w-2 h-2 rounded-full bg-violet-500 animate-pulse" />
              Live Tasks ({runningTasks.length})
            </h2>
            <button className="text-xs text-slate-500 hover:text-slate-300" onClick={() => setShowRunning(false)}>Hide ✕</button>
          </div>
          <div className="grid grid-cols-2 gap-3">
            {runningTasks.map(t => (
              <RunningTaskCard key={t.id} task={t} agents={agents} projects={projects} />
            ))}
          </div>
        </div>
      )}

      <div className="grid grid-cols-3 gap-6">
        {/* Projects */}
        <div className="col-span-1 space-y-3">
          <div className="flex items-center justify-between">
            <h2 className="text-sm font-medium text-slate-400 uppercase tracking-wide">Projects</h2>
            <Link to="/projects" className="text-xs text-violet-400 hover:text-violet-300">View all →</Link>
          </div>
          {loading ? (
            <p className="text-slate-500 text-sm">Loading…</p>
          ) : projects.length === 0 ? (
            <Card>
              <CardBody className="py-8 text-center">
                <p className="text-slate-500 text-sm mb-3">No projects yet</p>
                <Link to="/projects" className="text-violet-400 text-xs hover:underline">Create one →</Link>
              </CardBody>
            </Card>
          ) : projects.slice(0, 5).map(p => (
            <Link key={p.id} to={`/projects/${p.id}`}>
              <Card className="hover:border-slate-700 transition-colors cursor-pointer">
                <CardBody className="py-3">
                  <p className="text-sm font-medium text-white truncate">{p.name}</p>
                  <div className="flex items-center gap-2 mt-1">
                    <Badge variant={p.status === 'active' ? 'success' : 'muted'}>{p.status}</Badge>
                    <span className="text-xs text-slate-600">{timeAgo(p.created_at)}</span>
                  </div>
                </CardBody>
              </Card>
            </Link>
          ))}
        </div>

        {/* Recent Tasks */}
        <div className="col-span-2 space-y-3">
          <div className="flex items-center justify-between">
            <h2 className="text-sm font-medium text-slate-400 uppercase tracking-wide">Recent Activity</h2>
          </div>
          {loading ? (
            <p className="text-slate-500 text-sm">Loading…</p>
          ) : recentTasks.length === 0 ? (
            <Card>
              <CardBody className="py-12 text-center">
                <div className="text-3xl mb-3">✦</div>
                <p className="text-white font-medium mb-2">Ready to orchestrate</p>
                <p className="text-slate-400 text-sm mb-4">Configure a provider, create an agent, then start a project.</p>
                <div className="flex gap-3 justify-center">
                  <Link to="/providers" className="bg-violet-600 hover:bg-violet-500 text-white text-sm font-medium px-4 py-2 rounded-lg transition-colors">
                    Add Provider
                  </Link>
                  <Link to="/agents" className="bg-slate-800 hover:bg-slate-700 text-white text-sm font-medium px-4 py-2 rounded-lg transition-colors">
                    Create Agent
                  </Link>
                </div>
              </CardBody>
            </Card>
          ) : (
            <Card>
              <CardHeader>
                <p className="text-sm font-medium text-slate-300">Task Activity</p>
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
