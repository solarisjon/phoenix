import { useState, useEffect, useCallback } from 'react'
import { Link } from 'react-router-dom'
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

const SANDBOX_PROJECT_ID = '00000000-0000-0000-0000-000000000002'

type StatusFilter = 'all' | 'running' | 'queued' | 'completed' | 'failed' | 'awaiting_approval'

const FILTERS: { id: StatusFilter; label: string }[] = [
  { id: 'all', label: 'All' },
  { id: 'running', label: 'Running' },
  { id: 'queued', label: 'Queued' },
  { id: 'completed', label: 'Completed' },
  { id: 'failed', label: 'Failed' },
  { id: 'awaiting_approval', label: 'Needs Attention' },
]

function TaskDetailModal({ task, agents, projects, onRetry, onClose }: {
  task: Task; agents: Agent[]; projects: Project[]; onRetry: () => void; onClose: () => void
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
          {task.project_id === SANDBOX_PROJECT_ID ? (
            <span className="text-slate-400 text-sm">✦ Quick Task</span>
          ) : (
            <Link to={`/projects/${task.project_id}`} className="text-violet-400 hover:underline" onClick={onClose}>
              {project?.name ?? 'Unknown'}
            </Link>
          )}
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

export function TasksPage() {
  const [tasks, setTasks] = useState<Task[]>([])
  const [agents, setAgents] = useState<Agent[]>([])
  const [projects, setProjects] = useState<Project[]>([])
  const [loading, setLoading] = useState(true)
  const [filter, setFilter] = useState<StatusFilter>('all')
  const [search, setSearch] = useState('')
  const [selectedTask, setSelectedTask] = useState<Task | null>(null)

  const load = useCallback(async () => {
    try {
      const [ts, agts, projs] = await Promise.all([
        api.tasks.listAll(),
        api.agents.list(),
        api.projects.list(),
      ])
      setTasks(ts)
      setAgents(agts)
      setProjects(projs)
    } finally { setLoading(false) }
  }, [])

  useEffect(() => {
    load()
    const unsub = phoenixWS.on((ev) => {
      if (ev.type === 'task.status_changed') load()
    })
    return unsub
  }, [load])

  const filtered = tasks.filter(t => {
    if (filter !== 'all' && t.status !== filter) return false
    if (search && !t.title.toLowerCase().includes(search.toLowerCase())) return false
    return true
  })

  // Count per status for filter badges
  const counts: Record<string, number> = {}
  for (const t of tasks) counts[t.status] = (counts[t.status] ?? 0) + 1

  const agentMap = Object.fromEntries(agents.map(a => [a.id, a]))
  const projectMap = Object.fromEntries(projects.map(p => [p.id, p]))

  return (
    <div className="space-y-6">
      <div>
        <h1 className="text-2xl font-bold text-white">Tasks</h1>
        <p className="text-slate-400 text-sm mt-1">All tasks across every project</p>
      </div>

      {/* Filters + search */}
      <div className="flex items-center gap-4 flex-wrap">
        <div className="flex gap-1 bg-slate-900 border border-slate-800 rounded-lg p-1">
          {FILTERS.map(f => {
            const count = f.id === 'all'
              ? tasks.length
              : (counts[f.id] ?? 0)
            return (
              <button
                key={f.id}
                onClick={() => setFilter(f.id)}
                className={`px-3 py-1.5 rounded-md text-sm font-medium transition-colors flex items-center gap-1.5 ${
                  filter === f.id
                    ? 'bg-violet-600 text-white'
                    : 'text-slate-400 hover:text-white'
                }`}
              >
                {f.label}
                {count > 0 && (
                  <span className={`text-xs px-1.5 py-0.5 rounded-full font-medium ${
                    filter === f.id ? 'bg-violet-500 text-white' : 'bg-slate-800 text-slate-400'
                  }`}>
                    {count}
                  </span>
                )}
              </button>
            )
          })}
        </div>
        <input
          value={search}
          onChange={e => setSearch(e.target.value)}
          placeholder="Search tasks…"
          className="flex-1 min-w-48 bg-slate-900 border border-slate-800 rounded-lg px-3 py-2 text-sm text-white placeholder-slate-500 focus:outline-none focus:border-violet-500"
        />
      </div>

      {/* Task list */}
      {loading ? (
        <div className="text-slate-500 text-sm">Loading…</div>
      ) : filtered.length === 0 ? (
        <EmptyState
          icon="✦"
          title={tasks.length === 0 ? 'No tasks yet' : 'No matching tasks'}
          description={tasks.length === 0
            ? 'Create a project and assign agents to start running tasks.'
            : 'Try a different filter or search term.'
          }
          action={tasks.length === 0
            ? <Link to="/projects"><Button>Go to Projects</Button></Link>
            : <Button variant="secondary" onClick={() => { setFilter('all'); setSearch('') }}>Clear filters</Button>
          }
        />
      ) : (
        <Card>
          <div className="divide-y divide-slate-800">
            {/* Header row */}
            <div className="grid grid-cols-[1fr_160px_160px_120px_80px_90px] gap-3 px-5 py-2.5 text-xs text-slate-500 uppercase tracking-wide">
              <span>Task</span>
              <span>Project</span>
              <span>Agent</span>
              <span>Status</span>
              <span>Cost</span>
              <span>When</span>
            </div>

            {filtered.map(t => {
              const agent = agentMap[t.agent_id]
              const project = projectMap[t.project_id]
              const isPulse = t.status === 'running' || t.status === 'queued'

              return (
                <button
                  key={t.id}
                  className="w-full grid grid-cols-[1fr_160px_160px_120px_80px_90px] gap-3 px-5 py-3 hover:bg-slate-800/50 transition-colors text-left items-center"
                  onClick={() => setSelectedTask(t)}
                >
                  <div className="flex items-center gap-2 min-w-0">
                    <span className={`w-1.5 h-1.5 rounded-full flex-shrink-0 ${
                      isPulse ? 'bg-violet-500 animate-pulse' :
                      t.status === 'completed' ? 'bg-emerald-500' :
                      t.status === 'failed' ? 'bg-red-500' :
                      t.status === 'awaiting_approval' ? 'bg-amber-500' : 'bg-slate-600'
                    }`} />
                    <span className="text-sm text-white truncate">{t.title}</span>
                  </div>
                  <span className="text-xs text-slate-400 truncate">
                    {t.project_id === SANDBOX_PROJECT_ID ? (
                      <span className="inline-flex items-center gap-1">
                        <span className="text-violet-400">✦</span>
                        <span className="text-slate-500">Quick Task</span>
                      </span>
                    ) : project ? (
                      <Link
                        to={`/projects/${t.project_id}`}
                        className="hover:text-violet-400 transition-colors"
                        onClick={e => e.stopPropagation()}
                      >
                        {project.name}
                      </Link>
                    ) : '—'
                    }
                  </span>
                  <span className="text-xs text-slate-400 truncate">{agent?.name ?? '—'}</span>
                  <Badge variant={taskStatusVariant(t.status)}>{taskStatusLabel(t.status)}</Badge>
                  <span className="text-xs text-slate-500">{t.cost_usd > 0 ? formatCost(t.cost_usd) : '—'}</span>
                  <span className="text-xs text-slate-500">{timeAgo(t.created_at)}</span>
                </button>
              )
            })}
          </div>
          <CardBody className="py-2 border-t border-slate-800">
            <p className="text-xs text-slate-600">
              {filtered.length} task{filtered.length !== 1 ? 's' : ''}
              {filter !== 'all' || search ? ` (filtered from ${tasks.length})` : ''}
            </p>
          </CardBody>
        </Card>
      )}

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
