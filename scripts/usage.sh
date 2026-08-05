#!/usr/bin/env bash
# Usage report for point.vote, aggregated from the Pi's structured logs
# over Tailscale. Server-side and aggregate only — consistent with the
# privacy page (no IPs are logged, so none can appear here).
#
#   scripts/usage.sh            # last 7 days
#   scripts/usage.sh 24h        # last 24 hours
#   PI=othername scripts/usage.sh
set -euo pipefail

WINDOW="${1:-7d}"
PI="${PI:-clankerville}"
case "$WINDOW" in
  *h) SINCE="-$(( ${WINDOW%h} * 60 )) min" ;;
  *d) SINCE="-${WINDOW%d} days" ;;
  *) echo "window like 24h or 7d" >&2; exit 1 ;;
esac

command -v jq >/dev/null || { echo "jq required" >&2; exit 1; }

LOGS=$(tailscale ssh "$PI" "journalctl -u pointvote --since '$SINCE' -o cat --no-pager; echo '---SYS---'; systemctl show pointvote -p NRestarts -p ActiveEnterTimestamp; free -m | awk 'NR==2{print \"mem_used_mb=\"\$3\" mem_total_mb=\"\$2}'; df -m / | awk 'NR==2{print \"disk_used_mb=\"\$3\" disk_avail_mb=\"\$4}'")

APP=$(printf '%s\n' "$LOGS" | sed '/^---SYS---$/,$d' | grep '^{' || true)
SYS=$(printf '%s\n' "$LOGS" | sed -n '/^---SYS---$/,$p')

echo "point.vote usage — last $WINDOW"
echo "================================"
echo
echo "## Product"
printf '%s\n' "$APP" | jq -rs '
  def count(f): map(select(f)) | length;
  "rooms created:        \(count(.msg=="room_created"))
participants joined:  \(count(.msg=="participant_joined"))  (human \(count(.msg=="participant_joined" and .kind=="human")), agent \(count(.msg=="participant_joined" and .kind=="agent")))
votes cast:           \(count(.msg=="vote_cast"))  (human \(count(.msg=="vote_cast" and .kind=="human")), agent \(count(.msg=="vote_cast" and .kind=="agent")))
rounds revealed:      \(count(.msg=="revealed"))  (consensus \(count(.msg=="revealed" and .consensus==true)))
reactions:            \(count(.msg=="reaction"))"'
echo
echo "## Rooms by day (created)"
printf '%s\n' "$APP" | jq -r 'select(.msg=="room_created") | .time[0:10]' | sort | uniq -c | awk '{printf "  %s  %s\n", $2, $1}'
echo
echo "## Traffic"
printf '%s\n' "$APP" | jq -rs '
  map(select(.msg=="request")) |
  "requests:             \(length)
2xx/3xx:              \(map(select(.status < 400)) | length)
4xx:                  \(map(select(.status >= 400 and .status < 500)) | length)
5xx:                  \(map(select(.status >= 500)) | length)
slow (>250ms):        \(map(select(.duration_ms > 250)) | length)"'
echo
echo "## Top pages (2xx, excluding API/stream noise)"
printf '%s\n' "$APP" | jq -r 'select(.msg=="request" and .status<400) | .path' \
  | grep -vE '^/api/|^/healthz' | sort | uniq -c | sort -rn | head -8 | awk '{printf "  %5s  %s\n", $1, $2}'
echo
echo "## Errors"
ERRS=$(printf '%s\n' "$APP" | jq -r 'select(.level=="ERROR") | "\(.time[0:19])  \(.msg)  \(.err // "")"' | tail -5)
if [ -n "$ERRS" ]; then printf '%s\n' "$ERRS"; else echo "  none logged in window"; fi
echo
echo "## Host"
printf '%s\n' "$SYS" | grep -E 'NRestarts|ActiveEnter|mem_used|disk_used' | sed 's/^/  /'
