import { useState, useEffect, useCallback } from 'react'
import { Link, useNavigate } from 'react-router-dom'
import { api, type Project, type Agent } from '@/lib/api'
import { Card, CardBody } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Modal } from '@/components/ui/modal'
import { Input, Textarea, Label, Select } from '@/components/ui/input'
import { EmptyState } from '@/components/ui/empty'
import { timeAgo } from '@/lib/utils'

// ---- Shared helpers ----

const formatInterval = (secs: number | null) => {
  if (!secs) return ''
  if (secs < 60) return `every ${secs}s`
  if (secs < 3600) return `every ${Math.round(secs / 60)}m`
  return `every ${Math.round(secs / 3600)}h`
}

// ---- Monitor form (create + edit) ----

function MonitorForm({ initial, initialAgents, allAgents, onSave, onClose }: {
  initial?: Project
  initialAgents?: Agent[]
  allAgents: Agent[]
  onSave: () => void
  onClose: () => void
}) {
  const isEdit = !!initial
  const [name, setName] = useState(initial?.name ?? '')
  const [description, setDescription] = useState(initial?.description ?? '')
  const [workingDir, setWorkingDir] = useState(initial?.working_dir ?? '')
  const [assignedAgents, setAssignedAgents] = useState<Agent[]>(initialAgents ?? [])
  const [addAgentId, setAddAgentId] = useState('')
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)

  const unassigned = allAgents.filter(a => !assignedAgents.find(x => x.id === a.id))

  const addAgent = async () => {
    if (!addAgentId || !initial) return
    const agent = allAgents.find(a => a.id === addAgentId)
    if (!agent) return
    await api.projects.assignAgent(initial.id, addAgentId)
    setAssignedAgents(prev => [...prev, agent])
    setAddAgentId('')
  }

  const removeAgent = async (agentId: string) => {
    if (!initial) return
    await api.projects.removeAgent(initial.id, agentId)
    setAssignedAgents(prev => prev.filter(a => a.id !== agentId))
  }

  const hasHeartbeatAgent = assignedAgents.some(a => (a.heartbeat_interval ?? 0) > 0)

  const save = async () => {
    if (!name.trim()) { setError('Name is required'); return }
    setSaving(true)
    setError('')
    try {
      if (isEdit) {
        await api.projects.update(initial!.id, {
          name: name.trim(), description, working_dir: workingDir.trim(), kind: 'monitor',
        })
      } else {
        if (assignedAgents.length === 0) { setError('Add at least one agent'); setSaving(false); return }
        const proj = await api.projects.create({
          name: name.trim(), description, working_dir: workingDir.trim(), kind: 'monitor',
        })
        await Promise.all(assignedAgents.map(a => api.projects.assignAgent(proj.id, a.id)))
      }
      onSave()
    } catch (e: unknown) {
      setError(e instanceof Error ? e.message : 'Failed to save monitor')
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="space-y-4">
      <div>
        <Label htmlFor="mon-name">Monitor Name</Label>
        <Input id="mon-name" value={name} onChange={e => setName(e.target.value)}
          placeholder="e.g. Jira Queue Monitor" />
      </div>
      <div>
        <Label htmlFor="mon-desc">Description</Label>
        <Textarea id="mon-desc" value={description} onChange={e => setDescription(e.target.value)} rows={2}
          placeholder="What does this monitor watch and do?" />
      </div>
      <div>
        <Label htmlFor="mon-wdir">Working Directory <span className="text-slate-500 font-normal">(optional)</span></Label>
        <Input id="mon-wdir" value={workingDir} onChange={e => setWorkingDir(e.target.value)}
          placeholder="/path/to/project" />
      </div>

      {/* Agents section */}
      <div>
        <Label>Agents</Label>
        {!hasHeartbeatAgent && (
          <div className="bg-amber-900/20 border border-amber-700/40 rounded-lg px-3 py-2 text-xs text-amber-300 mb-2">
            No assigned agent has a heartbeat interval — this monitor won't fire automatically.
            Set a heartbeat interval in{' '}
            <a href="/settings?tab=agents" className="underline hover:text-amber-200">Settings → Agents</a>.
          </div>
        )}
        {assignedAgents.length > 0 && (
          <div className="space-y-1 mb-2">
            {assignedAgents.map(a => (
              <div key={a.id} className="flex items-center justify-between bg-slate-800 rounded-lg px-3 py-2">
                <div>
                  <span className="text-sm text-white">{a.name}</span>
                  {(a.heartbeat_interval ?? 0) > 0 && (
                    <span className="ml-2 text-xs text-violet-400">{formatInterval(a.heartbeat_interval ?? null)}</span>
                  )}
                </div>
                {isEdit && (
                  <button onClick={() => removeAgent(a.id)}
                    className="text-xs text-slate-500 hover:text-red-400 transition-colors">
                    Remove
                  </button>
                )}
              </div>
            ))}
          </div>
        )}
        {unassigned.length > 0 && (
          <div className="flex gap-2">
            <Select value={addAgentId} onChange={e => setAddAgentId(e.target.value)} className="flex-1">
              <option value="">Add an agent…</option>
              {unassigned.map(a => (
                <option key={a.id} value={a.id}>
                  {a.name}{(a.heartbeat_interval ?? 0) > 0 ? ` — ${formatInterval(a.heartbeat_interval ?? null)}` : ''}
                </option>
              ))}
            </Select>
            <Button variant="secondary" onClick={isEdit ? addAgent : () => {
              const agent = allAgents.find(a => a.id === addAgentId)
              if (agent && !assignedAgents.find(x => x.id === agent.id)) {
                setAssignedAgents(prev => [...prev, agent])
                setAddAgentId('')
              }
            }} disabled={!addAgentId}>
              Add
            </Button>
          </div>
        )}
      </div>

      {error && <p className="text-sm text-red-400">{error}</p>}
      <div className="flex gap-3 justify-end pt-2">
        <Button variant="secondary" onClick={onClose}>Cancel</Button>
        <Button onClick={save} disabled={saving}>
          {saving ? 'Saving…' : isEdit ? 'Save' : 'Create Monitor'}
        </Button>
      </div>
    </div>
  )
}

