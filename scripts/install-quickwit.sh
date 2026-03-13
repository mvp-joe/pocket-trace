#!/usr/bin/env bash
set -euo pipefail

# Install Quickwit locally with config and systemd service.

INSTALL_DIR="/usr/local/bin"
CONFIG_DIR="/etc/quickwit"
DATA_DIR="/var/lib/quickwit"
CONFIG_FILE="${CONFIG_DIR}/quickwit.yaml"
SERVICE_FILE="/etc/systemd/system/quickwit.service"

# --- Install binary ---

if command -v quickwit &>/dev/null; then
    echo "quickwit already installed at $(command -v quickwit)"
else
    echo "Downloading Quickwit..."
    tmpdir=$(mktemp -d)
    trap 'rm -rf "$tmpdir"' EXIT
    cd "$tmpdir"
    curl -sL https://install.quickwit.io | sh
    sudo cp quickwit-v*/quickwit "${INSTALL_DIR}/quickwit"
    sudo chmod +x "${INSTALL_DIR}/quickwit"
    cd -
    echo "Installed quickwit to ${INSTALL_DIR}/quickwit"
fi

# --- Config ---

sudo mkdir -p "${CONFIG_DIR}" "${DATA_DIR}"

if [ -f "${CONFIG_FILE}" ]; then
    echo "Config already exists at ${CONFIG_FILE}, skipping"
else
    sudo tee "${CONFIG_FILE}" > /dev/null << 'EOF'
version: 0.8
node_id: local
listen_address: 127.0.0.1
data_dir: /var/lib/quickwit

indexer:
  enable_otlp_endpoint: true

jaeger:
  enable_endpoint: true
EOF
    echo "Created config at ${CONFIG_FILE}"
fi

# --- Systemd service ---

if [ -f "${SERVICE_FILE}" ]; then
    echo "Service file already exists at ${SERVICE_FILE}, skipping"
else
    sudo tee "${SERVICE_FILE}" > /dev/null << EOF
[Unit]
Description=Quickwit Search Engine
After=network.target

[Service]
Type=simple
ExecStart=${INSTALL_DIR}/quickwit run --config ${CONFIG_FILE}
Restart=on-failure
RestartSec=5
LimitNOFILE=65535

[Install]
WantedBy=multi-user.target
EOF
    sudo systemctl daemon-reload
    echo "Created systemd service"
fi

sudo systemctl enable quickwit
sudo systemctl start quickwit

echo ""
echo "Done. Quickwit is running and will start on boot."
echo ""
echo "Ports:"
echo "  7280 - REST API + Web UI"
echo "  7281 - OTLP ingestion + Jaeger gRPC"
