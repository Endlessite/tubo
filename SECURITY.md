# Security Policy

## Reporting a vulnerability

Please don't open a public issue. Write to **info@endlessite.com**; you should
get an answer within a few days.

## Threat model

The relay is treated as untrusted. With a compromised relay, an attacker:

- **cannot** read the contents of a transfer (AES-256-CTR, the key never
  leaves the two clients);
- **cannot** modify a transfer between two CLIs without the receiver noticing
  (HMAC-SHA256 over the ciphertext, checked before the file is saved);
- **can** modify a transfer that uses `run.sh` or plain `curl` on either side
  without the receiver noticing: `openssl enc` has no streaming authenticated
  mode, so that path only provides confidentiality;
- **can** see the file name, size, timing and the IP addresses of both peers;
- **can** refuse or drop transfers.

Anyone who has the full token can receive the file. There is no PAKE: the
token is a bearer secret and must be shared over a channel you trust.

On a shared machine, pass the token as `TUBO_TOKEN=...` rather than as an
argument, so other users can't read it with `ps`. Neither the CLI nor `run.sh`
puts the key or the session password on a command line of their own.

## Crypto overview

- **Key derivation**: PBKDF2-HMAC-SHA256(key, empty salt, 10000 iterations),
  48 bytes: first 32 are the AES key, last 16 the CTR IV. The key is 22 random
  characters (~131 bits), so the KDF is not what protects it. Each key is used
  for a single transfer, so the fixed IV is never reused with the same key.
- **Encryption**: AES-256-CTR.
- **Integrity (CLI to CLI)**: HMAC-SHA256 of the ciphertext, MAC key
  `SHA-256("tubo-mac-v2:" + key)`, compared in constant time.
- **Session IDs and passwords**: `SecureRandom` (Java); keys: `crypto/rand` (Go).
- **Password check on the relay**: `MessageDigest.isEqual()` (constant time).

This code has not been audited.
