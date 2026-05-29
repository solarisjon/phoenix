import { useState, useEffect, useCallback } from 'react'
import { useParams, Link } from 'react-router-dom'
import { api, type Project, type Agent, type Task } from '@/lib/api'
import { phoenixWS } from '@/lib/ws'
import { Card, CardHeader, CardBody } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Modal } from '@/components/ui/modal'
import { Input, Textarea, Select, Label } from '@/components/ui/input'
import { EmptyState } from '@/components/ui/empty'
import { taskStatusVariant, taskStatusLabel, parseOutput, formatCost, timeAgo } from '@/lib/utils'

function TaskForm({ projectId, agents, onSave, onClose }: {
  projectId: string; agents: Agent[]; onSave: () => void; onClose: () => void
}) {
  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')
  const [agentId, setAgentId] = useState(agents[0]?.id ?? '')
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)

  const save = async () => {
    if (!title.trim()) { setError('Title is required'); return }
    if (!agentId) { setError('Select an agent'); return }
    setSaving(true)
    try {
      await api.tasks.create({ project_id: projectId, agent_id: agentId, title, description })
      onSave()
    } catch (e: any) { setError(e.message) }
    finally { setSaving(false) }
  }

  return (
    <div className="space-y-4">
      <div>
        <Label htmlFor="title">Task Title</Label>
        <Input id="title" value={title} onChange={e => setTitle(e.target.value)} placeholder="e.g. Research OKR best practices" />
      </div>
      <div>
        <Label htmlFor="agent">Assign to Agent</Label>
        <Select id="agent" value={agentId} onChange={e => setAgentId(e.target.value)}>
          <option value="">Select agent…</option>
          {agents.map(a => <option key={a.id} value={a.id}>{a.name}</option>)}
        </Select>
      </div>
      <div>
        <Label htmlFor="desc">Description</Label>
        <Textarea id="desc" value={description} onChange={e => setDescription(e.target.value)} rows={4}
          placeholder="Detailed instructions for the agent…" />
      </div>
      {error && <p className="text-sm text-red-400">{error}</p>}
      <div className="flex gap-3 justify-end pt-2">
        <Button variant="secondary" onClick={onClose}>Cancel</Button>
        <Button onClick={save} disabled={saving}>{saving ? 'Running…' : 'Create & Run Task'}</Button>
      </div>
    </div>
  )
}

function TaskCard({ task, agents, onUpdate }: { task: Task; agents: Agent[]; onUpdate: () => void }) {
  const [expanded, setExpanded] = useState(false)
  const [stream, setStream] = useState('')
  const [retrying, setRetrying] = useState(false)
  const agent = agents.find(a => a.id === task.agent_id)

  useEffect(() => {
    if (task.status !== 'running') { setStream(''); return }
    const unsub = phoenixWS.on((ev) => {
      if (ev.type === 'task.output_stream') {
        const p = ev.payload as any
        if (p.task_id === task.id) setStream(prev => prev + p.chunk)
      }
      if (ev.type === 'task.status_changed') {
        const p = ev.payload as any
        if (p.task_id === task.id) onUpdate()
      }
    })
    return unsub
  }, [task.id, task.status, onUpdate])

  const retry = async () => {
    setRetrying(true)
    try { await api.tasks.retry(task.id); onUpdate() } finally { setRetrying(false) }
  }

  const output = task.status === 'running' && stream ? stream : parseOutput(task.output)
  const showOutput = expanded && (output && output !== '{}')

  return (
    <div className="border border-slate-800 rounded-xl bg-slate-900/50">
      <div className="flex items-start gap-3 p-4 cursor-pointer" onClick={() => setExpanded(!expanded)}>
        <div className="flex-1 min-w-0">
          <div className="flex items-center gap-2 flex-wrap mb-1">
            <h4 className="text-sm font-medium text-white">{task.title}</h4>
            <Badge variant={taskStatusVariant(task.status)}>{taskStatusLabel(task.status)}</Badge>
            {task.cost_usd > 0 && (
              <span className="text-xs text-slate-500">{formatCost(task.cost_usd)}</span>
            )}
          </div>
          <p className="text-xs text-slate-500">
            {agent?.name ?? 'Unknown'} · {timeAgo(task.created_at)}
          </p>
          {task.status === 'running' && (
            <div className="flex items-center gap-1.5 mt-2">
              <span className="w-1.5 h-1.5 rounded-full bg-violet-500 animate-pulse" />
              <span className="text-xs text-violet-400">Running…</span>
            </div>
          )}
          {task.status === 'failed' && (
            <div className="mt-2" onClick={e => e.stopPropagation()}>
              <Button size="sm" variant="secondary" onClick={retry} disabled={retrying}>
                {retrying ? 'Retrying…' : '↺ Retry'}
              </Button>
            </div>
          )}
        </div>
        <span className="text-slate-600 text-sm mt-0.5">{expanded ? '▲' : '▼'}</span>
      </div>

      {showOutput && (
        <div className="px-4 pb-4 border-t border-slate-800 pt-3">
          <pre className="text-xs text-slate-300 whitespace-pre-wrap font-mono bg-slate-950 rounded-lg p-3 max-h-64 overflow-y-auto">
            {output}
          </pre>
        </div>
      )}
    </div>
  )
}

