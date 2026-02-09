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

### Pull images (from registry)

Replace `<REGISTRY>` with your container registry (e.g. `ghcr.io/your-org`, `docker.io/yourname`).

```bash
docker pull <REGISTRY>/llm-proxy:latest
docker pull <REGISTRY>/llm-collector:latest
```

### 2) Create OpenAI API secret (recommended)

This secret is mounted into the proxy pod and used to authenticate upstream OpenAI requests.

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

```bash
curl [http://localhost:8080/v1/chat/completions](http://localhost:8080/v1/chat/completions) 
-H "Authorization: Bearer gw_live_demo_key" 
-H "X-LLM-Tenant: demo" 
-H "Content-Type: application/json" 
-d '{"model":"gpt-4o-mini","messages":[{"role":"user","content":"Hello from LLM Gateway"}]}'
```

```bash
kubectl -n llm-system logs deploy/llm-collector -f
```

Note: The `Authorization` header value is not validated in the MVP and is used only for client compatibility.

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

## CI Regression & Chart Testing (for Contributors)

This repository includes a mandatory CI regression pipeline to ensure Helm chart correctness,
Kubernetes compatibility, and safe upgrade behavior.

All pull requests that modify Helm charts or CI workflows must pass this pipeline before being merged.

---

### What is validated in CI?

The helm-regression workflow performs the following checks, in order:

1. Helm chart linting
   - Runs `helm lint` on the chart
   - Catches common issues:
     - Invalid chart metadata
     - Obvious template mistakes
     - Missing required values

2. Template rendering (dry-run)
   - Renders manifests using:
     - `values-ci.yaml` (required)
     - `values-test.yaml` (optional, if present)
   - Ensures templates render successfully without a live cluster

3. Kubernetes schema validation (kubeconform)
   - Validates rendered YAML against official Kubernetes schemas
   - Uses strict mode
   - Catches:
     - Invalid API versions
     - Invalid fields
     - Structural mismatches that Helm itself does not detect

4. Helm unit tests
   - Runs `helm unittest` against the chart
   - Validates:
     - Expected resources are created
     - Correct values are applied
     - Conditional logic behaves as intended

5. In-cluster install & upgrade test (kind)
   - Spins up a real Kubernetes cluster using kind
   - Builds proxy and collector images locally
   - Loads images into the cluster
   - Installs the chart using `helm upgrade --install`
   - Verifies:
     - Pods start successfully
     - Deployments become ready
   - Performs a real Helm upgrade:
     - Uses `values-test.yaml` if present
     - Ensures upgrades do not break running workloads

6. Smoke checks
   - Confirms:
     - Pods are running
     - Services are created
     - Deployments reach Available condition

7. OpenAI-compatible contract tests
   - Deploys a lightweight in-cluster mock OpenAI upstream
   - Configures the gateway to point to the mock via `UPSTREAM_OPENAI_BASE_URL`
   - Sends real HTTP requests to the gateway service
   - Validates the public OpenAI-compatible contract, including:
     - Chat completions (non-streaming)
     - VLM-style payloads (`image_url` content)
     - Streaming (SSE) passthrough with `[DONE]`
     - Presence of `usage` fields in responses

   These tests intentionally validate the API contract and behavior,
   not model semantics or response quality.

   They ensure that changes to the proxy, chart, or deployment logic
   do not silently break OpenAI compatibility.

---

### Files contributors should be aware of

- charts/llm-gateway/values-ci.yaml
  Required for CI. Used for deterministic, non-secret test installs.

- charts/llm-gateway/values-test.yaml (optional)
  Used to simulate upgrades. Useful for testing config changes, feature flags, or resource changes.

- .github/workflows/helm-regression.yml
  CI definition. Any change here is also gated by this workflow.

- tests/contract/
  Contains OpenAI-compatible contract tests executed in CI and runnable locally.
  See tests/contract/README.md for details.

---

### Running key checks locally (recommended)

helm lint charts/llm-gateway

helm template llm-gateway charts/llm-gateway -f charts/llm-gateway/values-ci.yaml

helm unittest charts/llm-gateway

For full parity with CI (optional but ideal):
- Use a local kind cluster
- Build images locally
- Install the chart with helm upgrade --install

---

### CI expectations

- CI failures must be fixed, not bypassed
- Do not disable schema validation or tests to make CI green
- Changes that affect chart behavior should include:
  - Updated unit tests
  - Or updates to values-test.yaml to cover upgrade scenarios

This pipeline exists to ensure the Helm chart remains:
- Safe to install
- Safe to upgrade
- Kubernetes-version compatible
- Predictable across environments

---

## Planned next steps

### Regression & compatibility test suite
- Introduce a minimal regression test suite to validate the gateway’s public contract ✅
- Focus on high-risk areas:
  - OpenAI-compatible request/response schemas ✅
  - Streaming (SSE) pass-through behavior ✅
  - Error and status code mapping
  - Metering event emission (usage present vs missing)
- Use a mock upstream and lightweight in-cluster setup (e.g., kind or docker-compose) ✅
- Keep the suite fast and deterministic for CI usage ✅

### Go code quality & unit tests (CI gate)
- Add Go unit tests for core logic (e.g., request/response mapping, metering extraction, SSE framing helpers)
- Add CI checks for Go code quality:
  - `go test ./...` (with `-race` where feasible)
  - `golangci-lint` (or at least `go vet`)
  - formatting checks (`gofmt`) and module tidiness (`go mod tidy` / `go mod verify`) ✅
- Keep these checks fast to run on every PR, and required before release ✅

### Streaming (SSE) – final validation (edge cases)
- Client disconnect propagation (client → proxy → upstream)
- Metering correctness for streaming edge cases:
  - stream aborted early
  - usage present vs missing
  - duration measurement accuracy

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

### Observability & metering enhancements
- Extend metering event schema with capture-related fields:
  - captured request / response byte counts
  - capture truncation indicators (`METERING_CAPTURE_BYTES`)
- Optional lightweight hashing of captured payloads (without storing full bodies)
- Improve correlation between streaming lifecycle events and final metering records

### Others
- Batch and compression for metering events
- Persistent storage (ClickHouse or Postgres)
- Tokenizer-based estimation when usage is missing
- Kubernetes Operator and Mutating Webhook
- Per-tenant quotas and rate limits
- Grafana dashboards
- Stable image tag naming and versioning strategy
  - Semantic versioning (`vX.Y.Z`)
  - Immutable tags (no `latest` in production)
  - Optional digest-based pinning

---

## License

MIT
