# Microservice with Service Discovery

CMPE-273 Week 7 — Service discovery using Consul, Go, and Docker Compose.

<!-- ## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                   Docker Compose Network                    │
│                                                             │
│  ┌────────────────┐  ┌────────────────┐  ┌──────────────┐  │
│  │   service-1    │  │   service-2    │  │    consul    │  │
│  │   Go :5001     │  │   Go :5002     │  │  :8500 (UI)  │  │
│  │  GET /hello    │  │  GET /hello    │  │  Service     │  │
│  │  GET /health   │  │  GET /health   │  │  Registry    │  │
│  └───────┬────────┘  └───────┬────────┘  └──────┬───────┘  │
│          │  PUT /register     │                  │          │
│          └───────────────────►│──────────────────►          │
└──────────────────────────────────────────────────┼──────────┘
                                                   │
                              ┌────────────────────▼────────┐
                              │          client (Go)         │
                              │  1. GET /v1/health/service/  │
                              │     hello-service?passing    │
                              │  2. rand.Intn(instances)     │
                              │  3. GET <address>:<port>/    │
                              │     hello                    │
                              └─────────────────────────────┘
``` -->
# Architecture Diagrams

## 1. System Overview

```mermaid
graph TD
    subgraph Docker Compose Network
        C[("Consul\nService Registry\n:8500")]

        subgraph Instances
            S1["service-1\nGo HTTP :5001\n/hello  /health"]
            S2["service-2\nGo HTTP :5002\n/hello  /health"]
        end

        CL["client\nGo — Discovery Loop"]
    end

    S1 -- "PUT /v1/agent/service/register" --> C
    S2 -- "PUT /v1/agent/service/register" --> C
    C -- "GET /health (every 10s)" --> S1
    C -- "GET /health (every 10s)" --> S2
    CL -- "GET /v1/health/service/hello-service?passing" --> C
    CL -- "GET /hello (random pick)" --> S1
    CL -- "GET /hello (random pick)" --> S2
```

---

## 2. Startup — Service Registration

```mermaid
sequenceDiagram
    autonumber
    participant DC as Docker Compose
    participant C as Consul :8500
    participant S1 as service-1 :5001
    participant S2 as service-2 :5002
    participant CL as client

    DC->>C: Start consul (dev mode)
    Note over C: healthcheck: consul members ✓

    DC->>S1: Start service-1
    S1->>C: PUT /v1/agent/service/register<br/>{ ID: "service-1", Name: "hello-service",<br/>  Address: "service-1", Port: 5001,<br/>  Check: { HTTP: "/health", Interval: "10s" } }
    C-->>S1: 200 OK

    DC->>S2: Start service-2
    S2->>C: PUT /v1/agent/service/register<br/>{ ID: "service-2", Name: "hello-service",<br/>  Address: "service-2", Port: 5002,<br/>  Check: { HTTP: "/health", Interval: "10s" } }
    C-->>S2: 200 OK

    Note over C: Both instances registered under "hello-service"

    DC->>CL: Start client (after service-1 & service-2 healthy)
```

---

## 3. Normal Operation — Discovery & Load Balancing

```mermaid
sequenceDiagram
    autonumber
    participant CL as client
    participant C as Consul :8500
    participant S1 as service-1 :5001
    participant S2 as service-2 :5002

    loop Every 10s (background)
        C->>S1: GET /health
        S1-->>C: 200 {"status":"passing"}
        C->>S2: GET /health
        S2-->>C: 200 {"status":"passing"}
    end

    loop Every 2s
        CL->>C: GET /v1/health/service/hello-service?passing
        C-->>CL: [{ Service: {ID:"service-1", Address:"service-1", Port:5001} },<br/>           { Service: {ID:"service-2", Address:"service-2", Port:5002} }]

        Note over CL: rand.Intn(2) → picks service-1
        CL->>S1: GET /hello
        S1-->>CL: { message: "Hello from service-1!",<br/>             instance: "service-1",<br/>             container_id: "a3f2b1c4" }

        CL->>C: GET /v1/health/service/hello-service?passing
        C-->>CL: [ service-1, service-2 ]

        Note over CL: rand.Intn(2) → picks service-2
        CL->>S2: GET /hello
        S2-->>CL: { message: "Hello from service-2!",<br/>             instance: "service-2",<br/>             container_id: "9b8c7d6e" }
    end
