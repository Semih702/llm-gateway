# LLM-Gateway (MVP)

LLM-Gateway is a Kubernetes-friendly, OpenAI-compatible proxy that transparently forwards requests to OpenAI and asynchronously collects token usage, latency, and request metadata.

This repository contains a minimal but production-shaped MVP:

* Proxy: forwards OpenAI requests and emits metering events (fail-open)
* Collector: receives metering events and logs them (stdout / NDJSON)
* Kubernetes manifests: deploy everything in a dedicated namespace

---

## Architecture (MVP)

Application
→ llm-proxy (OpenAI-compatible)
→ OpenAI API

llm-proxy also emits async metering events
→ llm-collector

Key points:

* Application only changes baseURL to point to llm-proxy
* OpenAI API key is stored only in the proxy
* Metering is asynchronous and fail-open
* Application latency impact is minimal
* No SDK changes required

The proxy acts as a drop-in replacement for the OpenAI API endpoint, while the collector runs fully out-of-band and never affects request latency.

Metering events are emitted after the upstream response completes and are delivered asynchronously on a best-effort basis.

---

## Repository structure

LLM-GATEWAY/
proxy/
Dockerfile
go.mod
main.go

collector/
Dockerfile
go.mod
main.go

deploy/k8s/
00-namespace.yaml (Namespace definition)
10-openai-secret.yaml (Example secret, do not commit real keys)
20-collector.yaml (Collector Deployment + Service)
30-proxy.yaml (Proxy Deployment + Service)

LICENSE
README.md

---

## Features (MVP)

* OpenAI-compatible /v1/chat/completions
* Transparent request forwarding
* Streaming (SSE) pass-through
* Token usage extraction from OpenAI usage field
* Per-request latency measurement
* Tenant attribution via headers (X-LLM-Tenant, fallback: X-Tenant, default: default)
* Async metering pipeline (non-blocking)
* Kubernetes-ready
* Adds X-LLM-Request-ID response header for request tracing

### Latency semantics

latency_ms represents the end-to-end request duration, measured from the moment the proxy receives the request until the upstream response completes.

For streaming requests, this corresponds to the total stream duration, not time-to-first-byte (TTFB).

---

## Limitations (MVP)

* Only /v1/chat/completions endpoint is implemented
* Events are logged (no database persistence yet)
* No tokenizer-based estimation if usage is missing

These are conscious trade-offs to keep the MVP minimal, auditable, and easy to operate.

---

## Build Docker images (local)

docker build -t llm-proxy:latest ./proxy
docker build -t llm-collector:latest ./collector

If using kind or minikube:

kind load docker-image llm-proxy:latest
kind load docker-image llm-collector:latest

---

## Kubernetes deployment (Quickstart)

### 1) Create namespace

kubectl apply -f deploy/k8s/00-namespace.yaml

---

### 2) Create OpenAI API secret (recommended)

kubectl -n llm-system create secret generic openai-credentials 
--from-literal=UPSTREAM_OPENAI_API_KEY="sk-REPLACE_ME" 
--dry-run=client -o yaml | kubectl apply -f -

The file deploy/k8s/10-openai-secret.yaml is provided only as an example and should not be committed with real keys.

---

### 3) Deploy collector

kubectl apply -f deploy/k8s/20-collector.yaml
kubectl -n llm-system get pods

---

### 4) Deploy proxy

kubectl apply -f deploy/k8s/30-proxy.yaml
kubectl -n llm-system get pods

---

## Node-local routing and deployment modes

By default, the proxy is deployed as a Deployment for simplicity.

The provided manifest includes:

* topologySpreadConstraints to distribute proxy pods evenly across nodes
* optional internalTrafficPolicy: Local on the Service (if supported by the cluster)

This allows applications to prefer same-node proxy instances, reducing network hops and latency.

For advanced setups requiring strict node-local guarantees, the proxy can alternatively be deployed as a DaemonSet (one proxy per node, optionally using hostPort). This mode is intentionally not enabled by default to keep the MVP simple.

---

## Testing

