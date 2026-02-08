# Contract tests

These tests validate the LLM Gateway's public OpenAI-compatible contract in a deterministic way.

## Scope
- Chat completions (non-stream)
- VLM-style payloads (messages content with `image_url`)
- Streaming passthrough (SSE) with `[DONE]`

## Design
- A lightweight in-cluster `mock-openai` acts as upstream.
- The gateway is deployed to a kind cluster via the Helm chart.
- Tests send HTTP requests to the gateway service and assert the response shape and streaming behavior (not model semantics).

## Files
- `mock-openai/`: mock upstream server implementation + Dockerfile
- `k8s/`: Kubernetes manifests for the mock upstream
- `fixtures/`: request payload fixtures

## Adding a new test
1. Add a new request fixture under `fixtures/`.
2. Extend `mock-openai/main.go` if a new upstream behavior is needed.
3. Extend the contract test runner (ci-contract.sh) with new assertions.

## CI usage

In CI, the helm-regression workflow:
- Creates a kind cluster
- Builds and loads images (llm-proxy, llm-collector, mock-openai)
- Deploys mock-openai
- Installs the Helm chart with UPSTREAM_OPENAI_BASE_URL pointing to mock-openai
- Runs tests/contract/ci-contract.sh

The CI runner assumes:
- A reachable Kubernetes cluster
- llm-gateway already installed
- mock-openai running
- Proxy service reachable inside the cluster

It does not perform any setup.

## Local usage

Make scripts executable:
chmod +x tests/contract/local-run.sh
chmod +x tests/contract/ci-contract.sh

Run:
./tests/contract/local-run.sh

This mirrors the CI workflow:
- Creates or reuses a kind cluster
- Builds and loads images
- Deploys mock-openai
- Installs the chart pointing to the mock upstream
- Runs the same CI contract tests
