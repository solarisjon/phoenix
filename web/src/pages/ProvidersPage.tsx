import { useState, useEffect } from 'react'
import { api, type Provider } from '@/lib/api'
import { Card, CardBody } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Modal } from '@/components/ui/modal'
import { Input, Textarea, Select, Label } from '@/components/ui/input'
import { EmptyState } from '@/components/ui/empty'

const defaultLLMConfig = JSON.stringify({
  endpoint: '',
  auth_header: '',
  model: 'gpt-4o',
  cost_per_input_token: 0.000005,
  cost_per_output_token: 0.000015,
}, null, 2)

function ProviderForm({ initial, onSave, onClose }: {
  initial?: Provider
  onSave: () => void
  onClose: () => void
}) {
  const [name, setName] = useState(initial?.name ?? '')
  const [type, setType] = useState<'llm' | 'coding_agent'>(initial?.type ?? 'llm')
  const [config, setConfig] = useState(initial?.config ?? defaultLLMConfig)
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)

  const save = async () => {
    setError('')
    try {
      JSON.parse(config)
    } catch {
      setError('Config must be valid JSON')
      return
    }
    setSaving(true)
    try {
      if (initial) {
        await api.providers.update(initial.id, { name, type, config })
      } else {
        await api.providers.create({ name, type, config })
      }
      onSave()
    } catch (e: any) {
      setError(e.message)
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="space-y-4">
      <div>
        <Label htmlFor="name">Name</Label>
        <Input id="name" value={name} onChange={e => setName(e.target.value)} placeholder="e.g. LLM Proxy" />
      </div>
      <div>
        <Label htmlFor="type">Type</Label>
        <Select id="type" value={type} onChange={e => setType(e.target.value as any)}>
          <option value="llm">LLM Endpoint</option>
          <option value="coding_agent">Coding Agent</option>
        </Select>
      </div>
      <div>
        <Label htmlFor="config">Configuration (JSON)</Label>
        <Textarea id="config" value={config} onChange={e => setConfig(e.target.value)} rows={10}
          className="font-mono text-xs" placeholder="{}" />
        {type === 'llm' && (
          <p className="text-xs text-slate-500 mt-1">
            Fields: endpoint, auth_header, model, cost_per_input_token, cost_per_output_token
          </p>
        )}
      </div>
      {error && <p className="text-sm text-red-400">{error}</p>}
      <div className="flex gap-3 justify-end pt-2">
        <Button variant="secondary" onClick={onClose}>Cancel</Button>
        <Button onClick={save} disabled={saving}>{saving ? 'Saving…' : 'Save Provider'}</Button>
      </div>
    </div>
  )
}

export function ProvidersPage() {
  const [providers, setProviders] = useState<Provider[]>([])
  const [loading, setLoading] = useState(true)
  const [showForm, setShowForm] = useState(false)
  const [editing, setEditing] = useState<Provider | undefined>()

  const load = async () => {
    try { setProviders(await api.providers.list()) }
    finally { setLoading(false) }
  }

  useEffect(() => { load() }, [])

  const remove = async (id: string) => {
    if (!confirm('Delete this provider?')) return
    await api.providers.delete(id)
    load()
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-white">Providers</h1>
          <p className="text-slate-400 text-sm mt-1">Configure LLM endpoints and coding agent connections</p>
        </div>
        <Button onClick={() => { setEditing(undefined); setShowForm(true) }}>+ Add Provider</Button>
      </div>

      {loading ? (
        <div className="text-slate-500 text-sm">Loading…</div>
      ) : providers.length === 0 ? (
        <EmptyState icon="⊕" title="No providers configured"
          description="Add your first LLM endpoint to start running agents."
          action={<Button onClick={() => setShowForm(true)}>Add Provider</Button>} />
      ) : (
        <div className="grid gap-4">
          {providers.map(p => (
            <Card key={p.id}>
              <CardBody className="flex items-start justify-between gap-4">
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-3 mb-1">
                    <h3 className="font-medium text-white">{p.name}</h3>
                    <Badge variant={p.type === 'llm' ? 'info' : 'default'}>
                      {p.type === 'llm' ? 'LLM Endpoint' : 'Coding Agent'}
                    </Badge>
                  </div>
                  <p className="text-xs text-slate-500 font-mono truncate">
                    {(() => { try { return JSON.parse(p.config).endpoint || JSON.parse(p.config).binary_path || '–' } catch { return '–' } })()}
                  </p>
                </div>
                <div className="flex gap-2 flex-shrink-0">
                  <Button variant="ghost" size="sm" onClick={() => { setEditing(p); setShowForm(true) }}>Edit</Button>
                  <Button variant="danger" size="sm" onClick={() => remove(p.id)}>Delete</Button>
                </div>
              </CardBody>
            </Card>
          ))}
        </div>
      )}

      {showForm && (
        <Modal title={editing ? 'Edit Provider' : 'Add Provider'} onClose={() => setShowForm(false)}>
          <ProviderForm initial={editing} onSave={() => { setShowForm(false); load() }} onClose={() => setShowForm(false)} />
        </Modal>
      )}
    </div>
  )
}
