// Package secretbox seals small secrets (the Cloudflare secret of a member) so that the database holds no plain text.
// AES-256-GCM with a key that lives in the backend's environment, never in the database. A sealed value is bound to a
// context (the address of the member), so a blob copied to another member's row does not open.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// Box seals and opens values with one key.
type Box struct {
	aead cipher.AEAD
}

// New reads a key given as base64 (standard or URL alphabet, padded or not) of exactly 32 bytes.
func New(key string) (*Box, error) {
	key = strings.TrimSpace(key)
	var raw []byte
	var err error
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if raw, err = enc.DecodeString(key); err == nil {
			break
		}
	}
	if err != nil {
		return nil, errors.New("the key is not base64")
	}
	if len(raw) != 32 {
		return nil, fmt.Errorf("the key must be 32 bytes, got %d", len(raw))
	}
	block, err := aes.NewCipher(raw)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead}, nil
}

// Seal encrypts plain; context is authenticated but not stored. The result is the nonce followed by the ciphertext.
func (b *Box) Seal(plain, context string) ([]byte, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	return b.aead.Seal(nonce, nonce, []byte(plain), []byte(context)), nil
}

// Open decrypts a sealed value; it fails for another context, another key or a changed value.
func (b *Box) Open(sealed []byte, context string) (string, error) {
	n := b.aead.NonceSize()
	if len(sealed) < n+b.aead.Overhead() {
		return "", errors.New("the sealed value is too short")
	}
	plain, err := b.aead.Open(nil, sealed[:n], sealed[n:], []byte(context))
	if err != nil {
		return "", errors.New("the sealed value does not open")
	}
	return string(plain), nil
}
