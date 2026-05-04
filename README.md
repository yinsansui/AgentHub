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
  -> file-backed event log for development
```

The durable event store is currently an append-only NDJSON development implementation. It is intentionally behind `EventStore` so it can be replaced by the agreed DB-backed event log.

## Verify

```bash
go test ./...
make images
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
