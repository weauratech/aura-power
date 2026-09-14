#!/usr/bin/env bash
# Shared process identity, process-tree quiescence and hard-timeout helpers.
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
  [[ -n "$launch_time" && "$boot_time" != *$'\n'* && "$launch_time" != *$'\n'* ]] || return 1
  printf 'darwin:%s:%s' "$boot_time" "$launch_time"
}

same_process_identity() {
  local pid="$1" expected="$2" actual
  actual="$(process_identity "$pid" 2>/dev/null)" || return 1
  [[ "$actual" == "$expected" ]]
}

process_state_identity() {
  local pid="$1" expected="$2" state stat_line stat_tail
  same_process_identity "$pid" "$expected" || return 1
  if [[ -r "/proc/${pid}/stat" ]]; then
    stat_line="$(cat "/proc/${pid}/stat" 2>/dev/null)" || return 1
    stat_tail="${stat_line##*) }"
    state="${stat_tail%% *}"
  else
    state="$(ps -p "$pid" -o state= 2>/dev/null | sed 's/^[[:space:]]*//')"
  fi
  [[ -n "$state" ]] || return 1
  printf '%s' "$state"
}

process_is_running_identity() {
  local state
  state="$(process_state_identity "$1" "$2")" || return 1
  [[ "$state" != Z* ]]
}

signal_process_identity() {
  local pid="$1" expected="$2" signal="${3:-TERM}"
  same_process_identity "$pid" "$expected" || return 0
  kill -s "$signal" "$pid" 2>/dev/null || true
}

freeze_and_capture_process_tree() {
  local pid="$1" expected_identity="$2" destination="$3" child child_identity state attempts=0
  same_process_identity "$pid" "$expected_identity" || return 0
  signal_process_identity "$pid" "$expected_identity" STOP
  while (( attempts < 100 )); do
    state="$(process_state_identity "$pid" "$expected_identity" 2>/dev/null || true)"
    [[ -z "$state" || "$state" == T* || "$state" == Z* ]] && break
    attempts=$((attempts + 1))
    sleep 0.01
  done
  [[ -z "$state" || "$state" == Z* ]] && return 0
  [[ "$state" == T* ]] || return 1
  # Every parent is unconditionally stopped before its children are listed.
  # A child created during traversal is therefore found when its own stopped
  # parent is enumerated, rather than escaping a one-shot live-tree snapshot.
  printf '%s\t%s\n' "$pid" "$expected_identity" >>"$destination"
  while IFS= read -r child; do
    [[ "$child" =~ ^[0-9]+$ ]] || continue
    child_identity="$(process_identity "$child" 2>/dev/null || true)"
    if [[ -n "$child_identity" ]]; then
      freeze_and_capture_process_tree "$child" "$child_identity" "$destination" || return 1
    fi
  done < <(ps -eo pid=,ppid= 2>/dev/null | awk -v parent="$pid" '$2 == parent {print $1}' || true)
}

signal_process_snapshot() {
  local snapshot="$1" signal="$2" pid identity
  while IFS=$'\t' read -r pid identity; do
    [[ -n "$pid" && -n "$identity" ]] || continue
    signal_process_identity "$pid" "$identity" "$signal"
  done <"$snapshot"
}

process_snapshot_is_running() {
  local snapshot="$1" pid identity
  while IFS=$'\t' read -r pid identity; do
    [[ -n "$pid" && -n "$identity" ]] || continue
    process_is_running_identity "$pid" "$identity" && return 0
  done <"$snapshot"
  return 1
}

terminate_and_wait_process_tree() {
  local pid="$1" expected_identity="$2" grace_seconds="${3:-1}" kill_wait_seconds="${4:-3}"
  local snapshot deadline refreshed_snapshot snapshot_pid snapshot_identity refresh_failed=0
  snapshot="$(mktemp)"
  chmod 600 "$snapshot"
  freeze_and_capture_process_tree "$pid" "$expected_identity" "$snapshot" || {
    signal_process_snapshot "$snapshot" CONT
    unlink "$snapshot"
    return 1
  }
  if [[ ! -s "$snapshot" ]]; then
    unlink "$snapshot"
    return 0
  fi
  signal_process_snapshot "$snapshot" TERM
  signal_process_snapshot "$snapshot" CONT
  deadline=$((SECONDS + grace_seconds))
  while process_snapshot_is_running "$snapshot" && (( SECONDS < deadline )); do sleep 0.1; done
  if process_snapshot_is_running "$snapshot"; then
    # Freeze surviving identities again before the final signal. This captures
    # grandchildren created by a TERM-ignoring process during its grace period.
    refreshed_snapshot="$(mktemp)"
    chmod 600 "$refreshed_snapshot"
    while IFS=$'\t' read -r snapshot_pid snapshot_identity; do
      [[ -n "$snapshot_pid" && -n "$snapshot_identity" ]] || continue
      process_is_running_identity "$snapshot_pid" "$snapshot_identity" || continue
      freeze_and_capture_process_tree "$snapshot_pid" "$snapshot_identity" "$refreshed_snapshot" || {
        refresh_failed=1
        break
      }
    done <"$snapshot"
    if [[ "$refresh_failed" -ne 0 ]]; then
      signal_process_snapshot "$snapshot" CONT
      signal_process_snapshot "$refreshed_snapshot" CONT
      unlink "$snapshot" "$refreshed_snapshot"
      return 1
    fi
    unlink "$snapshot"
    snapshot="$refreshed_snapshot"
    signal_process_snapshot "$snapshot" KILL
    deadline=$((SECONDS + kill_wait_seconds))
    while process_snapshot_is_running "$snapshot" && (( SECONDS < deadline )); do sleep 0.1; done
  fi
  if process_snapshot_is_running "$snapshot"; then
    unlink "$snapshot"
    return 1
  fi
  unlink "$snapshot"
}

