import { useEffect, useState } from 'react'
import { api } from '@/lib/api'
import type { PluginRecord, NotificationRule, SystemSettings } from '@/lib/api'

export function PluginsPage() {
  const [tab, setTab] = useState<'notifiers' | 'themes'>('notifiers')
  const [plugins, setPlugins] = useState<PluginRecord[]>([])
  const [settings, setSettings] = useState<SystemSettings | null>(null)
  const [loading, setLoading] = useState(true)

  const load = async () => {
    try {
      const [p, s] = await Promise.all([api.plugins.list(), api.admin.getSettings()])
      setPlugins(p)
      setSettings(s)
    } catch (e: any) { alert(e.message) }
    finally { setLoading(false) }
  }

  useEffect(() => { load() }, [])

  const toggleMaster = async (key: 'core_plugins_enabled' | 'community_plugins_enabled') => {
    if (!settings) return
    const updated = { ...settings, [key]: !settings[key] }
    try {
      await api.admin.saveSettings(updated)
      setSettings(updated)
    } catch (e: any) { alert(e.message) }
  }

  if (loading) return <div className="text-[var(--ph-text-muted)] p-6">Loading…</div>

  const notifiers = plugins.filter(p => p.type === 'notifier')
  const themes = plugins.filter(p => p.type === 'theme')

  return (
    <div className="max-w-4xl mx-auto p-6 space-y-6">
      <h1 className="text-2xl font-bold text-[var(--ph-text)]">Plugins</h1>

      {/* Master switches */}
      {settings && (
        <div className="bg-[var(--ph-card)] border border-[var(--ph-card-border)] rounded-lg p-4 space-y-3">
          <h2 className="text-sm font-semibold text-[var(--ph-text-muted)] uppercase tracking-wide">Master Switches</h2>
          <Toggle label="Enable Core Plugins" description="Telegram, Webhook — shipped with Phoenix"
            checked={settings.core_plugins_enabled} onChange={() => toggleMaster('core_plugins_enabled')} />
          <Toggle label="Enable Community Plugins" description="Custom themes and user-created plugins"
            checked={settings.community_plugins_enabled} onChange={() => toggleMaster('community_plugins_enabled')} />
        </div>
      )}

      {/* Tabs */}
      <div className="flex gap-2 border-b border-[var(--ph-border)]">
        {(['notifiers', 'themes'] as const).map(t => (
          <button key={t} onClick={() => setTab(t)}
            className={`px-4 py-2 text-sm font-medium border-b-2 -mb-px transition-colors ${
              tab === t
                ? 'border-[var(--ph-accent)] text-[var(--ph-accent)]'
                : 'border-transparent text-[var(--ph-text-muted)] hover:text-[var(--ph-text)]'
            }`}>
            {t === 'notifiers' ? 'Notifiers' : 'Themes'}
          </button>
        ))}
      </div>

      {tab === 'notifiers' && <NotifiersTab plugins={notifiers} coreEnabled={settings?.core_plugins_enabled ?? false} onRefresh={load} />}
      {tab === 'themes' && <ThemesTab plugins={themes} communityEnabled={settings?.community_plugins_enabled ?? false} onRefresh={load} />}
    </div>
  )
}

// ---- Toggle component ----
function Toggle({ label, description, checked, onChange }: {
  label: string; description?: string; checked: boolean; onChange: () => void
}) {
  return (
    <div className="flex items-center justify-between">
      <div>
        <div className="text-sm font-medium text-[var(--ph-text)]">{label}</div>
        {description && <div className="text-xs text-[var(--ph-text-muted)]">{description}</div>}
      </div>
      <button onClick={onChange}
        className={`relative w-10 h-5 rounded-full transition-colors ${
          checked ? 'bg-[var(--ph-accent)]' : 'bg-[var(--ph-border)]'
        }`}>
        <span className={`absolute top-0.5 w-4 h-4 rounded-full bg-white transition-transform ${
          checked ? 'translate-x-5' : 'translate-x-0.5'
        }`} />
      </button>
    </div>
  )
}

// ---- Notifiers Tab ----
function NotifiersTab({ plugins, coreEnabled, onRefresh }: {
  plugins: PluginRecord[]; coreEnabled: boolean; onRefresh: () => void
}) {
  return (
    <div className="space-y-4">
      {!coreEnabled && (
        <div className="text-sm text-[var(--ph-text-muted)] bg-[var(--ph-surface)] rounded-lg p-3">
          Core plugins are disabled. Enable them above to configure notifiers.
        </div>
      )}
      {plugins.map(p => (
        <NotifierCard key={p.id} plugin={p} dimmed={!coreEnabled} onRefresh={onRefresh} />
      ))}
      {plugins.length === 0 && (
        <div className="text-sm text-[var(--ph-text-muted)]">No notifier plugins found.</div>
      )}
    </div>
  )
}

