#!/usr/bin/env bash
# Aura Power — API Smoke Tests
# Usage: ./scripts/smoke-test.sh [BASE_URL] [ADMIN_USER] [ADMIN_PASS]
#
# Runs 35 API endpoint tests against a live Aura Power instance.
# All test resources use the "smoke-test-" prefix and are cleaned up.

set -euo pipefail

BASE_URL="${1:-https://power.int.weaura.tech}"
ADMIN_USER="${2:-admin}"
ADMIN_PASS="${3:-admin123}"

PASS=0
FAIL=0
ERRORS=""

# Colors
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

log_pass() { PASS=$((PASS + 1)); echo -e "  ${GREEN}✓${NC} $1"; }
log_fail() { FAIL=$((FAIL + 1)); ERRORS="${ERRORS}\n  ✗ $1: $2"; echo -e "  ${RED}✗${NC} $1: $2"; }

# Helper: HTTP request with cookie jar
COOKIE_JAR=$(mktemp)
trap "rm -f $COOKIE_JAR" EXIT

request() {
  local method="$1" path="$2" body="${3:-}"
  local args=(-s -w "\n%{http_code}" -b "$COOKIE_JAR" -c "$COOKIE_JAR")
  args+=(-H "Content-Type: application/json")
  if [ -n "$body" ]; then
    args+=(-d "$body")
  fi
  curl "${args[@]}" -X "$method" "${BASE_URL}${path}"
}

get_status() { echo "$1" | tail -1; }
get_body() { echo "$1" | sed '$d'; }

echo ""
echo -e "${YELLOW}═══════════════════════════════════════════════════${NC}"
echo -e "${YELLOW} Aura Power Smoke Tests${NC}"
echo -e "${YELLOW} Target: ${BASE_URL}${NC}"
echo -e "${YELLOW}═══════════════════════════════════════════════════${NC}"
echo ""

# ─── A. Health ────────────────────────────────────────────────────────────────
echo -e "${YELLOW}▸ Health Endpoints${NC}"

resp=$(request GET /healthz)
status=$(get_status "$resp")
if [ "$status" = "200" ]; then log_pass "A1 GET /healthz → 200"; else log_fail "A1 GET /healthz" "got $status"; fi

resp=$(request GET /readyz)
status=$(get_status "$resp")
if [ "$status" = "200" ]; then log_pass "A2 GET /readyz → 200"; else log_fail "A2 GET /readyz" "got $status"; fi

resp=$(request GET /metrics)
status=$(get_status "$resp")
body=$(get_body "$resp")
if [ "$status" = "200" ] && echo "$body" | grep -q "aura_power"; then
  log_pass "A3 GET /metrics → Prometheus format"
else
  log_fail "A3 GET /metrics" "status=$status or missing aura_power prefix"
fi

# ─── B. Auth ──────────────────────────────────────────────────────────────────
echo ""
echo -e "${YELLOW}▸ Authentication${NC}"

resp=$(request POST /api/v1/auth/login "{\"username\":\"$ADMIN_USER\",\"password\":\"$ADMIN_PASS\"}")
status=$(get_status "$resp")
if [ "$status" = "200" ]; then log_pass "A4 POST /auth/login (valid) → 200"; else log_fail "A4 POST /auth/login" "got $status"; fi

resp=$(request POST /api/v1/auth/login '{"username":"fake","password":"wrong"}')
status=$(get_status "$resp")
if [ "$status" = "401" ]; then log_pass "A5 POST /auth/login (invalid) → 401"; else log_fail "A5 POST /auth/login invalid" "got $status"; fi

# Re-login to ensure cookie is set
request POST /api/v1/auth/login "{\"username\":\"$ADMIN_USER\",\"password\":\"$ADMIN_PASS\"}" > /dev/null

resp=$(request GET /api/v1/auth/me)
status=$(get_status "$resp")
body=$(get_body "$resp")
if [ "$status" = "200" ] && echo "$body" | grep -q "username"; then
  log_pass "A6 GET /auth/me (with cookie) → 200"
else
  log_fail "A6 GET /auth/me" "status=$status"
fi

# A7: Test without cookie (use a temp separate request)
resp=$(curl -s -w "\n%{http_code}" -H "Content-Type: application/json" "${BASE_URL}/api/v1/auth/me")
status=$(get_status "$resp")
if [ "$status" = "401" ]; then log_pass "A7 GET /auth/me (no cookie) → 401"; else log_fail "A7 GET /auth/me no-auth" "got $status"; fi

resp=$(request POST /api/v1/auth/refresh)
status=$(get_status "$resp")
if [ "$status" = "200" ] || [ "$status" = "400" ]; then
  log_pass "A8 POST /auth/refresh → $status"
else
  log_fail "A8 POST /auth/refresh" "got $status"
fi

# ─── C. Core API (authenticated) ─────────────────────────────────────────────
echo ""
echo -e "${YELLOW}▸ Core API${NC}"

resp=$(request GET /api/v1/dashboard)
status=$(get_status "$resp")
body=$(get_body "$resp")
if [ "$status" = "200" ] && echo "$body" | grep -q "efficiency"; then
  log_pass "A10 GET /dashboard → 200 + efficiency"
