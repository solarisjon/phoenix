import { cn } from '@/lib/utils'

export function Input({ className, ...props }: React.InputHTMLAttributes<HTMLInputElement>) {
  return (
    <input
      className={cn(
        'w-full bg-slate-800 border border-slate-700 rounded-lg px-3 py-2 text-sm text-white placeholder-slate-500',
        'focus:outline-none focus:ring-2 focus:ring-violet-600 focus:border-transparent',
        'disabled:opacity-50',
        className
      )}
      {...props}
    />
  )
}

export function Textarea({ className, ...props }: React.TextareaHTMLAttributes<HTMLTextAreaElement>) {
  return (
    <textarea
      className={cn(
        'w-full bg-slate-800 border border-slate-700 rounded-lg px-3 py-2 text-sm text-white placeholder-slate-500',
        'focus:outline-none focus:ring-2 focus:ring-violet-600 focus:border-transparent',
        'disabled:opacity-50 resize-none',
        className
      )}
      {...props}
    />
  )
}

export function Select({ className, children, ...props }: React.SelectHTMLAttributes<HTMLSelectElement>) {
  return (
    <select
      className={cn(
        'w-full bg-slate-800 border border-slate-700 rounded-lg px-3 py-2 text-sm text-white',
        'focus:outline-none focus:ring-2 focus:ring-violet-600 focus:border-transparent',
        'disabled:opacity-50',
        className
      )}
      {...props}
    >
      {children}
    </select>
  )
}

export function Label({ children, className, htmlFor }: { children: React.ReactNode; className?: string; htmlFor?: string }) {
  return (
    <label htmlFor={htmlFor} className={cn('block text-sm font-medium text-slate-300 mb-1.5', className)}>
      {children}
    </label>
  )
}