function AssignAgentModal({ projectId, allAgents, assigned, onSave, onClose }: {
  projectId: string; allAgents: Agent[]; assigned: Agent[]; onSave: () => void; onClose: () => void
}) {
  const assignedIds = new Set(assigned.map(a => a.id))
  const available = allAgents.filter(a => !assignedIds.has(a.id))
  const [selected, setSelected] = useState(available[0]?.id ?? '')
  const [saving, setSaving] = useState(false)

  const save = async () => {
    if (!selected) return
    setSaving(true)
    try { await api.projects.assignAgent(projectId, selected); onSave() }
    catch { /* ignore */ }
    finally { setSaving(false) }
  }

  return (
    <div className="space-y-4">
      {available.length === 0 ? (
        <p className="text-slate-400 text-sm">All agents are already assigned. <Link to="/agents" className="text-violet-400 hover:underline">Create a new agent</Link>.</p>
      ) : (
        <>
          <div>
            <Label htmlFor="agent">Select Agent</Label>
            <Select id="agent" value={selected} onChange={e => setSelected(e.target.value)}>
              {available.map(a => <option key={a.id} value={a.id}>{a.name}</option>)}
            </Select>
          </div>
          <div className="flex gap-3 justify-end">
            <Button variant="secondary" onClick={onClose}>Cancel</Button>
            <Button onClick={save} disabled={saving || !selected}>{saving ? 'Assigning…' : 'Assign Agent'}</Button>
          </div>
        </>
      )}
    </div>
  )
}