// ---- SecretField: text input with show/hide toggle for sensitive values ----
function SecretField({ value, onChange, isSecret }: {
  value: string; onChange: (v: string) => void; isSecret: boolean
}) {
  const [hidden, setHidden] = useState(false)

  return (
    <div className="relative">
      <input
        type={hidden ? 'password' : 'text'}
        value={value}
        onChange={e => onChange(e.target.value)}
        placeholder={isSecret ? '${ENV_VAR} or paste value directly' : ''}
        className="w-full bg-[var(--ph-input)] text-[var(--ph-text)] text-sm rounded px-3 py-2 pr-16 border border-[var(--ph-border)]"
      />
      {isSecret && (
        <button
          type="button"
          onClick={() => setHidden(!hidden)}
          className="absolute right-2 top-1/2 -translate-y-1/2 text-[10px] px-1.5 py-0.5 rounded bg-[var(--ph-surface)] text-[var(--ph-text-muted)] hover:bg-[var(--ph-hover)]"
        >
          {hidden ? 'Show' : 'Hide'}
        </button>
      )}
    </div>
  )
}

// Schema field type from the backend ConfigSchema() response.
interface SchemaField {
  type: string
  title: string
  description?: string
  default?: any
  enum?: string[]
  secret?: boolean
}
interface ConfigSchema {
  type: string
  properties: Record<string, SchemaField>
  required?: string[]
}