```

---

## 4. Failure & Failover — Graceful Shutdown

```mermaid
sequenceDiagram
    autonumber
    participant OPS as Operator
    participant S1 as service-1 :5001
    participant C as Consul :8500
    participant CL as client

    OPS->>S1: docker compose stop service-1<br/>(sends SIGTERM)

    S1->>C: PUT /v1/agent/service/deregister/service-1
    C-->>S1: 200 OK
    Note over C: service-1 removed from registry immediately

    S1->>S1: srv.Shutdown() — drain in-flight requests
    Note over S1: Container exits cleanly

    CL->>C: GET /v1/health/service/hello-service?passing
    C-->>CL: [ service-2 only ]
    Note over CL: Discovered 1 instance. Picks service-2.

    CL->>C: GET /v1/health/service/hello-service?passing
    C-->>CL: [ service-2 only ]
```

---

## 5. Crash Recovery — Auto Deregistration

```mermaid
sequenceDiagram
    autonumber
    participant S1 as service-1 :5001
    participant C as Consul :8500
    participant CL as client

    Note over S1: Container crashes (SIGKILL / OOM)<br/>No graceful shutdown — SIGTERM handler does NOT run

    loop Every 10s
        C->>S1: GET /health
        S1--xC: Connection refused (container is down)
        Note over C: Health check → critical
    end

    Note over C: DeregisterCriticalServiceAfter: 30s<br/>Auto-deregisters service-1 after 30s of failure

    CL->>C: GET /v1/health/service/hello-service?passing
    C-->>CL: [ service-2 only ]
    Note over CL: Automatically routes to service-2
```

---

## 6. Recovery — Re-registration

```mermaid
sequenceDiagram
    autonumber
    participant OPS as Operator
    participant S1 as service-1 :5001
    participant C as Consul :8500
    participant CL as client

    OPS->>S1: docker compose start service-1

    S1->>C: PUT /v1/agent/service/register<br/>{ ID: "service-1", ... }
    C-->>S1: 200 OK

    loop Health check passes
        C->>S1: GET /health
        S1-->>C: 200 {"status":"passing"}
    end

    Note over C: service-1 status → passing ✓

    CL->>C: GET /v1/health/service/hello-service?passing
    C-->>CL: [ service-1, service-2 ]
    Note over CL: Discovered 2 instances again — load balancing restored
```


### Sequence Diagram

```mermaid
sequenceDiagram
    participant S1 as service-1 (Go :5001)
    participant S2 as service-2 (Go :5002)
    participant C  as Consul Registry
    participant CL as client (Go)

    Note over S1,S2: Docker Compose starts all containers

    S1->>C: PUT /v1/agent/service/register
    S2->>C: PUT /v1/agent/service/register

    loop Every 10s
        C->>S1: GET /health (health check)
        C->>S2: GET /health (health check)
    end

    loop Every 2s
        CL->>C: GET /v1/health/service/hello-service?passing
        C-->>CL: [{service-1, :5001}, {service-2, :5002}]
        CL->>CL: rand.Intn(2) → pick random instance
        CL->>S1: GET /hello  (or S2)
        S1-->>CL: {"message": "Hello from service-1!", "container_id": "..."}
    end

    Note over S1: docker compose stop service-1
    S1->>C: PUT /v1/agent/service/deregister/service-1
    Note over CL: Next discovery returns only service-2
