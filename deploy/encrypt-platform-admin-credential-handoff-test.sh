#!/usr/bin/env bash
set -euo pipefail

script_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
temp_dir="$(mktemp -d)"

cleanup() {
  rm -f "$temp_dir/private.pem" "$temp_dir/public.pem" "$temp_dir/plaintext.json" \
    "$temp_dir/encrypted.json" "$temp_dir/decrypted.json"
  rmdir "$temp_dir" 2>/dev/null || true
}
trap cleanup EXIT

openssl genpkey -algorithm RSA -pkeyopt rsa_keygen_bits:3072 -out "$temp_dir/private.pem" >/dev/null 2>&1
openssl pkey -in "$temp_dir/private.pem" -pubout -out "$temp_dir/public.pem" >/dev/null 2>&1
printf '%s\n' '{"email":"operator@example.test","assignment_version":3}' > "$temp_dir/plaintext.json"
chmod 600 "$temp_dir/private.pem" "$temp_dir/public.pem" "$temp_dir/plaintext.json"

node "$script_dir/encrypt-platform-admin-credential-handoff.mjs" \
  "$temp_dir/plaintext.json" \
  "$temp_dir/public.pem" \
  "$temp_dir/encrypted.json"

node - "$temp_dir" <<'NODE'
const crypto = require("crypto");
const fs = require("fs");
const path = require("path");
const dir = process.argv[2];
const envelopePath = path.join(dir, "encrypted.json");
const mode = fs.statSync(envelopePath).mode & 0o777;
if (mode !== 0o600) throw new Error(`encrypted output mode ${mode.toString(8)} is not 600`);
const envelope = JSON.parse(fs.readFileSync(envelopePath, "utf8"));
if (envelope.version !== 1 || envelope.key_algorithm !== "rsa-oaep-sha256" || envelope.content_algorithm !== "aes-256-gcm") {
  throw new Error("encrypted envelope metadata is invalid");
}
const privateKey = fs.readFileSync(path.join(dir, "private.pem"));
const contentKey = crypto.privateDecrypt({
  key: privateKey,
  oaepHash: "sha256",
}, Buffer.from(envelope.encrypted_key, "base64"));
const decipher = crypto.createDecipheriv("aes-256-gcm", contentKey, Buffer.from(envelope.iv, "base64"));
decipher.setAuthTag(Buffer.from(envelope.tag, "base64"));
const plaintext = Buffer.concat([
  decipher.update(Buffer.from(envelope.ciphertext, "base64")),
  decipher.final(),
]);
fs.writeFileSync(path.join(dir, "decrypted.json"), plaintext, {mode: 0o600, flag: "wx"});
NODE

cmp "$temp_dir/plaintext.json" "$temp_dir/decrypted.json"
printf 'platform-admin-recovery encryption round-trip: ok\n'
