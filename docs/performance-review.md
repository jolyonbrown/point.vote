# Performance review, 5 September 2026

## Recommendation

Fix browser state refresh and connection recovery first. The evidence does not
support replacing the Pi to solve a six-user CPU bottleneck. A small VPS is a
reasonable subsequent reliability improvement, but a managed-laptop proxy can
still stall SSE after a hosting move.

The reported symptoms are delayed vote/reveal feedback and delayed updates from
other participants. Participants are geographically distributed, using NHS
developer laptops that may share a centrally managed security proxy. That is a
plausible cause, not a confirmed diagnosis: this review did not capture traffic
from an affected laptop during an incident.

Reviewed commit: `1c08914`. Production reports `v1.2.8`; there are no differences
between that tag and the reviewed commit under `web`, `internal`, or `cmd`.
The findings below record the initial investigation, with code line references
at that commit. Follow-up implementation is described at the end of this file.
Production was not restarted.

## Measurements

Read-only checks on `clankerville` before the diagnostic sessions:

- Load average: 0.00; temperature: 49.4 C; `vcgencmd get_throttled`: `0x0`.
- Available RAM: 669 MiB of 906 MiB. App RSS: approximately 11.3 MiB;
  cloudflared RSS: approximately 39.5 MiB.
- App running since 13 August, with zero service restarts reported. Its systemd
  memory limit is 256 MiB, with no CPU quota.
- Ethernet was up; Wi-Fi was down. Four tunnel connections were healthy, with
  approximately 12-13 ms smoothed QUIC RTT to Cloudflare.
- Loopback health request: 2.1 ms. One public HTTPS health request: 143 ms,
  negotiating HTTP/2.

Completed requests in the preceding 14 days, before the test traffic:

| Operation | Samples | Server p95 | Server maximum |
| --- | ---: | ---: | ---: |
| Vote POST | 62 | 3 ms | 7 ms |
| Join POST | 22 | 0 ms | 1 ms |
| Start-round POST | 7 | 2 ms | 2 ms |
| Other GET API requests | 783 | 0 ms | 1 ms |

Times are integer milliseconds from the existing request logger; 0 means less
than 1 ms, not zero work. These measurements exclude client/network transit,
browser rendering, unfinished requests, and delivery of an SSE event. No
recorded request in those groups returned a 5xx. Samples of real mutations are
small, so they do not establish peak capacity or rule out intermittent stalls.

Two bounded diagnostic sessions each created a separate room, joined six
participants, held six independent SSE streams open, and performed 21 rounds
with six concurrent votes per round. Each session measured 126 vote responses
and 756 participant-vote observations across the streams. All diagnostic
participants were removed and streams closed afterwards; the two empty rooms
remain until the normal idle TTL expires. No existing rooms were modified.

| Measurement | Public HTTPS p50 / p95 / max | Pi loopback p50 / p95 / max |
| --- | --- | --- |
| Vote POST response | 57 / 80 / 165 ms | 23 / 53 / 92 ms |
| Vote start to SSE observation | 74 / 140 / 163 ms | 32 / 80 / 115 ms |
| Concurrent vote batch start to reveal | 92 / 159 / 165 ms | 63 / 98 / 120 ms |
| Room GET response | 42 / 45 / 47 ms | 3 / 4 / 5 ms |

Neither session reported a stream error or missing reveal. Snapshots grew from
about 1.2 KB to 11.8 KB as history reached its 20-round limit, using short names
and no rationales. Both probes used Python's standard HTTP client, independent
connections, and SSE parsing; this was not a browser-rendering benchmark or a
test through the NHS proxy. The sessions overlapped, and the loopback load
generator itself ran on the Pi, so their differences do not isolate network
cost. Server vote timings during these probes were p50 2 ms, p95 16 ms, max
54 ms. These results show that the tested workload did not cause multi-second
latency; they are not a maximum-capacity claim.

## Prioritized code findings

### 1. Successful actions wait unnecessarily for SSE

