#!/usr/bin/env bash
# ==============================================================
# Smoke test du durcissement production YahriaCad (une session) :
# auth JWT, CORS strict, rate limit IA, métriques Prometheus.
# Usage : bash scripts/smoke_prod_hardening.sh [port]
# ==============================================================
set -u
PORT="${1:-8090}"
B="http://localhost:$PORT"
BIN=/tmp/yahriacad-server
LOG=/tmp/yahriacad-smoke.log

export YAHRIACAD_AUTH_ENABLED=true
export YAHRIACAD_JWT_SECRET=smoke-secret
export YAHRIACAD_AUTH_USERS='admin:admin'
export YAHRIACAD_ALLOWED_ORIGINS='http://localhost:3000'
export YAHRIACAD_AI_RATE_RPS=1
export YAHRIACAD_AI_RATE_BURST=3
export YAHRIACAD_APP_ENV=production
export YAHRIACAD_HTTP_PORT=$PORT

fails=0
check() { # check <nom> <attendu> <obtenu>
  if [ "$2" = "$3" ]; then echo "  OK   $1 ($3)"; else echo "  FAIL $1 (attendu $2, obtenu $3)"; fails=$((fails+1)); fi
}

echo "== Démarrage serveur durci (port $PORT) =="
"$BIN" > "$LOG" 2>&1 &
SRV=$!
trap 'kill $SRV 2>/dev/null' EXIT
for _ in $(seq 1 30); do
  curl -sf -o /dev/null "$B/healthz" && break
  sleep 0.5
done

echo "== 1. Routes publiques =="
check "healthz sans jeton" 200 "$(curl -s -o /dev/null -w '%{http_code}' "$B/healthz")"
check "metrics sans jeton" 200 "$(curl -s -o /dev/null -w '%{http_code}' "$B/metrics")"

echo "== 2. Protection =="
check "projets sans jeton" 401 "$(curl -s -o /dev/null -w '%{http_code}' "$B/api/v1/projects")"
check "ws sans jeton" 401 "$(curl -s -o /dev/null -w '%{http_code}' "$B/ws/v1/progress?project_id=x")"

echo "== 3. Login =="
check "mauvais mot de passe" 401 "$(curl -s -o /dev/null -w '%{http_code}' -X POST "$B/api/v1/auth/login" -H 'Content-Type: application/json' -d '{"username":"admin","password":"wrong"}')"
LOGIN=$(curl -s -X POST "$B/api/v1/auth/login" -H 'Content-Type: application/json' -d '{"username":"admin","password":"admin"}')
TOKEN=$(printf '%s' "$LOGIN" | python3 -c 'import json,sys; print(json.load(sys.stdin).get("token",""))')
if [ -n "$TOKEN" ]; then echo "  OK   token émis (${TOKEN:0:24}…)"; else echo "  FAIL login sans token : $LOGIN"; fails=$((fails+1)); fi

echo "== 4. Accès authentifié =="
check "projets + Bearer" 200 "$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $TOKEN" "$B/api/v1/projects")"
check "auth/me" 200 "$(curl -s -o /dev/null -w '%{http_code}' -H "Authorization: Bearer $TOKEN" "$B/api/v1/auth/me")"
check "auth/me jeton bidon" 401 "$(curl -s -o /dev/null -w '%{http_code}' -H 'Authorization: Bearer abc.def.ghi' "$B/api/v1/auth/me")"
ME=$(curl -s -H "Authorization: Bearer $TOKEN" "$B/api/v1/auth/me")
check "identité renvoyée" '1' "$(printf '%s' "$ME" | rg -c '"username":"admin"')"
check "ws ?token= (auth passée -> 400 handshake curl)" 400 "$(curl -s -o /dev/null -w '%{http_code}' "$B/ws/v1/progress?project_id=x&token=$TOKEN")"

echo "== 5. CORS strict =="
CAO=$(curl -s -D - -o /dev/null -H 'Origin: http://localhost:3000' "$B/healthz" | rg -i '^access-control-allow-origin:' | tr -d '\r')
echo "  ACAO autorisée : $CAO"
[ -n "$CAO" ] && echo "  OK   origine autorisée reflétée" || { echo "  FAIL ACAO absente pour origine autorisée"; fails=$((fails+1)); }
CAO2=$(curl -s -D - -o /dev/null -H 'Origin: https://evil.example' "$B/healthz" | rg -i '^access-control-allow-origin:' | tr -d '\r')
[ -z "$CAO2" ] && echo "  OK   origine interdite sans ACAO" || { echo "  FAIL ACAO=$CAO2 pour origine interdite"; fails=$((fails+1)); }

echo "== 6. Rate limit IA (burst 3) =="
last=""; n429=0; nok=0
for i in $(seq 1 6); do
  code=$(curl -s -o /dev/null -w '%{http_code}' -X POST -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{}' "$B/api/v1/projects/p/route")
  last=$code
  [ "$code" = "429" ] && n429=$((n429+1)) || nok=$((nok+1))
done
echo "  codes: 429=$n429 autres=$nok (dernier=$last)"
[ "$n429" -gt 0 ] && [ "$nok" -gt 0 ] && echo "  OK   burst puis 429" || { echo "  FAIL rate limit"; fails=$((fails+1)); }
RA=$(curl -s -D - -o /dev/null -X POST -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{}' "$B/api/v1/projects/p/route" | rg -i '^retry-after:' | tr -d '\r')
[ -n "$RA" ] && echo "  OK   Retry-After présent ($RA)" || echo "  (info) Retry-After absent sur cet appel"

echo "== 7. Métriques Prometheus =="
M=$(curl -s "$B/metrics")
printf '%s\n' "$M" | rg -q 'yahriacad_http_requests_total' && echo "  OK   compteur requêtes" || { echo "  FAIL compteur absent"; fails=$((fails+1)); }
printf '%s\n' "$M" | rg -q 'yahriacad_http_request_duration_seconds' && echo "  OK   histogramme latence" || { echo "  FAIL histogramme absent"; fails=$((fails+1)); }
printf '%s\n' "$M" | rg -q 'yahriacad_build_info' && echo "  OK   build info" || { echo "  FAIL build info absent"; fails=$((fails+1)); }

echo
if [ "$fails" -eq 0 ]; then echo "SMOKE OK — durcissement validé"; else echo "SMOKE ÉCHOUÉ : $fails vérification(s)"; exit 1; fi
