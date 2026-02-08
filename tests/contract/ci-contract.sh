#!/usr/bin/env bash
set -euo pipefail

NAMESPACE="${NAMESPACE:-llm-system}"
PROXY_SVC="${PROXY_SVC:-llm-proxy}"
PROXY_PORT="${PROXY_PORT:-8080}"

need() { command -v "$1" >/dev/null 2>&1 || { echo "Missing: $1" >&2; exit 1; }; }
need kubectl

echo "[contract] namespace=${NAMESPACE} target=http://${PROXY_SVC}:${PROXY_PORT}"

# run inside cluster (no port-forward flake)
kubectl run -n "${NAMESPACE}" contract-curl --rm -i --restart=Never \
  --image=curlimages/curl:8.6.0 \
  -- sh -lc "
    set -e

    echo '--- normal ---'
    cat <<'JSON' | curl -sS http://${PROXY_SVC}:${PROXY_PORT}/v1/chat/completions \
      -H 'Content-Type: application/json' \
      -H 'Authorization: Bearer dummy' \
      -d @- | tee /tmp/normal.json >/dev/null
{
  \"model\": \"gpt-4o-mini\",
  \"messages\": [{\"role\":\"user\",\"content\":\"hi\"}]
}
JSON
    grep -q 'ok-from-mock' /tmp/normal.json
    grep -q '\"usage\"' /tmp/normal.json

    echo '--- vlm ---'
    cat <<'JSON' | curl -sS http://${PROXY_SVC}:${PROXY_PORT}/v1/chat/completions \
      -H 'Content-Type: application/json' \
      -H 'Authorization: Bearer dummy' \
      -d @- | tee /tmp/vlm.json >/dev/null
{
  \"model\": \"gpt-4o-mini\",
  \"messages\": [
    {
      \"role\": \"user\",
      \"content\": [
        {\"type\":\"text\",\"text\":\"what is in this image?\"},
        {\"type\":\"image_url\",\"image_url\":{\"url\":\"data:image/png;base64,iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO2Xc+UAAAAASUVORK5CYII=\"}}
      ]
    }
  ]
}
JSON
    grep -q 'ok-from-mock' /tmp/vlm.json
    grep -q '\"usage\"' /tmp/vlm.json

    echo '--- stream ---'
    cat <<'JSON' | curl -sS -N http://${PROXY_SVC}:${PROXY_PORT}/v1/chat/completions \
      -H 'Content-Type: application/json' \
      -H 'Authorization: Bearer dummy' \
      -d @- | tee /tmp/stream.txt >/dev/null
{
  \"model\": \"gpt-4o-mini\",
  \"stream\": true,
  \"messages\": [{\"role\":\"user\",\"content\":\"stream pls\"}]
}
JSON
    grep -q '\\[DONE\\]' /tmp/stream.txt

    echo 'OK: contract tests passed.'
  "
