#!/usr/bin/env bash
set -Eeuo pipefail

: "${KUBECONFIG:?set KUBECONFIG to the disposable Kind kubeconfig}"
[[ "$(kubectl config current-context)" == "kind-aura-power-quality" ]] || { echo "refusing mutation: this runner is restricted to kind-aura-power-quality" >&2; exit 2; }

run_id="$(date -u +%H%M%S)"
base_url="http://127.0.0.1:19092"
member_name="approval-member-${run_id}"
member_password="Quality approval ${run_id}!"
policy_name="approval-no-namespace-${run_id}"
admin_cookie="/tmp/aura-admin-cookie-${run_id}"
member_cookie="/tmp/aura-member-cookie-${run_id}"
user_id=""; pending_id=""; forward_pid=""

cleanup() {
  local original_status=$?
  trap - EXIT INT TERM HUP
  [[ -z "$pending_id" ]] || kubectl delete powerauditevent "approval-$pending_id" -n aura-system --ignore-not-found >/dev/null 2>&1 || true
  kubectl delete powerpolicy "$policy_name" -n aura-system --ignore-not-found >/dev/null 2>&1 || true
  [[ -z "$user_id" ]] || curl -fsS -b "$admin_cookie" -X DELETE "$base_url/api/v1/users/$user_id" >/dev/null 2>&1 || true
  if [[ -n "$forward_pid" ]]; then
    kill "$forward_pid" >/dev/null 2>&1 || true
    wait "$forward_pid" 2>/dev/null || true
  fi
  rm -f "$admin_cookie" "$member_cookie"
  exit "$original_status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM HUP

kubectl port-forward -n aura-system statefulset/aura-power-server 19092:8080 >/tmp/aura-power-approval-port-forward.log 2>&1 &
forward_pid=$!
for _ in $(seq 1 30); do
  if curl -fsS "$base_url/api/v1/health" >/dev/null 2>&1; then break; fi
  sleep 1
done
curl -fsS "$base_url/api/v1/health" >/dev/null

admin_password="$(kubectl get secret aura-power-server-secret -n aura-system -o jsonpath='{.data.admin-password}' | base64 -d)"
curl -fsS -c "$admin_cookie" -H 'Content-Type: application/json' -d "$(jq -nc --arg password "$admin_password" '{username:"admin",password:$password}')" "$base_url/api/v1/auth/login" >/dev/null
created_user="$(curl -fsS -b "$admin_cookie" -H 'Content-Type: application/json' -d "$(jq -nc --arg username "$member_name" --arg password "$member_password" '{username:$username,password:$password,role:"member"}')" "$base_url/api/v1/users")"
user_id="$(jq -er .id <<<"$created_user")"
curl -fsS -c "$member_cookie" -H 'Content-Type: application/json' -d "$(jq -nc --arg username "$member_name" --arg password "$member_password" '{username:$username,password:$password}')" "$base_url/api/v1/auth/login" >/dev/null

pending_payload="$(jq -nc --arg name "$policy_name" '{action:"create",resourceKind:"PowerPolicy",resourceName:$name,payload:{apiVersion:"power.aura.sh/v1alpha1",kind:"PowerPolicy",metadata:{name:$name},spec:{scope:{namespaces:["approval-fixture"]},schedule:{desiredState:"on",windows:[]},priority:1000}}}')"
created="$(curl -fsS -b "$member_cookie" -H 'Content-Type: application/json' -d "$pending_payload" "$base_url/api/v1/pending")"
pending_id="$(jq -er .id <<<"$created")"
approve_status="$(curl -sS -o /tmp/aura-approve-response.json -w '%{http_code}' -b "$admin_cookie" -X POST "$base_url/api/v1/pending/$pending_id/approve")"
[[ "$approve_status" == "200" ]] || { echo "FAIL: approval returned HTTP $approve_status" >&2; exit 10; }
[[ "$(kubectl get powerpolicy "$policy_name" -n aura-system -o jsonpath='{.metadata.namespace}')" == "aura-system" ]] || { echo "FAIL: approved payload did not default to the control namespace" >&2; exit 11; }
kubectl get powerauditevent "approval-$pending_id" -n aura-system >/dev/null
replay_status="$(curl -sS -o /dev/null -w '%{http_code}' -b "$admin_cookie" -X POST "$base_url/api/v1/pending/$pending_id/approve")"
[[ "$replay_status" == "409" ]] || { echo "FAIL: repeated approval returned HTTP $replay_status" >&2; exit 12; }
echo "kind_approval_journey=passed namespace_defaulted=true mutation_once=true audit=true replay_denied=true"
