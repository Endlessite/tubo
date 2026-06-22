package main

import (
	"crypto/rand"
	"math/big"
)
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