`web/app.js:451`, `:461`, and `:471`: reveal, settle, and next-round handlers
discard the fresh state returned by their POSTs. If SSE stalls, a successful
action appears to do nothing. `castVote` at `:436` only stores the selected
card and re-renders the existing state; participant ticks, totals, and an
auto-reveal still depend on SSE. There is no pending feedback before the POST
finishes.

Consume successful mutation snapshots immediately through a common state
application function, and refresh after votes because their existing response
only contains an acknowledgement. Guard against a delayed HTTP response or
buffered SSE snapshot overwriting newer state. An additive monotonic room
revision shared by snapshots and streams is more robust than round number
alone, since several votes happen in one round. Capture the vote's round/key
before awaiting the request, and reconcile uncertain outcomes before enabling
retries. Keep blind vote values local until the server reveals them.

### 2. The fallback is too slow for an interactive meeting

`web/app.js:568`, `:577`, and `:612`: polling happens only as a side effect of
reconnecting. A stream that opens but delivers no data is checked every ten
seconds and declared stale only after more than sixty seconds. Opening the
next stream resets liveness and backoff again. The live indicator turns green
on headers arriving, before a snapshot or heartbeat proves delivery.

A deterministic Node simulation running the actual reconnect/watchdog block,
with fake timers, successful fetches, and fixed midpoint jitter, refreshed at
71 and 141 seconds over three minutes of silent streams. This reproduces a
minute-scale refresh cadence, contrary to PR #36's approximately 30-second
worst-case description. It does not prove the production proxy is buffering.

Add explicit polling recovery: an initial snapshot delivery deadline of about
5 seconds, then one room GET every 2-3 seconds while degraded. Retry SSE less
frequently and only mark it healthy after data is delivered. A periodic state
refresh while the page is visible, or state revisions in heartbeats, can also
catch missed state while pings continue. Keep timers and requests bounded;
pause polling in hidden tabs and resync immediately on return. Avoid letting
background polling keep abandoned rooms alive forever, because GET touches
the room's idle TTL. Preserve the server's existing 25-second heartbeat.

### 3. Hung fetches prevent recovery and permit overlapping probes

`web/app.js:22` has no request deadline. At `:584`, `reconnectTimer` is cleared
before awaiting the GET at `:590`, and `connect()` is called only afterwards.
A stalled fetch can prevent reconnecting while subsequent watchdog ticks
launch additional pending GETs. If those later resolve, multiple callbacks
can each open a stream without closing the previous one.

The same simulation with a never-resolving fetch started probes at 71, 82, 94,
108, 126, and 160 seconds, with no replacement stream or rendered refresh.

Use cancellation/deadlines for ordinary REST requests, a single in-flight
refresh guard, and a connection generation token that invalidates stale
callbacks. Handle expiry as a terminal state and clear all timers/streams;
currently the 404 path returns but leaves the watchdog and old `es` reference
alive. Do not automatically retry timed-out mutation POSTs: the server may
already have accepted them. Refresh state to reconcile first.

### 4. Slow subscribers can lose the final state indefinitely

`internal/room/room.go:553` defines a 16-event subscriber buffer;
`fanoutLocked` at `:600` silently drops the incoming event when it is full.
If that event is the final reveal, the subscriber drains older snapshots and
can remain on the voting screen indefinitely. Heartbeats continue from
`internal/api/stream.go:52`, so the browser watchdog considers it healthy.
Six voters alone do not fill this buffer; bursts, reactions, or an already
blocked writer can create the condition. This is a correctness risk found by
inspection, not something observed in the public probe.

Define an explicit overflow policy that guarantees resynchronization for state
changes, such as disconnecting a lagging subscriber so it reloads a snapshot.
Dropping transient reactions can remain acceptable. Test final-reveal delivery
and long-poll/MCP behavior because subscriptions are shared. Add per-write
deadlines and propagate SSE write/flush errors so a broken consumer is released;
avoid a global response timeout that would terminate healthy long-lived streams.
The response-writer middleware must support any response-controller operations
used for those deadlines.

### 5. Current reporting confuses stream duration with slow requests