export function ProjectDetailPage() {
  const { id } = useParams<{ id: string }>()
  const [project, setProject] = useState<Project | null>(null)
  const [agents, setAgents] = useState<Agent[]>([])
  const [allAgents, setAllAgents] = useState<Agent[]>([])
  const [tasks, setTasks] = useState<Task[]>([])
  const [loading, setLoading] = useState(true)
  const [showTaskForm, setShowTaskForm] = useState(false)
  const [showAssignAgent, setShowAssignAgent] = useState(false)

  const load = useCallback(async () => {
    if (!id) return
    try {
      const [proj, agts, allAgts, tsks] = await Promise.all([
        api.projects.get(id),
        api.projects.listAgents(id),
        api.agents.list(),
        api.tasks.list(id),
      ])
      setProject(proj)
      setAgents(agts)
      setAllAgents(allAgts)
      setTasks(tsks)
    } finally { setLoading(false) }
  }, [id])

  useEffect(() => { load() }, [load])

  const removeAgent = async (agentId: string) => {
    if (!id) return
    await api.projects.removeAgent(id, agentId)
    load()
  }

  const totalCost = tasks.reduce((s, t) => s + t.cost_usd, 0)

  if (loading) return <div className="text-slate-500 text-sm">Loading…</div>
  if (!project) return <div className="text-slate-500 text-sm">Project not found.</div>

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-start justify-between">
        <div>
          <div className="flex items-center gap-2 mb-1">
            <Link to="/projects" className="text-slate-500 hover:text-white text-sm transition-colors">Projects</Link>
            <span className="text-slate-700">/</span>
            <span className="text-white text-sm">{project.name}</span>
          </div>
          <h1 className="text-2xl font-bold text-white">{project.name}</h1>
          {project.description && <p className="text-slate-400 text-sm mt-1">{project.description}</p>}
        </div>
        <div className="flex items-center gap-3">
          {totalCost > 0 && (
            <span className="text-sm text-slate-400">Total: <span className="text-white font-medium">{formatCost(totalCost)}</span></span>
          )}
          <Button onClick={() => setShowTaskForm(true)} disabled={agents.length === 0}>+ New Task</Button>
        </div>
      </div>

      <div className="grid grid-cols-3 gap-6">
        {/* Agents column */}
        <div className="col-span-1 space-y-3">
          <div className="flex items-center justify-between">
            <h2 className="text-sm font-medium text-slate-400 uppercase tracking-wide">Agents</h2>
            <Button variant="ghost" size="sm" onClick={() => setShowAssignAgent(true)}>+ Assign</Button>
          </div>
          {agents.length === 0 ? (
            <p className="text-slate-500 text-xs">No agents assigned. <button className="text-violet-400 hover:underline" onClick={() => setShowAssignAgent(true)}>Add one</button>.</p>
          ) : agents.map(a => (
            <Card key={a.id}>
              <CardBody className="py-3">
                <div className="flex items-center justify-between">
                  <div className="flex items-center gap-2 min-w-0">
                    <div className="w-7 h-7 rounded-md bg-violet-900/50 border border-violet-800/50 flex items-center justify-center text-violet-400 font-bold text-xs flex-shrink-0">
                      {a.name.charAt(0)}
                    </div>
                    <div className="min-w-0">
                      <p className="text-sm font-medium text-white truncate">{a.name}</p>
                      <Badge variant={a.status === 'active' ? 'success' : 'muted'} className="mt-0.5">{a.status}</Badge>
                    </div>
                  </div>
                  <Button variant="ghost" size="sm" onClick={() => removeAgent(a.id)}>✕</Button>
                </div>
              </CardBody>
            </Card>
          ))}
        </div>

        {/* Tasks column */}
        <div className="col-span-2 space-y-3">
          <div className="flex items-center justify-between">
            <h2 className="text-sm font-medium text-slate-400 uppercase tracking-wide">
              Tasks <span className="text-slate-600 font-normal normal-case">({tasks.length})</span>
            </h2>
          </div>
          {tasks.length === 0 ? (
            <EmptyState icon="◈" title="No tasks yet"
              description="Create a task to assign work to an agent."
              action={agents.length > 0
                ? <Button size="sm" onClick={() => setShowTaskForm(true)}>New Task</Button>
                : <p className="text-xs text-slate-500">Assign an agent first</p>
              } />
          ) : (
            <div className="space-y-2">
              {tasks.map(t => (
                <TaskCard key={t.id} task={t} agents={allAgents} onUpdate={load} />
              ))}
            </div>
          )}
        </div>
      </div>

      {showTaskForm && (
        <Modal title="New Task" onClose={() => setShowTaskForm(false)}>
          <TaskForm projectId={project.id} agents={agents}
            onSave={() => { setShowTaskForm(false); load() }}
            onClose={() => setShowTaskForm(false)} />
        </Modal>
      )}

      {showAssignAgent && (
        <Modal title="Assign Agent" onClose={() => setShowAssignAgent(false)}>
          <AssignAgentModal projectId={project.id} allAgents={allAgents} assigned={agents}
            onSave={() => { setShowAssignAgent(false); load() }}
            onClose={() => setShowAssignAgent(false)} />
        </Modal>
      )}
    </div>
  )
}
