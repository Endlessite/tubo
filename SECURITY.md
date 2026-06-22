# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in Tubo, **please do not open a public issue.**

Report it via email: **info@endlessite.com**

You will receive an acknowledgment within 48 hours.

## Threat Model

Tubo assumes the relay server is **untrusted**. Even if the server is compromised, an attacker cannot:

- Read transferred files (AES-256-CTR, key never leaves clients)
- Tamper with files undetected (SHA-256 checksum)
- Replay old transfers (single-use session IDs, `SecureRandom`)

The server *can* observe that a transfer is happening and its approximate size. It cannot see file contents or the encryption key.

## Crypto Overview

- **Key Derivation**: SHA-512 of the E2EE key
- **Encryption**: AES-256-CTR (first 32 bytes of hash = key, next 16 = IV)
- **Integrity**: SHA-256 checksum (Go CLI)
- **Session Tokens**: `SecureRandom` (Java) / `crypto/rand` (Go)
- **Password Comparison**: `MessageDigest.isEqual()` (constant-time)
