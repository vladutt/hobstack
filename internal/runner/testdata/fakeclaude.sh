#!/bin/sh
# claude fals pentru teste: scrie linii stream-json in functie de FAKE_MODE.
case "$FAKE_MODE" in
slow)
  echo '{"type":"system","subtype":"init","session_id":"sess-slow"}'
  sleep 30
  ;;
budget)
  echo '{"type":"system","subtype":"init","session_id":"sess-b"}'
  echo '{"type":"result","subtype":"error_max_budget_usd","is_error":true,"total_cost_usd":1.2,"errors":["Reached maximum budget ($1)"],"result":null,"session_id":"sess-b"}'
  ;;
crash)
  echo "boom" >&2
  exit 3
  ;;
*)
  echo '{"type":"system","subtype":"init","session_id":"sess-1"}'
  echo "un avertisment" >&2
  echo "{\"type\":\"result\",\"subtype\":\"success\",\"is_error\":false,\"result\":\"gata: $*\",\"total_cost_usd\":0.25,\"session_id\":\"sess-1\"}"
  ;;
esac
