export const COLORS = {
  gray: {
    50: '#f9fafb', 100: '#f3f4f6', 200: '#e5e7eb', 300: '#d1d5db',
    400: '#9ca3af', 500: '#6b7280', 600: '#4b5563', 700: '#374151',
    800: '#1f2937', 900: '#111827',
  },
  blue: {
    50: '#eff6ff', 100: '#dbeafe', 200: '#bfdbfe', 300: '#93c5fd',
    400: '#60a5fa', 500: '#3b82f6', 600: '#2563eb', 700: '#1d4ed8',
    800: '#1e40af', 900: '#1e3a5f',
  },
  green: {
    50: '#f0fdf4', 100: '#dcfce7', 200: '#bbf7d0',
    500: '#22c55e', 600: '#16a34a', 700: '#15803d',
  },
  amber: {
    50: '#fffbeb', 100: '#fef3c7', 200: '#fde68a',
    500: '#f59e0b', 600: '#d97706', 700: '#b45309',
  },
  red: {
    50: '#fef2f2', 100: '#fee2e2', 200: '#fecaca',
    500: '#ef4444', 600: '#dc2626', 700: '#b91c1c',
  },
} as const

export const SPACING = {
  xs: '4px', sm: '8px', md: '16px', lg: '24px', xl: '32px', '2xl': '48px',
} as const

export const RADIUS = {
  sm: '6px', md: '8px', lg: '12px', xl: '16px', full: '9999px',
} as const

export const FONT = {
  sans: "-apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif",
  mono: "ui-monospace, SFMono-Regular, 'SF Mono', Menlo, monospace",
  size: { xs: '12px', sm: '13px', base: '14px', lg: '16px', xl: '20px', '2xl': '24px' },
  weight: { normal: '400', medium: '500', semibold: '600' },
} as const
