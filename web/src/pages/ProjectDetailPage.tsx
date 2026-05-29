import { useState, useEffect, useCallback } from 'react'
import { useParams, Link, useNavigate } from 'react-router-dom'
import { api, type Project, type Agent, type Task, type Team } from '@/lib/api'
import { phoenixWS } from '@/lib/ws'
import { Card, CardHeader, CardBody } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Modal } from '@/components/ui/modal'
import { Input, Textarea, Select, Label } from '@/components/ui/input'
import { EmptyState } from '@/components/ui/empty'
import { taskStatusVariant, taskStatusLabel, parseOutput, formatCost, timeAgo } from '@/lib/utils'

function TaskForm({ projectId, allAgents, projectAgents, teams, onSave, onClose }: {
  projectId: string
  allAgents: Agent[]
  projectAgents: Agent[]
  teams: Team[]
  onSave: () => void
  onClose: () => void
}) {
  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')
  const [mode, setMode] = useState<'agent' | 'team'>('agent')
  const [agentId, setAgentId] = useState(allAgents[0]?.id ?? '')
  const [teamId, setTeamId] = useState(teams[0]?.id ?? '')
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)
  const [progress, setProgress] = useState('')

  // Group agents: assigned to project first, then the rest
  const projectAgentIds = new Set(projectAgents.map(a => a.id))
  const assignedAgents = allAgents.filter(a => projectAgentIds.has(a.id))
  const otherAgents = allAgents.filter(a => !projectAgentIds.has(a.id))

  const selectedTeam = teams.find(t => t.id === teamId)

  const save = async () => {
    if (!title.trim()) { setError('Title is required'); return }
    setSaving(true)
    setError('')
    setProgress('')
    try {
      if (mode === 'agent') {
        if (!agentId) { setError('Select an agent'); setSaving(false); return }
        // Auto-assign agent to project if not already there
        if (!projectAgentIds.has(agentId)) {
          await api.projects.assignAgent(projectId, agentId)
        }
        await api.tasks.create({ project_id: projectId, agent_id: agentId, title, description })
      } else {
        // Team mode: create one task per team member
        if (!teamId || !selectedTeam?.agents?.length) {
          setError('Selected team has no members'); setSaving(false); return
        }
        const members = selectedTeam.agents
        for (let i = 0; i < members.length; i++) {
          const a = members[i]
          setProgress(`Creating task ${i + 1}/${members.length} → ${a.name}…`)
          if (!projectAgentIds.has(a.id)) {
            await api.projects.assignAgent(projectId, a.id)
          }
          await api.tasks.create({ project_id: projectId, agent_id: a.id, title, description })
        }
      }
      onSave()
    } catch (e: any) { setError(e.message) }
    finally { setSaving(false); setProgress('') }
  }

  return (
    <div className="space-y-4">
      <div>
        <Label htmlFor="title">Task Title</Label>
        <Input id="title" value={title} onChange={e => setTitle(e.target.value)} placeholder="e.g. Research OKR best practices" />
      </div>

      {/* Mode toggle */}
      <div>
        <Label>Assign to</Label>
        <div className="flex gap-1 mt-1 bg-slate-800 rounded-lg p-1">
          <button
            className={`flex-1 py-1.5 rounded-md text-sm font-medium transition-colors ${
              mode === 'agent' ? 'bg-violet-600 text-white' : 'text-slate-400 hover:text-white'
            }`}
            onClick={() => setMode('agent')}
          >
            Agent
          </button>
          <button
            className={`flex-1 py-1.5 rounded-md text-sm font-medium transition-colors ${
              mode === 'team' ? 'bg-violet-600 text-white' : 'text-slate-400 hover:text-white'
            }`}
            onClick={() => setMode('team')}
            disabled={teams.length === 0}
            title={teams.length === 0 ? 'No teams configured' : undefined}
          >
            Team {teams.length === 0 ? '(none)' : `(${teams.length})`}
          </button>
        </div>
      </div>

      {mode === 'agent' ? (
        <div>
          <Select value={agentId} onChange={e => setAgentId(e.target.value)}>
            <option value="">Select agent…</option>
            {assignedAgents.length > 0 && (
              <optgroup label="— Assigned to this project —">
                {assignedAgents.map(a => (
                  <option key={a.id} value={a.id}>{a.name}</option>
                ))}
              </optgroup>
            )}
            {otherAgents.length > 0 && (
              <optgroup label="— All other agents —">
                {otherAgents.map(a => (
                  <option key={a.id} value={a.id}>{a.name}</option>
                ))}
              </optgroup>
            )}
          </Select>
          <p className="text-xs text-slate-500 mt-1">
            {allAgents.length} agent{allAgents.length !== 1 ? 's' : ''} available
            {otherAgents.length > 0 && ' — unassigned agents will be auto-added to this project'}
          </p>
        </div>
      ) : (
        <div>
          <Select value={teamId} onChange={e => setTeamId(e.target.value)}>
            {teams.map(t => (
              <option key={t.id} value={t.id}>
                {t.name} ({t.agents?.length ?? 0} member{(t.agents?.length ?? 0) !== 1 ? 's' : ''})
              </option>
            ))}
          </Select>
          {selectedTeam?.agents && selectedTeam.agents.length > 0 && (
            <div className="mt-2 flex flex-wrap gap-1">
              {selectedTeam.agents.map(a => (
                <span key={a.id} className="text-xs bg-slate-800 text-slate-300 px-2 py-0.5 rounded-full">
                  {a.name}
                </span>
              ))}
            </div>
          )}
          <p className="text-xs text-slate-500 mt-1">
            Creates one task per team member. Agents not yet in this project will be auto-added.
          </p>
        </div>
      )}

      <div>
        <Label htmlFor="desc">Description</Label>
        <Textarea id="desc" value={description} onChange={e => setDescription(e.target.value)} rows={4}
          placeholder="Detailed instructions for the agent…" />
      </div>

      {progress && <p className="text-sm text-violet-400">{progress}</p>}
      {error && <p className="text-sm text-red-400">{error}</p>}
      <div className="flex gap-3 justify-end pt-2">
        <Button variant="secondary" onClick={onClose} disabled={saving}>Cancel</Button>
        <Button onClick={save} disabled={saving}>
          {saving ? 'Creating…' : mode === 'team' && selectedTeam?.agents?.length
            ? `Create ${selectedTeam.agents.length} Task${selectedTeam.agents.length !== 1 ? 's' : ''}`
            : 'Create & Run Task'
          }
        </Button>
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

function AssignTeamModal({ projectId, onSave, onClose }: {
  projectId: string; onSave: (msg: string) => void; onClose: () => void
}) {
  const [teams, setTeams] = useState<Team[]>([])
  const [selected, setSelected] = useState('')
  const [saving, setSaving] = useState(false)
  const [result, setResult] = useState('')

  useEffect(() => {
    api.teams.list().then(t => { setTeams(t); setSelected(t[0]?.id ?? '') })
  }, [])

  const save = async () => {
    if (!selected) return
    setSaving(true)
    try {
      const r = await api.projects.assignTeam(projectId, selected)
      onSave(`Added ${r.assigned} of ${r.total} agent(s) from "${r.team}"`)
    } catch (e: any) { setResult(e.message) }
    finally { setSaving(false) }
  }

  return (
    <div className="space-y-4">
      {teams.length === 0 ? (
        <p className="text-slate-400 text-sm">No teams yet. <a href="/teams" className="text-violet-400 hover:underline">Create a team</a> first.</p>
      ) : (
        <>
          <div>
            <Label htmlFor="team">Select Team</Label>
            <Select id="team" value={selected} onChange={e => setSelected(e.target.value)}>
              {teams.map(t => <option key={t.id} value={t.id}>{t.name} ({t.agents?.length ?? 0} members)</option>)}
            </Select>
          </div>
          {result && <p className="text-sm text-slate-400">{result}</p>}
          <div className="flex gap-3 justify-end">
            <Button variant="secondary" onClick={onClose}>Cancel</Button>
            <Button onClick={save} disabled={saving || !selected}>{saving ? 'Assigning…' : 'Assign Team'}</Button>
          </div>
        </>
      )}
    </div>
  )
}

export function ProjectDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const [project, setProject] = useState<Project | null>(null)
  const [agents, setAgents] = useState<Agent[]>([])
  const [allAgents, setAllAgents] = useState<Agent[]>([])
  const [teams, setTeams] = useState<Team[]>([])
  const [tasks, setTasks] = useState<Task[]>([])
  const [loading, setLoading] = useState(true)
  const [showTaskForm, setShowTaskForm] = useState(false)
  const [showAssignAgent, setShowAssignAgent] = useState(false)
  const [showAssignTeam, setShowAssignTeam] = useState(false)
  const [teamAssignMsg, setTeamAssignMsg] = useState('')
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false)
  const [deleteError, setDeleteError] = useState('')
  const [deleting, setDeleting] = useState(false)

  const load = useCallback(async () => {
    if (!id) return
    try {
      const [proj, agts, allAgts, tsks, tms] = await Promise.all([
        api.projects.get(id),
        api.projects.listAgents(id),
        api.agents.list(),
        api.tasks.list(id),
        api.teams.list(),
      ])
      setProject(proj)
      setAgents(agts)
      setAllAgents(allAgts)
      setTasks(tsks)
      setTeams(tms)
    } finally { setLoading(false) }
  }, [id])

  useEffect(() => { load() }, [load])

  const removeAgent = async (agentId: string) => {
    if (!id) return
    await api.projects.removeAgent(id, agentId)
    load()
  }

  const deleteProject = async () => {
    if (!id) return
    setDeleting(true)
    setDeleteError('')
    try {
      await api.projects.delete(id)
      navigate('/projects')
    } catch (e: any) {
      setDeleteError(e.message || 'Failed to delete project')
      setDeleting(false)
    }
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
          {project.working_dir && (
            <p className="text-xs text-slate-500 font-mono mt-1" title={project.working_dir}>
              📁 {project.working_dir}
            </p>
          )}
        </div>
        <div className="flex items-center gap-3">
          {totalCost > 0 && (
            <span className="text-sm text-slate-400">Total: <span className="text-white font-medium">{formatCost(totalCost)}</span></span>
          )}
          <Button variant="secondary" onClick={() => setShowDeleteConfirm(true)}>Delete Project</Button>
          <Button onClick={() => setShowTaskForm(true)} disabled={allAgents.length === 0}>+ New Task</Button>
        </div>
      </div>

      {/* Delete confirmation modal */}
      {showDeleteConfirm && (() => {
        const activeCount = tasks.filter(t => t.status === 'running' || t.status === 'queued').length
        return (
          <Modal title="Delete Project" onClose={() => { setShowDeleteConfirm(false); setDeleteError('') }}>
            <div className="space-y-4">
              <p className="text-slate-300 text-sm">
                Are you sure you want to delete <span className="text-white font-semibold">{project.name}</span>?
                This will permanently remove all {tasks.length} task{tasks.length !== 1 ? 's' : ''}, agent assignments, and history.
              </p>
              {activeCount > 0 && (
                <div className="bg-amber-900/30 border border-amber-700/50 rounded p-3">
                  <p className="text-amber-400 text-sm font-medium">⚠️ Cannot delete yet</p>
                  <p className="text-amber-300/70 text-xs mt-1">
                    {activeCount} task{activeCount !== 1 ? 's are' : ' is'} currently running or queued.
                    Wait for them to finish or retry them to fail them first.
                  </p>
                </div>
              )}
              {deleteError && <p className="text-red-400 text-sm">{deleteError}</p>}
              <div className="flex gap-3 justify-end">
                <Button variant="secondary" onClick={() => { setShowDeleteConfirm(false); setDeleteError('') }}>Cancel</Button>
                <Button
                  className="bg-red-600 hover:bg-red-700 text-white disabled:opacity-40"
                  onClick={deleteProject}
                  disabled={deleting || activeCount > 0}
                >
                  {deleting ? 'Deleting…' : 'Delete Project'}
                </Button>
              </div>
            </div>
          </Modal>
        )
      })()}

      <div className="grid grid-cols-3 gap-6">
        {/* Agents column */}
        <div className="col-span-1 space-y-3">
          <div className="flex items-center justify-between">
            <h2 className="text-sm font-medium text-slate-400 uppercase tracking-wide">Agents</h2>
            <div className="flex gap-1">
              <Button variant="ghost" size="sm" onClick={() => setShowAssignTeam(true)}>+ Team</Button>
              <Button variant="ghost" size="sm" onClick={() => setShowAssignAgent(true)}>+ Agent</Button>
            </div>
          </div>
          {teamAssignMsg && (
            <p className="text-xs text-green-400 bg-green-900/20 border border-green-800/40 rounded px-2 py-1">
              ✓ {teamAssignMsg}
            </p>
          )}
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
              action={allAgents.length > 0
                ? <Button size="sm" onClick={() => setShowTaskForm(true)}>New Task</Button>
                : <p className="text-xs text-slate-500">Create an agent first</p>
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
          <TaskForm
            projectId={project.id}
            allAgents={allAgents}
            projectAgents={agents}
            teams={teams}
            onSave={() => { setShowTaskForm(false); load() }}
            onClose={() => setShowTaskForm(false)}
          />
        </Modal>
      )}

      {showAssignAgent && (
        <Modal title="Assign Agent" onClose={() => setShowAssignAgent(false)}>
          <AssignAgentModal projectId={project.id} allAgents={allAgents} assigned={agents}
            onSave={() => { setShowAssignAgent(false); load() }}
            onClose={() => setShowAssignAgent(false)} />
        </Modal>
      )}
      {showAssignTeam && (
        <Modal title="Assign Team" onClose={() => setShowAssignTeam(false)}>
          <AssignTeamModal
            projectId={project.id}
            onSave={(msg) => { setShowAssignTeam(false); setTeamAssignMsg(msg); load(); setTimeout(() => setTeamAssignMsg(''), 5000) }}
            onClose={() => setShowAssignTeam(false)}
          />
        </Modal>
      )}
    </div>
  )
}
