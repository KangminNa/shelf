/** 서버 설정 — 환경변수 기반 */
export const SERVER = {
  PORT: Number(process.env.PORT || 9666),
  HOST: process.env.HOST || '0.0.0.0',
  NODE_ENV: process.env.NODE_ENV || 'development',
} as const