function NotifierCard({ plugin, dimmed, onRefresh }: {
  plugin: PluginRecord; dimmed: boolean; onRefresh: () => void
}) {
  const [configOpen, setConfigOpen] = useState(false)
  const [configValues, setConfigValues] = useState<Record<string, any>>(() => {
    try { return JSON.parse(plugin.config) } catch { return {} }
  })
  const [schema, setSchema] = useState<ConfigSchema | null>(null)
  const [schemaLoading, setSchemaLoading] = useState(false)
  const [testing, setTesting] = useState(false)
  const [testResult, setTestResult] = useState<string | null>(null)
  const [rules, setRules] = useState<NotificationRule[]>([])
  const [rulesLoaded, setRulesLoaded] = useState(false)
  const [saving, setSaving] = useState(false)
  const [detectingChats, setDetectingChats] = useState(false)
  const [discoveredChats, setDiscoveredChats] = useState<{id: number, title: string, first_name: string, type: string}[] | null>(null)
  const [chatError, setChatError] = useState<string | null>(null)

  const loadSchema = async () => {
    if (schema) return
    setSchemaLoading(true)
    try {
      const s = await api.plugins.schema(plugin.id)
      setSchema(s)
    } catch { /* schema not available — will show raw JSON fallback */ }
    finally { setSchemaLoading(false) }
  }

  const loadRules = async () => {
    try {
      const r = await api.plugins.rules.list(plugin.id)
      setRules(r)
      setRulesLoaded(true)
    } catch (e: any) { alert(e.message) }
  }

  useEffect(() => {
    if (configOpen) {
      loadSchema()
      if (!rulesLoaded) loadRules()
    }
  }, [configOpen])

  // Re-parse config when plugin prop changes (after save/refresh).
  useEffect(() => {
    try { setConfigValues(JSON.parse(plugin.config)) } catch { setConfigValues({}) }
  }, [plugin.config])

  const toggleEnabled = async () => {
    try {
      plugin.enabled ? await api.plugins.disable(plugin.id) : await api.plugins.enable(plugin.id)
      onRefresh()
    } catch (e: any) { alert(e.message) }
  }

  const saveConfig = async () => {
    setSaving(true)
    try {
      const configStr = JSON.stringify(configValues)
      await api.plugins.update(plugin.id, { ...plugin, config: configStr })
      setConfigOpen(false)
      onRefresh()
    } catch (e: any) { alert(e.message) }
    finally { setSaving(false) }
  }

  const testPlugin = async () => {
    setTesting(true)
    setTestResult(null)
    try {
      // Save current form state first so the test uses the latest config.
      const configStr = JSON.stringify(configValues)
      await api.plugins.update(plugin.id, { ...plugin, config: configStr })
      const res = await api.plugins.test(plugin.id)
      setTestResult(res.message)
      onRefresh()
    } catch (e: any) { setTestResult(`Error: ${e.message}`) }
    finally { setTesting(false) }
  }

  const addRule = async (eventType: string) => {
    try {
      await api.plugins.rules.create(plugin.id, { event_type: eventType, enabled: true })
      loadRules()
    } catch (e: any) { alert(e.message) }
  }

  const deleteRule = async (ruleId: string) => {
    try {
      await api.plugins.rules.delete(plugin.id, ruleId)
      loadRules()
    } catch (e: any) { alert(e.message) }
  }

  const toggleRule = async (rule: NotificationRule) => {
    try {
      await api.plugins.rules.update(plugin.id, rule.id, { ...rule, enabled: !rule.enabled })
      loadRules()
    } catch (e: any) { alert(e.message) }
  }

  const updateField = (key: string, value: any) => {
    setConfigValues(prev => ({ ...prev, [key]: value }))
  }

  const detectChats = async () => {
    setDetectingChats(true)
    setChatError(null)
    setDiscoveredChats(null)
    try {
      // Pass the bot token directly from the form — no save required.
      const chats = await api.plugins.discoverChats(plugin.id, configValues.bot_token)
      setDiscoveredChats(chats)
    } catch (e: any) {
      setChatError(e.message)
    } finally {
      setDetectingChats(false)
    }
  }

  const selectChat = (chatId: number) => {
    updateField('chat_id', String(chatId))
    setDiscoveredChats(null)
  }

  return (
    <div className={`bg-[var(--ph-card)] border border-[var(--ph-card-border)] rounded-lg p-4 space-y-3 ${dimmed ? 'opacity-50 pointer-events-none' : ''}`}>
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-2">
          <span className="font-medium text-[var(--ph-text)]">{plugin.name}</span>
          {plugin.is_core && (
            <span className="text-[10px] font-semibold px-1.5 py-0.5 rounded bg-[var(--ph-accent-bg)] text-[var(--ph-accent)]">CORE</span>
          )}
        </div>
        <div className="flex items-center gap-2">
          <Toggle label="" checked={plugin.enabled} onChange={toggleEnabled} />
          <button onClick={() => setConfigOpen(!configOpen)}
            className="text-xs px-2 py-1 rounded bg-[var(--ph-surface)] text-[var(--ph-text-muted)] hover:bg-[var(--ph-hover)]">
            Configure
          </button>
          <button onClick={testPlugin} disabled={testing}
            className="text-xs px-2 py-1 rounded bg-[var(--ph-accent-bg)] text-[var(--ph-accent)] hover:bg-[var(--ph-accent)] hover:text-[var(--ph-accent-text)] disabled:opacity-50">
            {testing ? 'Sending…' : 'Test'}
          </button>
        </div>
      </div>

      {testResult && (
        <div className={`text-xs p-2 rounded ${testResult.startsWith('Error') ? 'bg-red-900/20 text-red-400' : 'bg-emerald-900/20 text-emerald-400'}`}>
          {testResult}
        </div>
      )}

      {configOpen && (
        <div className="space-y-3 pt-2 border-t border-[var(--ph-border)]">

          {/* Schema-driven config form */}
          {schemaLoading && <div className="text-xs text-[var(--ph-text-muted)]">Loading configuration…</div>}

          {schema && schema.properties && (
            <div className="space-y-3">
              {Object.entries(schema.properties).map(([key, field]) => (
                <div key={key}>
                  <label className="block text-xs font-medium text-[var(--ph-text)] mb-1">
                    {field.title}
                    {schema.required?.includes(key) && <span className="text-red-400 ml-0.5">*</span>}
                  </label>
                  {field.description && (
                    <p className="text-[11px] text-[var(--ph-text-faint)] mb-1.5">{field.description}</p>
                  )}
                  {field.enum ? (
                    <select value={configValues[key] ?? field.default ?? ''}
                      onChange={e => updateField(key, e.target.value)}
                      className="w-full bg-[var(--ph-input)] text-[var(--ph-text)] text-sm rounded px-3 py-2 border border-[var(--ph-border)]">
                      {field.enum.map(v => <option key={v} value={v}>{v}</option>)}
                    </select>
                  ) : field.type === 'integer' ? (
                    <input type="number" value={configValues[key] ?? field.default ?? ''}
                      onChange={e => updateField(key, parseInt(e.target.value) || 0)}
                      className="w-full bg-[var(--ph-input)] text-[var(--ph-text)] text-sm rounded px-3 py-2 border border-[var(--ph-border)]" />
                  ) : (
                    <SecretField
                      value={configValues[key] ?? ''}
                      onChange={v => updateField(key, v)}
                      isSecret={field.secret ?? false}
                    />
                  )}

                  {/* Telegram chat discovery — shown after the chat_id field */}
                  {key === 'chat_id' && plugin.kind === 'telegram' && (
                    <div className="mt-2 space-y-2">
                      <button onClick={detectChats} disabled={detectingChats}
                        className="text-xs px-2 py-1 rounded bg-[var(--ph-surface)] text-[var(--ph-accent)] hover:bg-[var(--ph-hover)] disabled:opacity-50">
                        {detectingChats ? 'Detecting…' : 'Detect Chat ID'}
                      </button>
                      <p className="text-[10px] text-[var(--ph-text-faint)]">
                        Send /start to your bot in Telegram first, then click Detect.
                      </p>

                      {chatError && (
                        <div className="text-xs p-2 rounded bg-red-900/20 text-red-400">{chatError}</div>
                      )}

                      {discoveredChats && discoveredChats.length > 0 && (
                        <div className="bg-[var(--ph-surface)] rounded p-2 space-y-1">
                          <span className="text-[10px] text-[var(--ph-text-muted)] uppercase font-semibold">Available chats — click to use:</span>
                          {discoveredChats.map(chat => (
                            <button key={chat.id} onClick={() => selectChat(chat.id)}
                              className="w-full text-left text-xs px-2 py-1.5 rounded hover:bg-[var(--ph-hover)] flex items-center justify-between">
                              <span className="text-[var(--ph-text)]">
                                {chat.first_name || chat.title || 'Unknown'}
                                <span className="text-[var(--ph-text-faint)] ml-1">({chat.type})</span>
                              </span>
                              <span className="text-[var(--ph-text-muted)] font-mono">{chat.id}</span>
                            </button>
                          ))}
                        </div>
                      )}
                    </div>
                  )}
                </div>
              ))}
            </div>
          )}

          {/* Fallback: raw JSON if no schema available */}
          {!schemaLoading && !schema && (
            <div>
              <label className="block text-xs text-[var(--ph-text-muted)] mb-1">Configuration (JSON)</label>
              <textarea value={JSON.stringify(configValues, null, 2)}
                onChange={e => { try { setConfigValues(JSON.parse(e.target.value)) } catch {} }}
                rows={4}
                className="w-full bg-[var(--ph-input)] text-[var(--ph-text)] text-sm rounded px-3 py-2 border border-[var(--ph-border)] font-mono" />
            </div>
          )}

          <button onClick={saveConfig} disabled={saving}
            className="text-xs px-3 py-1.5 rounded bg-[var(--ph-accent)] text-[var(--ph-accent-text)] hover:opacity-90 disabled:opacity-50">
            {saving ? 'Saving…' : 'Save Configuration'}
          </button>

          {/* Notification Rules */}
          <div className="pt-2 border-t border-[var(--ph-border)]">
            <div className="flex items-center justify-between mb-2">
              <span className="text-xs font-semibold text-[var(--ph-text-muted)] uppercase">Notification Rules</span>
              <select onChange={e => { if (e.target.value) addRule(e.target.value); e.target.value = '' }}
                className="text-xs bg-[var(--ph-input)] text-[var(--ph-text)] rounded px-2 py-1 border border-[var(--ph-border)]">
                <option value="">+ Add Rule…</option>
                <option value="task.completed">Task Completed</option>
                <option value="task.failed">Task Failed</option>
                <option value="task.needs_approval">Needs Approval</option>
                <option value="task.guardrail_triggered">Guardrail Triggered</option>
              </select>
            </div>
            {rules.map(rule => (
              <div key={rule.id} className="flex items-center justify-between py-1.5 text-sm">
                <span className="text-[var(--ph-text)]">{rule.event_type.replace('task.', '').replace(/_/g, ' ')}</span>
                <div className="flex items-center gap-2">
                  <span className="text-xs text-[var(--ph-text-faint)]">
                    {rule.project_id ? `Project: ${rule.project_id.slice(0, 8)}…` : 'All projects'}
                  </span>
                  <Toggle label="" checked={rule.enabled} onChange={() => toggleRule(rule)} />
                  <button onClick={() => deleteRule(rule.id)}
                    className="text-xs text-red-400 hover:text-red-300">✕</button>
                </div>
              </div>
            ))}
            {rulesLoaded && rules.length === 0 && (
              <div className="text-xs text-[var(--ph-text-faint)]">No rules — add one above.</div>
            )}
          </div>
        </div>
      )}
    </div>
  )
}

