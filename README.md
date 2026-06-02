# bedrock-relay

Local Go proxy for Anthropic-compatible and OpenAI-compatible chat APIs backed by Amazon Bedrock Claude native invocation.

<img width="1323" height="706" alt="bedrock" src="https://github.com/user-attachments/assets/114ebdf9-4ce9-43ba-978a-2b74b7bbb38e" />

## Quick start

```bash
export AWS_PROFILE="your-profile"
go run ./cmd/bedrock-relay
```

Endpoints are served at:

```text
Server:               http://127.0.0.1:18456
OpenAI-compatible:    http://127.0.0.1:18456/v1
Anthropic-compatible: http://127.0.0.1:18456
```

## Configuration

`AWS_PROFILE` is required. The selected AWS profile must contain credentials and a region.

Models are configured only in `models.json`; Bedrock model-listing APIs are never called.

```json
{
  "models": [
    {
      "alias": "claude-opus-4-7[1m]",
      "id": "us.anthropic.claude-opus-4-7[1m]"
    },
    {
      "alias": "claude-sonnet-4-6",
      "id": "us.anthropic.claude-sonnet-4-6"
    }
  ]
}
```

When `alias` is present, clients use the alias and Bedrock receives `id`. Without `alias`, clients use `id` directly.

Supported effort values are `low`, `medium`, `high`, `xhigh`, and `max`. The relay accepts OpenAI-style `reasoning_effort` or `reasoning.effort`, and Anthropic-style `effortLevel`, `effort_level`, or `output_config.effort`.

For the OpenAI Responses API only, Codex-facing `reasoning.effort` values are shifted to Bedrock's scale: `minimal` -> `low`, `low` -> `medium`, `medium` -> `high`, `high` -> `xhigh`, and `xhigh` -> `max`.

## Available models

The checked-in `models.json` currently exposes:

```text
claude-sonnet-4-6
claude-sonnet-4-6[1m]
claude-opus-4-7
claude-opus-4-7[1m]
claude-opus-4-6
claude-opus-4-6-v1[1m]
claude-haiku-4-5
```

## Client configuration

Point any OpenAI-compatible coding agent or editor at the relay:

```text
Base URL: http://127.0.0.1:18456/v1
API key:  any placeholder value (e.g. sk-dummy)
Model:    claude-sonnet-4-6
```

The relay does not require or validate API keys. If a client demands one, any placeholder works.

### Codex

Add the provider once in `~/.codex/config.toml`:

```toml
[model_providers.bedrock_relay]
name = "AWS Bedrock Relay"
base_url = "http://127.0.0.1:18456/v1"
wire_api = "responses"
stream_idle_timeout_ms = 10000000
```

Create a separate profile file at `~/.codex/bedrock.config.toml`. Put profile
settings at the top level of that file; do not nest them under
`[profiles.bedrock]`.

```toml
# ~/.codex/bedrock.config.toml
model = "claude-opus-4-6"
model_provider = "bedrock_relay"
web_search = "disabled"
model_catalog_json = "/path/to/bedrock-relay/model_catalog.json"
model_reasoning_effort = "medium"
```

Set `model_catalog_json` to the absolute path of the checked-in
`model_catalog.json` example. The catalog is for Codex model metadata only; the
relay's Bedrock aliases still come from `models.json`.

For current Codex builds, `apply_patch_tool_type` in the catalog must be
`"freeform"` when enabled. Older `"function"` values will fail configuration
loading with an unknown-variant error.

Then start Codex with the Bedrock profile:

```bash
codex -p bedrock
```

See [API_EXAMPLES.md](API_EXAMPLES.md) for runnable examples of every endpoint.

## Endpoints

- `GET /health`
- `GET /healthz`
- `GET /v1/models`
- `POST /v1/messages`
- `POST /v1/messages/count_tokens`
- `POST /v1/chat/completions`
- `POST /v1/responses`

## Notes

- The server reads `AWS_PROFILE` and loads region/credentials from the selected AWS profile.
- It does not call `ListFoundationModels`, `ListInferenceProfiles`, or other Bedrock discovery APIs.
- It does not require or validate API keys.
- Model aliases are local only; Bedrock receives the `id` from `models.json`.
