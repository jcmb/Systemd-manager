#!/bin/bash

# Ensure the script is run as root
if [ "$EUID" -ne 0 ]; then
  echo "Please run as root (use sudo)"
  exit
fi

echo "Installing Embedded Systemd Manager..."

# 1. Move the binary to standard executable path
cp systemd-web /usr/local/bin/
chmod +x /usr/local/bin/systemd-web

# 2. Create the systemd service file dynamically
cat << 'EOF' > /etc/systemd/system/systemd-web.service
[Unit]
Description=Embedded Systemd Web Manager
After=network.target

[Service]
Type=simple
User=root
ExecStart=/usr/local/bin/systemd-web --bind=127.0.0.1 --port=6999
Restart=on-failure
RestartSec=5

[Install]
WantedBy=multi-user.target
EOF

# 3. Reload systemd and enable/start the service
systemctl daemon-reload
systemctl enable systemd-web.service
systemctl restart systemd-web.service

echo "Installation complete! The web manager is now running on port 6999."