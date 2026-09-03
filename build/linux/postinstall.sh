#!/bin/sh
# Create the unprivileged account the service runs as, and its state directory.
# The service is not started: KNOTT should not begin listening until an operator
# has set API_KEYS.
set -e

if ! getent group knott >/dev/null 2>&1; then
    groupadd --system knott
fi
if ! getent passwd knott >/dev/null 2>&1; then
    useradd --system --gid knott --home-dir /var/lib/knott \
            --shell /usr/sbin/nologin --comment "KNOTT workflow orchestration" knott
fi

install -d -o knott -g knott -m 0750 /var/lib/knott
install -d -m 0755 /etc/knott

if [ ! -f /etc/knott/knott.env ]; then
    cat > /etc/knott/knott.env <<'ENV'
# KNOTT service configuration.
#
# Set at least one API key before exposing KNOTT beyond loopback. The format is
# key:role, comma separated; roles are admin, operator and viewer.
#
#   API_KEYS=change-me:admin,readonly-key:viewer
#
# Require signed inbound webhooks:
#   WEBHOOK_SECRET=change-me
#
# Bind address and port (default: loopback on 8002):
#   KNOTT_BIND_HOST=0.0.0.0
#   PORT=8002
ENV
    chmod 0640 /etc/knott/knott.env
fi

systemctl daemon-reload >/dev/null 2>&1 || true

cat <<'MSG'

KNOTT installed.

  Run it now:        knott serve --open
  Or as a service:   edit /etc/knott/knott.env, then
                     sudo systemctl enable --now knott

The service is not enabled automatically — set API_KEYS first.

MSG
