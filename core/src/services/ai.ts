import type { AiService, AiGenerateOpts, AiStreamOpts, ScaffoldResult, ConfigService } from '../types.js'

interface ProviderConfig {
  name: string
  baseUrl: string
  model: string
  apiKey: string
}

export async function createAiService(configService: ConfigService): Promise<AiService> {
  async function getProvider(name?: string): Promise<ProviderConfig | null> {
    const providers = await configService.get<ProviderConfig[]>('ai_providers', [])
    if (!providers.length) return null
    if (name) return providers.find((p) => p.name === name) || null
    return providers[0]
  }

  async function callProvider(provider: ProviderConfig, opts: AiGenerateOpts): Promise<string> {
    const isAnthropic = provider.baseUrl.includes('anthropic')

    if (isAnthropic) {
      const res = await fetch(`${provider.baseUrl}/v1/messages`, {
        method: 'POST',
        headers: {
          'Content-Type': 'application/json',
          'x-api-key': provider.apiKey,
          'anthropic-version': '2023-06-01',
        },
        body: JSON.stringify({
          model: provider.model,
          max_tokens: opts.maxTokens || 4096,
          system: opts.system,
          messages: [{ role: 'user', content: opts.prompt }],
        }),
      })
      const data = await res.json() as any
      return data.content?.[0]?.text || ''
    }

    // OpenAI-compatible
    const res = await fetch(`${provider.baseUrl}/v1/chat/completions`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Authorization: `Bearer ${provider.apiKey}`,
      },
      body: JSON.stringify({
        model: provider.model,
        max_tokens: opts.maxTokens || 4096,
        messages: [
          ...(opts.system ? [{ role: 'system', content: opts.system }] : []),
          { role: 'user', content: opts.prompt },
        ],
      }),
    })
    const data = await res.json() as any
    return data.choices?.[0]?.message?.content || ''
  }

  return {
    async generate(opts) {
      const provider = await getProvider(opts.provider)
      if (!provider) throw new Error('No AI provider configured')

      const text = await callProvider(provider, opts)

      if (opts.schema) {
        try {
          const parsed = JSON.parse(text)
          return opts.schema.parse(parsed)
        } catch {
          return text
        }
      }

      return text
    },

    async stream(opts) {
      const provider = await getProvider(opts.provider)
      if (!provider) throw new Error('No AI provider configured')
      // Simplified: full streaming implementation would use SSE parsing
      const text = await callProvider(provider, opts)
      opts.onChunk(text)
    },

    async scaffold(description): Promise<ScaffoldResult> {
      const provider = await getProvider()
      if (!provider) throw new Error('No AI provider configured')

      const prompt = `You are a module generator for the Shelf framework.
Given a description, generate the 4 files needed for a Shelf module.
Return valid JSON with this structure:
{
  "manifest": { "name": "...", "displayName": "...", "icon": "...", "menu": [...] },
  "schema": "// TypeScript code for schema.ts",
  "logic": "// TypeScript code for logic.ts",
  "pages": { "index.tsx": "// React component code" }
}

Description: ${description}`

      const text = await callProvider(provider, { prompt, maxTokens: 4096 })
      try {
        return JSON.parse(text)
      } catch {
        throw new Error('AI returned invalid module structure')
      }
    },
  }
}
