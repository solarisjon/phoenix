import { useState, useEffect, useCallback } from 'react'
import { useParams, Link, useNavigate } from 'react-router-dom'
import { api, type Team, type Task, type Project, type Provider } from '@/lib/api'
import { Card, CardBody } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Modal } from '@/components/ui/modal'
import { Input, Textarea, Select, Label } from '@/components/ui/input'
import { EmptyState } from '@/components/ui/empty'
import { formatCost } from '@/lib/utils'
import { getErrorMessage } from '@/lib/errors'
import { ProviderSelect } from '@/components/ui/provider-select'

function formatInterval(secs: number): string {
  if (secs < 60) return `${secs}s`
  if (secs < 3600) return `${Math.round(secs / 60)}m`
  if (secs < 86400) return `${(secs / 3600).toFixed(1).replace(/\.0$/, '')}h`
  return `${(secs / 86400).toFixed(1).replace(/\.0$/, '')}d`
}

// Compute "next scheduled run" for a monitor project
function nextFire(projectId: string, interval: number, allTasks: Task[]): string {
  const scheduledTasks = allTasks
    .filter(t => t.project_id === projectId && t.title.startsWith('Scheduled run'))
    .sort((a, b) => new Date(b.created_at).getTime() - new Date(a.created_at).getTime())
  const lastFired = scheduledTasks[0] ? new Date(scheduledTasks[0].created_at) : null
  const nextDate = lastFired
    ? new Date(lastFired.getTime() + interval * 1000)
    : null
  if (!nextDate) return 'soon'
  const diff = nextDate.getTime() - Date.now()
  if (diff <= 0) return 'any moment'
  const mins = Math.round(diff / 60000)
  if (mins < 60) return `in ${mins}m`
  const hrs = (diff / 3600000).toFixed(1).replace(/\.0$/, '')
  return `in ${hrs}h`
}

