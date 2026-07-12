import type { ErrorHandler } from 'hono'
import { AppError } from '../helpers/errors.js'

export const errorBoundary: ErrorHandler = (err, c) => {
  if (err instanceof AppError) {
    return c.json(
      {
        ok: false,
        data: null,
        error: {
          code: err.code,
          message: err.message,
          ...(err.details ? { details: err.details } : {}),
        },
      },
      err.statusCode as any
    )
  }

  console.error('[UNHANDLED]', err)
  return c.json(
    {
      ok: false,
      data: null,
      error: { code: 'INTERNAL', message: 'Something went wrong' },
    },
    500
  )
}