else
  log_fail "A10 GET /dashboard" "status=$status"
fi

resp=$(request GET /api/v1/status)
status=$(get_status "$resp")
body=$(get_body "$resp")
if [ "$status" = "200" ] && echo "$body" | grep -q "totalTargets"; then
  log_pass "A11 GET /status → 200"
else
  log_fail "A11 GET /status" "status=$status"
fi

resp=$(request GET /api/v1/targets)
status=$(get_status "$resp")
body=$(get_body "$resp")
if [ "$status" = "200" ] && echo "$body" | grep -q "targets"; then
  log_pass "A12 GET /targets → 200"
else
  log_fail "A12 GET /targets" "status=$status"
fi

resp=$(request GET "/api/v1/targets?namespace=argocd")
status=$(get_status "$resp")
if [ "$status" = "200" ]; then log_pass "A13 GET /targets?namespace=argocd → 200"; else log_fail "A13 GET /targets filtered" "got $status"; fi

resp=$(request GET /api/v1/discover)
status=$(get_status "$resp")
if [ "$status" = "200" ]; then log_pass "A35 GET /discover → 200"; else log_fail "A35 GET /discover" "got $status"; fi

resp=$(request GET /api/v1/policies)
status=$(get_status "$resp")
if [ "$status" = "200" ]; then log_pass "A15 GET /policies → 200"; else log_fail "A15 GET /policies" "got $status"; fi

resp=$(request GET /api/v1/overrides)
status=$(get_status "$resp")
if [ "$status" = "200" ]; then log_pass "A18 GET /overrides → 200"; else log_fail "A18 GET /overrides" "got $status"; fi

resp=$(request GET /api/v1/savings)
status=$(get_status "$resp")
if [ "$status" = "200" ]; then log_pass "A21 GET /savings → 200"; else log_fail "A21 GET /savings" "got $status"; fi

resp=$(request GET /api/v1/savings/breakdown)
status=$(get_status "$resp")
if [ "$status" = "200" ]; then log_pass "A22 GET /savings/breakdown → 200"; else log_fail "A22 GET /savings/breakdown" "got $status"; fi

resp=$(request GET /api/v1/audit)
status=$(get_status "$resp")
if [ "$status" = "200" ]; then log_pass "A23 GET /audit → 200"; else log_fail "A23 GET /audit" "got $status"; fi

resp=$(request GET "/api/v1/audit?limit=5")
status=$(get_status "$resp")
body=$(get_body "$resp")
if [ "$status" = "200" ]; then log_pass "A24 GET /audit?limit=5 → 200"; else log_fail "A24 GET /audit limited" "got $status"; fi

resp=$(request GET /api/v1/namespaces)
status=$(get_status "$resp")
if [ "$status" = "200" ]; then log_pass "A25 GET /namespaces → 200"; else log_fail "A25 GET /namespaces" "got $status"; fi

resp=$(request GET /api/v1/namespace-groups)
status=$(get_status "$resp")
if [ "$status" = "200" ]; then log_pass "A26 GET /namespace-groups → 200"; else log_fail "A26 GET /namespace-groups" "got $status"; fi

resp=$(request GET /api/v1/users)
status=$(get_status "$resp")
if [ "$status" = "200" ]; then log_pass "A29 GET /users → 200"; else log_fail "A29 GET /users" "got $status"; fi

# ─── D. Metrics Provider ─────────────────────────────────────────────────────
echo ""
echo -e "${YELLOW}▸ Metrics Provider${NC}"

resp=$(request GET "/api/v1/metrics/cluster?range=1h")
status=$(get_status "$resp")
if [ "$status" = "200" ] || [ "$status" = "503" ]; then
  log_pass "A33 GET /metrics/cluster → $status (provider may be unavailable)"
else
  log_fail "A33 GET /metrics/cluster" "got $status"
fi

resp=$(request GET /api/v1/metrics/cost)
status=$(get_status "$resp")
if [ "$status" = "200" ] || [ "$status" = "503" ]; then
  log_pass "A34 GET /metrics/cost → $status"
else
  log_fail "A34 GET /metrics/cost" "got $status"
fi

# ─── E. CRUD Tests (with cleanup) ────────────────────────────────────────────
echo ""
echo -e "${YELLOW}▸ CRUD Operations (smoke-test- prefix)${NC}"

# Create policy
POLICY_BODY='{"metadata":{"name":"smoke-test-policy","namespace":"aura-system"},"spec":{"scope":{"namespaces":["default"]},"schedule":{"desiredState":"off","windows":[{"start":"23:00","end":"23:30","timezone":"UTC"}]},"priority":1,"description":"Smoke test - auto cleanup"}}'
resp=$(request POST /api/v1/policies "$POLICY_BODY")
status=$(get_status "$resp")
if [ "$status" = "200" ] || [ "$status" = "201" ]; then
  log_pass "A16 POST /policies (create) → $status"
else
  log_fail "A16 POST /policies create" "got $status: $(get_body "$resp")"
