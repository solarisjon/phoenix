import { useState, useEffect } from 'react'
import { api, type Provider } from '@/lib/api'
import { Card, CardBody } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Modal } from '@/components/ui/modal'
import { Input, Select, Label } from '@/components/ui/input'
import { EmptyState } from '@/components/ui/empty'
import { cn } from '@/lib/utils'

// Env variable helper — renders a small hint and lets the user type ${ENV_VAR}
function EnvHint() {
  return (
    <p className="text-xs text-slate-600 mt-1">
      Use <code className="bg-slate-800 px-1 rounded text-slate-400">{'${ENV_VAR}'}</code> to reference environment variables
    </p>
  )
}

interface LLMConfig {
  endpoint: string
  auth_header: string
  model: string
  cost_per_input_token: number
  cost_per_output_token: number
  timeout_seconds: number
}

interface CodingAgentConfig {
  kind: string
  binary_path: string
  model: string
  agent: string
  working_dir: string
  dangerously_skip_permissions: boolean
  extra_args: string[]
}

function LLMFields({ cfg, onChange }: {
  cfg: LLMConfig
  onChange: (c: LLMConfig) => void
}) {
  const set = (key: keyof LLMConfig) => (e: React.ChangeEvent<HTMLInputElement>) =>
    onChange({ ...cfg, [key]: key.startsWith('cost') || key === 'timeout_seconds' ? Number(e.target.value) : e.target.value })

  return (
    <div className="space-y-4">
      <div>
        <Label htmlFor="endpoint">Endpoint URL *</Label>
        <Input id="endpoint" value={cfg.endpoint} onChange={set('endpoint')}
          placeholder="https://api.openai.com/v1  or  ${LLM_ENDPOINT}" />
        <EnvHint />
      </div>
      <div>
        <Label htmlFor="auth">Auth Header</Label>
        <Input id="auth" value={cfg.auth_header} onChange={set('auth_header')}
          placeholder="Bearer ${OPENAI_API_KEY}" />
        <EnvHint />
      </div>
      <div>
        <Label htmlFor="model">Model</Label>
        <Input id="model" value={cfg.model} onChange={set('model')}
          placeholder="gpt-4o" />
      </div>
      <div className="grid grid-cols-2 gap-4">
        <div>
          <Label htmlFor="cost_in">Cost / Input Token (USD)</Label>
          <Input id="cost_in" type="number" step="0.000001" value={cfg.cost_per_input_token} onChange={set('cost_per_input_token')}
            placeholder="0.000005" />
        </div>
        <div>
          <Label htmlFor="cost_out">Cost / Output Token (USD)</Label>
          <Input id="cost_out" type="number" step="0.000001" value={cfg.cost_per_output_token} onChange={set('cost_per_output_token')}
            placeholder="0.000015" />
        </div>
      </div>
      <div>
        <Label htmlFor="timeout">Timeout (seconds)</Label>
        <Input id="timeout" type="number" value={cfg.timeout_seconds} onChange={set('timeout_seconds')}
          placeholder="60" />
      </div>
    </div>
  )
}

