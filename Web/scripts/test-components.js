import { createRequire } from 'node:module'
import { dirname, join } from 'node:path'
import { pathToFileURL } from 'node:url'

// Set this before loading Vitest/Vite/Vue. In production mode Vue disables the
// devtools hook that Vue Test Utils uses to record wrapper.emitted() events.
// This is a test-runner invariant, not an application configuration option.
process.env.NODE_ENV = 'test'

const require = createRequire(import.meta.url)
const cli = join(dirname(require.resolve('vitest/package.json')), 'vitest.mjs')
await import(pathToFileURL(cli).href)
