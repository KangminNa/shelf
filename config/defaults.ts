export const MODULE_DEFAULTS = {
  RATE_LIMIT: {
    WINDOW: '1m',
    MAX: 60,
  },
  TIMEOUT_MS: 30_000,
  PAGINATION: {
    DEFAULT_LIMIT: 20,
    MAX_LIMIT: 100,
  },
} as const

export const AI_DEFAULTS = {
  MAX_TOKENS: 4096,
  TEMPERATURE: 0.7,
  PROVIDERS: {
    ANTHROPIC: {
      BASE_URL: 'https://api.anthropic.com',
      DEFAULT_MODEL: 'claude-sonnet-4-6',
      API_VERSION: '2023-06-01',
    },
    OPENAI: {
      BASE_URL: 'https://api.openai.com',
      DEFAULT_MODEL: 'gpt-4o',
    },
    OLLAMA: {
      BASE_URL: 'http://localhost:11434',
      DEFAULT_MODEL: 'llama3',
    },
  },
} as const

export const SCHEDULER_DEFAULTS = {
  MIN_INTERVAL_MS: 60_000, // minimum 1 minute
  MAX_RETRIES: 3,
  RETRY_DELAY_MS: 5_000,
} as const

export const STORAGE_DEFAULTS = {
  MAX_FILE_SIZE_BYTES: 50 * 1024 * 1024, // 50MB
  ALLOWED_EXTENSIONS: ['jpg', 'jpeg', 'png', 'gif', 'webp', 'pdf', 'txt', 'md', 'json', 'csv'],
} as const