function CodingAgentFields({ cfg, onChange }: {
  cfg: CodingAgentConfig
  onChange: (c: CodingAgentConfig) => void
}) {
  const setStr = (key: keyof CodingAgentConfig) => (e: React.ChangeEvent<HTMLInputElement | HTMLSelectElement>) =>
    onChange({ ...cfg, [key]: e.target.value })

  return (
    <div className="space-y-4">
      <div>
        <Label htmlFor="kind">Agent Kind</Label>
        <Select id="kind" value={cfg.kind} onChange={setStr('kind') as any}>
          <option value="opencode">opencode</option>
        </Select>
        <p className="text-xs text-slate-600 mt-1">More coding agents coming in future phases</p>
      </div>
      <div>
        <Label htmlFor="binary">Binary Path</Label>
        <Input id="binary" value={cfg.binary_path} onChange={setStr('binary_path') as any}
          placeholder="opencode  or  /usr/local/bin/opencode  or  ${OPENCODE_PATH}" />
        <p className="text-xs text-slate-600 mt-1">Leave blank to use <code className="bg-slate-800 px-1 rounded text-slate-400">opencode</code> from PATH</p>
        <EnvHint />
      </div>
      <div className="grid grid-cols-2 gap-4">
        <div>
          <Label htmlFor="oc-model">Model</Label>
          <Input id="oc-model" value={cfg.model} onChange={setStr('model') as any}
            placeholder="llm-proxy/claude-sonnet-4.6" />
          <p className="text-xs text-slate-600 mt-1">Format: provider/model</p>
        </div>
        <div>
          <Label htmlFor="oc-agent">Agent Config</Label>
          <Input id="oc-agent" value={cfg.agent} onChange={setStr('agent') as any}
            placeholder="my-agent" />
          <p className="text-xs text-slate-600 mt-1">Named opencode agent (optional)</p>
        </div>
      </div>
      <div>
        <Label htmlFor="workdir">Working Directory</Label>
        <Input id="workdir" value={cfg.working_dir} onChange={setStr('working_dir') as any}
          placeholder="/home/user/project  or  ${WORKSPACE}" />
        <EnvHint />
      </div>
    </div>
  )
}

const defaultLLM: LLMConfig = {
  endpoint: '',
  auth_header: '',
  model: 'gpt-4o',
  cost_per_input_token: 0.000005,
  cost_per_output_token: 0.000015,
  timeout_seconds: 60,
}

const defaultCoding: CodingAgentConfig = {
  kind: 'opencode',
  binary_path: '',
  model: '',
  agent: '',
  working_dir: '',
  dangerously_skip_permissions: false,
  extra_args: [],
}

function parseConfig(type: string, configJSON: string): LLMConfig | CodingAgentConfig {
  try {
    return JSON.parse(configJSON)
  } catch {
    return type === 'llm' ? { ...defaultLLM } : { ...defaultCoding }
  }
}

