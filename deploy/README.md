# Deploying point.vote to the Pi

Target: Raspberry Pi 3B+ (1GB, quad A53, aarch64 userland) behind a
Cloudflare Tunnel. One static binary, no container — sticky single-instance
is fine: rooms are in RAM and that is a feature, not an apology.

## Build & deploy

```sh
make release                      # dist/pointvote-linux-arm64
PI_HOST=pi@point-vote make deploy-pi
```

Or skip the local toolchain: every `v*` tag has the same binary attached to
its GitHub release (`gh release download v1.0.0 -p pointvote-linux-arm64`).

`deploy-pi` copies the binary and restarts the service. A restart wipes
live rooms — documented, accepted behaviour. It assumes the Pi user has
NOPASSWD sudo (stock Raspberry Pi OS does); with a password prompt the
non-interactive ssh will fail — run the install commands by hand instead.

## One-time Pi setup

```sh
# the binary
sudo cp pointvote /usr/local/bin/pointvote

# the service (binds 127.0.0.1:8080 only; the tunnel is the sole ingress)
sudo cp deploy/pointvote.service /etc/systemd/system/
sudo systemctl enable --now pointvote
```

## The tunnel

Two ways to run cloudflared. **Production uses the dashboard flow** —
know which one you're on, because their configs live in different places
and silently ignore each other.

### Dashboard flow (remotely-managed — what production runs)

Create the tunnel in [Zero Trust](https://one.dash.cloudflare.com) →
Networks → Tunnels, and install the connector with the token it gives you:

```sh
sudo cloudflared service install <token>
```

Ingress lives in **Cloudflare's remote config**, not on the Pi: add a
Public Hostname of `point.vote` → `HTTP://127.0.0.1:8080` in the dashboard
(or via API: `PUT /accounts/{account}/cfd_tunnel/{tunnel}/configurations`
with `{"config":{"ingress":[{"hostname":"point.vote","service":
"http://127.0.0.1:8080"},{"service":"http_status:404"}]}}`).

In this mode `/etc/cloudflared/config.yml` is **ignored**. A connected
tunnel with no remote ingress config serves nothing — the symptom is a
Cloudflare 521 with healthy services on the Pi.

### Config-file flow (locally-managed — the alternative)

`deploy/cloudflared-config.yml` is for this flow only:

```sh
cloudflared tunnel login
cloudflared tunnel create pointvote
# copy the credentials JSON it prints to /etc/cloudflared/pointvote.json
cloudflared tunnel route dns pointvote point.vote
sudo cp deploy/cloudflared-config.yml /etc/cloudflared/config.yml
sudo cloudflared --config /etc/cloudflared/config.yml service install
```

## DNS gotcha (a 521 that is not the tunnel's fault)

The apex record must be a **proxied CNAME to
`<tunnel-id>.cfargotunnel.com`**. The dashboard's Public Hostname editor
creates it *only if no record already exists* — a leftover A record from a
previous registrar (parking pages, web forwarding) wins silently, and
Cloudflare proxies to that dead IP: 521, while tunnel and app both report
healthy. Check `Zone → DNS` for stale apex records when the edge says the
origin is down but the Pi disagrees.

## Verifying an outage from the outside in

```sh
curl -s https://point.vote/healthz          # 521/530 → edge can't reach an origin
tailscale ssh <pi> curl -s http://127.0.0.1:8080/healthz   # app itself
tailscale ssh <pi> systemctl is-active pointvote cloudflared
tailscale ssh <pi> systemctl cat cloudflared | grep ExecStart
#   --token …      → dashboard flow: ingress is remote; config.yml is ignored
#   --config …yml  → config-file flow: ingress is local
```

## SD-card hygiene

The app writes nothing (rooms live in RAM). Cap journald so logs can't eat
the card:

```ini
# /etc/systemd/journald.conf.d/size.conf
[Journal]
SystemMaxUse=64M
```

## Timeouts that matter behind Cloudflare

The 25s SSE heartbeats keep the edge from reaping idle streams; the 55s
long-poll cap fits Cloudflare's ~100s proxy timeout by design. Change
neither casually.

The Dockerfile in the repo root remains for non-Pi targets; the Pi path is
a bare static binary on purpose.
