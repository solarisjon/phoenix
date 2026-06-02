import { useState, useEffect } from 'react'
import { Link } from 'react-router-dom'
import { api, type Agent, type Provider } from '@/lib/api'
import { Card, CardBody } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Modal } from '@/components/ui/modal'
import { Input, Textarea, Select, Label } from '@/components/ui/input'
import { EmptyState } from '@/components/ui/empty'
import { ModelComboBox } from '@/components/ui/model-combo-box'

// ---- Generate modal ----

function formatInterval(secs: number): string {
  if (secs < 60) return `${secs}s`
  if (secs < 3600) return `${Math.round(secs / 60)}m`
  if (secs < 86400) return `${(secs / 3600).toFixed(1).replace(/\.0$/, '')}h`
  return `${(secs / 86400).toFixed(1).replace(/\.0$/, '')}d`
}

function GenerateModal({ providers, onApply, onClose }: {
  providers: Provider[]
  onApply: (behaviour: string, guardrails: string, hardGuardrails: string) => void
  onClose: () => void
}) {
  const [description, setDescription] = useState('')
  const [providerId, setProviderId] = useState(
    providers.find(p => p.type === 'llm')?.id ?? providers[0]?.id ?? ''
  )
  const [generating, setGenerating] = useState(false)
  const [error, setError] = useState('')

  const generate = async () => {
    if (!description.trim()) return
    setError('')
    setGenerating(true)
    try {
      const result = await api.agents.generate(description, providerId)
      onApply(
        result.behaviour || [result.persona, result.instructions].filter(Boolean).join('\n\n'),
        result.guardrails,
        result.hard_guardrails ?? ''
      )
    } catch (e: any) {
      setError(e.message)
    } finally {
      setGenerating(false)
    }
  }

  return (
    <div className="space-y-4">
      <p className="text-sm text-slate-400">
        Describe what you want this agent to do and an AI will generate its behaviour and guardrails.
      </p>
      <div>
        <Label htmlFor="gen-provider">Generate using</Label>
        <Select id="gen-provider" value={providerId} onChange={e => setProviderId(e.target.value)}>
          {providers.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
        </Select>
      </div>
      <div>
        <Label htmlFor="gen-desc">Agent description</Label>
        <Textarea id="gen-desc" value={description} onChange={e => setDescription(e.target.value)} rows={5}
          placeholder="e.g. A senior software engineer who reviews pull requests, focuses on security and performance, writes concise actionable feedback, and escalates critical issues immediately." />
      </div>
      {error && <p className="text-sm text-red-400">{error}</p>}
      <div className="flex gap-3 justify-end">
        <Button variant="secondary" onClick={onClose}>Cancel</Button>
        <Button onClick={generate} disabled={generating || !description.trim()}>
          {generating ? 'Generating…' : '✦ Generate'}
        </Button>
      </div>
    </div>
  )
}

// ---- Agent form ----

function AgentForm({ initial, providers, onSave, onClose }: {
  initial?: Agent
  providers: Provider[]
  onSave: () => void
  onClose: () => void
}) {
  const [name, setName] = useState(initial?.name ?? '')
  const [behaviour, setBehaviour] = useState(
    initial?.behaviour || [initial?.persona, initial?.instructions].filter(Boolean).join('\n\n') || ''
  )
  const [guardrails, setGuardrails] = useState(initial?.guardrails ?? '')
  const [hardGuardrails, setHardGuardrails] = useState(initial?.hard_guardrails ?? '')
  const [providerID, setProviderID] = useState(initial?.provider_id ?? providers[0]?.id ?? '')
  const [modelOverride, setModelOverride] = useState(initial?.model_override ?? '')
  const [canSpawnAgents, setCanSpawnAgents] = useState(initial?.can_spawn_agents ?? false)
  const [canHireAgents, setCanHireAgents] = useState(initial?.can_hire_agents ?? false)
  const [heartbeatInterval, setHeartbeatInterval] = useState<string>(
    initial?.heartbeat_interval != null ? String(initial.heartbeat_interval) : ''
  )
  const [maxConcurrent, setMaxConcurrent] = useState<number>(initial?.max_concurrent ?? 1)
  const [status, setStatus] = useState(initial?.status ?? 'active')
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)
  const [showGenerate, setShowGenerate] = useState(false)

  const selectedProvider = providers.find(p => p.id === providerID)

  const save = async () => {
    setError('')
    if (!name.trim()) { setError('Name is required'); return }
    if (!providerID) { setError('Select a provider'); return }
    setSaving(true)
    try {
      const hbSecs = heartbeatInterval.trim() ? parseInt(heartbeatInterval, 10) : null
      const data = { name, behaviour, guardrails, hard_guardrails: hardGuardrails, provider_id: providerID, model_override: modelOverride, can_spawn_agents: canSpawnAgents, can_hire_agents: canHireAgents, heartbeat_interval: hbSecs, max_concurrent: maxConcurrent, status }
      if (initial) await api.agents.update(initial.id, data)
      else await api.agents.create(data)
      onSave()
    } catch (e: any) {
      setError(e.message)
    } finally {
      setSaving(false)
    }
  }

  const applyGenerated = (b: string, g: string, hg: string) => {
    setBehaviour(b)
    setGuardrails(g)
    setHardGuardrails(hg)
    setShowGenerate(false)
  }

  return (
    <>
      <div className="space-y-4">
        <div className="grid grid-cols-2 gap-4">
          <div className="col-span-2">
            <Label htmlFor="name">Name</Label>
            <Input id="name" value={name} onChange={e => setName(e.target.value)} placeholder="e.g. Senior Ops Manager" />
          </div>
          <div>
            <Label htmlFor="provider">Provider</Label>
            <Select id="provider" value={providerID} onChange={e => setProviderID(e.target.value)}>
              <option value="">Select provider…</option>
              {providers.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
            </Select>
          </div>
          <div>
            <Label htmlFor="status">Status</Label>
            <Select id="status" value={status} onChange={e => setStatus(e.target.value as any)}>
              <option value="active">Active</option>
              <option value="paused">Paused</option>
              <option value="disabled">Disabled</option>
            </Select>
          </div>
        </div>

        {/* Model override */}
        <div>
          <Label htmlFor="model-override">
            Model Override
            <span className="text-slate-600 font-normal ml-2">
              (overrides {selectedProvider?.name ?? 'provider'}'s default)
            </span>
          </Label>
          <ModelComboBox
            providerId={providerID}
            value={modelOverride}
            onChange={setModelOverride}
            allowEmpty
            placeholder="Select or type a model name (blank = provider default)"
          />
        </div>

        {/* Heartbeat interval */}
        <div className="border-t border-slate-800 pt-4">
          <Label htmlFor="hb">Heartbeat Interval <span className="text-slate-500 font-normal">(optional)</span></Label>
          <div className="flex items-center gap-2 mt-1">
            <Input
              id="hb"
              type="number"
              min="60"
              step="60"
              value={heartbeatInterval}
              onChange={e => setHeartbeatInterval(e.target.value)}
              placeholder="e.g. 3600"
              className="w-36"
            />
            <span className="text-slate-500 text-sm">seconds</span>
            {heartbeatInterval && !isNaN(parseInt(heartbeatInterval)) && (
              <span className="text-slate-400 text-xs">
                ≈ every {formatInterval(parseInt(heartbeatInterval))}
              </span>
            )}
          </div>
          <p className="text-xs text-slate-500 mt-1">
            When set, the agent will automatically receive a scheduled check-in task at this interval for each project it's assigned to.
            Minimum 60 seconds. Leave blank for manual-only.
          </p>
        </div>

        {/* Max concurrent tasks */}
        <div className="border-t border-slate-800 pt-4">
          <Label htmlFor="max-concurrent">Max Concurrent Tasks</Label>
          <div className="flex items-center gap-2 mt-1">
            <Input
              id="max-concurrent"
              type="number"
              min="0"
              max="20"
              step="1"
              value={maxConcurrent}
              onChange={e => setMaxConcurrent(Math.max(0, parseInt(e.target.value) || 0))}
              className="w-24"
            />
            <span className="text-slate-500 text-sm">
              {maxConcurrent === 0 ? '(unlimited)' : maxConcurrent === 1 ? 'task at a time' : 'tasks at a time'}
            </span>
          </div>
          <p className="text-xs text-slate-500 mt-1">
            Limits how many tasks this agent runs simultaneously. Extra tasks queue until a slot opens. Set to 0 for unlimited.
          </p>
        </div>

        {/* Spawn agents toggle */}
        <div className="border-t border-slate-800 pt-4">
          <label className="flex items-start gap-3 cursor-pointer">
            <input
              type="checkbox"
              checked={canSpawnAgents}
              onChange={e => setCanSpawnAgents(e.target.checked)}
              className="mt-0.5 rounded"
            />
            <div>
              <p className="text-sm text-slate-200 font-medium">Allow agent to spawn tasks for other agents</p>
              <p className="text-xs text-slate-500 mt-0.5">
                When enabled, this agent's system prompt includes instructions to call
                <code className="bg-slate-800 px-1 rounded mx-1 text-slate-400">POST /api/agents/spawn</code>
                to delegate work. Off by default.
              </p>
            </div>
          </label>
        </div>

        {/* Hire agents toggle */}
        <div className="border-t border-slate-800 pt-4">
          <label className="flex items-start gap-3 cursor-pointer">
            <input
              type="checkbox"
              checked={canHireAgents}
              onChange={e => setCanHireAgents(e.target.checked)}
              className="mt-0.5 rounded"
            />
            <div>
              <p className="text-sm text-slate-200 font-medium">Allow agent to hire new agents 🧑‍💼</p>
              <p className="text-xs text-slate-500 mt-0.5">
                When enabled, this agent can propose new agent hires during task execution.
                Proposals land in the <strong className="text-slate-400">Inbox</strong> for human review and approval before any agent is created.
              </p>
            </div>
          </label>
        </div>

        {/* Persona / Instructions / Guardrails with generate button */}
        <div className="border-t border-slate-800 pt-4">
          <div className="flex items-center justify-between mb-4">
            <p className="text-xs font-medium text-slate-400 uppercase tracking-wide">Agent Configuration</p>
            <Button variant="ghost" size="sm" onClick={() => setShowGenerate(true)}>
              ✦ Generate with AI
            </Button>
          </div>

          <div className="space-y-4">
            <div>
              <Label htmlFor="behaviour">Behaviour</Label>
              <Textarea id="behaviour" value={behaviour} onChange={e => setBehaviour(e.target.value)} rows={7}
                placeholder="Describe who this agent is, their personality, communication style, and detailed operational instructions for what they do and how…" />
            </div>
            <div>
              <Label htmlFor="guardrails">Soft Guardrails <span className="text-slate-500 font-normal">(advisory)</span></Label>
              <Textarea id="guardrails" value={guardrails} onChange={e => setGuardrails(e.target.value)} rows={3}
                placeholder="Advisory constraints the agent should try to follow. Documented if unavoidable." />
            </div>
            <div>
              <Label htmlFor="hard-guardrails" className="flex items-center gap-2">
                Hard Guardrails <span className="text-amber-400 text-xs font-normal">⚠ Requires human approval</span>
              </Label>
              <Textarea id="hard-guardrails" value={hardGuardrails} onChange={e => setHardGuardrails(e.target.value)} rows={3}
                className="border-amber-900/40 focus:border-amber-600/60"
                placeholder="Mandatory rules. If triggered, the agent stops and waits for your approval before proceeding. E.g: Never delete data without approval. Never send external emails." />
            </div>
          </div>
        </div>

        {error && <p className="text-sm text-red-400">{error}</p>}
        <div className="flex gap-3 justify-end pt-2">
          <Button variant="secondary" onClick={onClose}>Cancel</Button>
          <Button onClick={save} disabled={saving}>{saving ? 'Saving…' : 'Save Agent'}</Button>
        </div>
      </div>

      {showGenerate && (
        <Modal title="Generate Agent Configuration" onClose={() => setShowGenerate(false)} className="max-w-xl">
          <GenerateModal
            providers={providers}
            onApply={applyGenerated}
            onClose={() => setShowGenerate(false)}
          />
        </Modal>
      )}
    </>
  )
}

// ---- Status helpers ----

const statusVariant: Record<string, 'success' | 'warning' | 'muted'> = {
  active: 'success', paused: 'warning', disabled: 'muted'
}

// ---- Page ----

export function AgentsPage() {
  const [agents, setAgents] = useState<Agent[]>([])
  const [providers, setProviders] = useState<Provider[]>([])
  const [loading, setLoading] = useState(true)
  const [showForm, setShowForm] = useState(false)
  const [editing, setEditing] = useState<Agent | undefined>()

  const load = async () => {
    try {
      const [a, p] = await Promise.all([api.agents.list(), api.providers.list()])
      setAgents(a); setProviders(p)
    } finally { setLoading(false) }
  }

  useEffect(() => { load() }, [])

  const remove = async (id: string) => {
    if (!confirm('Delete this agent?')) return
    await api.agents.delete(id)
    load()
  }

  const providerName = (id: string) => providers.find(p => p.id === id)?.name ?? '–'

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-white">Agents</h1>
          <p className="text-slate-400 text-sm mt-1">Configure your AI agents and their personas</p>
        </div>
        <Button onClick={() => { setEditing(undefined); setShowForm(true) }}>+ Create Agent</Button>
      </div>

      {loading ? (
        <div className="text-slate-500 text-sm">Loading…</div>
      ) : agents.length === 0 ? (
        <EmptyState icon="⬡" title="No agents yet"
          description="Create your first agent with a behaviour description and a provider."
          action={<Button onClick={() => setShowForm(true)}>Create Agent</Button>} />
      ) : (
        <div className="grid gap-4">
          {agents.map(a => (
            <Card key={a.id}>
              <CardBody>
                <div className="flex items-start justify-between gap-4">
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center gap-3 mb-2">
                      <div className="w-8 h-8 rounded-lg bg-violet-900/50 border border-violet-800/50 flex items-center justify-center text-violet-400 font-bold text-sm flex-shrink-0">
                        {a.name.charAt(0).toUpperCase()}
                      </div>
                      <div>
                        <h3 className="font-medium text-white">{a.name}</h3>
                        <p className="text-xs text-slate-500">
                          {providerName(a.provider_id)}
                          {a.model_override && <span className="text-slate-600"> · {a.model_override}</span>}
                          {a.heartbeat_interval && (
                            <span className="text-slate-600"> · ⏱ {formatInterval(a.heartbeat_interval)}</span>
                          )}
                        </p>
                      </div>
                      <Badge variant={statusVariant[a.status]}>{a.status}</Badge>
                    </div>
                    {(a.behaviour || a.persona) && (
                      <p className="text-sm text-slate-400 line-clamp-2 pl-11">{a.behaviour || a.persona}</p>
                    )}
                  </div>
                  <div className="flex gap-2 flex-shrink-0">
                    <Link to={`/agents/${a.id}/activity`}>
                      <Button variant="ghost" size="sm">Activity</Button>
                    </Link>
                    <Button variant="ghost" size="sm" onClick={() => { setEditing(a); setShowForm(true) }}>Edit</Button>
                    <Button variant="danger" size="sm" onClick={() => remove(a.id)}>Delete</Button>
                  </div>
                </div>
              </CardBody>
            </Card>
          ))}
        </div>
      )}

      {showForm && (
        <Modal title={editing ? 'Edit Agent' : 'Create Agent'} onClose={() => setShowForm(false)} className="max-w-2xl">
          <AgentForm initial={editing} providers={providers} onSave={() => { setShowForm(false); load() }} onClose={() => setShowForm(false)} />
        </Modal>
      )}
    </div>
  )
}
