# AgentHub

[English](README.md) | [简体中文](README.zh-CN.md)

AgentHub is a prototype control plane for agent runtimes. It is useful for local development, architecture exploration, and smoke testing a Docker backed `agent-pod` runtime path. It is not production ready.

## Overview

AgentHub separates the user facing control plane from the actual agent runtime. The current prototype runs a Go `control-plane`, a Go `agent-pod`, a TypeScript runtime host, and the implemented React user portal.

The first runtime path is:

```text
user-portal
  -> control-plane
  -> DockerAgentPodDriver
  -> agent-pod
  -> AgentPodServer
  -> ts-runtime-host
  -> pi-coding-agent adapter
  -> UniversalEvent SSE
  -> PostgreSQL session event log and message projections
```

## Current capabilities

- Cookie based prototype login for user facing APIs, with the local built in account `admin` / `admin`.
- Workspace list and default workspace creation for the current owner.
- Session creation, follow up turns, live SSE streaming, durable event replay, message snapshots, and run interrupt handling.
- PostgreSQL backed storage when `AGENTHUB_DATABASE_URL` is set, with an in memory development store when it is not.
- Per workspace LLM connection and model settings, resolved into the runtime host process for each turn.
- TypeScript runtime host support for `@mariozechner/pi-coding-agent` through the `pi-coding-agent` adapter.
- React user portal under `web/user-portal` for local interaction with the control plane.

## Architecture

The Go services keep executable entrypoints thin under `cmd/*`, put business logic under `internal/*`, and expose reusable protocol and SSE helpers under `pkg/*`.

`control-plane` owns workspace, session, task, run, event, message, skill, MCP, and LLM connection state. `agent-pod` owns the local runtime boundary and runs the configured runtime command. `runtimes/ts-runtime-host` maps AgentHub runtime requests into the first TypeScript adapter.

For a deeper design note, see [docs/architecture.md](docs/architecture.md).

## Prerequisites

- Go 1.26, as declared in `go.mod`.
- Node.js and npm, used by `runtimes/ts-runtime-host` and `web/user-portal`.
- Docker, used for local PostgreSQL and image builds.
- `curl`, used by the local startup script for readiness checks.
- PostgreSQL is started by the local script when `AGENTHUB_DATABASE_URL` is not set. The local example uses user `agenthub`, password `agenthub`, database `agenthub`, and port `5432`.

## 5-minute first run

Clone the repo, create a local environment file, and start the development stack:

```bash
git clone <your-agenthub-repo-url>
cd AgentHub
cp .env.example .env
make dev
```

The checked-in `.env.example` works for the default local prototype. You only need to edit `.env` when changing ports, using an external PostgreSQL database, or running the real LLM smoke test.

By default `make dev` runs `scripts/start-dev.sh`. The script loads `.env`, builds the TypeScript runtime host and Go binaries, starts local PostgreSQL in Docker when `AGENTHUB_DATABASE_URL` is empty, then starts:

```text
ControlPlane: http://127.0.0.1:3000
AgentPod:     http://127.0.0.1:3001
UserPortal:   http://127.0.0.1:5174
Login:        admin / admin
```

Open `http://127.0.0.1:5174` and log in with `admin` / `admin`. These credentials are only for the local prototype.

Press `Ctrl+C` in the startup terminal to stop the services, or run `make stop-dev` from another terminal. The script stops its PostgreSQL container unless `AGENTHUB_KEEP_POSTGRES=1` is set.

## Manual run and verification

Run the main local checks:

```bash
make check
```

This runs `go test ./...`, then installs and builds `runtimes/ts-runtime-host` with npm.

Build the local Docker images:

```bash
make images
```

Verify run cancel lifecycle behavior without a real LLM:

```bash
make smoke-cancel-lifecycle
```

Run the real runtime smoke path only when you have a local Anthropic compatible endpoint and a disposable API key:

```bash
export AGENTHUB_SMOKE_LLM_BASE_URL=http://example.local:8084
export AGENTHUB_SMOKE_LLM_API_KEY=replace-with-local-test-key
export AGENTHUB_SMOKE_LLM_MODEL_ID=k2p5
make smoke-real-runtime
```

The real runtime smoke script starts PostgreSQL, `control-plane`, a local `agent-pod`, configures workspace skill, MCP, and LLM definitions, asks the real LLM to call a local stdio MCP tool, then checks `/state`, `/messages`, and `/events?after=0`.

## Configuration

Useful local environment variables:

- `AGENTHUB_CONTROL_PLANE_PORT`, default `3000`.
- `AGENTHUB_AGENT_POD_PORT`, default `3001`.
- `AGENTHUB_USER_PORTAL_PORT`, default `5174`.
- `AGENTHUB_POSTGRES_PORT`, default `5432`.
- `AGENTHUB_DATABASE_URL`, default empty. When empty, `scripts/start-dev.sh` starts Docker PostgreSQL and uses `postgres://agenthub:agenthub@127.0.0.1:5432/agenthub?sslmode=disable`.
- `AGENTHUB_INTERNAL_TOKEN`, default `dev-token`, used between the local control plane and AgentPod.
- `AGENTHUB_RUNTIME_COMMAND`, optional. When unset, `scripts/start-dev.sh` uses `node <repo>/runtimes/ts-runtime-host/dist/main.js --adapter pi-coding-agent`.
- `AGENTHUB_USER_PORTAL_PROXY_TARGET`, used by the Vite user portal to proxy API calls to another control plane.

See [.env.example](.env.example) for the complete local startup template.

Workspace LLM connection settings are managed through the control plane API, not by hand editing runtime host environment variables. Current related APIs include:

```text
PUT /workspaces/{workspaceId}/llm-connection
GET /workspaces/{workspaceId}/llm-connection
POST /workspaces/{workspaceId}/llm-models:refresh
PUT /workspaces/{workspaceId}/llm-models
GET /workspaces/{workspaceId}/llm-models
```

## Repo layout

```text
cmd/control-plane          Go control-plane executable entrypoint
cmd/agent-pod              Go AgentPod executable entrypoint
internal/controlplane      control-plane core
internal/agentpod          AgentPod core
internal/driver            container driver interface
internal/driver/docker     Docker Engine API driver
internal/runtime           runtime interface
internal/runtime/process   process backed runtime host adapter
pkg/protocol               UniversalEvent and turn contracts
pkg/sse                    SSE transport helpers
runtimes/ts-runtime-host   TypeScript runtime host and pi-coding-agent adapter
web/user-portal            implemented React user portal
web/admin-console          planned admin console
deploy                     Dockerfiles and deployment assets
docs                       design and development notes
```

## Security and prototype notes

- AgentHub is a prototype. Do not expose the current local stack to untrusted networks.
- `admin` / `admin`, `agenthub` / `agenthub`, and `dev-token` are local development examples, not real secrets.
- The smoke LLM API key example should be a disposable local test value.
- Runtime secrets are passed through the control plane API and runtime subprocess environment. Avoid putting real credentials in shell history, docs, or committed files.
- The current repo instructions state that the project has not launched yet and does not preserve compatibility with old local database shapes.

## Further docs

- [docs/architecture.md](docs/architecture.md) for the architecture model, core terms, event model, and current constraints.
- [web/user-portal/README.md](web/user-portal/README.md) for the implemented user portal.
