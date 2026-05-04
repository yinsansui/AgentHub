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
