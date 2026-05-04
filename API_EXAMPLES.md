# Bedrock Relay — API Quick Reference

> For setup, configuration, model list, and Cursor settings see [README.md](README.md).

Base URL used in all examples: `http://127.0.0.1:18456`

---

## Health

```bash
curl http://127.0.0.1:18456/health
```

## List models

```bash
curl http://127.0.0.1:18456/v1/models
```

## OpenAI

```bash
curl http://127.0.0.1:18456/v1/chat/completions \
  -H "content-type: application/json" \
  -d '{
    "model": "claude-sonnet-4-6",
    "max_tokens": 128,
    "messages": [{"role": "user", "content": "Hello world"}]
  }'
```

## OpenAI With Effort

```bash
curl http://127.0.0.1:18456/v1/chat/completions \
  -H "content-type: application/json" \
  -d '{
    "model": "claude-opus-4-7",
    "max_tokens": 512,
    "reasoning_effort": "xhigh",
    "messages": [{"role": "user", "content": "Think carefully and answer briefly."}]
  }'
```

Effort values: `low` `medium` `high` `xhigh` `max`

## OpenAI Responses

```bash
curl http://127.0.0.1:18456/v1/responses \
  -H "content-type: application/json" \
  -d '{
    "model": "claude-sonnet-4-6",
    "input": "Hello from the Responses API"
  }'
```

## Anthropic

```bash
curl http://127.0.0.1:18456/v1/messages \
  -H "content-type: application/json" \
  -d '{
    "model": "claude-sonnet-4-6",
    "max_tokens": 128,
    "messages": [{"role": "user", "content": "Hello world"}]
  }'
```

## Anthropic With Effort

```bash
curl http://127.0.0.1:18456/v1/messages \
  -H "content-type: application/json" \
  -d '{
    "model": "claude-opus-4-7",
    "max_tokens": 512,
    "effortLevel": "max",
    "messages": [{"role": "user", "content": "Think carefully and answer briefly."}]
  }'
```

Anthropic-style effort keys: `effortLevel`, `effort_level`, `output_config.effort`

## Streaming

OpenAI streaming:

```bash
curl -N http://127.0.0.1:18456/v1/chat/completions \
  -H "content-type: application/json" \
  -d '{
    "model": "claude-sonnet-4-6",
    "stream": true,
    "max_tokens": 128,
    "messages": [{"role": "user", "content": "Stream a short greeting."}]
  }'
```

Anthropic streaming:

```bash
curl -N http://127.0.0.1:18456/v1/messages \
  -H "content-type: application/json" \
  -d '{
    "model": "claude-sonnet-4-6",
    "stream": true,
    "max_tokens": 128,
    "messages": [{"role": "user", "content": "Stream a short greeting."}]
  }'
```
