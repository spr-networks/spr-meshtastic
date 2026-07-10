#!/bin/bash
# Command line install alternative to the UI
set -e

echo "Please enter your SPR path (/home/spr/super/)"
read -r SUPERDIR

if [ -z "$SUPERDIR" ]; then
    SUPERDIR="/home/spr/super/"
fi

export SUPERDIR

echo "Please enter your SPR API token:"
read -r SPR_API_TOKEN

if [ -z "$SPR_API_TOKEN" ]; then
  echo "need api token, generate one on the auth keys page"
  exit 1
fi

echo "LAN IP of the Meshtastic node (TCP mode, port 4403). Leave empty to configure later in the UI:"
read -r MESH_HOST

mkdir -p "$SUPERDIR/configs/plugins/spr-meshtastic"

echo "SPR_API_TOKEN=$SPR_API_TOKEN" > "$SUPERDIR/configs/plugins/spr-meshtastic/config.sh"
printf '%s' "$SPR_API_TOKEN" > "$SUPERDIR/configs/plugins/spr-meshtastic/api-token"
chmod 600 "$SUPERDIR/configs/plugins/spr-meshtastic/config.sh" "$SUPERDIR/configs/plugins/spr-meshtastic/api-token"

if [ -n "$MESH_HOST" ]; then
  # backend re-validates (RFC1918) on load and on every save
  printf '{ "ConnectionMode": "tcp", "Host": "%s", "SerialDevice": "" }\n' "$MESH_HOST" \
    > "$SUPERDIR/configs/plugins/spr-meshtastic/config.json"
else
  printf '{ "ConnectionMode": "tcp", "Host": "", "SerialDevice": "" }\n' \
    > "$SUPERDIR/configs/plugins/spr-meshtastic/config.json"
fi
chmod 600 "$SUPERDIR/configs/plugins/spr-meshtastic/config.json"

./build_docker_compose.sh
docker compose up -d

CONTAINER_IP=$(docker inspect --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}' "spr-meshtastic")
API=127.0.0.1

# Group-only access: assign the Meshtastic node to meshstatic; no broad policy.
curl "http://${API}/firewall/custom_interface" \
-H "Authorization: Bearer ${SPR_API_TOKEN}" \
-X 'PUT' \
--data-raw "{\"SrcIP\":\"${CONTAINER_IP}\",\"Interface\":\"spr-meshtastic\",\"Policies\":[],\"Groups\":[\"meshstatic\"]}"

docker compose restart
