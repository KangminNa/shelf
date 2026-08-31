import { test } from 'node:test'
import assert from 'node:assert/strict'
import { DeployPipeline } from '../src/system/deploy/pipeline.js'
import { ContainerManager } from '../src/system/deploy/container-manager.js'
import type { Project } from '../src/system/deploy/repositories.js'

const base: Project = {
  id: 1, name: 'demo', source_type: 'git',
  repo_url: 'https://github.com/user/repo.git', branch: 'main', git_token: '',
  image: '', port: 3000, container_port: 3000, env: '', volumes: '', domain: '',
  webhook_secret: 's', auto_deploy: 1,
  install_cmd: '', build_cmd: '', start_cmd: '', created_at: 0, updated_at: 0,
}

test('authenticatedUrl injects token into https URLs only, never local paths or ssh', () => {
  assert.equal(DeployPipeline.authenticatedUrl(base), 'https://github.com/user/repo.git')

  const withToken = { ...base, git_token: 'ghp_abc123' }
  assert.equal(
    DeployPipeline.authenticatedUrl(withToken),
    'https://x-access-token:ghp_abc123@github.com/user/repo.git'
  )

  const localRepo = { ...base, git_token: 'ghp_abc123', repo_url: '/srv/repos/app' }
  assert.equal(DeployPipeline.authenticatedUrl(localRepo), '/srv/repos/app')
  const sshRepo = { ...base, git_token: 'ghp_abc123', repo_url: 'git@github.com:user/repo.git' }
  assert.equal(DeployPipeline.authenticatedUrl(sshRepo), 'git@github.com:user/repo.git')
})

test('container and image naming conventions', () => {
  assert.equal(ContainerManager.containerName(base), 'shelf-demo')
  assert.equal(ContainerManager.imageTag(base), 'shelf-app-demo')
  const imageApp = { ...base, source_type: 'image' as const, image: 'nginx:alpine' }
  assert.equal(ContainerManager.imageTag(imageApp), 'nginx:alpine')
})

test('a batched status lookup keeps the containers it could read when others are missing', async () => {
  const { DockerService } = await import('../src/system/docker.js')
  const docker = new DockerService({ warn() {}, info() {}, error() {}, debug() {} } as any)

  // docker inspect exits non-zero when any name is unknown, but still prints the ones it found.
  const failure: any = new Error('no such object')
  failure.output = '/shelf-live running 0\n'
  ;(docker as any).run = async () => { throw failure }

  const statuses = await docker.statuses(['shelf-live', 'shelf-missing'])
  assert.equal(statuses.get('shelf-live'), 'running', 'a running container must not be reported as stopped')
  assert.equal(statuses.get('shelf-missing'), undefined, 'unknown containers are simply absent')
})

test('a batched status lookup asks docker nothing when there are no apps', async () => {
  const { DockerService } = await import('../src/system/docker.js')
  const docker = new DockerService({ warn() {}, info() {}, error() {}, debug() {} } as any)
  let called = false
  ;(docker as any).run = async () => { called = true; return { output: '' } }

  assert.equal((await docker.statuses([])).size, 0)
  assert.equal(called, false)
})

test('a build path selects a folder inside the repository', async () => {
  const { DeployPipeline } = await import('../src/system/deploy/pipeline.js')
  const repo = '/srv/shelf/data/deploy/repos/landing'

  assert.equal(DeployPipeline.buildContext(repo, ''), repo, 'no build path means the repository root')
  assert.equal(DeployPipeline.buildContext(repo, '   '), repo, 'blank is the same as unset')
  assert.equal(DeployPipeline.buildContext(repo, 'site'), `${repo}/site`)
  assert.equal(DeployPipeline.buildContext(repo, 'apps/web'), `${repo}/apps/web`)
  assert.equal(DeployPipeline.buildContext(repo, './site/'), `${repo}/site`, 'leading ./ and trailing / are tolerated')
})

test('a build path can never escape the repository', async () => {
  const { DeployPipeline } = await import('../src/system/deploy/pipeline.js')
  const repo = '/srv/shelf/data/deploy/repos/landing'

  const escapes = ['..', '../..', 'site/../..', '/etc', '/', '../landing-other', 'a/../../b']
  for (const attempt of escapes) {
    assert.throws(
      () => DeployPipeline.buildContext(repo, attempt),
      /points outside the repository/,
      `"${attempt}" must be refused`
    )
  }
})

test('a sibling directory sharing the repository name prefix is still outside', async () => {
  const { DeployPipeline } = await import('../src/system/deploy/pipeline.js')
  assert.throws(
    () => DeployPipeline.buildContext('/repos/app', '../app-evil'),
    /points outside the repository/,
    'prefix matching must not let /repos/app-evil pass as inside /repos/app'
  )
})