`internal/api/middleware.go:34` logs duration only after the handler returns.
`scripts/usage.sh:48` counts every request over 250 ms as slow, including SSE
and long polls. Of 592 completed SSE streams in the historical sample, 589
crossed that threshold simply because they remained open. Their median
lifetime was about 50 seconds; that is not a 50-second server response time.

Split ordinary request latency from stream lifetime and long-poll waits. Add
aggregate counts for active streams, write failures, overflow/resync, and
client recovery reasons. Capture browser request duration and time since the
last applied state when investigating a session, without room content, tokens,
names, or vote values. Cloudflared's lifetime error counter is cumulative and
includes disconnects; measure changes around incidents before interpreting it.

### 6. Reduce repeated work after the recovery fixes

`web/app.js:112` rebuilds participants, results, settlement and history on each
state event. `internal/api/stream.go:77` marshals the same full snapshot once
per subscriber, including unchanged history. More users produce both more
events and more recipients. Cache unchanged rendered sections and batch
multiple snapshots into one animation-frame render; profile before changing
the full-snapshot protocol. Serialize shared event data once if measurements
show encoding becomes significant, preserving immutable redacted snapshots.

These are growth optimizations, not demonstrated causes of the six-user issue.
Per-room locks, precomputed reveal results, startup-loaded static assets and
nonblocking fanout are already sensible choices. A framework, database,
WebSocket rewrite or distributed backend is not needed for this workload.

## Suggested delivery sequence

1. Implement findings 1-3 on a fix branch. Add deterministic browser tests for
   successful POSTs with stalled SSE, missing initial snapshots, hung GETs,
   delayed/out-of-order responses, expiry, wake/resume and one-stream ownership.
   Add browser smoke coverage against the real Go server and run the existing
   Go race suite, vet and API e2e checks.
2. Fix subscriber overflow and stream cleanup with regression coverage, and
   correct the usage report so later measurements distinguish request latency
   from connection lifetime.
3. Trial the changes with an NHS laptop on its normal managed connection.
   Compare the POST completion time with visible state updates and a control
   client; a phone hotspot alone might not bypass an always-on endpoint proxy.
   Target healthy-network p95 updates under 500 ms, degraded-mode updates within
   about 3 seconds after polling recovery, and bounded recovery from hangs.
4. Deploy in an agreed quiet window. Existing rooms are in RAM, so a service
   restart destroys them. Keep the preceding binary for rollback and verify
   both local health and a public multi-client vote/reveal after deployment.
5. Move hosting when removing dependence on home power, broadband and the SD
   card is worth a small monthly charge. Repeat the same measurements after
   moving; optimize repeated rendering/serialization only if needed.

## Hosting path

