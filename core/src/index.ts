import { ShelfApplication } from './kernel/application.js'
import { SERVER } from '../../config/server.js'

ShelfApplication.instance.start(SERVER.PORT).catch((err) => {
  console.error('[shelf] failed to boot:', err)
  process.exit(1)
})
