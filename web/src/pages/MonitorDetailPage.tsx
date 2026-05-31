import { useState, useEffect, useCallback, useRef } from 'react'
import { useParams, Link, useNavigate } from 'react-router-dom'
import { api, type Project, type Agent, type Task } from '@/lib/api'
import { phoenixWS } from '@/lib/ws'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Modal } from '@/components/ui/modal'
import { Input, Textarea, Label } from '@/components/ui/input'
import { EmptyState } from '@/components/ui/empty'
import { MarkdownOutput } from '@/components/ui/markdown-output'
import { taskStatusVariant, taskStatusLabel, parseOutput, formatCost, timeAgo } from '@/lib/utils'
import { AgentsSection } from '@/components/shared/AgentsSection'

// ---- Countdown clock ----

function Countdown({ agent, tasks }: { agent: Agent; tasks: Task[] }) {
  const [remaining, setRemaining] = useState<number | null>(null)

  useEffect(() => {
    if (!agent.heartbeat_interval) return
    const interval = agent.heartbeat_interval * 1000

    const calc = () => {
      const heartbeats = tasks
        .filter(t => t.title.startsWith('Heartbeat'))
        .sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime())
      if (!heartbeats.length) { setRemaining(null); return }
      const last = new Date(heartbeats[0].created_at).getTime()
      const next = last + interval
      setRemaining(Math.max(0, next - Date.now()))
    }

    calc()
    const timer = setInterval(calc, 1000)
    return () => clearInterval(timer)
  }, [agent, tasks])

  if (remaining === null) return <span className="text-slate-500 text-sm">No runs yet</span>
  if (remaining === 0) return <span className="text-violet-400 text-sm animate-pulse">Firing soon…</span>

  const totalSecs = Math.floor(remaining / 1000)
  const m = Math.floor(totalSecs / 60)
  const s = totalSecs % 60
  const display = m > 0 ? `${m}m ${s}s` : `${s}s`

  return (
    <span className="text-slate-300 text-sm font-mono">Next run in {display}</span>
  )
}

// ---- Run card ----

