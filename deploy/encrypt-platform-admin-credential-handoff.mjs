import crypto from "node:crypto";
import fs from "node:fs";

const [inputPath, publicKeyPath, outputPath] = process.argv.slice(2);
if (!inputPath || !publicKeyPath || !outputPath) {
  throw new Error("usage: node encrypt-platform-admin-credential-handoff.mjs <input> <public-key> <output>");
}

const plaintext = fs.readFileSync(inputPath);
const publicKey = crypto.createPublicKey(fs.readFileSync(publicKeyPath));
if (publicKey.asymmetricKeyType !== "rsa") {
  throw new Error("recovery public key must be RSA");
}
const modulusLength = publicKey.asymmetricKeyDetails?.modulusLength ?? 0;
if (modulusLength < 3072) {
  throw new Error("recovery RSA public key must contain at least 3072 bits");
}

const contentKey = crypto.randomBytes(32);
const iv = crypto.randomBytes(12);
const cipher = crypto.createCipheriv("aes-256-gcm", contentKey, iv);
const ciphertext = Buffer.concat([cipher.update(plaintext), cipher.final()]);
const envelope = {
  version: 1,
  key_algorithm: "rsa-oaep-sha256",
  content_algorithm: "aes-256-gcm",
  encrypted_key: crypto.publicEncrypt({ key: publicKey, oaepHash: "sha256" }, contentKey).toString("base64"),
  iv: iv.toString("base64"),
  tag: cipher.getAuthTag().toString("base64"),
  ciphertext: ciphertext.toString("base64"),
};
fs.writeFileSync(outputPath, `${JSON.stringify(envelope)}\n`, { mode: 0o600, flag: "wx" });
