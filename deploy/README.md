# Deploying point.vote to the Pi

Target: Raspberry Pi 3B+ (1GB, quad A53, aarch64 userland) behind a
Cloudflare Tunnel. One static binary, no container — sticky single-instance
is fine: rooms are in RAM and that is a feature, not an apology.

## Build & deploy

```sh
make release                      # dist/pointvote-linux-arm64
PI_HOST=pi@point-vote make deploy-pi
```

`deploy-pi` copies the binary and restarts the service. A restart wipes
live rooms — documented, accepted behaviour.

## One-time Pi setup

```sh
# the binary
sudo cp pointvote /usr/local/bin/pointvote

# the service (binds 127.0.0.1:8080 only; the tunnel is the sole ingress)
sudo cp deploy/pointvote.service /etc/systemd/system/
sudo systemctl enable --now pointvote

# the tunnel (see comments in cloudflared-config.yml for the login dance)
sudo cp deploy/cloudflared-config.yml /etc/cloudflared/config.yml
sudo cloudflared --config /etc/cloudflared/config.yml service install
```

DNS lives on Cloudflare's free plan; `cloudflared tunnel route dns
pointvote point.vote` points the apex at the tunnel. TLS terminates at the
edge. Nothing on the LAN or internet reaches the app directly — the
rate-limiter's trust in `CF-Connecting-IP` depends on that loopback-only
bind, so don't widen it.

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