write_active_process_state() {
  local state_file="$1" pid="$2" identity="$3" pending
  [[ -n "$state_file" && "$identity" != *$'\n'* ]] || return 1
  pending="$(mktemp "${state_file}.pending.XXXXXX")"
  chmod 600 "$pending"
  printf '%s\n%s\n' "$pid" "$identity" >"$pending"
  mv -f "$pending" "$state_file"
}

read_active_process_state() {
  local state_file="$1" extra
  [[ -f "$state_file" && ! -L "$state_file" ]] || return 1
  IFS= read -r ACTIVE_PROCESS_PID <"$state_file" || return 1
  IFS= read -r ACTIVE_PROCESS_IDENTITY < <(sed -n '2p' "$state_file") || return 1
  extra="$(sed -n '3p' "$state_file")"
  [[ "$ACTIVE_PROCESS_PID" =~ ^[0-9]+$ && -n "$ACTIVE_PROCESS_IDENTITY" && -z "$extra" ]] || return 1
}

clear_active_process_state() {
  local state_file="$1" expected_pid="$2" expected_identity="$3"
  read_active_process_state "$state_file" || return 1
  [[ "$ACTIVE_PROCESS_PID" == "$expected_pid" && "$ACTIVE_PROCESS_IDENTITY" == "$expected_identity" ]] || return 1
  unlink "$state_file"
}

quiesce_active_process_state() {
  local state_file="$1"
  [[ ! -e "$state_file" ]] && return 0
  read_active_process_state "$state_file" || {
    echo "active process state is invalid: ${state_file}" >&2
    return 1
  }
  if process_is_running_identity "$ACTIVE_PROCESS_PID" "$ACTIVE_PROCESS_IDENTITY"; then
    terminate_and_wait_process_tree "$ACTIVE_PROCESS_PID" "$ACTIVE_PROCESS_IDENTITY" 1 3 || {
      echo "active process tree did not quiesce: pid=${ACTIVE_PROCESS_PID}" >&2
      return 1
    }
  fi
  clear_active_process_state "$state_file" "$ACTIVE_PROCESS_PID" "$ACTIVE_PROCESS_IDENTITY"
}

restore_process_guard_trap() {
  local signal="$1" saved="$2"
  if [[ -n "$saved" ]]; then
    eval "$saved"
  else
    trap - "$signal"
  fi
}

