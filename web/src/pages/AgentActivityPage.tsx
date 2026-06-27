import { useState, useEffect, useCallback } from 'react'
import { useParams, Link } from 'react-router-dom'
import { api, type Task, type Agent, type Project } from '@/lib/api'
import { phoenixWS } from '@/lib/ws'
import { Card, CardBody } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Modal } from '@/components/ui/modal'
import { EmptyState } from '@/components/ui/empty'
import { taskStatusVariant, taskStatusLabel, parseOutput, formatCost, timeAgo } from '@/lib/utils'
import { MarkdownOutput } from '@/components/ui/markdown-output'
import { FollowUpThread } from '@/components/ui/follow-up-thread'

type StatusFilter = 'all' | 'running' | 'queued' | 'completed' | 'failed' | 'awaiting_approval'

const FILTERS: { id: StatusFilter; label: string }[] = [
  { id: 'all', label: 'All' },
  { id: 'running', label: 'Running' },
  { id: 'queued', label: 'Queued' },
  { id: 'completed', label: 'Completed' },
  { id: 'failed', label: 'Failed' },
  { id: 'awaiting_approval', label: 'Needs Attention' },
]

const SANDBOX_PROJECT_ID = '00000000-0000-0000-0000-000000000002'

function TaskDetailModal({ task, agents, projects, onRefresh, onClose }: {
  task: Task; agents: Agent[]; projects: Project[]; onRefresh: () => void; onClose: () => void
}) {
  const project = projects.find(p => p.id === task.project_id)
  const output = parseOutput(task.output)
  const [retrying, setRetrying] = useState(false)
  const [cancelling, setCancelling] = useState(false)

  const retry = async () => {
    setRetrying(true)
    try { await api.tasks.retry(task.id); onRefresh() } finally { setRetrying(false) }
  }

  const cancel = async () => {
    setCancelling(true)
    try { await api.tasks.cancel(task.id); onRefresh() } finally { setCancelling(false) }
  }

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-2 gap-3 text-sm">
        <div>
          <p className="text-slate-500 text-xs mb-0.5">Project</p>
          {task.project_id === SANDBOX_PROJECT_ID ? (
            <span className="text-slate-400 text-sm">✦ Quick Task</span>
          ) : (
            <Link to={`/projects/${task.project_id}`} className="text-violet-400 hover:underline" onClick={onClose}>
              {project?.name ?? 'Unknown'}
            </Link>
          )}
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
        {task.project_id !== SANDBOX_PROJECT_ID && (
          <Link to={`/projects/${task.project_id}`} onClick={onClose}>
            <Button variant="secondary" size="sm">View Project →</Button>
          </Link>
        )}
        {(task.status === 'running' || task.status === 'queued') && (
          <Button size="sm" variant="secondary" onClick={cancel} disabled={cancelling}>
            {cancelling ? 'Cancelling…' : '✕ Cancel'}
          </Button>
        )}
        {task.status === 'failed' && (
          <Button size="sm" onClick={retry} disabled={retrying}>{retrying ? 'Retrying…' : '↺ Retry'}</Button>
        )}
      </div>
      <FollowUpThread task={task} agents={agents} onSent={onRefresh} />
    </div>
  )
}

