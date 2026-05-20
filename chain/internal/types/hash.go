package types

import (
	"crypto/sha256"
	"encoding/hex"
)

// Sha256 returns the SHA-256 digest of in.
func Sha256(in []byte) [32]byte { return sha256.Sum256(in) }

// Sha256Hex returns SHA-256 of in as lowercase hex.
func Sha256Hex(in []byte) string {
	h := sha256.Sum256(in)
	return hex.EncodeToString(h[:])
}
