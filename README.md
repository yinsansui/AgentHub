# AgentHub

AgentHub is an agent runtime control-plane prototype. The first development slice focuses on a Docker-backed `agent-pod` abstraction and a Pi Agent adapter boundary.

## Current slice

```text
control-plane
  -> DockerAgentPodDriver
  -> agent-pod container
  -> AgentPodServer
  -> PiAgentCoreAdapter boundary
  -> UniversalEvent SSE
  -> PostgreSQL-backed session_events event log with global event ids
  -> messages + message_blocks projection for session reads
```

The durable control-plane store uses PostgreSQL when `AGENTHUB_DATABASE_URL` is configured. Runtime events are appended to `session_events` with a global `BIGSERIAL` cursor, message metadata is projected into `messages`, and completed item content is expanded into `message_blocks` for lightweight blocks-first session reads. Streaming `item.delta` events stay in the event log and are not treated as final blocks. If no database URL is configured, the server falls back to an in-memory development store only.

Session reads follow a snapshot + replay + live pattern:

```text
GET /sessions/{sessionId}/messages        # current message snapshot
GET /sessions/{sessionId}/events?after=id # durable replay / gap fill
GET /sessions/{sessionId}/stream          # live SSE; supports Last-Event-ID
```

`/events` returns stored event envelopes (`id`, `type`, `payload`, `createdAt`), and `/stream` writes the same `id` as the SSE `id:` field.

## Verify

```bash
go test ./...
make images
```

For durable local runs, point the control-plane at PostgreSQL:

```bash
export AGENTHUB_DATABASE_URL=postgres://agenthub:agenthub@127.0.0.1:5432/agenthub?sslmode=disable
go run ./cmd/control-plane
```

## Layout

```text
cmd/control-plane       # executable entrypoint, wire-up only
cmd/agent-pod           # executable entrypoint, wire-up only
internal/controlplane   # control-plane core
internal/agentpod       # agent-pod core
internal/driver         # replaceable container driver interface
internal/driver/docker  # Docker Engine API implementation
internal/runtime        # replaceable AI runtime interface
internal/runtime/pi     # Pi CLI adapter boundary
pkg/protocol            # reusable UniversalEvent / TurnRequest contracts
pkg/sse                 # reusable SSE transport helpers
web/admin-console       # future frontend
web/user-portal         # future frontend
deploy                  # Dockerfiles and deployment assets
```

`cmd/*` must stay thin; business logic belongs under `internal/*`. `pkg/*` is limited to pure reusable contracts/utilities.
