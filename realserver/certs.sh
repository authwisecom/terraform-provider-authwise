#!/usr/bin/env bash
# Throwaway mTLS certificates for one run of the real-server stack (tf#86):
# kit and guard-control reach platform-stub and id over mTLS. Generated per
# run into .data/certs, never committed (the repository is public), and
# useless once the stack is down: nothing outside it trusts this CA.
set -euo pipefail
out="$(cd "$(dirname "$0")" && pwd)/.data/certs"
rm -rf "$out" && mkdir -p "$out" && cd "$out"

openssl req -x509 -newkey rsa:2048 -nodes -days 2 -keyout ca.key -out ca.pem \
  -subj "/CN=tf-realserver test CA" 2>/dev/null

leaf() { # name usage
  openssl req -newkey rsa:2048 -nodes -keyout "$1.key" -out "$1.csr" -subj "/CN=$1" 2>/dev/null
  printf 'subjectAltName=DNS:localhost,IP:127.0.0.1\nextendedKeyUsage=%s\n' "$2" > "$1.ext"
  openssl x509 -req -days 2 -in "$1.csr" -CA ca.pem -CAkey ca.key -CAcreateserial \
    -out "$1.pem" -extfile "$1.ext" 2>/dev/null
  rm "$1.csr" "$1.ext"
}
leaf server serverAuth
leaf client clientAuth
rm -f ca.key ca.srl
# The images run as nonroot: the keys must be readable by any uid.
chmod 644 ./*
