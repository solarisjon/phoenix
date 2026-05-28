import { cn } from '@/lib/utils'

type Variant = 'default' | 'success' | 'warning' | 'danger' | 'info' | 'muted'

const variants: Record<Variant, string> = {
  default: 'bg-slate-700 text-slate-200',
  success: 'bg-emerald-900/60 text-emerald-400 border border-emerald-800',
  warning: 'bg-amber-900/60 text-amber-400 border border-amber-800',
  danger: 'bg-red-900/60 text-red-400 border border-red-800',
  info: 'bg-violet-900/60 text-violet-400 border border-violet-800',
  muted: 'bg-slate-800 text-slate-500',
}

export function Badge({ children, variant = 'default', className }: {
  children: React.ReactNode
  variant?: Variant
  className?: string
}) {
  return (
    <span className={cn('inline-flex items-center px-2 py-0.5 rounded-md text-xs font-medium', variants[variant], className)}>
      {children}
    </span>
  )
}
