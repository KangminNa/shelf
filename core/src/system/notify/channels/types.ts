export type AlertLevel = 'problem' | 'recovery'

export interface Alert {
  event: string
  level: AlertLevel
  title: string
  detail: string
  app?: string
}

export interface NotificationChannel {
  readonly kind: string
  send(alert: Alert, target: { url: string; secret: string }): Promise<void>
}
