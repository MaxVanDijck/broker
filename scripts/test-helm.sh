#!/bin/bash
set -euo pipefail

# Helm chart integration test. Used by both CI and local development.
# Prerequisites: docker, kind, kubectl, helm
#
# Usage:
#   ./scripts/test-helm.sh

CLUSTER_NAME="broker-test"
IMAGE_NAME="broker:test"

cleanup() {
  echo "=== cleaning up ==="
  kind delete cluster --name "$CLUSTER_NAME" 2>/dev/null || true
}
trap cleanup EXIT

echo "=== building docker image ==="
docker build -t "$IMAGE_NAME" .

echo "=== creating kind cluster ==="
kind delete cluster --name "$CLUSTER_NAME" 2>/dev/null || true
kind create cluster --name "$CLUSTER_NAME" --wait 60s

echo "=== loading image into kind ==="
kind load docker-image "$IMAGE_NAME" --name "$CLUSTER_NAME"

echo "=== linting helm chart ==="
helm lint charts/broker/ --strict

echo "=== installing helm chart ==="
helm install broker charts/broker/ \
  --set image.repository=broker \
  --set image.tag=test \
  --set image.pullPolicy=Never \
  --set persistence.enabled=false \
  --wait \
  --timeout 120s

echo "=== verifying pod ==="
kubectl get pods -l app.kubernetes.io/name=broker
kubectl wait --for=condition=ready pod -l app.kubernetes.io/name=broker --timeout=60s

echo "=== port-forwarding ==="
kubectl port-forward svc/broker 18080:8080 >/dev/null 2>&1 &
PF_PID=$!
sleep 3

echo "=== testing healthz ==="
curl -sf http://localhost:18080/healthz
echo " OK"

echo "=== testing API ==="
curl -sf -X POST http://localhost:18080/broker.v1.BrokerService/Status \
  -H 'Content-Type: application/json' -d '{}'
echo " OK"

echo "=== testing dashboard ==="
curl -sf http://localhost:18080/ | head -1
echo " OK"

echo "=== testing clusters API ==="
curl -sf http://localhost:18080/api/v1/clusters
echo " OK"

{ kill $PF_PID && wait $PF_PID; } 2>/dev/null || true

echo "=== running helm test ==="
helm test broker --timeout 60s

echo ""
echo "=== all tests passed ==="
