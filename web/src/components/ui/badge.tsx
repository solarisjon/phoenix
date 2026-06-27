import { cn } from '@/lib/utils'

type Variant = 'default' | 'success' | 'warning' | 'danger' | 'info' | 'queued' | 'muted'

const variants: Record<Variant, string> = {
  default: 'ph-badge-default',
  success: 'ph-badge-success',
  warning: 'ph-badge-warning',
  danger:  'ph-badge-danger',
  info:    'ph-badge-info',
  queued:  'ph-badge-queued',
  muted:   'ph-badge-muted',
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
