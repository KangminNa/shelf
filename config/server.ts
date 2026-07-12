export const SERVER = {
  PORT: Number(process.env.PORT || 9666),
  HOST: process.env.HOST || '0.0.0.0',
  NODE_ENV: process.env.NODE_ENV || 'development',
} as const

export const PATHS = {
  DATA_DIR: process.env.DATA_DIR || './data',
  PLUGINS_DIR: process.env.PLUGINS_DIR || './plugins',
  STORAGE_DIR: process.env.STORAGE_DIR || './data/storage',
} as const

export const AUTH = {
  SESSION_TTL_SECONDS: 7 * 24 * 3600, // 7 days
  SESSION_COOKIE_NAME: 'shelf_session',
  BCRYPT_ROUNDS: 12,
} as const

export const LIMITS = {
  RATE_LIMIT_WINDOW_MS: 60_000, // 1 minute
  RATE_LIMIT_MAX_REQUESTS: 60,
  REQUEST_TIMEOUT_MS: 30_000,
  MAX_BODY_SIZE_BYTES: 10 * 1024 * 1024, // 10MB
  MAX_UPLOAD_SIZE_BYTES: 50 * 1024 * 1024, // 50MB
  PAGINATION_DEFAULT_LIMIT: 20,
  PAGINATION_MAX_LIMIT: 100,
} as const

export const DB = {
  PRAGMA_JOURNAL_MODE: 'WAL',
  PRAGMA_FOREIGN_KEYS: 'ON',
  PRAGMA_BUSY_TIMEOUT: 5000,
} as const