// ---- Monitor card ----

function MonitorCard({ monitor, agents, allAgents, onDelete, onEdited }: {
  monitor: Project
  agents: Agent[]
  allAgents: Agent[]
  onDelete: () => void
  onEdited: () => void
}) {
  const navigate = useNavigate()
  const [showEdit, setShowEdit] = useState(false)
  const heartbeatAgent = agents.find(a => (a.heartbeat_interval ?? 0) > 0)
  const interval = heartbeatAgent ? formatInterval(heartbeatAgent.heartbeat_interval ?? null) : null
  const hasHeartbeat = agents.some(a => (a.heartbeat_interval ?? 0) > 0)

  return (
    <>
      <Card className="hover:border-slate-700 transition-colors">
        <CardBody className="flex items-start justify-between gap-4">
          {/* Clickable info area — navigates to detail page */}
          <div
            className="flex-1 min-w-0 cursor-pointer"
            onClick={() => navigate(`/monitors/${monitor.id}`)}
          >
            <div className="flex items-center gap-3 mb-1">
              <span className="text-base">⟳</span>
              <h3 className="font-medium text-white">{monitor.name}</h3>
              <Badge variant={monitor.status === 'active' ? 'success' : 'muted'}>{monitor.status}</Badge>
              {!hasHeartbeat && (
                <Badge variant="warning">no heartbeat agent</Badge>
              )}
            </div>
            {monitor.description && (
              <p className="text-sm text-slate-400 line-clamp-1 ml-6">{monitor.description}</p>
            )}
            <div className="flex items-center gap-3 ml-6 mt-1.5 text-xs text-slate-500">
              {agents.length > 0
                ? agents.map(a => (
                    <span key={a.id} className="text-slate-400">
                      {a.name}{(a.heartbeat_interval ?? 0) > 0 && interval ? ` · ${interval}` : ''}
                    </span>
                  ))
                : <span className="text-amber-500">No agents assigned</span>
              }
              {monitor.working_dir && (
                <span className="font-mono truncate" title={monitor.working_dir}>
                  📁 {monitor.working_dir.split('/').pop()}
                </span>
              )}
              <span>Created {timeAgo(monitor.created_at)}</span>
            </div>
          </div>
          <div className="flex gap-2 flex-shrink-0">
            <Button variant="secondary" size="sm" onClick={() => setShowEdit(true)}>Edit</Button>
            <Button variant="danger" size="sm" onClick={onDelete}>Delete</Button>
          </div>
        </CardBody>
      </Card>

      {showEdit && (
        <Modal title={`Edit: ${monitor.name}`} onClose={() => setShowEdit(false)} className="max-w-lg">
          <MonitorForm
            initial={monitor}
            initialAgents={agents}
            allAgents={allAgents}
            onSave={() => { setShowEdit(false); onEdited() }}
            onClose={() => setShowEdit(false)}
          />
        </Modal>
      )}
    </>
  )
}

// ---- Page ----

export function MonitorsPage() {
  const [monitors, setMonitors] = useState<Project[]>([])
  const [agentsByMonitor, setAgentsByMonitor] = useState<Record<string, Agent[]>>({})
  const [allAgents, setAllAgents] = useState<Agent[]>([])
  const [loading, setLoading] = useState(true)
  const [showForm, setShowForm] = useState(false)

  const load = useCallback(async () => {
    try {
      const [mons, agents] = await Promise.all([
        api.projects.list('monitor'),
        api.agents.list(),
      ])
      setMonitors(mons)
      setAllAgents(agents)
      // Load assigned agents for each monitor
      const agentMap: Record<string, Agent[]> = {}
      await Promise.all(mons.map(async m => {
        agentMap[m.id] = await api.projects.listAgents(m.id)
      }))
      setAgentsByMonitor(agentMap)
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { load() }, [load])

  const deleteMonitor = async (id: string, name: string) => {
    if (!confirm(`Delete monitor "${name}" and all its run history?`)) return
    await api.projects.delete(id)
    load()
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-white">Monitors</h1>
          <p className="text-slate-400 text-sm mt-1">Autonomous agents that wake on a schedule, do their work, and sleep</p>
        </div>
        <Button onClick={() => setShowForm(true)}>+ New Monitor</Button>
      </div>

      {loading ? (
        <div className="text-slate-500 text-sm">Loading…</div>
      ) : monitors.length === 0 ? (
        <EmptyState
          icon="⟳"
          title="No monitors yet"
          description="Create a monitor to let a heartbeat agent watch external systems, triage work, and dispatch tasks automatically."
          action={<Button onClick={() => setShowForm(true)}>New Monitor</Button>}
        />
      ) : (
        <div className="grid gap-4">
          {monitors.map(m => (
            <MonitorCard
              key={m.id}
              monitor={m}
              agents={agentsByMonitor[m.id] ?? []}
              allAgents={allAgents}
              onDelete={() => deleteMonitor(m.id, m.name)}
              onEdited={load}
            />
          ))}
        </div>
      )}

      {showForm && (
        <Modal title="New Monitor" onClose={() => setShowForm(false)} className="max-w-lg">
          <MonitorForm
            allAgents={allAgents}
            onSave={() => { setShowForm(false); load() }}
            onClose={() => setShowForm(false)}
          />
        </Modal>
      )}
    </div>
  )
}
