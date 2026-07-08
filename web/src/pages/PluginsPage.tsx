import { useEffect, useState } from 'react'
import { api } from '@/lib/api'
import type { PluginRecord, NotificationRule, SystemSettings, Agent, Project, PluginConfigSchema, PluginChat, ObsidianVault, ObsidianDiscoveredVault } from '@/lib/api'
import { injectCommunityThemes, getTheme, setTheme } from '@/lib/theme'
import { getErrorMessage } from '@/lib/errors'

export function PluginsPage() {
  const [tab, setTab] = useState<'notifiers' | 'themes' | 'memory' | 'obsidian'>('notifiers')
  const [plugins, setPlugins] = useState<PluginRecord[]>([])
  const [settings, setSettings] = useState<SystemSettings | null>(null)
  const [loading, setLoading] = useState(true)

  const load = () =>
    Promise.all([api.plugins.list(), api.admin.getSettings()])
      .then(([p, s]) => { setPlugins(p); setSettings(s) })
      .catch((e: unknown) => alert(getErrorMessage(e)))
      .finally(() => setLoading(false))

  useEffect(() => { load() }, [])

  const toggleMaster = async (key: 'core_plugins_enabled' | 'community_plugins_enabled') => {
    if (!settings) return
    const updated = { ...settings, [key]: !settings[key] }
    try {
      await api.admin.saveSettings(updated)
      setSettings(updated)
    } catch (e: unknown) { alert(getErrorMessage(e)) }
  }

  if (loading) return <div className="text-[var(--ph-text-muted)] p-6">Loading…</div>

  const notifiers = plugins.filter(p => p.type === 'notifier')
  const themes = plugins.filter(p => p.type === 'theme')
  const memoryPlugins = plugins.filter(p => p.type === 'memory')

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
        {(['notifiers', 'themes', 'memory', 'obsidian'] as const).map(t => (
          <button key={t} onClick={() => setTab(t)}
            className={`px-4 py-2 text-sm font-medium border-b-2 -mb-px transition-colors ${
              tab === t
                ? 'border-[var(--ph-accent)] text-[var(--ph-accent)]'
                : 'border-transparent text-[var(--ph-text-muted)] hover:text-[var(--ph-text)]'
            }`}>
            {t === 'notifiers' ? 'Notifiers' : t === 'themes' ? 'Themes' : t === 'memory' ? 'Memory' : 'Obsidian'}
          </button>
        ))}
      </div>

      {tab === 'notifiers' && <NotifiersTab plugins={notifiers} coreEnabled={settings?.core_plugins_enabled ?? false} onRefresh={load} />}
      {tab === 'themes' && <ThemesTab plugins={themes} communityEnabled={settings?.community_plugins_enabled ?? false} onRefresh={load} />}
      {tab === 'memory' && <MemoryTab plugins={memoryPlugins} coreEnabled={settings?.core_plugins_enabled ?? false} onRefresh={load} />}
      {tab === 'obsidian' && <ObsidianTab />}
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
        <NotifierCard key={`${p.id}:${p.config}`} plugin={p} dimmed={!coreEnabled} onRefresh={onRefresh} />
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


function NotifierCard({ plugin, dimmed, onRefresh }: {
  plugin: PluginRecord; dimmed: boolean; onRefresh: () => void
}) {
  const [configOpen, setConfigOpen] = useState(false)
  const [configValues, setConfigValues] = useState<Record<string, unknown>>(() => {
    try { return JSON.parse(plugin.config) as Record<string, unknown> } catch { return {} }
  })
  const [schema, setSchema] = useState<PluginConfigSchema | null>(null)
  const [schemaLoading, setSchemaLoading] = useState(false)
  const [testing, setTesting] = useState(false)
  const [testResult, setTestResult] = useState<string | null>(null)
  const [rules, setRules] = useState<NotificationRule[]>([])
  const [rulesLoaded, setRulesLoaded] = useState(false)
  const [saving, setSaving] = useState(false)
  const [detectingChats, setDetectingChats] = useState(false)
  const [discoveredChats, setDiscoveredChats] = useState<PluginChat[] | null>(null)
  const [chatError, setChatError] = useState<string | null>(null)

  // Inbound task creation — projects/agents for the Telegram picker.
  const [inboundProjects, setInboundProjects] = useState<Project[]>([])
  const [inboundAgents, setInboundAgents] = useState<Agent[]>([])
  const [inboundResourcesLoaded, setInboundResourcesLoaded] = useState(false)

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
    } catch (e: unknown) { alert(getErrorMessage(e)) }
  }

  const openConfig = () => {
    if (!configOpen) {
      void loadSchema()
      if (!rulesLoaded) void loadRules()
      if (plugin.kind === 'telegram' && !inboundResourcesLoaded) {
        void Promise.all([api.projects.list('project'), api.agents.list()])
          .then(([projs, agts]) => {
            setInboundProjects(projs)
            setInboundAgents(agts)
            setInboundResourcesLoaded(true)
          })
      }
    }
    setConfigOpen(!configOpen)
  }

  const toggleEnabled = async () => {
    try {
      if (plugin.enabled) {
        await api.plugins.disable(plugin.id)
      } else {
        await api.plugins.enable(plugin.id)
      }
      onRefresh()
    } catch (e: unknown) { alert(getErrorMessage(e)) }
  }

  const saveConfig = async () => {
    setSaving(true)
    try {
      const configStr = JSON.stringify(configValues)
      await api.plugins.update(plugin.id, { ...plugin, config: configStr })
      setConfigOpen(false)
      onRefresh()
    } catch (e: unknown) { alert(getErrorMessage(e)) }
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
    } catch (e: unknown) { setTestResult(`Error: ${getErrorMessage(e)}`) }
    finally { setTesting(false) }
  }

  const addRule = async (eventType: string) => {
    try {
      await api.plugins.rules.create(plugin.id, { event_type: eventType, enabled: true })
      loadRules()
    } catch (e: unknown) { alert(getErrorMessage(e)) }
  }

  const deleteRule = async (ruleId: string) => {
    try {
      await api.plugins.rules.delete(plugin.id, ruleId)
      loadRules()
    } catch (e: unknown) { alert(getErrorMessage(e)) }
  }

  const toggleRule = async (rule: NotificationRule) => {
    try {
      await api.plugins.rules.update(plugin.id, rule.id, { ...rule, enabled: !rule.enabled })
      loadRules()
    } catch (e: unknown) { alert(getErrorMessage(e)) }
  }

  const updateField = (key: string, value: unknown) => {
    setConfigValues(prev => ({ ...prev, [key]: value }))
  }

  const detectChats = async () => {
    setDetectingChats(true)
    setChatError(null)
    setDiscoveredChats(null)
    try {
      // Pass the bot token directly from the form — no save required.
      const chats = await api.plugins.discoverChats(plugin.id, configValues.bot_token as string | undefined)
      setDiscoveredChats(chats)
    } catch (e: unknown) {
      setChatError(getErrorMessage(e))
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
          <button onClick={openConfig}
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
                    <select value={(configValues[key] as string | undefined) ?? (field.default as string | undefined) ?? ''}
                      onChange={e => updateField(key, e.target.value)}
                      className="w-full bg-[var(--ph-input)] text-[var(--ph-text)] text-sm rounded px-3 py-2 border border-[var(--ph-border)]">
                      {field.enum.map(v => <option key={v} value={v}>{v}</option>)}
                    </select>
                  ) : field.type === 'integer' ? (
                    <input type="number" value={(configValues[key] as number | undefined) ?? (field.default as number | undefined) ?? ''}
                      onChange={e => updateField(key, parseInt(e.target.value) || 0)}
                      className="w-full bg-[var(--ph-input)] text-[var(--ph-text)] text-sm rounded px-3 py-2 border border-[var(--ph-border)]" />
                  ) : (
                    <SecretField
                      value={(configValues[key] as string | undefined) ?? ''}
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
                onChange={e => { try { setConfigValues(JSON.parse(e.target.value) as Record<string, unknown>) } catch { /* ignore invalid JSON while typing */ } }}
                rows={4}
                className="w-full bg-[var(--ph-input)] text-[var(--ph-text)] text-sm rounded px-3 py-2 border border-[var(--ph-border)] font-mono" />
            </div>
          )}

          <button onClick={saveConfig} disabled={saving}
            className="text-xs px-3 py-1.5 rounded bg-[var(--ph-accent)] text-[var(--ph-accent-text)] hover:opacity-90 disabled:opacity-50">
            {saving ? 'Saving…' : 'Save Configuration'}
          </button>

          {/* Telegram inbound task creation */}
          {plugin.kind === 'telegram' && (
            <div className="pt-3 border-t border-[var(--ph-border)] space-y-3">
              <div className="flex items-center justify-between">
                <div>
                  <div className="text-xs font-semibold text-[var(--ph-text)]">Inbound Task Creation</div>
                  <div className="text-[11px] text-[var(--ph-text-faint)]">
                    Send /task &lt;description&gt; (or plain text) to your bot to create a Phoenix task.
                  </div>
                </div>
                <Toggle
                  label=""
                  checked={!!configValues.inbound_enabled}
                  onChange={() => updateField('inbound_enabled', !configValues.inbound_enabled)}
                />
              </div>

              {configValues.inbound_enabled && (
                <div className="space-y-2 pl-1">
                  <div>
                    <label className="block text-xs font-medium text-[var(--ph-text)] mb-1">
                      Default Project <span className="text-red-400">*</span>
                    </label>
                    <select
                      value={(configValues.default_project_id as string | undefined) ?? ''}
                      onChange={e => updateField('default_project_id', e.target.value)}
                      className="w-full bg-[var(--ph-input)] text-[var(--ph-text)] text-sm rounded px-3 py-2 border border-[var(--ph-border)]"
                    >
                      <option value="">— select project —</option>
                      {inboundProjects.map(p => (
                        <option key={p.id} value={p.id}>{p.name}</option>
                      ))}
                    </select>
                  </div>

                  <div>
                    <label className="block text-xs font-medium text-[var(--ph-text)] mb-1">
                      Default Agent <span className="text-red-400">*</span>
                    </label>
                    <select
                      value={(configValues.default_agent_id as string | undefined) ?? ''}
                      onChange={e => updateField('default_agent_id', e.target.value)}
                      className="w-full bg-[var(--ph-input)] text-[var(--ph-text)] text-sm rounded px-3 py-2 border border-[var(--ph-border)]"
                    >
                      <option value="">— select agent —</option>
                      {inboundAgents.map(a => (
                        <option key={a.id} value={a.id}>{a.name}</option>
                      ))}
                    </select>
                  </div>

                  <p className="text-[11px] text-[var(--ph-text-faint)]">
                    The selected agent must be assigned to the selected project.
                    Messages from any other chat ID are silently ignored.
                  </p>
                </div>
              )}
            </div>
          )}

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

