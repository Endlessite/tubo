package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"hash"
	"math/big"
)

// keyLength is the number of characters in the E2EE part of the token.
// 22 characters from a 62-symbol alphabet give ~131 bits of entropy.
const keyLength = 22

// kdfIterations must match the -iter value used by run.sh.
const kdfIterations = 10000

func generateRandomKey(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	max := big.NewInt(int64(len(charset)))
	result := make([]byte, length)
	for i := range result {
		n, err := rand.Int(rand.Reader, max)
		if err != nil {
			fatal("Failed to generate random key: ", err)
		}
		result[i] = charset[n.Int64()]
	}
	return string(result)
}

// deriveCipherParams returns the AES-256 key and CTR IV for an E2EE key.
// It is equivalent to:
//
//	openssl enc -aes-256-ctr -pbkdf2 -iter 10000 -md sha256 -nosalt -pass env:KEY
//
// which is what run.sh uses, so the shell script and the CLI interoperate.
func deriveCipherParams(key string) (aesKey, iv []byte) {
	out, err := pbkdf2.Key(sha256.New, key, nil, kdfIterations, 48)
	if err != nil {
		fatal("Key derivation failed: ", err)
	}
	return out[:32], out[32:48]
}

func newCipherStream(key string) cipher.Stream {
	aesKey, iv := deriveCipherParams(key)
	block, err := aes.NewCipher(aesKey)
	if err != nil {
		fatal("Failed to initialize AES cipher: ", err)
	}
	return cipher.NewCTR(block, iv)
}

// newMAC returns an HMAC-SHA256 over the ciphertext, keyed independently
// from the encryption key. The relay never sees the MAC key.
func newMAC(key string) hash.Hash {
	macKey := sha256.Sum256([]byte("tubo-mac-v2:" + key))
	return hmac.New(sha256.New, macKey[:])
}
