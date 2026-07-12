export class AppError extends Error {
  constructor(
    public code: string,
    public statusCode: number,
    message: string,
    public details?: unknown
  ) {
    super(message)
    this.name = 'AppError'
  }
}

export const Errors = {
  notFound: (entity: string) =>
    new AppError('NOT_FOUND', 404, `${entity} not found`),
  forbidden: () =>
    new AppError('FORBIDDEN', 403, 'Access denied'),
  unauthorized: () =>
    new AppError('UNAUTHORIZED', 401, 'Authentication required'),
  validation: (details: unknown) =>
    new AppError('VALIDATION', 400, 'Invalid input', details),
  conflict: (message: string) =>
    new AppError('CONFLICT', 409, message),
  rateLimit: () =>
    new AppError('RATE_LIMIT', 429, 'Too many requests'),
  internal: (message = 'Something went wrong') =>
    new AppError('INTERNAL', 500, message),
}
