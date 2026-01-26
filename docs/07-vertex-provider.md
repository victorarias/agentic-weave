# Vertex Gemini Provider (Optional)

This provider calls Vertex AI Gemini. It supports two authentication methods:

1. **API Key** - Simpler setup, good for development and CI
2. **Application Default Credentials (ADC)** - For production with GCP IAM

## Package

- `agentic/providers/vertex`

## Environment Variables

**API Key Auth (simpler):**
- `VERTEX_AI_API_KEY` (required for API key auth)
- `VERTEX_MODEL` (required)

**OAuth2/ADC Auth:**
- `VERTEX_PROJECT` (required for ADC auth)
- `VERTEX_MODEL` (required)
- `VERTEX_LOCATION` (optional, default: `global`)

**Common:**
- `VERTEX_API_BASE` (optional, default: `https://aiplatform.googleapis.com/v1`)
- `VERTEX_TEMPERATURE` (optional)
- `VERTEX_MAX_TOKENS` (optional)

## Usage

```go
client, err := vertex.NewFromEnv()
if err != nil {
    // handle config error
}

result, err := client.Decide(ctx, vertex.Input{
    SystemPrompt: "You are a helpful assistant.",
    UserMessage:  "Summarize the latest changes.",
    Tools:        tools,
})
if err != nil {
    // handle request error
}

fmt.Println(result.Reply)
```

## Notes

**API Key Auth:**
- Get an API key from the Google AI Studio or GCP Console
- Set `VERTEX_AI_API_KEY` and `VERTEX_MODEL` environment variables

**ADC Auth:**
- The caller must run with a GCP identity that has `roles/aiplatform.user` on the target project
- Use Application Default Credentials via `gcloud auth application-default login` or set `GOOGLE_APPLICATION_CREDENTIALS` to a service account JSON file
- Set `VERTEX_LOCATION=global` when using the global Gemini endpoint