function RunCard({ task, agent }: { task: Task; agent?: Agent }) {
  const [expanded, setExpanded] = useState(false)
  const output = parseOutput(task.output)

  const duration = task.started_at && task.completed_at
    ? Math.round((new Date(task.completed_at).getTime() - new Date(task.started_at).getTime()) / 1000)
    : null

  return (
    <div className="bg-slate-900 border border-slate-800 rounded-xl overflow-hidden">
      <div className="flex items-center gap-4 px-4 py-3">
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-3 flex-wrap">
            <Badge variant={taskStatusVariant(task.status)}>{taskStatusLabel(task.status)}</Badge>
            <span className="text-sm text-slate-300">{task.title}</span>
            {task.source && (
              <span className="text-xs text-slate-500">↳ {task.source}</span>
            )}
          </div>
          <div className="flex items-center gap-3 mt-1 text-xs text-slate-500">
            <span>{timeAgo(task.created_at)}</span>
            {duration !== null && <span>{duration}s</span>}
            {task.cost_usd > 0 && <span>{formatCost(task.cost_usd)}</span>}
            {agent && <span>{agent.name}</span>}
          </div>
        </div>
        {output && (
          <button
            onClick={() => setExpanded(e => !e)}
            className="text-xs text-slate-500 hover:text-slate-300 transition-colors shrink-0"
          >
            {expanded ? 'Hide ▲' : 'Show output ▼'}
          </button>
        )}
        {!output && task.status === 'running' && (
          <span className="text-xs text-violet-400 animate-pulse flex items-center gap-1">
            <span className="w-1.5 h-1.5 bg-violet-500 rounded-full animate-ping" />
            Running…
          </span>
        )}
      </div>
      {expanded && output && (
        <div className="border-t border-slate-800 px-4 py-3 bg-slate-950 max-h-96 overflow-y-auto">
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
  const [agents, setAgents] = useState<Agent[]>([])
  const [allAgents, setAllAgents] = useState<Agent[]>([])
  const [tasks, setTasks] = useState<Task[]>([])
  const [loading, setLoading] = useState(true)
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [triggering, setTriggering] = useState(false)
  const [triggerError, setTriggerError] = useState('')
  const [showEdit, setShowEdit] = useState(false)
  const [editName, setEditName] = useState('')
  const [editDesc, setEditDesc] = useState('')
  const [editWorkingDir, setEditWorkingDir] = useState('')
  const [saving, setSaving] = useState(false)
  const [saveError, setSaveError] = useState('')

  const load = useCallback(async () => {
    if (!id) return
    try {
      const [proj, agts, allAgts, tsks] = await Promise.all([
        api.projects.get(id),
        api.projects.listAgents(id),
        api.agents.list(),
        api.tasks.list(id),
      ])
      setMonitor(proj)
      setAgents(agts)
      setAllAgents(allAgts)
      setTasks(tsks)
    } finally {
      setLoading(false)
    }
  }, [id])

  useEffect(() => { load() }, [load])

  useEffect(() => {
    const unsub = phoenixWS.on((ev) => {
      if (
        ev.type === 'task.status_changed' ||
        ev.type === 'task.output_stream'
      ) load()
    })
    return unsub
  }, [load])

  const primaryAgent = agents.find(a => (a.heartbeat_interval ?? 0) > 0)

  const formatInterval = (secs: number | null) => {
    if (!secs) return null
    if (secs < 60) return `every ${secs}s`
    if (secs < 3600) return `every ${Math.round(secs / 60)} min`
    return `every ${Math.round(secs / 3600)}h`
  }

  const runNow = async () => {
    if (!primaryAgent || !id) return
    setTriggering(true)
    setTriggerError('')
    try {
      await api.tasks.create({
        project_id: id,
        agent_id: primaryAgent.id,
        title: `Manual run — ${new Date().toLocaleString()}`,
        description: 'Manually triggered run.',
      })
      load()
    } catch (e: unknown) {
      setTriggerError(e instanceof Error ? e.message : 'Failed to trigger run')
    } finally {
      setTriggering(false)
    }
  }

  const openEdit = () => {
    if (!monitor) return
    setEditName(monitor.name)
    setEditDesc(monitor.description ?? '')
    setEditWorkingDir(monitor.working_dir ?? '')
    setSaveError('')
    setShowEdit(true)
  }

  const saveEdit = async () => {
    if (!id || !monitor) return
    if (!editName.trim()) { setSaveError('Name is required'); return }
    setSaving(true)
    setSaveError('')
    try {
      await api.projects.update(id, {
        name: editName.trim(),
        description: editDesc,
        working_dir: editWorkingDir.trim(),
        kind: 'monitor',
      })
      setShowEdit(false)
      load()
    } catch (e: unknown) {
      setSaveError(e instanceof Error ? e.message : 'Failed to save')
    } finally {
      setSaving(false)
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
            <Button
              variant="secondary"
              size="sm"
              onClick={runNow}
              disabled={triggering || !primaryAgent}
            >
              {triggering ? 'Triggering…' : '▶ Run now'}
            </Button>
            <Button variant="secondary" size="sm" onClick={openEdit}>Edit</Button>
            <Button variant="danger" size="sm" onClick={() => setShowDeleteConfirm(true)}>Delete</Button>
          </div>
        </div>

        {/* Agent + schedule info */}
        <div className="flex items-center gap-6 ml-8 flex-wrap">
          {primaryAgent && (
            <div>
              <p className="text-xs text-slate-500 mb-0.5">Agent</p>
              <p className="text-sm text-slate-300">{primaryAgent.name}</p>
            </div>
          )}
          {primaryAgent?.heartbeat_interval && (
            <div>
              <p className="text-xs text-slate-500 mb-0.5">Schedule</p>
              <p className="text-sm text-violet-400 font-medium">
                {formatInterval(primaryAgent.heartbeat_interval)}
              </p>
            </div>
          )}
          {monitor.working_dir && (
            <div>
              <p className="text-xs text-slate-500 mb-0.5">Working dir</p>
              <p className="text-xs text-slate-400 font-mono">{monitor.working_dir}</p>
            </div>
          )}
          <div>
            <p className="text-xs text-slate-500 mb-0.5">Next run</p>
            {primaryAgent
              ? <Countdown agent={primaryAgent} tasks={tasks} />
              : <span className="text-slate-500 text-sm">No heartbeat agent</span>
            }
          </div>
        </div>

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
            description={primaryAgent
              ? `Waiting for the first heartbeat. ${primaryAgent.name} runs ${formatInterval(primaryAgent.heartbeat_interval ?? null)}.`
              : 'Assign a heartbeat agent to start scheduling runs.'
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
        <Modal title="Edit Monitor" onClose={() => setShowEdit(false)}>
          <div className="space-y-4">
            <div>
              <Label htmlFor="edit-name">Name</Label>
              <Input id="edit-name" value={editName} onChange={e => setEditName(e.target.value)} />
            </div>
            <div>
              <Label htmlFor="edit-desc">Description</Label>
              <Textarea id="edit-desc" value={editDesc} onChange={e => setEditDesc(e.target.value)} rows={2} />
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