fi

# Verify policy exists
resp=$(request GET /api/v1/policies)
body=$(get_body "$resp")
if echo "$body" | grep -q "smoke-test-policy"; then
  log_pass "A16b Verify policy exists in list"
else
  log_fail "A16b Verify policy" "not found in list"
fi

# Delete policy
resp=$(request DELETE /api/v1/policies/aura-system/smoke-test-policy)
status=$(get_status "$resp")
if [ "$status" = "200" ]; then log_pass "A17 DELETE /policies → 200"; else log_fail "A17 DELETE /policies" "got $status"; fi

# Create override
EXPIRES=$(date -u -v+2H +"%Y-%m-%dT%H:%M:%SZ" 2>/dev/null || date -u -d "+2 hours" +"%Y-%m-%dT%H:%M:%SZ")
OVERRIDE_BODY="{\"metadata\":{\"name\":\"smoke-test-override\",\"namespace\":\"aura-system\"},\"spec\":{\"scope\":{\"namespaces\":[\"default\"]},\"state\":\"on\",\"priority\":999,\"expiresAt\":\"$EXPIRES\",\"reason\":\"Smoke test - auto cleanup\"}}"
resp=$(request POST /api/v1/overrides "$OVERRIDE_BODY")
status=$(get_status "$resp")
if [ "$status" = "200" ] || [ "$status" = "201" ]; then
  log_pass "A19 POST /overrides (create) → $status"
else
  log_fail "A19 POST /overrides create" "got $status: $(get_body "$resp")"
fi

# Delete override
resp=$(request DELETE /api/v1/overrides/aura-system/smoke-test-override)
status=$(get_status "$resp")
if [ "$status" = "200" ]; then log_pass "A20 DELETE /overrides → 200"; else log_fail "A20 DELETE /overrides" "got $status"; fi

# Create namespace group
NG_BODY='{"metadata":{"name":"smoke-test-group","namespace":"aura-system"},"spec":{"namespaces":["default","kube-system"]}}'
resp=$(request POST /api/v1/namespace-groups "$NG_BODY")
status=$(get_status "$resp")
if [ "$status" = "200" ] || [ "$status" = "201" ]; then
  log_pass "A27 POST /namespace-groups (create) → $status"
else
  log_fail "A27 POST /namespace-groups" "got $status"
fi

# Delete namespace group
resp=$(request DELETE /api/v1/namespace-groups/aura-system/smoke-test-group)
status=$(get_status "$resp")
if [ "$status" = "200" ]; then log_pass "A28 DELETE /namespace-groups → 200"; else log_fail "A28 DELETE /namespace-groups" "got $status"; fi

# Create user
USER_BODY='{"username":"smoke-test-user","password":"TestPass123!","role":"member"}'
resp=$(request POST /api/v1/users "$USER_BODY")
status=$(get_status "$resp")
body=$(get_body "$resp")
if [ "$status" = "200" ] || [ "$status" = "201" ]; then
  log_pass "A30 POST /users (create) → $status"
  USER_ID=$(echo "$body" | grep -o '"id":"[^"]*"' | head -1 | cut -d'"' -f4)
else
  log_fail "A30 POST /users" "got $status: $body"
  USER_ID=""
fi

# Delete user
if [ -n "$USER_ID" ]; then
  resp=$(request DELETE "/api/v1/users/$USER_ID")
  status=$(get_status "$resp")
  if [ "$status" = "200" ]; then log_pass "A31 DELETE /users → 200"; else log_fail "A31 DELETE /users" "got $status"; fi
fi

# Preview policy
PREVIEW_BODY='{"scope":{"namespaces":["default"]},"schedule":{"desiredState":"off","windows":[{"start":"00:00","end":"06:00","timezone":"UTC"}]},"priority":1}'
resp=$(request POST /api/v1/preview/policy "$PREVIEW_BODY")
status=$(get_status "$resp")
if [ "$status" = "200" ]; then log_pass "A32 POST /preview/policy → 200"; else log_fail "A32 POST /preview/policy" "got $status"; fi

# Logout
resp=$(request POST /api/v1/auth/logout)
status=$(get_status "$resp")
if [ "$status" = "200" ]; then log_pass "A9 POST /auth/logout → 200"; else log_fail "A9 POST /auth/logout" "got $status"; fi

# ─── Summary ─────────────────────────────────────────────────────────────────
echo ""
echo -e "${YELLOW}═══════════════════════════════════════════════════${NC}"
TOTAL=$((PASS + FAIL))
echo -e " Results: ${GREEN}${PASS} passed${NC}, ${RED}${FAIL} failed${NC}, ${TOTAL} total"
if [ "$FAIL" -gt 0 ]; then
  echo -e "${RED}Failures:${NC}${ERRORS}"
  echo -e "${YELLOW}═══════════════════════════════════════════════════${NC}"
  exit 1
else
  echo -e "${YELLOW}═══════════════════════════════════════════════════${NC}"
  echo -e " ${GREEN}All smoke tests passed.${NC}"
  exit 0
fi
