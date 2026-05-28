import { useState, useEffect } from 'react'
import { api, type Agent, type Provider } from '@/lib/api'
import { Card, CardBody } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Modal } from '@/components/ui/modal'
import { Input, Textarea, Select, Label } from '@/components/ui/input'
import { EmptyState } from '@/components/ui/empty'
import { formatCost } from '@/lib/utils'

function AgentForm({ initial, providers, onSave, onClose }: {
  initial?: Agent
  providers: Provider[]
  onSave: () => void
  onClose: () => void
}) {
  const [name, setName] = useState(initial?.name ?? '')
  const [persona, setPersona] = useState(initial?.persona ?? '')
  const [instructions, setInstructions] = useState(initial?.instructions ?? '')
  const [guardrails, setGuardrails] = useState(initial?.guardrails ?? '')
  const [providerID, setProviderID] = useState(initial?.provider_id ?? providers[0]?.id ?? '')
  const [status, setStatus] = useState(initial?.status ?? 'active')
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)

  const save = async () => {
    setError('')
    if (!name.trim()) { setError('Name is required'); return }
    if (!providerID) { setError('Select a provider'); return }
    setSaving(true)
    try {
      const data = { name, persona, instructions, guardrails, provider_id: providerID, status }
      if (initial) await api.agents.update(initial.id, data)
      else await api.agents.create(data)
      onSave()
    } catch (e: any) {
      setError(e.message)
    } finally {
      setSaving(false)
    }
  }

  return (
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
      <div>
        <Label htmlFor="persona">Persona</Label>
        <Textarea id="persona" value={persona} onChange={e => setPersona(e.target.value)} rows={3}
          placeholder="High-level personality and role description…" />
      </div>
      <div>
        <Label htmlFor="instructions">Instructions</Label>
        <Textarea id="instructions" value={instructions} onChange={e => setInstructions(e.target.value)} rows={4}
          placeholder="Detailed operational instructions…" />
      </div>
      <div>
        <Label htmlFor="guardrails">Guardrails</Label>
        <Textarea id="guardrails" value={guardrails} onChange={e => setGuardrails(e.target.value)} rows={3}
          placeholder="Constraints, boundaries, escalation rules…" />
      </div>
      {error && <p className="text-sm text-red-400">{error}</p>}
      <div className="flex gap-3 justify-end pt-2">
        <Button variant="secondary" onClick={onClose}>Cancel</Button>
        <Button onClick={save} disabled={saving}>{saving ? 'Saving…' : 'Save Agent'}</Button>
      </div>
    </div>
  )
}

const statusVariant: Record<string, 'success' | 'warning' | 'muted'> = {
  active: 'success', paused: 'warning', disabled: 'muted'
}

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
          description="Create your first agent with a persona, instructions, and a provider."
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
                        <p className="text-xs text-slate-500">{providerName(a.provider_id)}</p>
                      </div>
                      <Badge variant={statusVariant[a.status]}>{a.status}</Badge>
                    </div>
                    {a.persona && (
                      <p className="text-sm text-slate-400 line-clamp-2 pl-11">{a.persona}</p>
                    )}
                  </div>
                  <div className="flex gap-2 flex-shrink-0">
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