// ---- Theme plugin config shape ----
interface ThemeConfig {
  kind?: string
  vars?: Record<string, string>
  preview?: string[]
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

// Human-readable labels and grouping for CSS variables.
const COLOR_GROUPS: { label: string; fields: { key: string; label: string }[] }[] = [
  {
    label: 'Backgrounds',
    fields: [
      { key: 'ph-bg', label: 'Page Background' },
      { key: 'ph-surface', label: 'Surface (sidebar, panels)' },
      { key: 'ph-card', label: 'Card Background' },
      { key: 'ph-input', label: 'Input Fields' },
      { key: 'ph-hover', label: 'Hover State' },
    ],
  },
  {
    label: 'Text',
    fields: [
      { key: 'ph-text', label: 'Primary Text' },
      { key: 'ph-text-muted', label: 'Secondary Text' },
      { key: 'ph-text-faint', label: 'Disabled / Hint Text' },
    ],
  },
  {
    label: 'Accent',
    fields: [
      { key: 'ph-accent', label: 'Accent (buttons, links)' },
      { key: 'ph-accent-light', label: 'Accent Light' },
      { key: 'ph-accent-bg', label: 'Accent Tint (badges)' },
      { key: 'ph-accent-text', label: 'Text on Accent' },
    ],
  },
  {
    label: 'Borders',
    fields: [
      { key: 'ph-card-border', label: 'Card Border' },
      { key: 'ph-border', label: 'Default Border' },
      { key: 'ph-border-mid', label: 'Strong Border (dividers)' },
    ],
  },
]

// ---- Live preview component ----
function ThemePreview({ vars }: { vars: Record<string, string> }) {
  const v = (key: string) => vars[key] || '#000'
  return (
    <div className="rounded-lg overflow-hidden border" style={{ borderColor: v('ph-border'), background: v('ph-bg') }}>
      {/* Header bar */}
      <div className="px-3 py-2 flex items-center justify-between" style={{ background: v('ph-surface'), borderBottom: `1px solid ${v('ph-border')}` }}>
        <span className="text-xs font-semibold" style={{ color: v('ph-text') }}>Phoenix Preview</span>
        <span className="text-[10px] px-1.5 py-0.5 rounded" style={{ background: v('ph-accent-bg'), color: v('ph-accent') }}>Badge</span>
      </div>
      {/* Content */}
      <div className="p-3 space-y-2">
        {/* Card */}
        <div className="rounded-md p-2.5" style={{ background: v('ph-card'), border: `1px solid ${v('ph-card-border')}` }}>
          <div className="text-xs font-medium mb-1" style={{ color: v('ph-text') }}>Task: Generate Report</div>
          <div className="text-[11px] mb-2" style={{ color: v('ph-text-muted') }}>Agent is processing the monthly data analysis...</div>
          <div className="flex gap-1.5">
            <span className="text-[10px] px-2 py-0.5 rounded" style={{ background: v('ph-accent'), color: v('ph-accent-text') }}>Running</span>
            <span className="text-[10px] px-2 py-0.5 rounded" style={{ background: v('ph-hover'), color: v('ph-text-muted') }}>Details</span>
          </div>
        </div>
        {/* Input */}
        <div className="rounded-md px-2.5 py-1.5 text-[11px]" style={{ background: v('ph-input'), border: `1px solid ${v('ph-border')}`, color: v('ph-text-faint') }}>
          Type a message...
        </div>
        {/* Text samples */}
        <div className="flex items-center justify-between px-1">
          <span className="text-[11px]" style={{ color: v('ph-text') }}>Primary text</span>
          <span className="text-[11px]" style={{ color: v('ph-text-muted') }}>Muted</span>
          <span className="text-[11px]" style={{ color: v('ph-text-faint') }}>Faint</span>
          <span className="text-[11px] underline cursor-pointer" style={{ color: v('ph-accent') }}>Link</span>
        </div>
        {/* Divider */}
        <div style={{ borderTop: `1px solid ${v('ph-border-mid')}` }} />
        <div className="text-[10px] text-center" style={{ color: v('ph-text-faint') }}>Divider above uses strong border</div>
      </div>
    </div>
  )
}

// ---- Theme color form (shared between create and edit) ----
function ThemeColorForm({ name, setName, kind, setKind, vars, setVars, onSave, onCancel, saveLabel }: {
  name: string; setName: (v: string) => void
  kind: 'dark' | 'light'; setKind: (v: 'dark' | 'light') => void
  vars: Record<string, string>; setVars: (fn: (v: Record<string, string>) => Record<string, string>) => void
  onSave: () => void; onCancel: () => void; saveLabel: string
}) {
  return (
    <div className="bg-[var(--ph-card)] border border-[var(--ph-card-border)] rounded-lg p-4 space-y-4">
      {/* Name + Kind */}
      <div className="grid grid-cols-2 gap-3">
        <div>
          <label className="block text-xs font-medium text-[var(--ph-text)] mb-1">Theme Name</label>
          <input value={name} onChange={e => setName(e.target.value)}
            className="w-full bg-[var(--ph-input)] text-[var(--ph-text)] text-sm rounded px-3 py-2 border border-[var(--ph-border)]"
            placeholder="My Custom Theme" />
        </div>
        <div>
          <label className="block text-xs font-medium text-[var(--ph-text)] mb-1">Mode</label>
          <select value={kind} onChange={e => setKind(e.target.value as 'dark' | 'light')}
            className="w-full bg-[var(--ph-input)] text-[var(--ph-text)] text-sm rounded px-3 py-2 border border-[var(--ph-border)]">
            <option value="dark">Dark</option>
            <option value="light">Light</option>
          </select>
        </div>
      </div>

      {/* Color pickers + live preview side by side */}
      <div className="grid grid-cols-2 gap-4">
        {/* Left: grouped color pickers */}
        <div className="space-y-3">
          {COLOR_GROUPS.map(group => (
            <div key={group.label}>
              <h3 className="text-[10px] font-semibold text-[var(--ph-text-muted)] uppercase tracking-wide mb-1.5">{group.label}</h3>
              <div className="space-y-1">
                {group.fields.map(f => (
                  <div key={f.key} className="flex items-center gap-2">
                    <input type="color"
                      value={(vars[f.key] || '#000000').startsWith('#') ? vars[f.key] : '#000000'}
                      onChange={e => setVars(v => ({ ...v, [f.key]: e.target.value }))}
                      className="w-7 h-7 rounded cursor-pointer border border-[var(--ph-border)] p-0" />
                    <div className="flex-1 min-w-0">
                      <span className="text-xs text-[var(--ph-text)]">{f.label}</span>
                    </div>
                    <input type="text" value={vars[f.key] || ''}
                      onChange={e => setVars(v => ({ ...v, [f.key]: e.target.value }))}
                      className="w-20 bg-[var(--ph-input)] text-[var(--ph-text-muted)] text-[10px] font-mono rounded px-1.5 py-0.5 border border-[var(--ph-border)]" />
                  </div>
                ))}
              </div>
            </div>
          ))}
        </div>

        {/* Right: live preview */}
        <div>
          <h3 className="text-[10px] font-semibold text-[var(--ph-text-muted)] uppercase tracking-wide mb-1.5">Live Preview</h3>
          <ThemePreview vars={vars} />
        </div>
      </div>

      {/* Actions */}
      <div className="flex gap-2 pt-1 border-t border-[var(--ph-border)]">
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
// ---- Memory Tab ----
function MemoryTab({ plugins, coreEnabled, onRefresh }: {
  plugins: PluginRecord[]; coreEnabled: boolean; onRefresh: () => void
}) {
  return (
    <div className="space-y-4">
      {!coreEnabled && (
        <div className="text-sm text-[var(--ph-text-muted)] bg-[var(--ph-surface)] rounded-lg p-3">
          Core plugins are disabled. Enable them above to configure memory backends.
        </div>
      )}
      {plugins.map(p => (
        <MemoryCard key={`${p.id}:${p.config}`} plugin={p} dimmed={!coreEnabled} onRefresh={onRefresh} />
      ))}
      {plugins.length === 0 && (
        <div className="text-sm text-[var(--ph-text-muted)]">No memory plugins found.</div>
      )}
    </div>
  )
}

function MemoryCard({ plugin, dimmed, onRefresh }: {
  plugin: PluginRecord; dimmed: boolean; onRefresh: () => void
}) {
  const [configOpen, setConfigOpen] = useState(false)
  const [configValues, setConfigValues] = useState<Record<string, unknown>>(() => {
    try { return JSON.parse(plugin.config) as Record<string, unknown> } catch { return {} }
  })
  const [schema, setSchema] = useState<PluginConfigSchema | null>(null)
  const [schemaLoading, setSchemaLoading] = useState(false)
  const [testing, setTesting] = useState(false)
  const [testResult, setTestResult] = useState<string | null>(null)
  const [saving, setSaving] = useState(false)

  const loadSchema = async () => {
    if (schema) return
    setSchemaLoading(true)
    try {
      const s = await api.plugins.schema(plugin.id)
      setSchema(s)
    } catch { /* no schema — fall back to raw JSON */ }
    finally { setSchemaLoading(false) }
  }

  const openConfig = () => {
    if (!configOpen) void loadSchema()
    setConfigOpen(!configOpen)
  }

  const toggleEnabled = async () => {
    try {
      if (plugin.enabled) {
        await api.plugins.disable(plugin.id)
      } else {
        await api.plugins.enable(plugin.id)
      }
      onRefresh()
    } catch (e: unknown) { alert(getErrorMessage(e)) }
  }

  const saveConfig = async () => {
    setSaving(true)
    try {
      await api.plugins.update(plugin.id, { ...plugin, config: JSON.stringify(configValues) })
      setConfigOpen(false)
      onRefresh()
    } catch (e: unknown) { alert(getErrorMessage(e)) }
    finally { setSaving(false) }
  }

  const testPlugin = async () => {
    setTesting(true)
    setTestResult(null)
    try {
      const r = await api.plugins.test(plugin.id)
      setTestResult(r.status === 'ok' ? 'Connected successfully' : (r.message ?? 'Test failed'))
    } catch (e: unknown) { setTestResult(getErrorMessage(e)) }
    finally { setTesting(false) }
  }

  return (
    <div className={`bg-[var(--ph-card)] border border-[var(--ph-card-border)] rounded-lg p-4 space-y-3 ${dimmed ? 'opacity-50 pointer-events-none' : ''}`}>
      <div className="flex items-center justify-between">
        <div>
          <div className="text-sm font-medium text-[var(--ph-text)]">{plugin.name}</div>
          <div className="text-xs text-[var(--ph-text-muted)]">{plugin.kind}</div>
        </div>
        <div className="flex items-center gap-2">
          <button onClick={toggleEnabled}
            className={`text-xs px-3 py-1 rounded font-medium transition-colors ${
              plugin.enabled
                ? 'bg-[var(--ph-accent)] text-white'
                : 'bg-[var(--ph-surface)] text-[var(--ph-text-muted)] hover:bg-[var(--ph-hover)]'
            }`}>
            {plugin.enabled ? 'Enabled' : 'Disabled'}
          </button>
          <button onClick={openConfig}
            className="text-xs px-3 py-1 rounded bg-[var(--ph-surface)] text-[var(--ph-text-muted)] hover:bg-[var(--ph-hover)] transition-colors">
            Configure
          </button>
        </div>
      </div>

      {configOpen && (
        <div className="border-t border-[var(--ph-border)] pt-3 space-y-3">
          {schemaLoading && <div className="text-xs text-[var(--ph-text-muted)]">Loading schema…</div>}
          {schema ? (
            Object.entries(schema.properties ?? {}).map(([key, f]) => (
              <div key={key} className="space-y-1">
                <label className="text-xs font-medium text-[var(--ph-text)]">
                  {f.title ?? key}{(schema.required ?? []).includes(key) && <span className="text-red-400 ml-1">*</span>}
                </label>
                {f.description && <div className="text-xs text-[var(--ph-text-muted)]">{f.description}</div>}
                <SecretField
                  value={String(configValues[key] ?? '')}
                  onChange={v => setConfigValues(prev => ({ ...prev, [key]: v }))}
                  isSecret={f.secret ?? false}
                />
              </div>
            ))
          ) : (
            !schemaLoading && (
              <textarea
                className="w-full bg-[var(--ph-input)] text-[var(--ph-text)] text-xs rounded px-3 py-2 border border-[var(--ph-border)] font-mono h-24"
                value={JSON.stringify(configValues, null, 2)}
                onChange={e => {
                  try { setConfigValues(JSON.parse(e.target.value) as Record<string, unknown>) } catch { /* ignore parse errors */ }
                }}
              />
            )
          )}
          <div className="flex items-center gap-2">
            <button onClick={saveConfig} disabled={saving}
              className="text-xs px-3 py-1 rounded bg-[var(--ph-accent)] text-white hover:opacity-90 disabled:opacity-50">
              {saving ? 'Saving…' : 'Save'}
            </button>
            <button onClick={testPlugin} disabled={testing}
              className="text-xs px-3 py-1 rounded bg-[var(--ph-surface)] text-[var(--ph-text-muted)] hover:bg-[var(--ph-hover)] disabled:opacity-50">
              {testing ? 'Testing…' : 'Test connection'}
            </button>
            {testResult && (
              <span className={`text-xs ${testResult.includes('success') || testResult.includes('Connected') ? 'text-green-400' : 'text-red-400'}`}>
                {testResult}
              </span>
            )}
          </div>
        </div>
      )}
    </div>
  )
}

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
    let cfg: ThemeConfig = {}
    try { cfg = JSON.parse(p.config) as ThemeConfig } catch { /* use defaults if config is not valid JSON */ }
    setName(p.name)
    setKind(cfg.kind === 'light' ? 'light' : 'dark')
    setVars(cfg.vars || { ...DEFAULT_THEME_VARS })
    setEditingId(p.id)
    setCreating(false)
  }

  const cancelForm = () => {
    setCreating(false)
    setEditingId(null)
    resetForm()
  }

  // After any theme save, re-inject CSS and re-apply current theme so changes are visible immediately.
  const refreshThemeCSS = async () => {
    try {
      const community = await api.themes.list()
      const toInject = community
        .filter(t => t.vars && Object.keys(t.vars).length > 0)
        .map(t => ({ id: t.id, vars: t.vars! }))
      injectCommunityThemes(toInject)
      // Re-apply the current theme to pick up updated CSS variables.
      setTheme(getTheme())
    } catch { /* ignore */ }
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
      await refreshThemeCSS()
    } catch (e: unknown) { alert(getErrorMessage(e)) }
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
      await refreshThemeCSS()
    } catch (e: unknown) { alert(getErrorMessage(e)) }
  }

  const toggleEnabled = async (p: PluginRecord) => {
    try {
      if (p.enabled) {
        await api.plugins.disable(p.id)
      } else {
        await api.plugins.enable(p.id)
      }
      onRefresh()
    } catch (e: unknown) { alert(getErrorMessage(e)) }
  }

  const deleteTheme = async (id: string) => {
    if (!confirm('Delete this theme?')) return
    try { await api.plugins.delete(id); onRefresh() }
    catch (e: unknown) { alert(getErrorMessage(e)) }
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
        let cfg: ThemeConfig = {}
        try { cfg = JSON.parse(p.config) as ThemeConfig } catch { /* use defaults if config is not valid JSON */ }

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

// ---- Obsidian Tab ----

function ObsidianTab() {
  const [settings, setSettings] = useState<SystemSettings>({
    global_guardrails_enabled: false, global_guardrails: '',
    core_plugins_enabled: false, community_plugins_enabled: false,
    obsidian_enabled: false, obsidian_root: '', obsidian_auto_write: false, theme: '',
    dynamic_orchestration_enabled: false, orchestrator_agent_id: '',
    max_subtask_depth: 2, max_subtasks_per_level: 5, orchestrator_confidence_threshold: 0.75,
  })
  const [vaults, setVaults] = useState<ObsidianVault[]>([])
  const [discovered, setDiscovered] = useState<ObsidianDiscoveredVault[]>([])
  const [loading, setLoading] = useState(true)
  const [discovering, setDiscovering] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const [generatingFor, setGeneratingFor] = useState<string | null>(null)
  const [addingVaultId, setAddingVaultId] = useState<string | null>(null)

  useEffect(() => {
    Promise.all([api.admin.getSettings(), api.obsidian.listVaults().catch(() => [])])
      .then(([s, v]) => { setSettings(s); setVaults(v) })
      .catch(e => setError(getErrorMessage(e)))
      .finally(() => setLoading(false))
  }, [])

  const saveSettings = async (patch: Partial<SystemSettings>) => {
    const updated = { ...settings, ...patch }
    setSettings(updated)
    try {
      await api.admin.saveSettings(updated)
    } catch (e: unknown) {
      setError(getErrorMessage(e))
    }
  }

  const discover = async () => {
    setDiscovering(true)
    setError(null)
    try {
      const result = await api.obsidian.discover(settings.obsidian_root || undefined)
      setDiscovered(result)
    } catch (e: unknown) {
      setError(getErrorMessage(e))
    } finally {
      setDiscovering(false)
    }
  }

  const addVault = async (d: ObsidianDiscoveredVault) => {
    setAddingVaultId(d.path)
    try {
      const v = await api.obsidian.createVault({ name: d.name, path: d.path, context: '', enabled: true })
      setVaults(prev => [...prev, v])
      setDiscovered(prev => prev.map(x => x.path === d.path ? { ...x, configured: true } : x))
    } catch (e: unknown) {
      setError(getErrorMessage(e))
    } finally {
      setAddingVaultId(null)
    }
  }

  const updateVaultContext = async (vault: ObsidianVault, context: string) => {
    try {
      const updated = await api.obsidian.updateVault(vault.id, { ...vault, context })
      setVaults(prev => prev.map(v => v.id === vault.id ? updated : v))
    } catch (e: unknown) {
      setError(getErrorMessage(e))
    }
  }

  const toggleVault = async (vault: ObsidianVault) => {
    try {
      const updated = await api.obsidian.updateVault(vault.id, { ...vault, enabled: !vault.enabled })
      setVaults(prev => prev.map(v => v.id === vault.id ? updated : v))
    } catch (e: unknown) {
      setError(getErrorMessage(e))
    }
  }

  const deleteVault = async (vault: ObsidianVault) => {
    if (!confirm(`Remove vault "${vault.name}" from Phoenix? (No files will be deleted.)`)) return
    try {
      await api.obsidian.deleteVault(vault.id)
      setVaults(prev => prev.filter(v => v.id !== vault.id))
    } catch (e: unknown) {
      setError(getErrorMessage(e))
    }
  }

  const generateContext = async (vault: ObsidianVault) => {
    setGeneratingFor(vault.id)
    try {
      const { context } = await api.obsidian.generateContext(vault.name)
      const updated = await api.obsidian.updateVault(vault.id, { ...vault, context })
      setVaults(prev => prev.map(v => v.id === vault.id ? updated : v))
    } catch (e: unknown) {
      setError(getErrorMessage(e))
    } finally {
      setGeneratingFor(null)
    }
  }

  if (loading) return <div className="text-[var(--ph-text-muted)] text-sm">Loading…</div>

  return (
    <div className="space-y-6">
      {/* Master enable toggle */}
      <div className="bg-[var(--ph-card)] border border-[var(--ph-card-border)] rounded-lg p-4">
        <Toggle
          label="Enable Obsidian Integration"
          description="When enabled, agents can route output into your Obsidian vaults and briefings are written automatically after task completion."
          checked={settings.obsidian_enabled}
          onChange={() => saveSettings({ obsidian_enabled: !settings.obsidian_enabled })}
        />
      </div>

      {error && (
        <div className="bg-red-900/20 border border-red-700/50 rounded-lg px-4 py-3 text-sm text-red-400">
          {error}
        </div>
      )}

      {settings.obsidian_enabled && (
        <>
          {/* Root directory */}
          <div className="bg-[var(--ph-card)] border border-[var(--ph-card-border)] rounded-lg p-4 space-y-3">
            <div>
              <h3 className="text-sm font-semibold text-[var(--ph-text)]">Vault Root Directory</h3>
              <p className="text-xs text-[var(--ph-text-muted)] mt-0.5">The folder that contains all your Obsidian vaults, e.g. /Users/jon/vaults</p>
            </div>
            <div className="flex gap-2">
              <input
                type="text"
                value={settings.obsidian_root}
                onChange={e => setSettings(s => ({ ...s, obsidian_root: e.target.value }))}
                onBlur={() => saveSettings({ obsidian_root: settings.obsidian_root })}
                placeholder="/Users/you/vaults"
                className="flex-1 bg-[var(--ph-input)] border border-[var(--ph-border)] rounded-lg px-3 py-2 text-sm text-[var(--ph-text)] placeholder-[var(--ph-text-faint)] focus:outline-none focus:ring-2 focus:ring-[var(--ph-accent)]"
              />
              <button
                onClick={discover}
                disabled={discovering || !settings.obsidian_root}
                className="px-4 py-2 text-sm font-medium rounded-lg bg-[var(--ph-accent)] hover:opacity-90 disabled:opacity-50 text-[var(--ph-accent-text)] transition-colors"
              >
                {discovering ? 'Scanning…' : 'Discover Vaults'}
              </button>
            </div>
          </div>

          {/* Auto-write toggle */}
          <div className="bg-[var(--ph-card)] border border-[var(--ph-card-border)] rounded-lg p-4">
            <Toggle
              label="Auto-write after task completion"
              description="Phoenix generates and saves an Obsidian note automatically after every completed task."
              checked={settings.obsidian_auto_write}
              onChange={() => saveSettings({ obsidian_auto_write: !settings.obsidian_auto_write })}
            />
          </div>

          {/* Discovered vaults not yet configured */}
          {discovered.filter(d => !d.configured).length > 0 && (
            <div className="bg-[var(--ph-card)] border border-[var(--ph-card-border)] rounded-lg p-4 space-y-3">
              <h3 className="text-sm font-semibold text-[var(--ph-text)]">Discovered Vaults</h3>
              <div className="space-y-2">
                {discovered.filter(d => !d.configured).map(d => (
                  <div key={d.path} className="flex items-center justify-between py-2 px-3 bg-[var(--ph-surface)] rounded-lg">
                    <div>
                      <p className="text-sm text-[var(--ph-text)] font-medium">{d.name}</p>
                      <p className="text-xs text-[var(--ph-text-muted)] font-mono">{d.path}</p>
                    </div>
                    <button
                      onClick={() => addVault(d)}
                      disabled={addingVaultId === d.path}
                      className="px-3 py-1.5 text-xs font-medium rounded-lg bg-[var(--ph-accent-bg)] text-[var(--ph-accent)] border border-[var(--ph-accent-bg)] hover:opacity-80 disabled:opacity-50 transition-colors"
                    >
                      {addingVaultId === d.path ? 'Adding…' : '+ Add'}
                    </button>
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Configured vaults */}
          {vaults.length > 0 && (
            <div className="space-y-3">
              <h3 className="text-sm font-semibold text-[var(--ph-text)]">Configured Vaults</h3>
              {vaults.map(vault => (
                <VaultCard
                  key={vault.id}
                  vault={vault}
                  generatingContext={generatingFor === vault.id}
                  onContextChange={ctx => updateVaultContext(vault, ctx)}
                  onGenerateContext={() => generateContext(vault)}
                  onToggle={() => toggleVault(vault)}
                  onDelete={() => deleteVault(vault)}
                />
              ))}
            </div>
          )}

          {vaults.length === 0 && discovered.length === 0 && (
            <div className="bg-[var(--ph-card)] border border-[var(--ph-card-border)] rounded-lg p-8 text-center">
              <p className="text-[var(--ph-text-muted)] text-sm">No vaults configured yet.</p>
              <p className="text-[var(--ph-text-faint)] text-xs mt-1">Set a root directory above and click Discover Vaults to get started.</p>
            </div>
          )}
        </>
      )}
    </div>
  )
}

interface VaultCardProps {
  vault: ObsidianVault
  generatingContext: boolean
  onContextChange: (ctx: string) => void
  onGenerateContext: () => void
  onToggle: () => void
  onDelete: () => void
}

function VaultCard({ vault, generatingContext, onContextChange, onGenerateContext, onToggle, onDelete }: VaultCardProps) {
  const [localContext, setLocalContext] = useState(vault.context)
  const [prevVaultContext, setPrevVaultContext] = useState(vault.context)

  if (vault.context !== prevVaultContext) {
    setPrevVaultContext(vault.context)
    setLocalContext(vault.context)
  }

  return (
    <div className={`bg-[var(--ph-card)] border border-[var(--ph-card-border)] rounded-lg p-4 space-y-3 transition-opacity ${vault.enabled ? '' : 'opacity-60'}`}>
      <div className="flex items-start justify-between gap-3">
        <div className="flex-1 min-w-0">
          <p className="text-sm font-semibold text-[var(--ph-text)]">{vault.name}</p>
          <p className="text-xs text-[var(--ph-text-muted)] font-mono truncate">{vault.path}</p>
        </div>
        <div className="flex items-center gap-2 shrink-0">
          <button
            onClick={onToggle}
            className={`text-xs px-2 py-1 rounded border transition-colors ${
              vault.enabled
                ? 'border-emerald-600/40 text-emerald-400 hover:bg-emerald-600/10'
                : 'border-[var(--ph-border)] text-[var(--ph-text-muted)] hover:bg-[var(--ph-hover)]'
            }`}
          >
            {vault.enabled ? 'Enabled' : 'Disabled'}
          </button>
          <button onClick={onDelete} className="text-xs text-[var(--ph-text-faint)] hover:text-red-400 transition-colors px-1">✕</button>
        </div>
      </div>
      <div className="space-y-1.5">
        <div className="flex items-center justify-between">
          <label className="text-xs font-medium text-[var(--ph-text-muted)]">Context description</label>
          <button
            onClick={onGenerateContext}
            disabled={generatingContext}
            className="text-xs text-[var(--ph-accent)] hover:opacity-80 disabled:opacity-50 transition-colors"
          >
            {generatingContext ? 'Generating…' : '✦ Generate with AI'}
          </button>
        </div>
        <textarea
          rows={2}
          value={localContext}
          onChange={e => setLocalContext(e.target.value)}
          onBlur={() => onContextChange(localContext)}
          placeholder="Describe what this vault is for, e.g. On-call incidents, customer escalations, SEV tickets…"
          className="w-full bg-[var(--ph-input)] border border-[var(--ph-border)] rounded-lg px-3 py-2 text-sm text-[var(--ph-text)] placeholder-[var(--ph-text-faint)] resize-none focus:outline-none focus:ring-2 focus:ring-[var(--ph-accent)]"
        />
      </div>
    </div>
  )
}