export function AgentActivityPage() {
  const { id } = useParams<{ id: string }>()
  const [agent, setAgent] = useState<Agent | null>(null)
  const [tasks, setTasks] = useState<Task[]>([])
  const [agents, setAgents] = useState<Agent[]>([])
  const [projects, setProjects] = useState<Project[]>([])
  const [loading, setLoading] = useState(true)
  const [filter, setFilter] = useState<StatusFilter>('all')
  const [selectedTaskId, setSelectedTaskId] = useState<string | null>(null)

  const load = useCallback(async () => {
    if (!id) return
    try {
      const [agentData, ts, agts, projs] = await Promise.all([
        api.agents.get(id),
        api.tasks.listByAgent(id),
        api.agents.list(),
        api.projects.list(),
      ])
      setAgent(agentData)
      setTasks(ts)
      setAgents(agts)
      setProjects(projs)
    } finally { setLoading(false) }
  }, [id])

  useEffect(() => {
    const timer = window.setTimeout(() => {
      void load()
    }, 0)
    const unsub = phoenixWS.on((ev) => {
      if (ev.type === 'task.status_changed') load()
    })
    return () => {
      clearTimeout(timer)
      unsub()
    }
  }, [load])

  const filtered = tasks.filter(t => filter === 'all' || t.status === filter)
  const counts: Record<string, number> = {}
  for (const t of tasks) counts[t.status] = (counts[t.status] ?? 0) + 1
  const projectMap = Object.fromEntries(projects.map(p => [p.id, p]))
  const selectedTask = selectedTaskId ? tasks.find(t => t.id === selectedTaskId) ?? null : null

  const totalCost = tasks.reduce((sum, t) => sum + (t.cost_usd ?? 0), 0)

  if (loading) {
    return <div className="text-slate-400 py-12 text-center">Loading…</div>
  }

  if (!agent) {
    return (
      <div className="space-y-4">
        <Link to="/settings?tab=agents" className="text-slate-400 hover:text-white text-sm">← Back to Agents</Link>
        <p className="text-slate-400">Agent not found.</p>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center gap-4">
        <Link to="/settings?tab=agents" className="text-slate-400 hover:text-white text-sm">← Agents</Link>
        <div className="flex items-center gap-3">
          <div className="w-9 h-9 rounded-lg bg-violet-900/50 border border-violet-800/50 flex items-center justify-center text-violet-400 font-bold text-sm">
            {agent.name.charAt(0).toUpperCase()}
          </div>
          <div>
            <h1 className="text-2xl font-bold text-white">{agent.name}</h1>
            <p className="text-sm text-slate-500">Activity log · {tasks.length} task{tasks.length !== 1 ? 's' : ''}{totalCost > 0 ? ` · ${formatCost(totalCost)} total` : ''}</p>
          </div>
        </div>
      </div>

      {/* Status filter tabs */}
      <div className="flex gap-1 flex-wrap">
        {FILTERS.map(f => (
          <button
            key={f.id}
            onClick={() => setFilter(f.id)}
            className={`px-3 py-1.5 rounded-lg text-sm font-medium transition-colors ${
              filter === f.id
                ? 'bg-violet-600 text-white'
                : 'text-slate-400 hover:text-white hover:bg-slate-800'
            }`}
          >
            {f.label}
            {f.id !== 'all' && counts[f.id] ? (
              <span className="ml-1.5 text-xs opacity-75">{counts[f.id]}</span>
            ) : null}
            {f.id === 'all' && tasks.length > 0 ? (
              <span className="ml-1.5 text-xs opacity-75">{tasks.length}</span>
            ) : null}
          </button>
        ))}
      </div>

      {/* Task list */}
      {filtered.length === 0 ? (
        <EmptyState icon="📋" title="No tasks" description={filter === 'all' ? 'This agent has no tasks yet.' : `No ${filter} tasks.`} />
      ) : (
        <div className="space-y-2">
          {filtered.map(task => {
            const project = task.project_id === SANDBOX_PROJECT_ID ? null : projectMap[task.project_id]
            return (
              <Card key={task.id}>
                <CardBody className="py-3">
                  <div className="cursor-pointer" onClick={() => setSelectedTaskId(task.id)}>
                  <div className="flex items-center gap-3">
                    <Badge variant={taskStatusVariant(task.status)} className="flex-shrink-0">
                      {taskStatusLabel(task.status)}
                    </Badge>
                    <div className="flex-1 min-w-0">
                      <p className="text-sm text-white truncate">{task.title}</p>
                      <p className="text-xs text-slate-500 mt-0.5">
                        {project ? (
                          <Link to={`/projects/${task.project_id}`} className="text-violet-400 hover:underline" onClick={e => e.stopPropagation()}>
                            {project.name}
                          </Link>
                        ) : (
                          <span>✦ Quick Task</span>
                        )}
                        <span className="mx-1.5">·</span>
                        {timeAgo(task.created_at)}
                      </p>
                    </div>
                    <div className="text-right flex-shrink-0 space-y-0.5">
                      {task.cost_usd > 0 && (
                        <p className="text-xs text-slate-400">{formatCost(task.cost_usd)}</p>
                      )}
                      {(task.tokens_in > 0 || task.tokens_out > 0) && (
                        <p className="text-xs text-slate-600 font-mono">↑{task.tokens_in.toLocaleString()} ↓{task.tokens_out.toLocaleString()}</p>
                      )}
                    </div>
                  </div>
                  </div>
                </CardBody>
              </Card>
            )
          })}
        </div>
      )}

      {selectedTask && (
        <Modal
          title={selectedTask.title}
          onClose={() => setSelectedTaskId(null)}
          className="max-w-2xl"
        >
          <TaskDetailModal
            task={selectedTask}
            agents={agents}
            projects={projects}
            onRefresh={load}
            onClose={() => setSelectedTaskId(null)}
          />
        </Modal>
      )}
    </div>
  )
}
