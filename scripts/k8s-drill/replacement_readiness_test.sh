#!/usr/bin/env bash
set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/k8s-drill.sh"
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
# Run the unchanged real scenario; stop at its first replacement port-forward.
scenario_begin(){ :; }; scenario_check(){ :; }; scenario_finish(){ :; }
helm_cmd(){ :; }; ledger_add(){ :; }; drill_login(){ :; }; api_expect(){ :; }
api_base_for_pod(){ echo http://control; }; drill_gen_hex(){ echo control; }
create_soul(){ SOUL_ID=control-soul; }
api_call(){ API_STATUS=200; API_BODY=control-soul; }
sleep(){ echo 3 > "$work/phase"; }
kube_ns(){
 if [[ $1 == delete ]]; then echo 1 > "$work/phase"; return; fi
 if [[ $1 == rollout ]]; then return; fi
 if [[ $1 == get && $2 == pods ]]; then
  phase=$(cat "$work/phase")
  if [[ "$*" == *'-o json' ]]; then
   ready=False;status=Pending;deleted=null
   if [[ $phase == 3 || $case_name == control ]]; then ready=True;status=Running; fi
   if [[ $phase == 0 ]]; then
    echo '{"items":[{"metadata":{"name":"old"},"status":{"phase":"Running","conditions":[{"type":"Ready","status":"True"}]}}]}'
   else
    printf '{"items":[{"metadata":{"name":"old","deletionTimestamp":"cut"},"status":{"phase":"Running","conditions":[{"type":"Ready","status":"True"}]}},{"metadata":{"name":"new"},"status":{"phase":"%s","conditions":[{"type":"Ready","status":"%s"}]}}]}' "$status" "$ready"
   fi
  elif [[ $phase == 0 ]]; then echo old; else echo new; fi
  return
 fi
 if [[ $2 == configmap ]]; then echo 'enabled: true'; return; fi
 if [[ $2 == secret ]]; then echo '{"data":{"key":"control"}}'; return; fi
 if [[ "$*" == *secretKeyRef.name* ]]; then echo secret; return; fi
 if [[ "$*" == *secretKeyRef.key* ]]; then echo key; return; fi
}
pf_start(){
 phase=$(cat "$work/phase")
 if [[ "$1" == new && ( $phase == 3 || $case_name == control ) ]]; then echo true > "$work/ready";else echo false > "$work/ready";fi
 exit 0
}
STATE_DIR=$work; SA_RELEASE=release;SA_FULLNAME=full;SA_NAMESPACE=namespace
SA_ADMIN_EMAIL=fixture;SA_ADMIN_PASSWORD=fixture;SA_ENCRYPTION_KEY=fixture
IMAGE_REPOSITORY=fixture;IMAGE_TAG=fixture;IMAGE_DIGEST='';CHART=fixture;TIMEOUT=1s;RESTART_WAIT_SECONDS=3;API_PORT=8080;API_BASE=fixture
for case_name in control pending; do
 echo 0 > "$work/phase"
 (scenario_standalone) > "$work/$case_name.log" 2>&1
 actual=$(cat "$work/ready")
 echo "EXPECTED: $case_name forwards only ready replacement=true ACTUAL: $actual"
 if [[ $case_name == control && $actual != true ]]; then echo 'INVALID CONTROL';exit 2;fi
 if [[ $case_name == pending && $actual != true ]]; then echo 'PROBLEM CONFIRMED';exit 1;fi
done
echo 'PROBLEM NOT REPRODUCED'

# Exercise controller completion order and fail-closed edges without real sleeps.
RESTART_WAIT_SECONDS=3
sleep(){ echo $(( $(cat "$work/step") + 1 )) > "$work/step"; }
kube_ns(){
 [[ "$edge" != api-error ]] || return 1
 step=$(cat "$work/step")
 case "$edge" in
  progression)
   if [[ $step == 0 ]]; then status=Pending;ready=False
   elif [[ $step == 1 ]]; then status=Running;ready=False
   else status=Running;ready=True;fi
   printf '{"items":[{"metadata":{"name":"old"},"status":{"phase":"Running","conditions":[{"type":"Ready","status":"True"}]}},{"metadata":{"name":"new"},"status":{"phase":"%s","conditions":[{"type":"Ready","status":"%s"}]}}]}' "$status" "$ready";;
  old-only) echo '{"items":[{"metadata":{"name":"old"},"status":{"phase":"Running","conditions":[{"type":"Ready","status":"True"}]}}]}' ;;
  terminating) echo '{"items":[{"metadata":{"name":"new","deletionTimestamp":"cut"},"status":{"phase":"Running","conditions":[{"type":"Ready","status":"True"}]}}]}' ;;
  empty) echo '{"items":[]}' ;;
  ambiguous) echo '{"items":[{"metadata":{"name":"new1"},"status":{"phase":"Running","conditions":[{"type":"Ready","status":"True"}]}},{"metadata":{"name":"new2"},"status":{"phase":"Running","conditions":[{"type":"Ready","status":"True"}]}}]}' ;;
 esac
}
for edge in progression old-only terminating empty ambiguous api-error; do
 echo 0 > "$work/step"
 if selected=$(wait_for_replacement_pod release old); then rc=0;else rc=$?;fi
 if [[ $edge == progression ]]; then
  [[ $rc == 0 && $selected == new && $(cat "$work/step") == 2 ]]
 else
  [[ $rc != 0 && -z $selected ]]
 fi
 printf 'PASS replacement readiness edge: %s\n' "$edge"
done
echo 'FIX VERIFIED'
