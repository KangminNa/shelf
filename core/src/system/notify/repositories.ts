import type Database from 'better-sqlite3'
import { Repository } from '../../db/repository.js'

export interface Channel {
  id: number
  kind: string
  url: string
  secret: string
  enabled: number
  description: string
  created_at: number
  updated_at: number
}

export interface Delivery {
  id: number
  channel_id: number
  event: string
  title: string
  ok: number
  detail: string
  created_at: number
}

export class ChannelRepository extends Repository<Channel> {
  constructor(db: Database.Database) {
    super(db, 'channels')
  }

  allSorted(): Channel[] {
    return this.query().orderBy('created_at', 'desc').all()
  }

  active(): Channel[] {
    return this.query().where('enabled', 1).all()
  }
}

export class DeliveryRepository extends Repository<Delivery> {
  constructor(db: Database.Database) {
    super(db, 'deliveries')
  }

  recent(limit = 20): Delivery[] {
    return this.query().orderBy('created_at', 'desc').limit(limit).all()
  }

  prune(keep = 200): void {
    const cutoff = this.query().orderBy('created_at', 'desc').limit(keep).pluck<number>('id')
    if (cutoff.length < keep) return
    this.query().where('id', '<', Math.min(...cutoff)).delete()
  }
}
