import type { Logger } from '../types.js'

export function createLogger(scope: string): Logger {
  const format = (level: string, message: string, data?: any) => {
    const timestamp = new Date().toISOString().slice(0, 19).replace('T', ' ')
    const suffix = data ? ` ${JSON.stringify(data)}` : ''
    console.log(`[${timestamp}] [${scope}] ${level} ${message}${suffix}`)
  }

  return {
    info: (message, data) => format('INFO', message, data),
    warn: (message, data) => format('WARN', message, data),
    error: (message, data) => format('ERROR', message, data),
    debug: (message, data) => {
      if (process.env.NODE_ENV === 'development') {
        format('DEBUG', message, data)
      }
    },
  }
}
