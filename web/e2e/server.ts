import { spawn, ChildProcess } from 'child_process'
import fs from 'fs'
import path from 'path'
import os from 'os'
import http from 'http'
import net from 'net'
import { fileURLToPath } from 'url'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const rootDir = path.resolve(__dirname, '../..')
const binaryName = process.platform === 'win32' ? '.tmp/anubis-e2e.exe' : '.tmp/anubis-e2e'
const binaryPath = path.join(rootDir, binaryName)

export interface TestServer {
  baseURL: string
  stop: () => Promise<void>
  dataDir: string
}

export async function startServer(): Promise<TestServer> {
  if (!fs.existsSync(binaryPath)) {
    throw new Error(`E2E binary not found at ${binaryPath}. Run: go build -o .tmp/anubis-e2e ./cmd/anubis`)
  }

  const dataDir = fs.mkdtempSync(path.join(os.tmpdir(), 'anubis-e2e-'))
  const port = await getAvailablePort()

  const configPath = path.join(dataDir, 'anubis.json')
  const config = {
    server: {
      host: '127.0.0.1',
      port,
      tls: { enabled: false },
      allowed_origins: [`http://localhost:${port}`, `http://127.0.0.1:${port}`],
    },
    storage: { path: path.join(dataDir, 'data').replace(/\\/g, '/') },
    auth: {
      enabled: true,
      type: 'local',
      local: {
        admin_email: 'admin@anubis.watch',
        admin_password: 'SecurePass123!',
      },
    },
    dashboard: { enabled: true },
    necropolis: { enabled: false, node_name: 'jackal-e2e' },
    souls: [],
    channels: [],
    journeys: [],
    logging: { level: 'warn', format: 'json' },
  }
  fs.writeFileSync(configPath, JSON.stringify(config, null, 2))

  const proc = spawn(binaryPath, ['serve', '--single', '--config', configPath], {
    cwd: rootDir,
    env: { ...process.env, ANUBIS_LOG_LEVEL: 'warn', ANUBIS_SSRF_ALLOW_PRIVATE: '1' },
  })

  await waitForServer(port)

  return {
    baseURL: `http://localhost:${port}`,
    dataDir,
    stop: () => stopServer(proc, dataDir),
  }
}

function getAvailablePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const server = net.createServer()
    server.listen(0, '127.0.0.1', () => {
      const address = server.address()
      server.close(() => {
        if (typeof address === 'object' && address?.port) {
          resolve(address.port)
        } else {
          reject(new Error('Failed to allocate an E2E port'))
        }
      })
    })
    server.on('error', reject)
  })
}

function waitForServer(port: number): Promise<void> {
  const deadline = Date.now() + 15000
  return new Promise((resolve, reject) => {
    const tryConnect = () => {
      http.get(`http://localhost:${port}/health`, (res) => {
        if (res.statusCode === 200) {
          resolve()
        } else {
          retry()
        }
      }).on('error', retry)
    }

    const retry = () => {
      if (Date.now() > deadline) {
        reject(new Error(`Server failed to start on port ${port} within 15s`))
      } else {
        setTimeout(tryConnect, 200)
      }
    }

    tryConnect()
  })
}

async function stopServer(proc: ChildProcess, dataDir: string): Promise<void> {
  proc.kill('SIGTERM')
  await new Promise((resolve) => setTimeout(resolve, 500))
  if (!proc.killed) {
    proc.kill('SIGKILL')
  }
  try {
    fs.rmSync(dataDir, { recursive: true, force: true })
  } catch {
    // ignore cleanup errors
  }
}