export function TeamDetailPage() {
  const { id } = useParams<{ id: string }>()
  const navigate = useNavigate()
  const [team, setTeam] = useState<Team | null>(null)
  const [allTasks, setAllTasks] = useState<Task[]>([])
  const [projects, setProjects] = useState<Project[]>([])
  const [loading, setLoading] = useState(true)
  const [showDeleteConfirm, setShowDeleteConfirm] = useState(false)
  const [deleting, setDeleting] = useState(false)
  const [showBroadcast, setShowBroadcast] = useState(false)
  const [broadcastProjectId, setBroadcastProjectId] = useState('')
  const [broadcastTitle, setBroadcastTitle] = useState('')
  const [broadcastDescription, setBroadcastDescription] = useState('')
  const [broadcastSaving, setBroadcastSaving] = useState(false)
  const [broadcastMessage, setBroadcastMessage] = useState('')
  const [providers, setProviders] = useState<Provider[]>([])
  const [showBroadcastAI, setShowBroadcastAI] = useState(false)
  const [broadcastAIHint, setBroadcastAIHint] = useState('')
  const [broadcastAIProviderID, setBroadcastAIProviderID] = useState('')
  const [broadcastAIGenerating, setBroadcastAIGenerating] = useState(false)
  const [broadcastAIError, setBroadcastAIError] = useState('')

  const load = useCallback(async () => {
    if (!id) return
    try {
      const [t, ts, projs, provs] = await Promise.all([
        api.teams.get(id),
        api.tasks.listAll(),
        api.projects.list(),
        api.providers.list(),
      ])
      setTeam(t)
      setAllTasks(ts)
      setProjects(projs)
      setProviders(provs)
      setBroadcastAIProviderID(prev => prev || provs.find(p => p.type === 'llm')?.id || provs[0]?.id || '')
    } finally { setLoading(false) }
  }, [id])

  useEffect(() => { load() }, [load])

  useEffect(() => {
    if (!broadcastProjectId && projects.length > 0) {
      const timer = window.setTimeout(() => {
        setBroadcastProjectId(projects[0].id)
      }, 0)
      return () => window.clearTimeout(timer)
    }
  }, [projects, broadcastProjectId])

  const broadcast = async () => {
    if (!id || !broadcastProjectId || !broadcastTitle.trim()) return
    setBroadcastSaving(true)
    setBroadcastMessage('')
    try {
      const result = await api.teams.broadcast(id, {
        project_id: broadcastProjectId,
        title: broadcastTitle,
        description: broadcastDescription,
      })
      setBroadcastMessage(`Broadcast queued ${result.count} task${result.count === 1 ? '' : 's'}.`)
      setBroadcastTitle('')
      setBroadcastDescription('')
      await load()
    } catch (error: unknown) {
      setBroadcastMessage(getErrorMessage(error, 'Broadcast failed'))
    } finally {
      setBroadcastSaving(false)
    }
  }

  const deleteTeam = async () => {
    if (!id) return
    setDeleting(true)
    try { await api.teams.delete(id); navigate('/teams') }
    finally { setDeleting(false) }
  }

  if (loading) return <div className="text-slate-500 text-sm">Loading…</div>
  if (!team) return <div className="text-slate-500 text-sm">Team not found.</div>

  const members = team.agents ?? []

  // Projects that have any team member assigned
  // We derive this from tasks — if any task is for a team member in a project, that project is "used" by this team
  const memberIds = new Set(members.map(a => a.id))
  const teamProjectIds = new Set(allTasks.filter(t => memberIds.has(t.agent_id)).map(t => t.project_id))
  const teamProjects = projects.filter(p => teamProjectIds.has(p.id))

  // Per-project task stats
  const projectStats = teamProjects.map(p => {
    const pts = allTasks.filter(t => t.project_id === p.id)
    const done = pts.filter(t => t.status === 'completed').length
    const running = pts.filter(t => t.status === 'running' || t.status === 'queued').length
    const totalCost = pts.reduce((s, t) => s + t.cost_usd, 0)
    return { project: p, total: pts.length, done, running, totalCost }
  })

  // Live agent status: which agent is running what right now
  const agentActiveTask: Record<string, Task> = {}
  for (const t of allTasks) {
    if ((t.status === 'running' || t.status === 'queued') && memberIds.has(t.agent_id)) {
      agentActiveTask[t.agent_id] = t
    }
  }

  // Schedule: monitors with schedule_interval assigned to team members
  const scheduleRows: Array<{ agent: typeof members[0]; project: Project; interval: number; next: string }> = []
  for (const proj of teamProjects) {
    if (proj.kind !== 'monitor' || !proj.schedule_interval) continue
    // Find the first active team member assigned to this monitor
    const assignedAgent = members.find(a => a.status === 'active')
    if (!assignedAgent) continue
    scheduleRows.push({
      agent: assignedAgent,
      project: proj,
      interval: proj.schedule_interval,
      next: nextFire(proj.id, proj.schedule_interval, allTasks),
    })
  }

  return (
    <div className="space-y-8">
      {/* Header */}
      <div className="flex items-start justify-between">
        <div>
          <div className="flex items-center gap-2 mb-1">
            <Link to="/teams" className="text-slate-500 hover:text-white text-sm transition-colors">Teams</Link>
            <span className="text-slate-700">/</span>
            <span className="text-white text-sm">{team.name}</span>
          </div>
          <h1 className="text-2xl font-bold text-white">{team.name}</h1>
          {team.description && <p className="text-slate-400 text-sm mt-1">{team.description}</p>}
        </div>
        <div className="flex items-center gap-2">
          <Button variant="secondary" onClick={() => setShowBroadcast(true)}>Broadcast</Button>
          <a href={api.teams.exportUrl(team.id)} download>
            <Button variant="secondary">Export Bundle ↓</Button>
          </a>
          <Button variant="secondary" onClick={() => navigate(`/teams?edit=${team.id}`)}>Edit Team</Button>
          <Button variant="danger" onClick={() => setShowDeleteConfirm(true)}>Delete</Button>
        </div>
      </div>

      {/* Members */}
      <section>
        <div className="flex items-center justify-between mb-3">
          <h2 className="text-sm font-medium text-slate-400 uppercase tracking-wide">
            Members ({members.length})
          </h2>
          <Button variant="ghost" size="sm" onClick={() => navigate(`/teams?edit=${team.id}`)}>+ Add Member</Button>
        </div>
        {members.length === 0 ? (
          <EmptyState icon="⬡" title="No members yet"
            description="Edit the team to add agents."
            action={<Button size="sm" onClick={() => navigate(`/teams?edit=${team.id}`)}>Add Members</Button>}
          />
        ) : (
          <div className="grid grid-cols-3 gap-4">
            {members.map(agent => {
              const activeTask = agentActiveTask[agent.id]
              const activeProject = activeTask ? projects.find(p => p.id === activeTask.project_id) : null
              return (
                <Card key={agent.id} className={activeTask ? 'border-violet-800/60' : ''}>
                  <CardBody>
                    <div className="flex items-start gap-3">
                      <div className="w-10 h-10 rounded-lg bg-violet-900/50 border border-violet-800/50 flex items-center justify-center text-violet-300 font-bold text-lg flex-shrink-0">
                        {agent.name.charAt(0)}
                      </div>
                      <div className="min-w-0 flex-1">
                        <p className="font-medium text-white truncate">{agent.name}</p>
                        {agent.persona && (
                          <p className="text-xs text-slate-500 mt-0.5 line-clamp-2">{agent.persona}</p>
                        )}
                      </div>
                    </div>
                    <div className="mt-3 pt-3 border-t border-slate-800">
                      {activeTask ? (
                        <div className="flex items-center gap-1.5">
                          <span className="w-1.5 h-1.5 rounded-full bg-violet-500 animate-pulse flex-shrink-0" />
                          <div className="min-w-0">
                            <p className="text-xs text-violet-300 truncate">{activeTask.title}</p>
                            {activeProject && (
                              <p className="text-xs text-slate-600 truncate">{activeProject.name}</p>
                            )}
                          </div>
                        </div>
                      ) : (
                        <div className="flex items-center gap-1.5">
                          <span className="w-1.5 h-1.5 rounded-full bg-slate-700 flex-shrink-0" />
                          <p className="text-xs text-slate-600">Idle</p>
                        </div>
                      )}
                    </div>
                    <div className="mt-2">
                      <Link to={`/settings?tab=agents`}>
                        <Button variant="ghost" size="sm" className="w-full text-xs">Configure →</Button>
                      </Link>
                    </div>
                  </CardBody>
                </Card>
              )
            })}
          </div>
        )}
      </section>

      {/* Projects */}
      <section>
        <div className="flex items-center justify-between mb-3">
          <h2 className="text-sm font-medium text-slate-400 uppercase tracking-wide">
            Projects ({teamProjects.length})
          </h2>
          <Link to="/projects">
            <Button size="sm">+ New Project</Button>
          </Link>
        </div>
        {teamProjects.length === 0 ? (
          <Card>
            <CardBody className="py-8 text-center">
              <p className="text-slate-500 text-sm mb-1">No projects yet</p>
              <p className="text-slate-600 text-xs mb-4">Create a project and assign team members to start running tasks.</p>
              <Link to="/projects">
                <Button size="sm">Create Project</Button>
              </Link>
            </CardBody>
          </Card>
        ) : (
          <Card>
            <div className="divide-y divide-slate-800">
              {projectStats.map(({ project, total, done, running, totalCost }) => (
                <div key={project.id} className="flex items-center gap-4 px-5 py-3">
                  <div className="flex-1 min-w-0">
                    <Link to={`/projects/${project.id}`} className="text-sm font-medium text-white hover:text-violet-300 transition-colors">
                      {project.name}
                    </Link>
                    <div className="flex items-center gap-3 mt-0.5 text-xs text-slate-500">
                      <span>{total} task{total !== 1 ? 's' : ''}</span>
                      <span>{done} completed</span>
                      {running > 0 && (
                        <span className="text-violet-400 flex items-center gap-1">
                          <span className="w-1 h-1 rounded-full bg-violet-500 animate-pulse" />
                          {running} running
                        </span>
                      )}
                      {totalCost > 0 && <span>{formatCost(totalCost)}</span>}
                    </div>
                  </div>
                  <div className="flex items-center gap-2">
                    <Badge variant={project.status === 'active' ? 'success' : 'muted'}>{project.status}</Badge>
                    <Link to={`/projects/${project.id}`}>
                      <Button variant="ghost" size="sm">Open →</Button>
                    </Link>
                  </div>
                </div>
              ))}
            </div>
          </Card>
        )}
      </section>

      {/* Schedule */}
      {scheduleRows.length > 0 && (
        <section>
          <h2 className="text-sm font-medium text-slate-400 uppercase tracking-wide mb-3">Schedule</h2>
          <Card>
            <div className="divide-y divide-slate-800">
              {scheduleRows.map((row, i) => (
                <div key={i} className="grid grid-cols-[1fr_1fr_100px_100px] gap-4 px-5 py-3 items-center text-sm">
                  <span className="text-white">{row.agent.name}</span>
                  <span className="text-slate-400">{row.project.name}</span>
                  <span className="text-slate-500">every {formatInterval(row.interval)}</span>
                  <span className="text-violet-400 text-xs">{row.next}</span>
                </div>
              ))}
            </div>
          </Card>
          <p className="text-xs text-slate-600 mt-2">
            Schedules are set per monitor in{' '}
            <Link to="/monitors" className="text-violet-500 hover:underline">Monitors</Link>.
          </p>
        </section>
      )}

      {showBroadcast && (
        <Modal title="Broadcast to Team" onClose={() => setShowBroadcast(false)}>
          <div className="space-y-4">
            <div>
              <Label htmlFor="broadcast-project">Project</Label>
              <Select id="broadcast-project" value={broadcastProjectId} onChange={e => setBroadcastProjectId(e.target.value)}>
                <option value="">Select project…</option>
                {projects.map(project => (
                  <option key={project.id} value={project.id}>{project.name}</option>
                ))}
              </Select>
            </div>
            <div>
              <Label htmlFor="broadcast-title">Title</Label>
              <Input id="broadcast-title" value={broadcastTitle} onChange={e => setBroadcastTitle(e.target.value)} placeholder="e.g. Audit current project status" />
            </div>
            <div>
              <div className="flex items-center justify-between mb-1">
                <Label htmlFor="broadcast-description">Description</Label>
                {providers.length > 0 && (
                  <button
                    type="button"
                    onClick={() => { setShowBroadcastAI(v => !v); setBroadcastAIError('') }}
                    className="text-xs text-violet-400 hover:text-violet-300 transition-colors flex items-center gap-1"
                  >
                    ✦ {showBroadcastAI ? 'Hide AI assist' : 'Generate with AI'}
                  </button>
                )}
              </div>
              {showBroadcastAI && (
                <div className="mb-3 rounded-lg border border-violet-800/50 bg-violet-950/30 p-3 space-y-3">
                  <p className="text-xs text-slate-400">Describe what you want the team to do and AI will write the broadcast instructions.</p>
                  {providers.length > 1 && (
                    <div>
                      <Label htmlFor="ai-provider-broadcast">Generate using</Label>
                      <ProviderSelect
                        id="ai-provider-broadcast"
                        value={broadcastAIProviderID}
                        onChange={setBroadcastAIProviderID}
                        providers={providers}
                      />
                    </div>
                  )}
                  <div>
                    <Label htmlFor="ai-hint-broadcast">Additional context <span className="text-slate-500 font-normal">(optional)</span></Label>
                    <Textarea
                      id="ai-hint-broadcast"
                      value={broadcastAIHint}
                      onChange={e => setBroadcastAIHint(e.target.value)}
                      rows={2}
                      placeholder="e.g. Focus on identifying blockers, report in bullet points"
                    />
                  </div>
                  {broadcastAIError && <p className="text-xs text-red-400">{broadcastAIError}</p>}
                  <div className="flex justify-end">
                    <Button
                      onClick={async () => {
                        if (!broadcastTitle.trim()) { setBroadcastAIError('Enter a title first'); return }
                        setBroadcastAIGenerating(true)
                        setBroadcastAIError('')
                        try {
                          const result = await api.tasks.generateDescription(broadcastTitle, broadcastAIHint, broadcastAIProviderID)
                          setBroadcastDescription(result.description)
                          setShowBroadcastAI(false)
                          setBroadcastAIHint('')
                        } catch (error: unknown) {
                          setBroadcastAIError(getErrorMessage(error))
                        } finally {
                          setBroadcastAIGenerating(false)
                        }
                      }}
                      disabled={broadcastAIGenerating}
                    >
                      {broadcastAIGenerating ? 'Generating…' : '✦ Generate'}
                    </Button>
                  </div>
                </div>
              )}
              <Textarea id="broadcast-description" value={broadcastDescription} onChange={e => setBroadcastDescription(e.target.value)} rows={5} placeholder="Instructions sent to every team member…" />
            </div>
            {broadcastMessage && (
              <p className={`text-sm ${broadcastMessage.startsWith('Broadcast queued') ? 'text-green-400' : 'text-red-400'}`}>
                {broadcastMessage}
              </p>
            )}
            <div className="flex gap-3 justify-end">
              <Button variant="secondary" onClick={() => setShowBroadcast(false)}>Cancel</Button>
              <Button onClick={broadcast} disabled={broadcastSaving || !broadcastProjectId || !broadcastTitle.trim()}>
                {broadcastSaving ? 'Broadcasting…' : 'Send Broadcast'}
              </Button>
            </div>
          </div>
        </Modal>
      )}

      {/* Delete confirmation */}
      {showDeleteConfirm && (
        <Modal title="Delete Team" onClose={() => setShowDeleteConfirm(false)}>
          <div className="space-y-4">
            <p className="text-slate-300 text-sm">
              Delete <span className="text-white font-semibold">{team.name}</span>?
              Agents will not be deleted — only the team grouping is removed.
            </p>
            <div className="flex gap-3 justify-end">
              <Button variant="secondary" onClick={() => setShowDeleteConfirm(false)}>Cancel</Button>
              <Button className="bg-red-600 hover:bg-red-700 text-white" onClick={deleteTeam} disabled={deleting}>
                {deleting ? 'Deleting…' : 'Delete Team'}
              </Button>
            </div>
          </div>
        </Modal>
      )}
    </div>
  )
}
