#!/usr/bin/env bash
set -euo pipefail

KIND_CLUSTER_NAME="${KIND_CLUSTER_NAME:-chart-testing}"
NAMESPACE="${NAMESPACE:-llm-system}"
RELEASE_NAME="${RELEASE_NAME:-llm-gateway}"
CHART_DIR="${CHART_DIR:-charts/llm-gateway}"

PROXY_IMAGE="${PROXY_IMAGE:-llm-proxy:ci}"
COLLECTOR_IMAGE="${COLLECTOR_IMAGE:-llm-collector:ci}"
MOCK_IMAGE="${MOCK_IMAGE:-mock-openai:ci}"

need() { command -v "$1" >/dev/null 2>&1 || { echo "Missing: $1" >&2; exit 1; }; }
need docker
need kind
need kubectl
need helm

echo "[local-run] cluster=${KIND_CLUSTER_NAME} ns=${NAMESPACE} release=${RELEASE_NAME}"

# 1) ensure kind cluster
if ! kind get clusters | grep -qx "${KIND_CLUSTER_NAME}"; then
  kind create cluster --name "${KIND_CLUSTER_NAME}"
fi
kind export kubeconfig --name "${KIND_CLUSTER_NAME}"

# 2) build images
docker build -t "${PROXY_IMAGE}" ./proxy
docker build -t "${COLLECTOR_IMAGE}" ./collector
docker build -t "${MOCK_IMAGE}" ./tests/contract/mock-openai

# 3) load into kind
kind load docker-image "${PROXY_IMAGE}" --name "${KIND_CLUSTER_NAME}"
kind load docker-image "${COLLECTOR_IMAGE}" --name "${KIND_CLUSTER_NAME}"
kind load docker-image "${MOCK_IMAGE}" --name "${KIND_CLUSTER_NAME}"

# 4) namespace + dummy secret
kubectl create namespace "${NAMESPACE}" --dry-run=client -o yaml | kubectl apply -f -
kubectl create secret generic ci-dummy-secret -n "${NAMESPACE}" \
  --from-literal=UPSTREAM_OPENAI_API_KEY=dummy-key \
  --dry-run=client -o yaml | kubectl apply -f -

# 5) deploy mock
kubectl apply -n "${NAMESPACE}" -f tests/contract/k8s/mock-openai.yaml
kubectl rollout status deployment/mock-openai -n "${NAMESPACE}" --timeout=180s

# 6) install/upgrade chart pointing upstream to mock
proxy_repo="${PROXY_IMAGE%:*}"; proxy_tag="${PROXY_IMAGE#*:}"
collector_repo="${COLLECTOR_IMAGE%:*}"; collector_tag="${COLLECTOR_IMAGE#*:}"

helm upgrade --install "${RELEASE_NAME}" "${CHART_DIR}" \
  -n "${NAMESPACE}" --create-namespace \
  -f "${CHART_DIR}/values-ci.yaml" \
  --set proxy.image.repository="${proxy_repo}" \
  --set proxy.image.tag="${proxy_tag}" \
  --set proxy.image.pullPolicy=IfNotPresent \
  --set collector.image.repository="${collector_repo}" \
  --set collector.image.tag="${collector_tag}" \
  --set collector.image.pullPolicy=IfNotPresent \
  --set proxy.existingSecretName=ci-dummy-secret \
  --set proxy.env.UPSTREAM_OPENAI_BASE_URL="http://mock-openai:8080"

kubectl rollout status deployment/llm-proxy -n "${NAMESPACE}" --timeout=180s
kubectl rollout status deployment/llm-collector -n "${NAMESPACE}" --timeout=180s

# 7) run the exact CI checks
chmod +x tests/contract/ci-contract.sh
NAMESPACE="${NAMESPACE}" ./tests/contract/ci-contract.sh