// ---- Default CSS variable values for new themes ----
const DEFAULT_THEME_VARS: Record<string, string> = {
  'ph-bg': '#1e1f28', 'ph-surface': '#282a36', 'ph-card': '#2d2f3d',
  'ph-card-border': '#3a3d52', 'ph-input': '#353849', 'ph-hover': '#3a3d52',
  'ph-text': '#f8f8f2', 'ph-text-muted': '#a0a4b8', 'ph-text-faint': '#6c7086',
  'ph-accent': '#bd93f9', 'ph-accent-light': '#d4bbff',
  'ph-accent-bg': 'rgba(189,147,249,0.12)', 'ph-accent-text': '#ffffff',
  'ph-border': '#3a3d52', 'ph-border-mid': '#4a4d62',
}

// ---- Theme color form (shared between create and edit) ----
function ThemeColorForm({ name, setName, kind, setKind, vars, setVars, onSave, onCancel, saveLabel }: {
  name: string; setName: (v: string) => void
  kind: 'dark' | 'light'; setKind: (v: 'dark' | 'light') => void
  vars: Record<string, string>; setVars: (fn: (v: Record<string, string>) => Record<string, string>) => void
  onSave: () => void; onCancel: () => void; saveLabel: string
}) {
  return (
    <div className="bg-[var(--ph-card)] border border-[var(--ph-card-border)] rounded-lg p-4 space-y-3">
      <div className="grid grid-cols-2 gap-3">
        <div>
          <label className="block text-xs text-[var(--ph-text-muted)] mb-1">Theme Name</label>
          <input value={name} onChange={e => setName(e.target.value)}
            className="w-full bg-[var(--ph-input)] text-[var(--ph-text)] text-sm rounded px-3 py-2 border border-[var(--ph-border)]"
            placeholder="My Custom Theme" />
        </div>
        <div>
          <label className="block text-xs text-[var(--ph-text-muted)] mb-1">Kind</label>
          <select value={kind} onChange={e => setKind(e.target.value as 'dark' | 'light')}
            className="w-full bg-[var(--ph-input)] text-[var(--ph-text)] text-sm rounded px-3 py-2 border border-[var(--ph-border)]">
            <option value="dark">Dark</option>
            <option value="light">Light</option>
          </select>
        </div>
      </div>

      <div className="grid grid-cols-3 gap-2">
        {Object.entries(vars).map(([key, val]) => (
          <div key={key} className="flex items-center gap-2">
            <input type="color" value={val.startsWith('#') ? val : '#000000'}
              onChange={e => setVars(v => ({ ...v, [key]: e.target.value }))}
              className="w-6 h-6 rounded cursor-pointer border-0" />
            <span className="text-xs text-[var(--ph-text-muted)] truncate">{key.replace('ph-', '')}</span>
          </div>
        ))}
      </div>

      <div className="flex items-center gap-3">
        <div className="flex gap-1">
          {[vars['ph-bg'], vars['ph-accent'], vars['ph-surface']].map((c, i) => (
            <span key={i} className="w-3 h-3 rounded-full" style={{ backgroundColor: c }} />
          ))}
        </div>
        <span className="text-xs text-[var(--ph-text-faint)]">Preview</span>
      </div>

      <div className="flex gap-2">
        <button onClick={onSave}
          className="text-xs px-3 py-1.5 rounded bg-[var(--ph-accent)] text-[var(--ph-accent-text)] hover:opacity-90">
          {saveLabel}
        </button>
        <button onClick={onCancel}
          className="text-xs px-3 py-1.5 rounded bg-[var(--ph-surface)] text-[var(--ph-text-muted)] hover:bg-[var(--ph-hover)]">
          Cancel
        </button>
      </div>
    </div>
  )
}