```

## How It Works

1. **Registration** — Each Go service calls `PUT /v1/agent/service/register` on Consul at startup. It registers with a unique `ID` (e.g. `service-1`), a shared logical `Name` (`hello-service`), its Docker DNS address, port, and a health check URL. Consul polls `/health` every 10 seconds to verify the instance is alive.

2. **Discovery** — The client calls `GET /v1/health/service/hello-service?passing`. The `?passing` parameter tells Consul to return only instances whose health check is currently passing — crashed or stopped instances are excluded automatically.

3. **Client-Side Load Balancing** — The client picks a random instance from the list using `rand.Intn` and calls `/hello` directly. No proxy or load balancer is involved. This is the simplest form of service discovery.

4. **Graceful Deregistration** — When a service receives `SIGTERM` (e.g. `docker compose stop`), it calls `PUT /v1/agent/service/deregister/<id>` before shutting down. Consul removes the instance immediately. The `DeregisterCriticalServiceAfter: 30s` config handles crash cases (SIGKILL) where graceful shutdown doesn't run.

## Stack

| Component | Technology |
|---|---|
| Service instances | Go (`net/http`) |
| Service registry | Consul 1.17 |
| Client | Go (`net/http`, `math/rand`) |
| Orchestration | Docker Compose |
| Container base | `golang:1.22-alpine` (build) + `alpine:3.19` (runtime) |

## Quick Start

```bash
git clone <repo-url>
cd week7
docker compose up --build
```

Wait ~20 seconds for all health checks to pass. The client logs will appear showing alternating calls to both instances.

Open the Consul UI at **http://localhost:8500**

## Demo Walkthrough

### Step 1 — Normal load balancing

```bash
docker compose logs -f client
```

Expected output (alternating between instances):
```
2024/03/18 10:00:02 [1] Discovered 2 instance(s). Picked: service-1 (service-1:5001)
2024/03/18 10:00:02 [CALL -> service-1]  message='Hello from service-1!'  container_id=a3f2b1c4
2024/03/18 10:00:04 [2] Discovered 2 instance(s). Picked: service-2 (service-2:5002)
2024/03/18 10:00:04 [CALL -> service-2]  message='Hello from service-2!'  container_id=9b8c7d6e
```

The `container_id` is the Docker container ID — proof that responses came from different physical containers.

### Step 2 — Dynamic failover (kill one instance)

In a second terminal:
```bash
docker compose stop service-1
```

Watch the client logs. Within 2 seconds, all calls route only to `service-2`:
```
2024/03/18 10:00:10 [5] Discovered 1 instance(s). Picked: service-2 (service-2:5002)
```

### Step 3 — Consul UI

Open **http://localhost:8500/ui** in your browser.

- Click **Services** → `hello-service` — shows registered instances and health status
- After stopping `service-1`, it disappears from the list immediately (graceful deregistration via SIGTERM)

### Step 4 — Query Consul API directly

```bash
curl -s "http://localhost:8500/v1/health/service/hello-service?passing" | python3 -m json.tool
```

This shows the raw JSON the client reads — makes the discovery mechanism fully transparent.

### Step 5 — Restore and verify recovery

```bash
docker compose start service-1
```

Wait ~15 seconds for the health check to pass. The client will return to routing calls to both instances.

### Step 6 — Call services directly from host

```bash
curl http://localhost:5001/hello
curl http://localhost:5002/hello
```

## Project Structure

```
week7/
├── docker-compose.yml      # Orchestrates consul + 2 service instances + client
├── README.md
├── service/
│   ├── main.go             # Go HTTP server: /hello, /health, Consul registration
│   ├── go.mod
│   └── Dockerfile          # Multi-stage build (golang:1.22-alpine → alpine:3.19)
└── client/
    ├── main.go             # Go client: discover, pick random, call /hello
    ├── go.mod
    └── Dockerfile          # Multi-stage build
```

## Environment Variables

### Service
| Variable | Default | Description |
|---|---|---|
| `INSTANCE_NAME` | `service-unknown` | Unique instance ID registered with Consul |
| `SERVICE_PORT` | `5000` | Port the HTTP server listens on |
| `CONSUL_HOST` | `consul` | Consul hostname |
| `CONSUL_PORT` | `8500` | Consul HTTP port |

### Client
| Variable | Default | Description |
|---|---|---|
| `CONSUL_HOST` | `consul` | Consul hostname |
| `CONSUL_PORT` | `8500` | Consul HTTP port |
| `POLL_INTERVAL` | `2` | Seconds between discovery + call iterations |
