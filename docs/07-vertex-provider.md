# Vertex Gemini Provider (Optional)

This provider calls Vertex AI Gemini using **Application Default Credentials (ADC)**.
It does **not** use API keys.

## Package

- `agentic/providers/vertex`

## Environment Variables

- `VERTEX_PROJECT` (required)
- `VERTEX_MODEL` (required)
- `VERTEX_LOCATION` (optional, default: `global`)
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

- The caller must run with a GCP identity that has `roles/aiplatform.user` on the target project.
- Use Application Default Credentials via `gcloud auth application-default login` or set `GOOGLE_APPLICATION_CREDENTIALS` to a service account JSON file.
- Set `VERTEX_LOCATION=global` when using the global Gemini endpoint.
