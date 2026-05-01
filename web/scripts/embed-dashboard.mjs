import { cpSync, existsSync, rmSync } from 'node:fs'
import { dirname, resolve } from 'node:path'
import { fileURLToPath } from 'node:url'

const scriptDir = dirname(fileURLToPath(import.meta.url))
const webRoot = resolve(scriptDir, '..')
const repoRoot = resolve(webRoot, '..')
const webDist = resolve(webRoot, 'dist')
const embeddedDist = resolve(repoRoot, 'internal', 'dashboard', 'dist')

if (!existsSync(webDist)) {
  throw new Error(`Missing dashboard build output: ${webDist}`)
}

rmSync(embeddedDist, { recursive: true, force: true })
cpSync(webDist, embeddedDist, { recursive: true })

console.log(`Embedded dashboard assets copied to ${embeddedDist}`)
