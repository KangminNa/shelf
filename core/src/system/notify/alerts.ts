import type { Alert } from './channels/types.js'

type Describe = (payload: any) => Alert

const problem = (event: string, title: string, detail: (p: any) => string): Describe => (payload) => ({
  event,
  level: 'problem',
  title: `${title}${payload?.name ? `: ${payload.name}` : ''}`,
  detail: detail(payload ?? {}),
  app: payload?.name,
})

export const ALERTS: Record<string, Describe> = {
  'monitor:app-down': problem('monitor:app-down', 'App is down', (p) => `${p.name} stopped responding${p.domain ? ` (${p.domain})` : ''}`),
  'deploy:failed': problem('deploy:failed', 'Deploy failed', (p) => p.error || 'deployment did not finish'),
  'proxy:cert-renewal-failed': problem('proxy:cert-renewal-failed', 'Certificate renewal failed', (p) => `${p.domain}: ${p.error || 'renewal did not finish'}`),
  'monitor:app-recovered': (payload) => ({
    event: 'monitor:app-recovered',
    level: 'recovery',
    title: `App recovered: ${payload?.name}`,
    detail: `${payload?.name} is running again`,
    app: payload?.name,
  }),
}

export const ALERT_EVENTS = Object.keys(ALERTS)