run_with_process_timeout() {
  local timeout_seconds="$1"
  shift
  local command_pid="" command_identity="" timer_pid="" timer_identity="" status=0 timeout_state="" timed_out=false
  local interrupted_signal="" interrupt_quiesce_failed=0 active_state_file="${PROCESS_GUARD_ACTIVE_FILE:-}" stopped_state="" attempts=0
  local saved_int saved_term saved_hup launch_state failsafe_pid="" failsafe_identity=""
  [[ "$timeout_seconds" =~ ^[1-9][0-9]*$ ]] || { echo "invalid process timeout: ${timeout_seconds}" >&2; return 2; }
  saved_int="$(trap -p INT)"
  saved_term="$(trap -p TERM)"
  saved_hup="$(trap -p HUP)"
  trap 'interrupted_signal=INT; trap "" INT TERM HUP; [[ -z "${command_pid:-}" || -z "${command_identity:-}" ]] || terminate_and_wait_process_tree "$command_pid" "$command_identity" 0 3 || interrupt_quiesce_failed=1' INT
  trap 'interrupted_signal=TERM; trap "" INT TERM HUP; [[ -z "${command_pid:-}" || -z "${command_identity:-}" ]] || terminate_and_wait_process_tree "$command_pid" "$command_identity" 0 3 || interrupt_quiesce_failed=1' TERM
  trap 'interrupted_signal=HUP; trap "" INT TERM HUP; [[ -z "${command_pid:-}" || -z "${command_identity:-}" ]] || terminate_and_wait_process_tree "$command_pid" "$command_identity" 0 3 || interrupt_quiesce_failed=1' HUP

  # Start stopped, and self-kill if the caller dies before persisting identity.
  # No Kubernetes/AWS side effect can precede the active-command record.
  if [[ -n "$active_state_file" ]]; then
    launch_state="$(mktemp -d "${active_state_file}.launch.XXXXXX")"
  else
    launch_state="$(mktemp -d)"
  fi
  sh -c 'guard=$1; shift; parent=$$; (sleep 10; kill -KILL "$parent" 2>/dev/null || true) & failsafe=$!; printf "%s\n" "$failsafe" >"${guard}/failsafe.pid"; kill -STOP "$parent"; exec "$@"' process-guard-wrapper "$launch_state" "$@" <&0 &
  command_pid=$!
  while (( attempts < 200 )); do
    command_identity="$(process_identity "$command_pid" 2>/dev/null || true)"
    if [[ -n "$command_identity" ]]; then
      stopped_state="$(process_state_identity "$command_pid" "$command_identity" 2>/dev/null || true)"
      [[ "$stopped_state" == T* ]] && break
    fi
    [[ -z "$interrupted_signal" ]] || break
    attempts=$((attempts + 1))
    sleep 0.01
  done
  if [[ -z "$command_identity" || "$stopped_state" != T* || -n "$interrupted_signal" ]]; then
    [[ -z "$command_identity" ]] || terminate_and_wait_process_tree "$command_pid" "$command_identity" 0 3 || true
    wait "$command_pid" 2>/dev/null || true
    status=2
  else
    if [[ -n "$active_state_file" ]]; then
      write_active_process_state "$active_state_file" "$command_pid" "$command_identity" || {
        terminate_and_wait_process_tree "$command_pid" "$command_identity" 0 3 || true
        wait "$command_pid" 2>/dev/null || true
        status=2
      }
    fi
    if [[ "$status" -eq 0 ]]; then
      if [[ -f "${launch_state}/failsafe.pid" ]]; then
        failsafe_pid="$(cat "${launch_state}/failsafe.pid")"
        failsafe_identity="$(process_identity "$failsafe_pid" 2>/dev/null || true)"
      fi
      if [[ -z "$failsafe_identity" ]] || ! terminate_and_wait_process_tree "$failsafe_pid" "$failsafe_identity" 0 3; then
        terminate_and_wait_process_tree "$command_pid" "$command_identity" 0 3 || true
        status=2
      fi
    fi
    if [[ "$status" -eq 0 ]]; then
      signal_process_identity "$command_pid" "$command_identity" CONT
      timeout_state="$(mktemp -d)"
      (
        trap 'exit 0' INT TERM HUP
        sleep "$timeout_seconds"
        mkdir "${timeout_state}/expired" 2>/dev/null || true
        terminate_and_wait_process_tree "$command_pid" "$command_identity" 1 3 || true
      ) 2>/dev/null &
      timer_pid=$!
      timer_identity="$(process_identity "$timer_pid" 2>/dev/null || true)"
      if [[ -z "$timer_identity" ]]; then
        terminate_and_wait_process_tree "$command_pid" "$command_identity" 0 3 || true
        status=2
      else
        wait "$command_pid" || status=$?
      fi
    fi
  fi

  if [[ -n "$timer_pid" && -n "$timer_identity" ]]; then
    terminate_and_wait_process_tree "$timer_pid" "$timer_identity" 0 3 || true
    wait "$timer_pid" 2>/dev/null || true
  fi
  [[ -z "$timeout_state" || ! -d "${timeout_state}/expired" ]] || timed_out=true
  [[ -z "$timeout_state" ]] || rmdir "${timeout_state}/expired" 2>/dev/null || true
  [[ -z "$timeout_state" ]] || rmdir "$timeout_state" 2>/dev/null || true
  [[ ! -f "${launch_state}/failsafe.pid" ]] || unlink "${launch_state}/failsafe.pid"
  rmdir "$launch_state" 2>/dev/null || true
  if [[ "$interrupt_quiesce_failed" -eq 0 && -n "$active_state_file" && -n "$command_pid" && -n "$command_identity" && -e "$active_state_file" ]]; then
    clear_active_process_state "$active_state_file" "$command_pid" "$command_identity" || true
  fi
  restore_process_guard_trap INT "$saved_int"
  restore_process_guard_trap TERM "$saved_term"
  restore_process_guard_trap HUP "$saved_hup"
  if [[ -n "$interrupted_signal" ]]; then
    kill -s "$interrupted_signal" "${BASHPID:-$$}"
    return 128
  fi
  [[ "$timed_out" == false ]] || return 124
  return "$status"
}
