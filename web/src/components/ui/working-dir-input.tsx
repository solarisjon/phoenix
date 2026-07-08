/**
 * WorkingDirInput — real-time path validation with create-dir shortcut.
 *
 * Debounces GET /api/fs/stat on change; shows status indicator and a
 * "Create" button when the directory doesn't exist yet.
 */

import { useState, useRef, useCallback } from 'react'
import { api } from '@/lib/api'

type DirStatus = 'unknown' | 'exists' | 'missing' | 'not_dir' | 'creating' | 'error'

interface WorkingDirInputProps {
  value: string
  onChange: (val: string) => void
  id?: string
  placeholder?: string
  className?: string
}

export function WorkingDirInput({
  value,
  onChange,
  id = 'working-dir',
  placeholder = '/path/to/project',
  className = '',
}: WorkingDirInputProps) {
  const [dirStatus, setDirStatus] = useState<DirStatus>('unknown')
  const [dirStatusMsg, setDirStatusMsg] = useState('')
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)

  const checkDir = useCallback(async (path: string) => {
    const trimmed = path.trim()
    if (!trimmed) { setDirStatus('unknown'); return }
    try {
      const res = await api.fs.stat(trimmed)
      if (res.exists) {
        setDirStatus(res.is_dir ? 'exists' : 'not_dir')
      } else {
        setDirStatus('missing')
      }
      setDirStatusMsg('')
    } catch (e: unknown) {
      setDirStatus('error')
      setDirStatusMsg(e instanceof Error ? e.message : 'Could not check path')
    }
  }, [])

  const handleChange = (val: string) => {
    onChange(val)
    setDirStatus('unknown')
    if (debounceRef.current) clearTimeout(debounceRef.current)
    debounceRef.current = setTimeout(() => checkDir(val), 600)
  }

  const handleBlur = () => {
    if (debounceRef.current) clearTimeout(debounceRef.current)
    checkDir(value)
  }

  const handleCreateDir = async () => {
    setDirStatus('creating')
    try {
      await api.fs.mkdir(value.trim())
      setDirStatus('exists')
      setDirStatusMsg('Created')
    } catch (e: unknown) {
      setDirStatus('error')
      setDirStatusMsg(e instanceof Error ? e.message : 'Failed to create directory')
    }
  }

  return (
    <div>
      <input
        id={id}
        type="text"
        value={value}
        onChange={e => handleChange(e.target.value)}
        onBlur={handleBlur}
        placeholder={placeholder}
        className={`w-full text-sm bg-slate-800 border border-slate-700 text-slate-300 rounded px-3 py-2 focus:outline-none focus:border-violet-500 font-mono ${className}`}
      />
      {dirStatus === 'exists' && (
        <p className="mt-1 text-xs text-emerald-400 flex items-center gap-1">
          <span>✓</span>
          <span>{dirStatusMsg || 'Directory exists'}</span>
        </p>
      )}
      {dirStatus === 'missing' && (
        <p className="mt-1 text-xs text-amber-400 flex items-center gap-1.5">
          <span>●</span>
          <span>Does not exist</span>
          <button
            type="button"
            onClick={handleCreateDir}
            className="ml-1 px-1.5 py-0.5 text-xs bg-amber-500/20 hover:bg-amber-500/30 text-amber-300 rounded border border-amber-500/30"
          >
            Create
          </button>
        </p>
      )}
      {dirStatus === 'not_dir' && (
        <p className="mt-1 text-xs text-red-400 flex items-center gap-1">
          <span>●</span>
          <span>Path exists but is not a directory</span>
        </p>
      )}
      {dirStatus === 'creating' && (
        <p className="mt-1 text-xs text-slate-400 flex items-center gap-1">
          <span className="animate-spin inline-block">⟳</span>
          <span>Creating…</span>
        </p>
      )}
      {dirStatus === 'error' && (
        <p className="mt-1 text-xs text-red-400 flex items-center gap-1">
          <span>●</span>
          <span>{dirStatusMsg || 'Error checking path'}</span>
        </p>
      )}
    </div>
  )
}
