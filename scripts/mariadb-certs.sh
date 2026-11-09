#!/bin/sh
# Local fixtures only. Never reuse this CA/server private key outside development.
set -eu
dir="$(dirname "$0")/../.local/mariadb/certs"
mkdir -p "$dir"
# Keep fixtures with at least 7 days left; otherwise reissue the CA and server
# pair together so an expired or replaced CA never signs a surviving server key.
if [ -e "$dir/ca.crt" ] && [ -e "$dir/server.crt" ] && [ -e "$dir/server.key" ] &&
  openssl x509 -checkend 604800 -noout -in "$dir/ca.crt" >/dev/null &&
  openssl x509 -checkend 604800 -noout -in "$dir/server.crt" >/dev/null; then
  exit 0
fi
echo "Issuing local MariaDB TLS fixtures (valid 30 days)..."
umask 077
openssl req -x509 -newkey rsa:2048 -nodes -days 30 -sha256 \
  -keyout "$dir/ca.key" -out "$dir/ca.crt" -subj /CN=ticketopia-local-ca
openssl req -newkey rsa:2048 -nodes -keyout "$dir/server.key" \
  -out "$dir/server.csr" -subj /CN=mariadb
cat > "$dir/server.ext" <<'EOF'
subjectAltName=DNS:mariadb,DNS:localhost,IP:127.0.0.1
extendedKeyUsage=serverAuth
EOF
openssl x509 -req -in "$dir/server.csr" -CA "$dir/ca.crt" -CAkey "$dir/ca.key" \
  -CAcreateserial -CAserial "$dir/ca.srl" -out "$dir/server.crt" -days 30 -sha256 -extfile "$dir/server.ext"
# The MariaDB container runs as a different UID. Public local server-key fixture;
# the CA private key remains owner-only and is never mounted into the app image.
chmod 644 "$dir/ca.crt" "$dir/server.crt" "$dir/server.key"
