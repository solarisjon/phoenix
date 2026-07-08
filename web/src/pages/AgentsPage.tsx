import { useState, useEffect, useRef } from 'react'
import { Link } from 'react-router-dom'
import { api, type Agent, type Provider } from '@/lib/api'
import { Card, CardBody } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Modal } from '@/components/ui/modal'
import { Input, Textarea, Select, Label } from '@/components/ui/input'
import { EmptyState } from '@/components/ui/empty'
import { ModelComboBox } from '@/components/ui/model-combo-box'
import { getErrorMessage } from '@/lib/errors'

// ---- Generate modal ----

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
  const [isOrchestrator, setIsOrchestrator] = useState(initial?.is_orchestrator ?? false)
  const [maxConcurrent, setMaxConcurrent] = useState<number>(initial?.max_concurrent ?? 1)
  const [maxCostPerRun, setMaxCostPerRun] = useState<number>(initial?.max_cost_per_run ?? 0)
  const [fallbackModel, setFallbackModel] = useState(initial?.fallback_model ?? '')
  const [status, setStatus] = useState(initial?.status ?? 'active')
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)
  const [showGenerate, setShowGenerate] = useState(false)
  const [genHint, setGenHint] = useState('')
  const [genProviderID, setGenProviderID] = useState(
    providers.find(p => p.type === 'llm')?.id ?? providers[0]?.id ?? ''
  )
  const [generating, setGenerating] = useState(false)
  const [genError, setGenError] = useState('')
  const [memoryEnabled, setMemoryEnabled] = useState(false)
  const [clearingMemory, setClearingMemory] = useState(false)
  const [memoryCleared, setMemoryCleared] = useState(false)

  const selectedProvider = providers.find(p => p.id === providerID)

  useEffect(() => {
    api.plugins.list('memory').then(plugins => {
      setMemoryEnabled(plugins.some(p => p.kind === 'hindsight' && p.enabled))
    }).catch(() => {})
  }, [])

  const clearMemory = async () => {
    if (!initial?.id) return
    if (!confirm('Clear all memories for this agent? This cannot be undone.')) return
    setClearingMemory(true)
    try {
      await api.agents.clearMemory(initial.id)
      setMemoryCleared(true)
      setTimeout(() => setMemoryCleared(false), 3000)
    } catch (e: unknown) {
      setError(getErrorMessage(e))
    } finally {
      setClearingMemory(false)
    }
  }

  const save = async () => {
    setError('')
    if (!name.trim()) { setError('Name is required'); return }
    if (!providerID) { setError('Select a provider'); return }
    setSaving(true)
    try {
      const data = { name, behaviour, guardrails, hard_guardrails: hardGuardrails, provider_id: providerID, model_override: modelOverride, can_spawn_agents: canSpawnAgents, can_hire_agents: canHireAgents, is_orchestrator: isOrchestrator, max_concurrent: maxConcurrent, max_cost_per_run: maxCostPerRun, fallback_model: fallbackModel, status }
      if (initial) await api.agents.update(initial.id, data)
      else await api.agents.create(data)
      onSave()
    } catch (error: unknown) {
      setError(getErrorMessage(error))
    } finally {
      setSaving(false)
    }
  }

  const generate = async () => {
    if (!genHint.trim()) return
    setGenError('')
    setGenerating(true)
    try {
      const result = await api.agents.generate(genHint, genProviderID)
      setBehaviour(result.behaviour || [result.persona, result.instructions].filter(Boolean).join('\n\n'))
      setGuardrails(result.guardrails)
      setHardGuardrails(result.hard_guardrails ?? '')
      setShowGenerate(false)
      setGenHint('')
    } catch (e: unknown) {
      setGenError(getErrorMessage(e))
    } finally {
      setGenerating(false)
    }
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
            <Select id="status" value={status} onChange={e => setStatus(e.target.value as Agent['status'])}>
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

        {/* Max cost per run */}
        <div className="border-t border-slate-800 pt-4">
          <Label htmlFor="max-cost-per-run">Max Cost Per Run</Label>
          <div className="flex items-center gap-2 mt-1">
            <span className="text-slate-400 text-sm">$</span>
            <Input
              id="max-cost-per-run"
              type="number"
              min="0"
              step="0.001"
              value={maxCostPerRun}
              onChange={e => setMaxCostPerRun(Math.max(0, parseFloat(e.target.value) || 0))}
              className="w-32"
            />
            <span className="text-slate-500 text-sm">
              {maxCostPerRun === 0 ? '(unlimited)' : `USD per run`}
            </span>
          </div>
          <p className="text-xs text-slate-500 mt-1">
            Hard ceiling on estimated cost per run (USD). Oldest context turns are dropped until the estimate fits. Only applies to LLM providers with cost rates configured. Set to 0 for unlimited.
          </p>
        </div>

        {/* Fallback model when cost budget overflows */}
        {maxCostPerRun > 0 && (
          <div className="border-t border-slate-800 pt-4">
            <Label htmlFor="fallback-model">
              Fallback Model
              <span className="text-slate-600 font-normal ml-2">
                (used when cost budget is exceeded after context truncation)
              </span>
            </Label>
            <ModelComboBox
              providerId={providerID}
              value={fallbackModel}
              onChange={setFallbackModel}
              allowEmpty
              placeholder="Select or type a model name (blank = fail on overflow)"
            />
            <p className="text-xs text-slate-500 mt-1">
              If the prompt still exceeds the cost budget after dropping all context, the runner switches to this model instead of failing. Typically a lighter, cheaper model.
            </p>
          </div>
        )}

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

        {/* Orchestrator toggle */}
        <div className="border-t border-slate-800 pt-4">
          <label className="flex items-start gap-3 cursor-pointer">
            <input
              type="checkbox"
              checked={isOrchestrator}
              onChange={e => setIsOrchestrator(e.target.checked)}
              className="mt-0.5 rounded"
            />
            <div>
              <p className="text-sm text-slate-200 font-medium">Global Orchestrator ★</p>
              <p className="text-xs text-slate-500 mt-0.5">
                Designates this agent as the global orchestrator for dynamic task routing.
                Configure the active orchestrator in Settings → Orchestration.
                Only one agent should be the designated orchestrator at a time.
              </p>
            </div>
          </label>
        </div>

        {/* Persona / Instructions / Guardrails with generate button */}
        <div className="border-t border-slate-800 pt-4">
          <div className="flex items-center justify-between mb-3">
            <p className="text-xs font-medium text-slate-400 uppercase tracking-wide">Agent Configuration</p>
            <Button variant="ghost" size="sm" onClick={() => { setShowGenerate(v => !v); setGenError('') }}>
              ✦ {showGenerate ? 'Hide AI assist' : 'Generate with AI'}
            </Button>
          </div>

          {showGenerate && (
            <div className="mb-4 rounded-lg border border-violet-800/50 bg-violet-950/30 p-3 space-y-3">
              <p className="text-sm text-slate-400">Describe what you want this agent to do and AI will generate its behaviour and guardrails.</p>
              {providers.length > 1 && (
                <div>
                  <Label htmlFor="gen-provider">Generate using</Label>
                  <Select id="gen-provider" value={genProviderID} onChange={e => setGenProviderID(e.target.value)}>
                    {providers.map(p => <option key={p.id} value={p.id}>{p.name}</option>)}
                  </Select>
                </div>
              )}
              <div>
                <Label htmlFor="gen-hint">Agent description</Label>
                <Textarea id="gen-hint" value={genHint} onChange={e => setGenHint(e.target.value)} rows={4}
                  placeholder="e.g. A senior software engineer who reviews pull requests, focuses on security and performance, writes concise actionable feedback, and escalates critical issues immediately." />
              </div>
              {genError && <p className="text-sm text-red-400">{genError}</p>}
              <div className="flex gap-3 justify-end">
                <Button variant="secondary" size="sm" onClick={() => setShowGenerate(false)}>Cancel</Button>
                <Button size="sm" onClick={generate} disabled={generating || !genHint.trim()}>
                  {generating ? 'Generating…' : '✦ Generate'}
                </Button>
              </div>
            </div>
          )}

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

        {/* Persistent Memory */}
        {initial?.id && memoryEnabled && (
          <div className="border-t border-slate-800 pt-4">
            <p className="text-xs font-medium text-slate-400 uppercase tracking-wide mb-2">Persistent Memory</p>
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-slate-300">This agent has persistent memory via Hindsight.</p>
                <p className="text-xs text-slate-500 mt-0.5">
                  Memories are stored after each successful task and recalled automatically before new tasks run.
                </p>
              </div>
              <div className="flex items-center gap-2 ml-4 shrink-0">
                {memoryCleared && <span className="text-xs text-emerald-400">Cleared</span>}
                <Button
                  variant="secondary"
                  size="sm"
                  onClick={clearMemory}
                  disabled={clearingMemory}
                >
                  {clearingMemory ? 'Clearing…' : 'Clear Memory'}
                </Button>
              </div>
            </div>
          </div>
        )}

        {error && <p className="text-sm text-red-400">{error}</p>}
        <div className="flex gap-3 justify-end pt-2">
          <Button variant="secondary" onClick={onClose}>Cancel</Button>
          <Button onClick={save} disabled={saving}>{saving ? 'Saving…' : 'Save Agent'}</Button>
        </div>
      </div>

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
  const [importMessage, setImportMessage] = useState('')
  const fileInputRef = useRef<HTMLInputElement | null>(null)

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

  const exportAgent = async (agent: Agent) => {
    const blob = await api.agents.export(agent.id)
    const url = window.URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = `${agent.name.toLowerCase().replace(/\s+/g, '-') || 'agent'}-agent.json`
    document.body.appendChild(a)
    a.click()
    a.remove()
    window.URL.revokeObjectURL(url)
  }

  const importAgent = async (file: File) => {
    try {
      setImportMessage('')
      const raw = await file.text()
      const bundle = JSON.parse(raw)
      const agent = await api.agents.importAgent({ bundle })
      setImportMessage(`Imported ${agent.name}.`)
      await load()
    } catch (error: unknown) {
      setImportMessage(getErrorMessage(error, 'Import failed'))
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-white">Agents</h1>
          <p className="text-slate-400 text-sm mt-1">Configure agents and their behaviours, guardrails, and capabilities</p>
        </div>
        <div className="flex items-center gap-2">
          <input
            ref={fileInputRef}
            type="file"
            accept="application/json"
            className="hidden"
            onChange={async e => {
              const file = e.target.files?.[0]
              if (file) await importAgent(file)
              e.target.value = ''
            }}
          />
          <Button variant="secondary" onClick={() => fileInputRef.current?.click()}>Import Agent</Button>
          <Button onClick={() => { setEditing(undefined); setShowForm(true) }}>+ Create Agent</Button>
        </div>
      </div>

      {importMessage && <p className={`text-sm ${importMessage.startsWith('Imported ') ? 'text-green-400' : 'text-red-400'}`}>{importMessage}</p>}

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
                        <div className="flex items-center gap-2">
                          <h3 className="font-medium text-white">{a.name}</h3>
                          {a.is_orchestrator && (
                            <span className="text-xs bg-violet-900/50 border border-violet-700/50 text-violet-300 px-1.5 py-0.5 rounded-full font-medium">★ Orchestrator</span>
                          )}
                          {a.created_by === 'orchestrator' && (
                            <span className="text-xs bg-slate-800 border border-slate-700 text-slate-400 px-1.5 py-0.5 rounded-full">auto</span>
                          )}
                        </div>
                        <p className="text-xs text-slate-500">
                          {providerName(a.provider_id)}
                          {a.model_override && <span className="text-slate-600"> · {a.model_override}</span>}
                        </p>
                      </div>
                      <Badge variant={statusVariant[a.status]}>{a.status}</Badge>
                    </div>
                    {(a.behaviour || a.persona) && (
                      <p className="text-sm text-slate-400 line-clamp-2 pl-11">{a.behaviour || a.persona}</p>
                    )}
                  </div>
                  <div className="flex gap-2 flex-shrink-0">
                    <Button variant="ghost" size="sm" onClick={() => exportAgent(a)}>Export</Button>
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