kubectl -n llm-system port-forward svc/llm-proxy 8080:8080

curl [http://localhost:8080/v1/chat/completions](http://localhost:8080/v1/chat/completions) 
-H "Authorization: Bearer gw_live_demo_key" 
-H "X-LLM-Tenant: demo" 
-H "Content-Type: application/json" 
-d '{"model":"gpt-4o-mini","messages":[{"role":"user","content":"Hello from LLM Gateway"}]}'

kubectl -n llm-system logs deploy/llm-collector -f

---

## Local testing without Kubernetes (Docker)

docker network create llm-gateway

docker run --rm -it --name llm-collector --network llm-gateway -p 8081:8081 llm-collector:latest

docker run --rm -it --name llm-proxy --network llm-gateway -p 8080:8080 
-e UPSTREAM_OPENAI_API_KEY="sk-REPLACE_ME" 
-e COLLECTOR_URL="[http://llm-collector:8081/events](http://llm-collector:8081/events)" 
llm-proxy:latest

---

## Configuration reference

Proxy environment variables:

LISTEN_ADDR – Proxy listen address (default :8080)
UPSTREAM_OPENAI_BASE_URL – OpenAI base URL (default [https://api.openai.com](https://api.openai.com))
UPSTREAM_OPENAI_API_KEY – OpenAI API key (required)
COLLECTOR_URL – Collector endpoint (async, best-effort)
EVENT_QUEUE_SIZE – In-memory async event buffer (default 10000)
EVENT_FLUSH_TIMEOUT – Stats ticker interval (default 2s)
HTTP_CLIENT_TIMEOUT – Upstream HTTP timeout (default 120s)
METERING_CAPTURE_BYTES – Capture first N bytes of upstream response (default 256KB)

Collector environment variables:

PORT – Collector listen port (default 8081)
EVENT_LOG_PATH – Optional NDJSON output file path (default stdout)

---

## Security notes

* OpenAI API key is never exposed to application pods
* Gateway keys are not validated in MVP
* No request payloads are persisted
* Only usage metadata is collected

Collector delivery uses a short HTTP timeout and never blocks the request path; failures are logged and events may be dropped.

---

## Design decisions (MVP)

* Fail-open by default: metering must never affect user traffic
* No request payload persistence to minimize security surface
* Post-response usage extraction using OpenAI usage field
* Best-effort parsing using limited response capture

---

## Planned next steps

### Large payload & multimodal validation
- Validate with large payloads (e.g., VLM / multimodal requests)
- Test large request bodies and large responses
- Verify memory usage, timeouts, and capture limits (`METERING_CAPTURE_BYTES`)

### Streaming (SSE) support & validation
- Streaming (SSE) validation & correctness tests
- Confirm end-to-end pass-through works (curl / Postman)
- Verify metering semantics for streaming:
  - total stream duration
  - availability of usage data (when provided by upstream)

### In-cluster integration
- Integrate with existing workloads running in the same Kubernetes cluster
- Document how applications can point to the proxy using a Kubernetes Service DNS name
- Provide minimal examples for common stacks:
  - Node.js (axios / fetch)
  - Python
  - Java
  - Go

### Multi-provider support (beyond OpenAI)
- Extend the gateway to support multiple LLM providers (not only OpenAI)
  - Provider selection via configuration and/or request routing rules (e.g., by header, tenant, or model prefix)
  - Pluggable upstream clients (OpenAI / Azure OpenAI / Anthropic / Google / local models) behind a common interface
- Unify metering across providers
  - Normalize token usage, latency, and error semantics into a single event schema
  - Handle provider-specific differences (streaming formats, usage availability, rate-limit headers)
- Improve compatibility layer
  - Support additional OpenAI-compatible endpoints where applicable
  - Provider-specific adapters when “OpenAI-compatible” is not available


### Others

* Batch and compression for metering events
* Persistent storage (ClickHouse or Postgres)
* Tokenizer-based estimation when usage is missing
* Kubernetes Operator and Mutating Webhook
* Per-tenant quotas and rate limits
* Grafana dashboards

---

## License

MIT