function ProviderForm({ initial, onSave, onClose }: {
  initial?: Provider
  onSave: () => void
  onClose: () => void
}) {
  const [name, setName] = useState(initial?.name ?? '')
  const [type, setType] = useState<'llm' | 'coding_agent'>(initial?.type ?? 'llm')
  const [llmCfg, setLlmCfg] = useState<LLMConfig>(
    initial?.type === 'llm' ? parseConfig('llm', initial.config) as LLMConfig : { ...defaultLLM }
  )
  const [codingCfg, setCodingCfg] = useState<CodingAgentConfig>(
    initial?.type === 'coding_agent' ? parseConfig('coding_agent', initial.config) as CodingAgentConfig : { ...defaultCoding }
  )
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)

  const save = async () => {
    setError('')
    if (!name.trim()) { setError('Name is required'); return }

    const cfg = type === 'llm' ? llmCfg : codingCfg

    if (type === 'llm' && !(cfg as LLMConfig).endpoint.trim()) {
      setError('Endpoint URL is required')
      return
    }
    // binary_path is optional for coding agents (defaults to PATH lookup)
    void cfg

    setSaving(true)
    try {
      const data = { name, type, config: JSON.stringify(cfg) }
      if (initial) await api.providers.update(initial.id, data)
      else await api.providers.create(data)
      onSave()
    } catch (e: any) {
      setError(e.message)
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="space-y-5">
      {/* Name + Type */}
      <div className="grid grid-cols-2 gap-4">
        <div>
          <Label htmlFor="name">Name *</Label>
          <Input id="name" value={name} onChange={e => setName(e.target.value)}
            placeholder="e.g. LLM Proxy" />
        </div>
        <div>
          <Label htmlFor="type">Type</Label>
          <Select id="type" value={type} onChange={e => {
            setType(e.target.value as any)
            setError('')
          }}>
            <option value="llm">LLM Endpoint</option>
            <option value="coding_agent">Coding Agent (pi, opencode…)</option>
          </Select>
        </div>
      </div>

      {/* Divider */}
      <div className="border-t border-slate-800 pt-4">
        <p className="text-xs font-medium text-slate-400 uppercase tracking-wide mb-4">
          {type === 'llm' ? 'LLM Endpoint Configuration' : 'Coding Agent Configuration'}
        </p>
        {type === 'llm'
          ? <LLMFields cfg={llmCfg} onChange={setLlmCfg} />
          : <CodingAgentFields cfg={codingCfg} onChange={setCodingCfg} />
        }
      </div>

      {error && (
        <div className="bg-red-900/20 border border-red-800 rounded-lg px-3 py-2">
          <p className="text-sm text-red-400">{error}</p>
        </div>
      )}

      <div className="flex gap-3 justify-end pt-2">
        <Button variant="secondary" onClick={onClose}>Cancel</Button>
        <Button onClick={save} disabled={saving}>
          {saving ? 'Saving…' : initial ? 'Update Provider' : 'Add Provider'}
        </Button>
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
    catch { /* ignore */ }
    finally { setLoading(false) }
  }

  useEffect(() => { load() }, [])

  const remove = async (id: string) => {
    if (!confirm('Delete this provider? Any agents using it will stop working.')) return
    try { await api.providers.delete(id); load() }
    catch (e: any) { alert(e.message) }
  }

  const endpointLabel = (p: Provider) => {
    try {
      const cfg = JSON.parse(p.config)
      return cfg.endpoint || cfg.binary_path || '–'
    } catch { return '–' }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold text-white">Providers</h1>
          <p className="text-slate-400 text-sm mt-1">
            Configure LLM endpoints and coding agent connections.{' '}
            <span className="text-slate-500">Use <code className="bg-slate-800 px-1 rounded text-xs">{'${ENV_VAR}'}</code> for secrets.</span>
          </p>
        </div>
        <Button onClick={() => { setEditing(undefined); setShowForm(true) }}>+ Add Provider</Button>
      </div>

      {loading ? (
        <div className="text-slate-500 text-sm">Loading…</div>
      ) : providers.length === 0 ? (
        <EmptyState icon="⊕" title="No providers configured"
          description="Add your first LLM endpoint or coding agent to start running agents."
          action={<Button onClick={() => setShowForm(true)}>Add Provider</Button>} />
      ) : (
        <div className="grid gap-4">
          {providers.map(p => (
            <Card key={p.id}>
              <CardBody className="flex items-start justify-between gap-4">
                <div className="flex-1 min-w-0">
                  <div className="flex items-center gap-3 mb-1.5">
                    <div className={cn(
                      'w-8 h-8 rounded-lg flex items-center justify-center text-sm flex-shrink-0',
                      p.type === 'llm'
                        ? 'bg-violet-900/50 border border-violet-800/50 text-violet-400'
                        : 'bg-emerald-900/50 border border-emerald-800/50 text-emerald-400'
                    )}>
                      {p.type === 'llm' ? '⬡' : '⌘'}
                    </div>
                    <div>
                      <h3 className="font-medium text-white">{p.name}</h3>
                      <Badge variant={p.type === 'llm' ? 'info' : 'success'} className="mt-0.5">
                        {p.type === 'llm' ? 'LLM Endpoint' : 'Coding Agent'}
                      </Badge>
                    </div>
                  </div>
                  <p className="text-xs text-slate-500 font-mono truncate pl-11">{endpointLabel(p)}</p>
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
        <Modal
          title={editing ? `Edit: ${editing.name}` : 'Add Provider'}
          onClose={() => setShowForm(false)}
          className="max-w-xl"
        >
          <ProviderForm
            initial={editing}
            onSave={() => { setShowForm(false); load() }}
            onClose={() => setShowForm(false)}
          />
        </Modal>
      )}
    </div>
  )
}
