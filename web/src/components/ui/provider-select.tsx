/**
 * ProviderSelect — a <select> that annotates each provider option with a
 * health status dot prefix so users can see degraded/unreachable providers
 * at a glance.
 *
 * Uses unicode characters in the option label since <select> options can't
 * contain styled HTML:
 *   🟢 healthy  🟡 degraded  🔴 unreachable  ○ unknown
 */

import type { Provider } from '@/lib/api'

function healthDot(status: Provider['health_status']): string {
  switch (status) {
    case 'ok': return '🟢 '
    case 'error': return '🔴 '
    default: return '○ '
  }
}

interface ProviderSelectProps {
  id?: string
  value: string
  onChange: (value: string) => void
  providers: Provider[]
  placeholder?: string
  className?: string
}

export function ProviderSelect({
  id,
  value,
  onChange,
  providers,
  placeholder,
  className = '',
}: ProviderSelectProps) {
  const base =
    'w-full text-sm bg-slate-800 border border-slate-700 text-slate-300 rounded px-3 py-2 focus:outline-none focus:border-violet-500'

  return (
    <select
      id={id}
      value={value}
      onChange={e => onChange(e.target.value)}
      className={`${base} ${className}`}
    >
      {placeholder && <option value="">{placeholder}</option>}
      {providers.map(p => (
        <option key={p.id} value={p.id}>
          {healthDot(p.health_status)}{p.name}
        </option>
      ))}
    </select>
  )
}
