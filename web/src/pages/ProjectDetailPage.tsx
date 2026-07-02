import { useState, useEffect, useCallback } from 'react'
import { useParams, Link, useNavigate } from 'react-router-dom'
import { api, type Project, type Agent, type Task, type Team, type Provider } from '@/lib/api'
import { phoenixWS } from '@/lib/ws'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Modal } from '@/components/ui/modal'
import { Input, Textarea, Select, Label } from '@/components/ui/input'
import { EmptyState } from '@/components/ui/empty'
import { taskStatusVariant, taskStatusLabel, parseOutput, formatCost, timeAgo } from '@/lib/utils'
import { ProjectHumanView } from '@/components/project/ProjectHumanView'
import { AgentsSection } from '@/components/shared/AgentsSection'
import { MarkdownOutput } from '@/components/ui/markdown-output'
import { FollowUpThread } from '@/components/ui/follow-up-thread'
import { getErrorMessage } from '@/lib/errors'

function TaskForm({ projectId, allAgents, projectAgents, teams, providers, onSave, onClose }: {
  projectId: string
  allAgents: Agent[]
  projectAgents: Agent[]
  teams: Team[]
  providers: Provider[]
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
  const [showAI, setShowAI] = useState(false)
  const [aiHint, setAiHint] = useState('')
  const [aiProviderID, setAiProviderID] = useState(
    providers.find(p => p.type === 'llm')?.id ?? providers[0]?.id ?? ''
  )
  const [aiGenerating, setAiGenerating] = useState(false)
  const [aiError, setAiError] = useState('')

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
    } catch (error: unknown) { setError(getErrorMessage(error)) }
    finally { setSaving(false); setProgress('') }
  }

  const generateDescription = async () => {
    if (!title.trim()) { setAiError('Enter a task title first'); return }
    setAiGenerating(true)
    setAiError('')
    try {
      const result = await api.tasks.generateDescription(title, aiHint, aiProviderID)
      setDescription(result.description)
      setShowAI(false)
      setAiHint('')
    } catch (error: unknown) {
      setAiError(getErrorMessage(error))
    } finally {
      setAiGenerating(false)
    }
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
        <div className="flex items-center justify-between mb-1">
          <Label htmlFor="desc">Description</Label>
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
            <p className="text-xs text-slate-400">Describe what the agent should do and AI will write the task description.</p>
            {providers.length > 1 && (
              <div>
                <Label htmlFor="ai-provider-task">Generate using</Label>
                <Select id="ai-provider-task" value={aiProviderID} onChange={e => setAiProviderID(e.target.value)}>
                  {providers.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
                </Select>
              </div>
            )}
            <div>
              <Label htmlFor="ai-hint-task">Additional context <span className="text-slate-500 font-normal">(optional)</span></Label>
              <Textarea
                id="ai-hint-task"
                value={aiHint}
                onChange={e => setAiHint(e.target.value)}
                rows={2}
                placeholder="e.g. Focus on security implications, output as a markdown checklist"
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

// ---- Guided Setup (3-step flow for empty projects) ----

function GuidedSetup({ projectId, allAgents, projectAgents, teams, onDone }: {
  projectId: string
  allAgents: Agent[]
  projectAgents: Agent[]
  teams: Team[]
  onDone: () => void
}) {
  const [step, setStep] = useState<1 | 2 | 3>(1)
  const [selectedAgentIds, setSelectedAgentIds] = useState<Set<string>>(
    new Set(projectAgents.map(a => a.id))
  )
  const [title, setTitle] = useState('')
  const [description, setDescription] = useState('')
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState('')
  const [progress, setProgress] = useState('')

  const projectAgentIds = new Set(projectAgents.map(a => a.id))

  // Group agents: project-assigned first
  const assignedAgents = allAgents.filter(a => projectAgentIds.has(a.id))
  const otherAgents = allAgents.filter(a => !projectAgentIds.has(a.id))

  const toggleAgent = (id: string) => {
    setSelectedAgentIds(prev => {
      const next = new Set(prev)
      if (next.has(id)) next.delete(id)
      else next.add(id)
      return next
    })
  }

  const selectedAgents = allAgents.filter(a => selectedAgentIds.has(a.id))

  const run = async () => {
    if (!title.trim()) { setError('Add a task title first'); return }
    if (selectedAgentIds.size === 0) { setError('Choose at least one agent'); return }
    setSaving(true)
    setError('')
    try {
      for (let i = 0; i < selectedAgents.length; i++) {
        const agent = selectedAgents[i]
        setProgress(`Creating task ${i + 1}/${selectedAgents.length} → ${agent.name}…`)
        if (!projectAgentIds.has(agent.id)) {
          await api.projects.assignAgent(projectId, agent.id)
        }
        await api.tasks.create({ project_id: projectId, agent_id: agent.id, title, description })
      }
      onDone()
    } catch (error: unknown) { setError(getErrorMessage(error)) }
    finally { setSaving(false); setProgress('') }
  }

  const steps = [
    { n: 1, label: 'Choose agents' },
    { n: 2, label: 'Describe goal' },
    { n: 3, label: 'Run' },
  ]

  return (
    <div className="border border-slate-800 rounded-2xl bg-slate-900/40 overflow-hidden">
      {/* Stepper header */}
      <div className="flex border-b border-slate-800">
        {steps.map((s, i) => (
          <div
            key={s.n}
            className={`flex-1 flex items-center gap-2 px-4 py-3 text-sm ${
              step === s.n ? 'bg-violet-900/20 text-violet-300' :
              step > s.n ? 'text-slate-400' : 'text-slate-600'
            } ${i > 0 ? 'border-l border-slate-800' : ''}`}
          >
            <span className={`w-5 h-5 rounded-full flex items-center justify-center text-xs font-bold flex-shrink-0 ${
              step > s.n ? 'bg-emerald-700 text-emerald-200' :
              step === s.n ? 'bg-violet-600 text-white' : 'bg-slate-800 text-slate-600'
            }`}>
              {step > s.n ? '✓' : s.n}
            </span>
            <span className="font-medium">{s.label}</span>
          </div>
        ))}
      </div>

      <div className="p-6 space-y-4">
        {/* Step 1 — Choose agents */}
        {step === 1 && (
          <>
            <p className="text-slate-400 text-sm">Who should work on this project? Select one or more agents.</p>

            {allAgents.length === 0 ? (
              <div className="text-center py-6">
                <p className="text-slate-500 text-sm mb-3">No agents yet.</p>
                <a href="/settings?tab=agents" className="text-violet-400 hover:underline text-sm">Create an agent in Settings →</a>
              </div>
            ) : (
              <div className="space-y-1 max-h-60 overflow-y-auto">
                {assignedAgents.length > 0 && (
                  <p className="text-xs text-slate-600 uppercase tracking-wide px-2 py-1">Already on this project</p>
                )}
                {assignedAgents.map(a => (
                  <label key={a.id} className="flex items-center gap-3 p-2.5 rounded-lg hover:bg-slate-800 cursor-pointer">
                    <input type="checkbox" checked={selectedAgentIds.has(a.id)} onChange={() => toggleAgent(a.id)}
                      className="rounded accent-violet-500" />
                    <div className="flex-1 min-w-0">
                      <p className="text-sm text-white">{a.name}</p>
                      {(a.behaviour || a.persona) && <p className="text-xs text-slate-500 truncate">{a.behaviour || a.persona}</p>}
                    </div>
                  </label>
                ))}
                {otherAgents.length > 0 && (
                  <p className="text-xs text-slate-600 uppercase tracking-wide px-2 py-1 mt-2">All other agents</p>
                )}
                {otherAgents.map(a => (
                  <label key={a.id} className="flex items-center gap-3 p-2.5 rounded-lg hover:bg-slate-800 cursor-pointer">
                    <input type="checkbox" checked={selectedAgentIds.has(a.id)} onChange={() => toggleAgent(a.id)}
                      className="rounded accent-violet-500" />
                    <div className="flex-1 min-w-0">
                      <p className="text-sm text-white">{a.name}</p>
                      {(a.behaviour || a.persona) && <p className="text-xs text-slate-500 truncate">{a.behaviour || a.persona}</p>}
                    </div>
                  </label>
                ))}
              </div>
            )}

            {teams.length > 0 && (
              <div className="border-t border-slate-800 pt-3">
                <p className="text-xs text-slate-500 mb-2">Or select an entire team:</p>
                <div className="flex flex-wrap gap-2">
                  {teams.map(t => (
                    <button
                      key={t.id}
                      className="text-xs px-3 py-1.5 rounded-full border border-slate-700 text-slate-300 hover:border-violet-500 hover:text-violet-300 transition-colors"
                      onClick={() => {
                        const ids = new Set(selectedAgentIds)
                        for (const a of t.agents ?? []) ids.add(a.id)
                        setSelectedAgentIds(ids)
                      }}
                    >
                      + {t.name} ({t.agents?.length ?? 0})
                    </button>
                  ))}
                </div>
              </div>
            )}

            <div className="flex items-center justify-between pt-2">
              <p className="text-xs text-slate-500">
                {selectedAgentIds.size} agent{selectedAgentIds.size !== 1 ? 's' : ''} selected
              </p>
              <Button onClick={() => setStep(2)} disabled={selectedAgentIds.size === 0}>
                Next →
              </Button>
            </div>
          </>
        )}

        {/* Step 2 — Describe goal */}
        {step === 2 && (
          <>
            <p className="text-slate-400 text-sm">
              What do you need done? This becomes the task that{' '}
              <span className="text-white">{selectedAgents.map(a => a.name).join(', ')}</span>{' '}
              will work on.
            </p>
            <div>
              <Label htmlFor="gs-title">Task title</Label>
              <Input
                id="gs-title"
                value={title}
                onChange={e => setTitle(e.target.value)}
                placeholder="e.g. Research OKR best practices for Q3"
              />
            </div>
            <div>
              <Label htmlFor="gs-desc">Instructions <span className="text-slate-500 font-normal">(optional)</span></Label>
              <Textarea
                id="gs-desc"
                value={description}
                onChange={e => setDescription(e.target.value)}
                rows={5}
                placeholder="Detailed instructions, context, or goals for the agent…"
              />
            </div>
            <div className="flex gap-3 justify-between pt-2">
              <Button variant="secondary" onClick={() => setStep(1)}>← Back</Button>
              <Button onClick={() => setStep(3)} disabled={!title.trim()}>Review →</Button>
            </div>
          </>
        )}

        {/* Step 3 — Review & Run */}
        {step === 3 && (
          <>
            <div className="bg-slate-800/60 border border-slate-700 rounded-lg p-4 space-y-3">
              <div>
                <p className="text-xs text-slate-500 mb-0.5">Task</p>
                <p className="text-sm text-white font-medium">{title}</p>
                {description && <p className="text-xs text-slate-400 mt-1 line-clamp-2">{description}</p>}
              </div>
              <div>
                <p className="text-xs text-slate-500 mb-1">Assigned to</p>
                <div className="flex flex-wrap gap-1.5">
                  {selectedAgents.map(a => (
                    <span key={a.id} className="text-xs bg-violet-900/50 border border-violet-800/50 text-violet-300 px-2 py-0.5 rounded-full">
                      {a.name}
                    </span>
                  ))}
                </div>
              </div>
              {selectedAgents.length > 1 && (
                <p className="text-xs text-slate-500">
                  Creates {selectedAgents.length} tasks — one per agent — all running in parallel.
                </p>
              )}
            </div>
            {progress && <p className="text-sm text-violet-400">{progress}</p>}
            {error && <p className="text-sm text-red-400">{error}</p>}
            <div className="flex gap-3 justify-between pt-2">
              <Button variant="secondary" onClick={() => setStep(2)} disabled={saving}>← Back</Button>
              <Button onClick={run} disabled={saving}>
                {saving ? 'Starting…' : selectedAgents.length > 1
                  ? `Start ${selectedAgents.length} Tasks`
                  : 'Start Task'
                }
              </Button>
            </div>
          </>
        )}
      </div>
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
    } catch (error: unknown) { setResult(getErrorMessage(error)) }
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

// ---- Task Detail Modal ----
function TaskDetailModal({ task, agents, onClose, onUpdate }: {
  task: Task; agents: Agent[]; onClose: () => void; onUpdate: () => void
}) {
  const [stream, setStream] = useState('')
  const [retrying, setRetrying] = useState(false)
  const agent = agents.find(a => a.id === task.agent_id)

  useEffect(() => {
    if (task.status !== 'running') return
    const unsub = phoenixWS.on((ev) => {
      if (ev.type === 'task.output_stream') {
        const p = ev.payload
        if (p.task_id === task.id) setStream(prev => prev + p.chunk)
      }
      if (ev.type === 'task.status_changed') {
        const p = ev.payload
        if (p.task_id === task.id) { onUpdate(); onClose() }
      }
    })
    return unsub
  }, [task.id, task.status, onUpdate, onClose])

  const retry = async () => {
    setRetrying(true)
    try { await api.tasks.retry(task.id); onUpdate(); onClose() } finally { setRetrying(false) }
  }

  const output = task.status === 'running' && stream ? stream : parseOutput(task.output)

  return (
    <Modal title={task.title} onClose={onClose}>
      <div className="space-y-3">
        <div className="flex items-center gap-2 text-xs text-slate-400">
          <Badge variant={taskStatusVariant(task.status)}>{taskStatusLabel(task.status)}</Badge>
          <span>{agent?.name ?? 'Unknown'}</span>
          <span>·</span>
          <span>{timeAgo(task.created_at)}</span>
          {task.cost_usd > 0 && <><span>·</span><span>{formatCost(task.cost_usd)}</span></>}
        </div>
        {task.description && (
          <p className="text-sm text-slate-300">{task.description}</p>
        )}
        {task.status === 'running' && (
          <div className="flex items-center gap-1.5">
            <span className="w-1.5 h-1.5 rounded-full bg-violet-500 animate-pulse" />
            <span className="text-xs text-violet-400">Running…</span>
          </div>
        )}
        {output && output !== '{}' && (
          <div className="bg-slate-950 border border-slate-800 rounded-lg p-4 max-h-96 overflow-y-auto">
            <MarkdownOutput content={output} />
          </div>
        )}
        {task.status === 'failed' && (
          <Button size="sm" variant="secondary" onClick={retry} disabled={retrying}>
            {retrying ? 'Retrying…' : '↺ Retry'}
          </Button>
        )}
        <FollowUpThread task={task} agents={agents} onSent={onUpdate} />
      </div>
    </Modal>
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
  const [showAssignTeam, setShowAssignTeam] = useState(false)
  const [teamAssignMsg, setTeamAssignMsg] = useState('')
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false)
  const [deleteError, setDeleteError] = useState('')
  const [deleting, setDeleting] = useState(false)
  const [selectedTask, setSelectedTask] = useState<Task | null>(null)
  const [criticMode, setCriticMode] = useState<string>('none')
  const [savingCritic, setSavingCritic] = useState(false)
  const [criticMessage, setCriticMessage] = useState('')
  const [providers, setProviders] = useState<Provider[]>([])

  const load = useCallback(async () => {
    if (!id) return
    try {
      const [proj, agts, allAgts, tsks, tms, provs] = await Promise.all([
        api.projects.get(id),
        api.projects.listAgents(id),
        api.agents.list(),
        api.tasks.list(id),
        api.teams.list(),
        api.providers.list(),
      ])
      setProject(proj)
      setAgents(agts)
      setAllAgents(allAgts)
      setTasks(tsks)
      setTeams(tms)
      setProviders(provs)
      setCriticMode(proj.critic_mode ?? 'none')
    } finally { setLoading(false) }
  }, [id])

  useEffect(() => { load() }, [load])

  const saveCritic = async () => {
    if (!project) return
    setSavingCritic(true)
    setCriticMessage('')
    try {
      const updated = await api.projects.update(project.id, {
        name: project.name,
        objective: project.objective,
        working_dir: project.working_dir,
        kind: project.kind,
        status: project.status,
        schedule_interval: project.schedule_interval,
        critic_mode: criticMode,
      })
      setProject(updated)
      setCriticMessage('Saved')
    } catch (error: unknown) {
      setCriticMessage(getErrorMessage(error, 'Failed to save'))
    } finally {
      setSavingCritic(false)
    }
  }

  const deleteProject = async () => {
    if (!id) return
    setDeleting(true)
    setDeleteError('')
    try {
      await api.projects.delete(id)
      navigate('/projects')
    } catch (error: unknown) {
      setDeleteError(getErrorMessage(error, 'Failed to delete project'))
      setDeleting(false)
    }
  }

  const totalCost = tasks.reduce((s, t) => s + t.cost_usd, 0)

  if (loading) return <div className="text-slate-500 text-sm">Loading…</div>
  if (!project) return <div className="text-slate-500 text-sm">Project not found.</div>

  return (
    <div className="space-y-6">
      {/* Breadcrumb */}
      <div className="flex items-center gap-2 text-sm">
        <Link to="/projects" className="text-slate-500 hover:text-white transition-colors">Projects</Link>
        <span className="text-slate-700">/</span>
        <span className="text-white">{project.name}</span>
        {project.working_dir && (
          <span className="text-xs text-slate-600 font-mono ml-2" title={project.working_dir}>
            📁 {project.working_dir.split('/').pop()}
          </span>
        )}
        <div className="ml-auto flex items-center gap-2">
          {totalCost > 0 && (
            <span className="text-sm text-slate-400">Total: <span className="text-white font-medium">{formatCost(totalCost)}</span></span>
          )}
          <Button variant="secondary" size="sm" onClick={() => setShowDeleteConfirm(true)}>Delete</Button>
          <Button size="sm" onClick={() => setShowTaskForm(true)} disabled={allAgents.length === 0}>+ New Task</Button>
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

      <div className="bg-slate-900 border border-slate-800 rounded-xl px-5 py-4">
        <div className="flex items-center justify-between gap-4 mb-3">
          <div>
            <p className="text-xs text-slate-500 uppercase tracking-wide">Devil's Advocate</p>
            <p className="text-xs text-slate-600 mt-1">After each task completes, automatically run a contrarian critic review.</p>
          </div>
        </div>
        <div className="space-y-3">
          <div className="flex flex-col gap-2">
            {[
              { value: 'none',    label: 'Off',                    desc: 'No critic review' },
              { value: 'builtin', label: '😈 Built-in Devil\'s Advocate', desc: 'Ephemeral contrarian — uses the same provider as the original agent, no agent record needed' },
            ].map(opt => (
              <label key={opt.value} className="flex items-start gap-3 cursor-pointer">
                <input
                  type="radio"
                  name="critic-mode"
                  value={opt.value}
                  checked={criticMode === opt.value}
                  onChange={() => setCriticMode(opt.value)}
                  className="mt-0.5"
                />
                <div>
                  <p className="text-sm text-slate-200">{opt.label}</p>
                  <p className="text-xs text-slate-500">{opt.desc}</p>
                </div>
              </label>
            ))}
            <label className="flex items-start gap-3 cursor-pointer">
              <input
                type="radio"
                name="critic-mode"
                value="custom"
                checked={criticMode.startsWith('agent:')}
                onChange={() => setCriticMode('agent:')}
                className="mt-0.5"
              />
              <div className="flex-1">
                <p className="text-sm text-slate-200">Custom agent</p>
                <p className="text-xs text-slate-500 mb-1">Use a specific registered agent as the critic</p>
                {criticMode.startsWith('agent:') && (
                  <Select
                    value={criticMode.replace('agent:', '')}
                    onChange={e => setCriticMode(e.target.value ? 'agent:' + e.target.value : 'agent:')}
                  >
                    <option value="">Select an agent…</option>
                    {allAgents.map(a => <option key={a.id} value={a.id}>{a.name}</option>)}
                  </Select>
                )}
              </div>
            </label>
          </div>
          <div className="flex items-center gap-3 pt-1">
            <Button variant="secondary" onClick={saveCritic} disabled={savingCritic}>
              {savingCritic ? 'Saving…' : 'Save'}
            </Button>
            {criticMessage && <p className={`text-xs ${criticMessage === 'Saved' ? 'text-green-400' : 'text-red-400'}`}>{criticMessage}</p>}
          </div>
        </div>
      </div>

      {/* Agents — same pattern as Monitors page */}
      <div className="bg-slate-900 border border-slate-800 rounded-xl px-5 py-4">
        <div className="flex items-center justify-between mb-3">
          <p className="text-xs text-slate-500 uppercase tracking-wide">Agents</p>
          <button
            onClick={() => setShowAssignTeam(true)}
            className="text-xs text-slate-500 hover:text-violet-400 transition-colors"
          >
            + Add team
          </button>
        </div>
        <AgentsSection
          assigned={agents}
          allAgents={allAgents}
          onAdd={async (agentId) => { await api.projects.assignAgent(id!, agentId); load() }}
          onRemove={async (agentId) => { await api.projects.removeAgent(id!, agentId); load() }}
        />
        {teamAssignMsg && <p className="text-xs text-green-400 mt-2">✓ {teamAssignMsg}</p>}
      </div>

      {/* Main view — always human-driven */}
      {tasks.length === 0 && allAgents.length > 0 ? (
        <GuidedSetup
          projectId={project.id}
          allAgents={allAgents}
          projectAgents={agents}
          teams={teams}
          onDone={() => load()}
        />
      ) : tasks.length === 0 ? (
        <EmptyState icon="◈" title="No agents yet"
          description="Create an agent in Settings before you can run tasks."
          action={<a href="/settings?tab=agents"><Button size="sm">Go to Settings →</Button></a>} />
      ) : (
        <ProjectHumanView
          project={project}
          tasks={tasks}
          agents={allAgents}
          onUpdate={load}
          onNewTask={() => setShowTaskForm(true)}
        />
      )}

      {showTaskForm && (
        <Modal title="New Task" onClose={() => setShowTaskForm(false)}>
          <TaskForm
            projectId={project.id}
            allAgents={allAgents}
            projectAgents={agents}
            teams={teams}
            providers={providers}
            onSave={() => { setShowTaskForm(false); load() }}
            onClose={() => setShowTaskForm(false)}
          />
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

      {selectedTask && (
        <TaskDetailModal
          task={selectedTask}
          agents={allAgents}
          onClose={() => setSelectedTask(null)}
          onUpdate={load}
        />
      )}
    </div>
  )
}
