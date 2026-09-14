#!/usr/bin/env bash
# Shared process identity and hard-timeout helpers for EKS recovery scripts.
# This file is sourced; callers retain their own strict-mode policy.

process_identity() {
  local pid="$1" boot_id stat_line stat_tail start_time boot_time launch_time
  [[ "$pid" =~ ^[0-9]+$ ]] || return 1
  if [[ -r "/proc/${pid}/stat" && -r /proc/sys/kernel/random/boot_id ]]; then
    boot_id="$(cat /proc/sys/kernel/random/boot_id 2>/dev/null)" || return 1
    stat_line="$(cat "/proc/${pid}/stat" 2>/dev/null)" || return 1
    stat_tail="${stat_line##*) }"
    start_time="$(awk '{print $20}' <<<"$stat_tail")"
    [[ "$start_time" =~ ^[0-9]+$ ]] || return 1
    printf 'linux:%s:%s' "$boot_id" "$start_time"
    return 0
  fi
  boot_time="$(sysctl -n kern.boottime 2>/dev/null)" || return 1
  launch_time="$(ps -p "$pid" -o lstart= 2>/dev/null | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')"
  [[ -n "$launch_time" ]] || return 1
  printf 'darwin:%s:%s' "$boot_time" "$launch_time"
}

same_process_identity() {
  local pid="$1" expected="$2" actual
  actual="$(process_identity "$pid" 2>/dev/null)" || return 1
  [[ "$actual" == "$expected" ]]
}

process_is_running_identity() {
  local pid="$1" expected="$2" state stat_line stat_tail
  same_process_identity "$pid" "$expected" || return 1
  if [[ -r "/proc/${pid}/stat" ]]; then
    stat_line="$(cat "/proc/${pid}/stat" 2>/dev/null)" || return 1
    stat_tail="${stat_line##*) }"
    state="${stat_tail%% *}"
  else
    state="$(ps -p "$pid" -o state= 2>/dev/null | sed 's/^[[:space:]]*//')"
  fi
  [[ -n "$state" && "$state" != Z* ]]
}

signal_process_identity() {
  local pid="$1" expected="$2" signal="${3:-TERM}"
  same_process_identity "$pid" "$expected" || return 0
  kill -s "$signal" "$pid" 2>/dev/null || true
}

terminate_process_tree() {
  local pid="$1" expected_identity="$2" signal="${3:-TERM}" child child_identity
  same_process_identity "$pid" "$expected_identity" || return 0
  while IFS= read -r child; do
    [[ "$child" =~ ^[0-9]+$ ]] || continue
    child_identity="$(process_identity "$child" 2>/dev/null || true)"
    [[ -z "$child_identity" ]] || terminate_process_tree "$child" "$child_identity" "$signal"
  done < <(ps -eo pid=,ppid= 2>/dev/null | awk -v parent="$pid" '$2 == parent {print $1}' || true)
  same_process_identity "$pid" "$expected_identity" || return 0
  signal_process_identity "$pid" "$expected_identity" "$signal"
}

run_with_process_timeout() {
  local timeout_seconds="$1"
  shift
  local command_pid command_identity timer_pid timer_identity status=0 timeout_state timed_out=false
  [[ "$timeout_seconds" =~ ^[1-9][0-9]*$ ]] || { echo "invalid process timeout: ${timeout_seconds}" >&2; return 2; }
  # Explicit stdin redirection keeps piped request bodies available to an
  # asynchronous command; otherwise non-interactive shells may attach /dev/null.
  "$@" <&0 &
  command_pid=$!
  if ! command_identity="$(process_identity "$command_pid")"; then
    wait "$command_pid" || status=$?
    return "$status"
  fi
  timeout_state="$(mktemp -d)"
  (
    sleep "$timeout_seconds"
    mkdir "${timeout_state}/expired" 2>/dev/null || true
    if same_process_identity "$command_pid" "$command_identity"; then
      terminate_process_tree "$command_pid" "$command_identity" TERM
      sleep 2
      terminate_process_tree "$command_pid" "$command_identity" KILL
    fi
  ) 2>/dev/null &
  timer_pid=$!
  timer_identity="$(process_identity "$timer_pid")" || {
    terminate_process_tree "$command_pid" "$command_identity" TERM
    wait "$command_pid" 2>/dev/null || true
    rmdir "$timeout_state" 2>/dev/null || true
    echo "failed to capture timeout process identity" >&2
    return 2
  }
  wait "$command_pid" || status=$?
  [[ ! -d "${timeout_state}/expired" ]] || timed_out=true
  terminate_process_tree "$timer_pid" "$timer_identity" TERM
  wait "$timer_pid" 2>/dev/null || true
  rmdir "${timeout_state}/expired" 2>/dev/null || true
  rmdir "$timeout_state" 2>/dev/null || true
  if [[ "$timed_out" == true ]]; then
    return 124
  fi
  return "$status"
}