My suggested target is one small always-on Linux VPS in London, running the
same systemd service and Cloudflare Tunnel, with Tailscale for administration.
A DigitalOcean Basic instance with 1 GiB RAM and 1 vCPU is listed at USD 6/month,
before tax and optional extras, and London is a supported region:
[DigitalOcean pricing](https://www.digitalocean.com/pricing/droplets).
That is a starting size based on observed resource use, not a capacity guarantee.

Keep one active application instance because rooms live in that process. Build
for the VPS architecture (the current `make release` only produces ARM64),
provision a separate tunnel for staging, validate it, and switch the production
hostname in a quiet window. Do not run the Pi and an independent VPS process as
interchangeable connectors for the same hostname: requests can land in different
in-memory room stores. Rollback restores service, not lost ephemeral rooms.

Keeping Cloudflare initially isolates the hosting change and preserves the
loopback-only ingress model. A later direct HTTPS origin could remove the
tunnel hop, but requires revisiting trusted proxy/IP handling for rate limits.
There is no evidence here that removing Cloudflare is necessary.

Cloudflare documents the `text/event-stream` header as the exception to tunnel
response buffering, and the app already sends it:
[Cloudflare troubleshooting](https://developers.cloudflare.com/cloudflare-one/troubleshooting/tunnel/).
Also, the often-cited six-connection SSE limit is per browser and domain under
HTTP/1, not six participants on separate laptops:
[MDN SSE connection limits](https://developer.mozilla.org/en-US/docs/Web/API/Server-sent_events/Using_server-sent_events).

## Verification limits

At the investigation stage, the bounded HTTP/SSE probes and the deterministic recovery simulation passed.
The simulation stubs EventSource and the DOM; it demonstrates timer/control
flow, not actual proxy behavior or rendering cost. No affected NHS laptop was
available, so a managed proxy remains the leading hypothesis rather than a
confirmed root cause. The Go suite was not run during the initial investigation:
no application code had changed and the environment had no Go executable.
The implementation checks below used the pinned Go toolchain under /tmp.

## Implementation and verification

Branch: `fix/resilient-live-updates`. The follow-up changes implement findings
1-5 with no production deployment or additional runtime dependencies:

- The client consumes successful reveal, settle and next-round responses
  immediately and refreshes after votes. Pending votes get feedback and cannot
  be submitted concurrently. A delayed vote response uses the original round's
  storage key.
- Snapshots include an additive monotonic `revision`; the UI skips equal or
  older revisions. This also avoids rebuilding unchanged DOM on periodic GETs.
- The transport polls every two seconds after a completed refresh while
  degraded and every fifteen seconds while healthy. A five-second initial
  snapshot deadline detects buffered streams. After an established stream has
  been silent for more than thirty-five seconds, polling recovery begins.
  Stalled streams are retried after thirty seconds, while explicit connection
  errors use jittered exponential backoff.
- Ordinary REST requests have an eight-second deadline including the response
  body. Refreshes are single-flight and cancelable; stale callbacks cannot
  publish state or create extra streams. Mutation POSTs are never retried
  automatically. Hidden tabs stop polling and close their stream; returning
  resynchronizes immediately. Expired rooms stop all retry timers.
- Full subscriber queues coalesce to the newest state snapshot, preserving
  delivery of a final reveal. Transient reactions may still be dropped. SSE
  writes and flushes propagate errors and have a ten-second deadline which is
  cleared between events, preserving the existing twenty-five-second heartbeat.
- The usage report separates streams/long polls/MCP from slow REST/page
  requests. It does not introduce client telemetry.

Validation completed locally:

- `go test -race ./...`, `go vet ./...`, Go formatting and API e2e tests.
- Nine deterministic Node transport tests, covering silent streams, hung
  requests/bodies, stale callbacks, expiry, visibility changes and reconciliation.
- Five Chrome browser tests: six-client vote/reveal/new-round flow, idle SSE
  heartbeat delivery beyond the write deadline, updates with
  SSE disabled, immediate mutation state with GETs unavailable, and delayed
  response ordering across round transitions. Desktop and mobile screenshots
  were inspected, with no horizontal overflow.
- The usage report was exercised against read-only aggregate production logs.
- The final application cross-compiled successfully for Linux ARM64.

CI runs both Node transport tests and browser tests. To run browser checks
locally, start a local server on port 8092 and use a temporary Playwright install:

```sh
go build -o bin/pointvote ./cmd/pointvote
bin/pointvote -addr 127.0.0.1:8092
```

In a second terminal:

```sh
npm install --prefix /tmp/pointvote-browser-check --no-audit --no-fund playwright@1.63.0
/tmp/pointvote-browser-check/node_modules/.bin/playwright install chromium
PLAYWRIGHT_MODULE=/tmp/pointvote-browser-check/node_modules/playwright node --test e2e/browser_test.cjs
```

`CHROME_PATH` can select an existing Chrome executable instead of downloading
Chromium; `POINTVOTE_URL` overrides the local server URL. Use a test server:
the suite creates rooms and synthetic participants. Screenshots go under
`/tmp/pointvote-screenshots` unless `SCREENSHOT_DIR` is set.

An actual NHS managed-laptop trial remains necessary to confirm the incident's
network cause and user-visible improvement. These tests simulate buffering and
delays; they do not reproduce that organization's security infrastructure.
