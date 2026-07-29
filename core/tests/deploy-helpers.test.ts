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

test('authenticatedUrl injects token into https URLs only', () => {
  assert.equal(DeployPipeline.authenticatedUrl(base), 'https://github.com/user/repo.git')

  const withToken = { ...base, git_token: 'ghp_abc123' }
  assert.equal(
    DeployPipeline.authenticatedUrl(withToken),
    'https://x-access-token:ghp_abc123@github.com/user/repo.git'
  )

  // 로컬 경로/SSH URL에는 주입하지 않는다
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
