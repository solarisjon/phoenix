import { useState, useEffect } from 'react'
import { api, type Provider } from '@/lib/api'
import { Card, CardBody } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Badge } from '@/components/ui/badge'
import { Modal } from '@/components/ui/modal'
import { Input, Select, Label } from '@/components/ui/input'
import { EmptyState } from '@/components/ui/empty'
import { cn } from '@/lib/utils'
import { ModelComboBox } from '@/components/ui/model-combo-box'

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
  agent: string          // opencode only
  working_dir: string
  dangerously_skip_permissions: boolean
  thinking: string       // pi only
  tools: string          // pi only
  no_session: boolean    // pi only
  yolo: boolean          // crush only
  max_budget_usd: number // claudecode only
  extra_args: string[]
}

function LLMFields({ cfg, onChange, providerId }: {
  cfg: LLMConfig
  onChange: (c: LLMConfig) => void
  providerId?: string
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
        <ModelComboBox
          providerId={providerId}
          directFetch={!providerId && cfg.endpoint ? { kind: 'llm', endpoint: cfg.endpoint, authHeader: cfg.auth_header } : undefined}
          value={cfg.model}
          onChange={v => onChange({ ...cfg, model: v })}
          placeholder="gpt-4o"
        />
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
  const setBool = (key: keyof CodingAgentConfig) => (e: React.ChangeEvent<HTMLInputElement>) =>
    onChange({ ...cfg, [key]: e.target.checked })
  const setNum = (key: keyof CodingAgentConfig) => (e: React.ChangeEvent<HTMLInputElement>) =>
    onChange({ ...cfg, [key]: Number(e.target.value) })

  const binaryPlaceholder: Record<string, string> = {
    opencode: 'opencode  or  /opt/homebrew/bin/opencode',
    pi: 'pi  or  /opt/homebrew/bin/pi',
    claudecode: 'claude  or  /opt/homebrew/bin/claude',
    crush: 'crush  or  /opt/homebrew/bin/crush',
  }

  return (
    <div className="space-y-4">
      {/* Kind */}
      <div>
        <Label htmlFor="kind">Agent Kind</Label>
        <Select id="kind" value={cfg.kind} onChange={setStr('kind') as any}>
          <option value="opencode">opencode</option>
          <option value="pi">pi</option>
          <option value="claudecode">Claude Code (claude)</option>
          <option value="crush">Crush</option>
        </Select>
      </div>

      {/* Binary + working dir — common to all */}
      <div>
        <Label htmlFor="binary">Binary Path</Label>
        <Input id="binary" value={cfg.binary_path} onChange={setStr('binary_path') as any}
          placeholder={binaryPlaceholder[cfg.kind] ?? 'path or leave blank for PATH'} />
        <p className="text-xs text-slate-600 mt-1">Leave blank to resolve from PATH</p>
        <EnvHint />
      </div>
      <div>
        <Label htmlFor="workdir">Working Directory</Label>
        <Input id="workdir" value={cfg.working_dir} onChange={setStr('working_dir') as any}
          placeholder="/home/user/project  or  ${WORKSPACE}" />
        <EnvHint />
      </div>

      {/* Model — common */}
      <div>
        <Label htmlFor="oc-model">Model</Label>
        <Input id="oc-model" value={cfg.model} onChange={setStr('model') as any}
          placeholder={
            cfg.kind === 'opencode' ? 'llm-proxy/claude-sonnet-4.6' :
            cfg.kind === 'pi'       ? 'llm-proxy/claude-sonnet-4.6  or  sonnet' :
            cfg.kind === 'crush'    ? 'anthropic/claude-sonnet-4-5  or  sonnet' :
                                      'claude-opus-4-5  or  sonnet'
          } />
        <p className="text-xs text-slate-600 mt-1">Leave blank to use the agent's default model</p>
      </div>

      {/* opencode-specific */}
      {cfg.kind === 'opencode' && (
        <div>
          <Label htmlFor="oc-agent">Agent Config Name</Label>
          <Input id="oc-agent" value={cfg.agent} onChange={setStr('agent') as any}
            placeholder="my-agent" />
          <p className="text-xs text-slate-600 mt-1">Named opencode agent configuration (optional)</p>
        </div>
      )}

      {/* pi-specific */}
      {cfg.kind === 'pi' && (
        <div className="space-y-4">
          <div>
            <Label htmlFor="pi-thinking">Thinking Level</Label>
            <Select id="pi-thinking" value={cfg.thinking} onChange={setStr('thinking') as any}>
              <option value="">(default)</option>
              <option value="off">off</option>
              <option value="minimal">minimal</option>
              <option value="low">low</option>
              <option value="medium">medium</option>
              <option value="high">high</option>
              <option value="xhigh">xhigh</option>
            </Select>
          </div>
          <div>
            <Label htmlFor="pi-tools">Allowed Tools</Label>
            <Input id="pi-tools" value={cfg.tools} onChange={setStr('tools') as any}
              placeholder="read,bash,write  (blank = all)" />
          </div>
          <label className="flex items-center gap-2 text-sm text-slate-300 cursor-pointer">
            <input type="checkbox" checked={cfg.no_session} onChange={setBool('no_session') as any}
              className="rounded" />
            No session (ephemeral — recommended for Phoenix-managed tasks)
          </label>
        </div>
      )}

      {/* crush-specific */}
      {cfg.kind === 'crush' && (
        <div>
          <p className="text-xs text-slate-500">
            System prompt is delivered via <code className="bg-slate-800 px-1 rounded text-slate-400">AGENTS.md</code> in the working directory.
            Tool permissions are configured via crush's own config (<code className="bg-slate-800 px-1 rounded text-slate-400">~/.config/crush/crush.json</code>).
          </p>
        </div>
      )}

      {/* claudecode-specific */}
      {cfg.kind === 'claudecode' && (
        <div className="space-y-4">
          <div>
            <Label htmlFor="cc-budget">Max Budget (USD, 0 = unlimited)</Label>
            <Input id="cc-budget" type="number" step="0.01" value={cfg.max_budget_usd}
              onChange={setNum('max_budget_usd') as any} placeholder="0" />
          </div>
        </div>
      )}

      {/* Skip permissions — common */}
      <label className="flex items-center gap-2 text-sm text-slate-300 cursor-pointer">
        <input type="checkbox" checked={cfg.dangerously_skip_permissions}
          onChange={setBool('dangerously_skip_permissions') as any}
          className="rounded" />
        Dangerously skip permissions (auto-approve all tool use)
      </label>
    </div>
  )
}

interface OllamaConfig {
  kind: 'ollama'
  base_url: string
  model: string
  keep_thinking: boolean
  timeout_seconds: number
}

const defaultOllama: OllamaConfig = {
  kind: 'ollama',
  base_url: 'http://localhost:11434',
  model: '',
  keep_thinking: false,
  timeout_seconds: 300,
}

function OllamaFields({ cfg, onChange, providerId }: {
  cfg: OllamaConfig
  onChange: (c: OllamaConfig) => void
  providerId?: string
}) {
  const set = (key: keyof OllamaConfig) => (e: React.ChangeEvent<HTMLInputElement>) =>
    onChange({ ...cfg, [key]: e.target.value })
  const setNum = (key: keyof OllamaConfig) => (e: React.ChangeEvent<HTMLInputElement>) =>
    onChange({ ...cfg, [key]: Number(e.target.value) })
  const setBool = (key: keyof OllamaConfig) => (e: React.ChangeEvent<HTMLInputElement>) =>
    onChange({ ...cfg, [key]: e.target.checked })

  return (
    <div className="space-y-4">
      <div>
        <Label htmlFor="ol-url">Ollama Server URL</Label>
        <Input id="ol-url" value={cfg.base_url} onChange={set('base_url')}
          placeholder="http://localhost:11434" />
        <p className="text-xs text-slate-600 mt-1">Default: http://localhost:11434 — change if Ollama runs remotely.</p>
      </div>
      <div>
        <Label htmlFor="ol-model">Model *</Label>
        <ModelComboBox
          providerId={providerId}
          directFetch={!providerId ? { kind: 'ollama', baseUrl: cfg.base_url || 'http://localhost:11434' } : undefined}
          value={cfg.model}
          onChange={v => onChange({ ...cfg, model: v })}
          placeholder="e.g. qwen3.5:latest, llama3.2:3b"
        />
      </div>
      <div>
        <Label htmlFor="ol-timeout">Timeout (seconds)</Label>
        <Input id="ol-timeout" type="number" value={cfg.timeout_seconds} onChange={setNum('timeout_seconds')}
          placeholder="300" />
        <p className="text-xs text-slate-600 mt-1">Local models can be slow to generate. 300s (5 min) is a safe default.</p>
      </div>
      <label className="flex items-center gap-2 text-sm text-slate-300 cursor-pointer">
        <input type="checkbox" checked={cfg.keep_thinking} onChange={setBool('keep_thinking')} className="rounded" />
        Show thinking tokens in output (chain-of-thought models like Qwen3)
      </label>
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
  thinking: '',
  tools: '',
  no_session: true,
  yolo: false,
  max_budget_usd: 0,
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
  const [llmCfg, setLlmCfg] = useState<LLMConfig>(
    initial?.type === 'llm' ? parseConfig('llm', initial.config) as LLMConfig : { ...defaultLLM }
  )
  const [codingCfg, setCodingCfg] = useState<CodingAgentConfig>(
    initial?.type === 'coding_agent' ? parseConfig('coding_agent', initial.config) as CodingAgentConfig : { ...defaultCoding }
  )
  // Ollama is stored as type=llm with kind=ollama in config
  const isOllama = initial?.type === 'llm' && (() => { try { return JSON.parse(initial.config).kind === 'ollama' } catch { return false } })()
  const [ollamaCfg, setOllamaCfg] = useState<OllamaConfig>(
    isOllama ? parseConfig('llm', initial!.config) as unknown as OllamaConfig : { ...defaultOllama }
  )
  const [uiType, setUiType] = useState<'llm' | 'ollama' | 'coding_agent'>(
    initial?.type === 'coding_agent' ? 'coding_agent' : isOllama ? 'ollama' : 'llm'
  )
  const [error, setError] = useState('')
  const [saving, setSaving] = useState(false)

  const save = async () => {
    setError('')
    if (!name.trim()) { setError('Name is required'); return }

    let storageType: 'llm' | 'coding_agent'
    let cfg: object
    if (uiType === 'llm') {
      storageType = 'llm'
      cfg = llmCfg
      if (!(llmCfg as LLMConfig).endpoint.trim()) { setError('Endpoint URL is required'); return }
    } else if (uiType === 'ollama') {
      storageType = 'llm'
      cfg = ollamaCfg
      if (!ollamaCfg.model.trim()) { setError('Model is required'); return }
    } else {
      storageType = 'coding_agent'
      cfg = codingCfg
    }

    setSaving(true)
    try {
      const data = { name, type: storageType, config: JSON.stringify(cfg) }
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
          <Select id="type" value={uiType} onChange={e => {
            setUiType(e.target.value as any)
            setError('')
          }}>
            <option value="ollama">🧠 Ollama (local models)</option>
            <option value="llm">LLM Endpoint (OpenAI-compatible)</option>
            <option value="coding_agent">Coding Agent (pi, opencode…)</option>
          </Select>
        </div>
      </div>

      {/* Divider */}
      <div className="border-t border-slate-800 pt-4">
        <p className="text-xs font-medium text-slate-400 uppercase tracking-wide mb-4">
          {uiType === 'llm' ? 'LLM Endpoint Configuration' : uiType === 'ollama' ? 'Ollama Configuration' : 'Coding Agent Configuration'}
        </p>
        {uiType === 'llm' && <LLMFields cfg={llmCfg} onChange={setLlmCfg} providerId={initial?.id} />}
        {uiType === 'ollama' && <OllamaFields cfg={ollamaCfg} onChange={setOllamaCfg} providerId={initial?.id} />}
        {uiType === 'coding_agent' && <CodingAgentFields cfg={codingCfg} onChange={setCodingCfg} />}
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
  const [resyncedId, setResyncedId] = useState<string | null>(null)
  const [testStates, setTestStates] = useState<Record<string, { testing: boolean; ok?: boolean; message?: string }>>({})

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

  const resync = async (id: string, name: string) => {
    try {
      await api.providers.resync(id)
      setResyncedId(id)
      setTimeout(() => setResyncedId(null), 2000)
    } catch (e: any) { alert(`Resync failed: ${e.message}`) }
  }

  const testProvider = async (id: string) => {
    setTestStates(prev => ({ ...prev, [id]: { testing: true } }))
    try {
      const result = await api.providers.test(id)
      setTestStates(prev => ({ ...prev, [id]: { testing: false, ok: result.ok, message: result.message } }))
      setTimeout(() => setTestStates(prev => {
        const next = { ...prev }; delete next[id]; return next
      }), 5000)
    } catch (e: any) {
      setTestStates(prev => ({ ...prev, [id]: { testing: false, ok: false, message: e.message } }))
      setTimeout(() => setTestStates(prev => {
        const next = { ...prev }; delete next[id]; return next
      }), 5000)
    }
  }

  const endpointLabel = (p: Provider) => {
    try {
      const cfg = JSON.parse(p.config)
      if (cfg.kind === 'ollama') return `🧠 ${cfg.base_url ?? 'localhost:11434'} · ${cfg.model}`
      const kind = cfg.kind ? `[${cfg.kind}] ` : ''
      return kind + (cfg.endpoint || cfg.binary_path || '–')
    } catch { return '–' }
  }

  const providerIcon = (p: Provider) => {
    try {
      const cfg = JSON.parse(p.config)
      if (cfg.kind === 'ollama') return '🧠'
    } catch { /* */ }
    return p.type === 'llm' ? '⯁' : '⌘'
  }

  const providerLabel = (p: Provider) => {
    try {
      const cfg = JSON.parse(p.config)
      if (cfg.kind === 'ollama') return 'Ollama (local)'
    } catch { /* */ }
    return p.type === 'llm' ? 'LLM Endpoint' : 'Coding Agent'
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
          {providers.map(p => {
            const ts = testStates[p.id]
            return (
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
                      {providerIcon(p)}
                    </div>
                    <div>
                      <h3 className="font-medium text-white">{p.name}</h3>
                      <Badge variant={p.type === 'llm' ? 'info' : 'success'} className="mt-0.5">
                        {providerLabel(p)}
                      </Badge>
                    </div>
                  </div>
                  <p className="text-xs text-slate-500 font-mono truncate pl-11">{endpointLabel(p)}</p>
                  {ts && !ts.testing && ts.message && (
                    <p className={cn(
                      'text-xs mt-2 pl-11',
                      ts.ok ? 'text-emerald-400' : 'text-red-400'
                    )}>
                      {ts.ok ? '✓' : '✗'} {ts.message}
                    </p>
                  )}
                </div>
                <div className="flex gap-2 flex-shrink-0">
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => testProvider(p.id)}
                    disabled={ts?.testing}
                  >
                    {ts?.testing
                      ? (p.type === 'coding_agent' ? '⏳ Testing… (up to 60s)' : '⏳ Testing…')
                      : '▶ Test'}
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => resync(p.id, p.name)}
                  >
                    {resyncedId === p.id ? '✓ Resynced' : '↺ Resync'}
                  </Button>
                  <Button variant="ghost" size="sm" onClick={() => { setEditing(p); setShowForm(true) }}>Edit</Button>
                  <Button variant="danger" size="sm" onClick={() => remove(p.id)}>Delete</Button>
                </div>
              </CardBody>
            </Card>
            )
          })}
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
