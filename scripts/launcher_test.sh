#!/usr/bin/env bash
set -euo pipefail
repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
fixture=$(mktemp -d)
trap 'rm -rf "$fixture"' EXIT
mkdir -p "$fixture/mock" "$fixture/state"
export LAUNCHER_CAPTURE="$fixture/process-actions"
cat > "$fixture/anubis" <<'MOCK'
#!/usr/bin/env bash
if [[ "$1" == init ]]; then printf '{}\n' > "$3"; fi
exit 0
MOCK
cat > "$fixture/mock/pgrep" <<'MOCK'
#!/usr/bin/env bash
echo 12345
MOCK
cat > "$fixture/mock/pkill" <<'MOCK'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$LAUNCHER_CAPTURE"
MOCK
printf '#!/usr/bin/env bash\nexit 0\n' > "$fixture/mock/curl"
printf '#!/usr/bin/env bash\nexit 0\n' > "$fixture/mock/sleep"
chmod +x "$fixture/anubis" "$fixture/mock/"*
export ANUBIS_BINARY="$fixture/anubis"
export PATH="$fixture/mock:$PATH"
cd "$fixture"
bash "$repo_root/run.sh" --data-dir "$fixture/state" --password Fixture-Launcher47 > "$fixture/first.log" 2>&1
test -s "$fixture/state/anubis.json"
echo 'CONTROL EXPECTED: fresh install creates config; ACTUAL: created'
printf '{"sentinel":"existing-installation"}\n' > "$fixture/state/anubis.json"
cp "$fixture/state/anubis.json" "$fixture/expected.json"
rm -f "$LAUNCHER_CAPTURE"
bash "$repo_root/run.sh" --data-dir "$fixture/state" --password Fixture-Launcher47 > "$fixture/second.log" 2>&1
failed=0
if cmp -s "$fixture/expected.json" "$fixture/state/anubis.json"; then
  echo 'EXPECTED: existing config preserved; ACTUAL: unchanged'
else
  echo 'EXPECTED: existing config preserved; ACTUAL: overwritten'
  failed=1
fi
if [[ -e "$LAUNCHER_CAPTURE" ]]; then
  echo 'EXPECTED: no process-name kill; ACTUAL: pkill invoked'
  failed=1
else
  echo 'EXPECTED: no process-name kill; ACTUAL: no pkill'
fi
if [[ "$failed" == 1 ]]; then echo 'PROBLEM CONFIRMED'; exit 1; fi
echo 'FIX VERIFIED'