// ---- Themes Tab ----
function ThemesTab({ plugins, communityEnabled, onRefresh }: {
  plugins: PluginRecord[]; communityEnabled: boolean; onRefresh: () => void
}) {
  const [creating, setCreating] = useState(false)
  const [editingId, setEditingId] = useState<string | null>(null)
  const [name, setName] = useState('')
  const [kind, setKind] = useState<'dark' | 'light'>('dark')
  const [vars, setVars] = useState<Record<string, string>>({ ...DEFAULT_THEME_VARS })

  const resetForm = () => {
    setName('')
    setKind('dark')
    setVars({ ...DEFAULT_THEME_VARS })
  }

  const startCreate = () => {
    resetForm()
    setEditingId(null)
    setCreating(true)
  }

  const startEdit = (p: PluginRecord) => {
    let cfg: any = {}
    try { cfg = JSON.parse(p.config) } catch {}
    setName(p.name)
    setKind(cfg.kind || 'dark')
    setVars(cfg.vars || { ...DEFAULT_THEME_VARS })
    setEditingId(p.id)
    setCreating(false)
  }

  const cancelForm = () => {
    setCreating(false)
    setEditingId(null)
    resetForm()
  }

  const createTheme = async () => {
    if (!name.trim()) return
    const preview = [vars['ph-bg'], vars['ph-accent'], vars['ph-surface']]
    try {
      await api.plugins.create({
        name, type: 'theme', kind: 'custom',
        config: JSON.stringify({ kind, preview, vars }),
      })
      cancelForm()
      onRefresh()
    } catch (e: any) { alert(e.message) }
  }

  const updateTheme = async () => {
    if (!editingId || !name.trim()) return
    const preview = [vars['ph-bg'], vars['ph-accent'], vars['ph-surface']]
    try {
      await api.plugins.update(editingId, {
        name, enabled: true,
        config: JSON.stringify({ kind, preview, vars }),
      })
      cancelForm()
      onRefresh()
    } catch (e: any) { alert(e.message) }
  }

  const toggleEnabled = async (p: PluginRecord) => {
    try {
      p.enabled ? await api.plugins.disable(p.id) : await api.plugins.enable(p.id)
      onRefresh()
    } catch (e: any) { alert(e.message) }
  }

  const deleteTheme = async (id: string) => {
    if (!confirm('Delete this theme?')) return
    try { await api.plugins.delete(id); onRefresh() }
    catch (e: any) { alert(e.message) }
  }

  return (
    <div className="space-y-4">
      {!communityEnabled && (
        <div className="text-sm text-[var(--ph-text-muted)] bg-[var(--ph-surface)] rounded-lg p-3">
          Community plugins are disabled. Enable them above to use custom themes.
        </div>
      )}

      {!creating && !editingId && (
        <button onClick={startCreate}
          className="text-sm px-3 py-1.5 rounded bg-[var(--ph-accent)] text-[var(--ph-accent-text)] hover:opacity-90">
          + Create Custom Theme
        </button>
      )}

      {creating && (
        <ThemeColorForm
          name={name} setName={setName} kind={kind} setKind={setKind}
          vars={vars} setVars={setVars}
          onSave={createTheme} onCancel={cancelForm} saveLabel="Create Theme"
        />
      )}

      {plugins.map(p => {
        let cfg: any = {}
        try { cfg = JSON.parse(p.config) } catch {}

        if (editingId === p.id) {
          return (
            <ThemeColorForm key={p.id}
              name={name} setName={setName} kind={kind} setKind={setKind}
              vars={vars} setVars={setVars}
              onSave={updateTheme} onCancel={cancelForm} saveLabel="Save Changes"
            />
          )
        }

        return (
          <div key={p.id} className="bg-[var(--ph-card)] border border-[var(--ph-card-border)] rounded-lg p-4 flex items-center justify-between">
            <div className="flex items-center gap-3">
              <div className="flex gap-1">
                {(cfg.preview || []).map((c: string, i: number) => (
                  <span key={i} className="w-3 h-3 rounded-full" style={{ backgroundColor: c }} />
                ))}
              </div>
              <div>
                <span className="text-sm font-medium text-[var(--ph-text)]">{p.name}</span>
                <span className="text-xs text-[var(--ph-text-muted)] ml-2">{cfg.kind || 'custom'}</span>
              </div>
            </div>
            <div className="flex items-center gap-2">
              <Toggle label="" checked={p.enabled} onChange={() => toggleEnabled(p)} />
              <button onClick={() => startEdit(p)}
                className="text-xs px-2 py-1 rounded bg-[var(--ph-surface)] text-[var(--ph-text-muted)] hover:bg-[var(--ph-hover)]">
                Edit
              </button>
              {!p.is_core && (
                <button onClick={() => deleteTheme(p.id)}
                  className="text-xs text-red-400 hover:text-red-300">Delete</button>
              )}
            </div>
          </div>
        )
      })}

      {plugins.length === 0 && !creating && (
        <div className="text-sm text-[var(--ph-text-muted)]">No custom themes yet. Create one above.</div>
      )}
    </div>
  )
}
