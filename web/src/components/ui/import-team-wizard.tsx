import { useState, useRef } from 'react'
import { useNavigate } from 'react-router-dom'
import { api } from '@/lib/api'
import { Button } from './button'
import { Input, Label } from './input'
import { Modal } from './modal'

type Step = 'upload' | 'configure' | 'confirm' | 'done'

interface BundleProvider {
  ref: string
  name: string
  type: string
  kind?: string
  config: { base_url?: string; model?: string; api_key: string }
}

interface BundleAgent {
  name: string
  persona: string
  instructions: string
  guardrails: string
  heartbeat_interval?: number
  can_spawn_agents: boolean
  provider_ref: string
}

interface TeamBundle {
  phoenix_bundle_version: string
  exported_at: string
  team: { name: string; description: string }
  agents: BundleAgent[]
  providers: BundleProvider[]
}

export function ImportTeamWizard({ onClose }: { onClose: () => void }) {
  const navigate = useNavigate()
  const fileRef = useRef<HTMLInputElement>(null)
  const [step, setStep] = useState<Step>('upload')
  const [bundle, setBundle] = useState<TeamBundle | null>(null)
  const [parseError, setParseError] = useState('')
  const [apiKeys, setApiKeys] = useState<Record<string, string>>({})
  const [importing, setImporting] = useState(false)
  const [importError, setImportError] = useState('')
  const [result, setResult] = useState<{ team_id: string; team_name: string; skipped: string[] } | null>(null)

  const handleFile = (file: File) => {
    setParseError('')
    const reader = new FileReader()
    reader.onload = (e) => {
      try {
        const data = JSON.parse(e.target?.result as string) as TeamBundle
        if (data.phoenix_bundle_version !== '1') {
          setParseError('Unsupported bundle version. This file may be from a newer version of Phoenix.')
          return
        }
        setBundle(data)
        // Pre-fill empty api_keys for each provider
        const keys: Record<string, string> = {}
        for (const p of data.providers ?? []) keys[p.ref] = ''
        setApiKeys(keys)
        setStep('configure')
      } catch {
        setParseError('Could not parse file. Make sure it\'s a valid Phoenix team bundle (.json).')
      }
    }
    reader.readAsText(file)
  }

  const handleDrop = (e: React.DragEvent) => {
    e.preventDefault()
    const file = e.dataTransfer.files[0]
    if (file) handleFile(file)
  }

  const doImport = async () => {
    if (!bundle) return
    setImporting(true)
    setImportError('')
    try {
      const res = await api.teams.import(bundle, apiKeys)
      setResult(res)
      setStep('done')
    } catch (e: any) {
      setImportError(e.message)
    } finally {
      setImporting(false)
    }
  }

  const title =
    step === 'upload' ? 'Import Team Bundle' :
    step === 'configure' ? `Configure "${bundle?.team.name}"` :
    step === 'confirm' ? 'Review & Import' :
    'Import Complete'

  return (
    <Modal title={title} onClose={onClose} className="max-w-xl">
      {/* Step: upload */}
      {step === 'upload' && (
        <div className="space-y-4">
          <p className="text-sm text-slate-400">
            Import a team bundle file (.json) exported from Phoenix. All agents and their configuration will be created automatically.
          </p>
          <div
            className="border-2 border-dashed border-slate-700 rounded-xl p-10 text-center cursor-pointer hover:border-violet-600 transition-colors"
            onClick={() => fileRef.current?.click()}
            onDrop={handleDrop}
            onDragOver={e => e.preventDefault()}
          >
            <p className="text-3xl mb-3">📦</p>
            <p className="text-slate-300 text-sm font-medium">Drop bundle file here</p>
            <p className="text-slate-600 text-xs mt-1">or click to browse</p>
            <input
              ref={fileRef}
              type="file"
              accept=".json"
              className="hidden"
              onChange={e => { if (e.target.files?.[0]) handleFile(e.target.files[0]) }}
            />
          </div>
          {parseError && <p className="text-sm text-red-400">{parseError}</p>}
          <div className="flex justify-end">
            <Button variant="secondary" onClick={onClose}>Cancel</Button>
          </div>
        </div>
      )}

      {/* Step: configure providers */}
      {step === 'configure' && bundle && (
        <div className="space-y-5">
          {/* Summary */}
          <div className="bg-slate-800/60 border border-slate-700 rounded-lg p-4 space-y-1">
            <p className="text-sm font-medium text-white">{bundle.team.name}</p>
            {bundle.team.description && <p className="text-xs text-slate-400">{bundle.team.description}</p>}
            <div className="flex gap-4 mt-2 text-xs text-slate-500">
              <span>👥 {bundle.agents.length} agent{bundle.agents.length !== 1 ? 's' : ''}</span>
              <span>🔌 {bundle.providers.length} provider{bundle.providers.length !== 1 ? 's' : ''}</span>
            </div>
            <div className="flex flex-wrap gap-1 mt-2">
              {bundle.agents.map(a => (
                <span key={a.name} className="text-xs bg-slate-700 text-slate-300 px-2 py-0.5 rounded-full">{a.name}</span>
              ))}
            </div>
          </div>

          {/* Provider credentials */}
          {bundle.providers.length > 0 && (
            <div>
              <Label>Provider Credentials</Label>
              <p className="text-xs text-slate-500 mb-3">
                API keys are never stored in bundles. Enter them below, or skip and add later in Settings.
              </p>
              <div className="space-y-3">
                {bundle.providers.map(p => (
                  <div key={p.ref} className="bg-slate-800 rounded-lg p-3 space-y-2">
                    <div className="flex items-center justify-between">
                      <p className="text-sm font-medium text-white">{p.name}</p>
                      <span className="text-xs text-slate-500 bg-slate-700 px-2 py-0.5 rounded">{p.type}</span>
                    </div>
                    {p.config.base_url && (
                      <p className="text-xs text-slate-500 font-mono truncate">{p.config.base_url}</p>
                    )}
                    {p.config.model && (
                      <p className="text-xs text-slate-500">Model: {p.config.model}</p>
                    )}
                    <div>
                      <Input
                        type="password"
                        placeholder="API key (optional — skip to add later)"
                        value={apiKeys[p.ref] ?? ''}
                        onChange={e => setApiKeys(prev => ({ ...prev, [p.ref]: e.target.value }))}
                      />
                    </div>
                  </div>
                ))}
              </div>
            </div>
          )}

          <div className="flex gap-3 justify-end pt-1">
            <Button variant="secondary" onClick={() => setStep('upload')}>← Back</Button>
            <Button onClick={() => setStep('confirm')}>Review →</Button>
          </div>
        </div>
      )}

      {/* Step: confirm */}
      {step === 'confirm' && bundle && (
        <div className="space-y-4">
          <div className="space-y-2">
            <p className="text-sm text-slate-400 mb-3">The following will be created:</p>
            <div className="space-y-1.5">
              {bundle.providers.map(p => {
                const hasKey = !!apiKeys[p.ref]
                return (
                  <div key={p.ref} className="flex items-center gap-2 text-sm">
                    <span className="text-emerald-400">✓</span>
                    <span className="text-slate-300">Provider: <span className="text-white">{p.name}</span></span>
                    {!hasKey && <span className="text-xs text-amber-400 ml-auto">⚠ no API key</span>}
                  </div>
                )
              })}
              {bundle.agents.map(a => (
                <div key={a.name} className="flex items-center gap-2 text-sm">
                  <span className="text-emerald-400">✓</span>
                  <span className="text-slate-300">Agent: <span className="text-white">{a.name}</span></span>
                </div>
              ))}
              <div className="flex items-center gap-2 text-sm">
                <span className="text-emerald-400">✓</span>
                <span className="text-slate-300">Team: <span className="text-white">{bundle.team.name}</span></span>
              </div>
            </div>
          </div>
          {bundle.providers.some(p => !apiKeys[p.ref]) && (
            <div className="bg-amber-900/20 border border-amber-800/40 rounded p-3">
              <p className="text-amber-400 text-xs">
                ⚠ Some providers have no API key. Tasks will fail until you add keys in{' '}
                <span className="text-amber-300">Settings → Providers</span>.
              </p>
            </div>
          )}
          {importError && <p className="text-sm text-red-400">{importError}</p>}
          <div className="flex gap-3 justify-end pt-1">
            <Button variant="secondary" onClick={() => setStep('configure')} disabled={importing}>← Back</Button>
            <Button onClick={doImport} disabled={importing}>
              {importing ? 'Importing…' : 'Import Team'}
            </Button>
          </div>
        </div>
      )}

      {/* Step: done */}
      {step === 'done' && result && (
        <div className="space-y-4">
          <div className="text-center py-4">
            <p className="text-4xl mb-3">🎉</p>
            <p className="text-lg font-semibold text-white">Team imported!</p>
            <p className="text-slate-400 text-sm mt-1">
              <span className="text-white font-medium">{result.team_name}</span> is ready to use.
            </p>
          </div>
          {result.skipped.length > 0 && (
            <div className="bg-slate-800 rounded-lg p-3">
              <p className="text-xs text-slate-400 mb-1">Notes:</p>
              {result.skipped.map((s, i) => (
                <p key={i} className="text-xs text-slate-500">• {s}</p>
              ))}
            </div>
          )}
          <p className="text-sm text-slate-400 text-center">
            Create a project to start using this team.
          </p>
          <div className="flex gap-3 justify-center pt-1">
            <Button variant="secondary" onClick={onClose}>Close</Button>
            <Button onClick={() => { onClose(); navigate(`/teams/${result.team_id}`) }}>
              View Team →
            </Button>
          </div>
        </div>
      )}
    </Modal>
  )
}
