package common

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
)

const encryptedSecretPrefix = "enc:v1:"

func EncryptSecret(plaintext, purpose string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	gcm, err := secretGCM(purpose)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("generate secret nonce: %w", err)
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plaintext), []byte(purpose))
	return encryptedSecretPrefix + base64.RawURLEncoding.EncodeToString(sealed), nil
}

func DecryptSecret(ciphertext, purpose string) (string, error) {
	if ciphertext == "" {
		return "", nil
	}
	if !strings.HasPrefix(ciphertext, encryptedSecretPrefix) {
		return "", fmt.Errorf("unsupported encrypted secret format")
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(ciphertext, encryptedSecretPrefix))
	if err != nil {
		return "", fmt.Errorf("decode encrypted secret: %w", err)
	}
	gcm, err := secretGCM(purpose)
	if err != nil {
		return "", err
	}
	if len(payload) < gcm.NonceSize() {
		return "", fmt.Errorf("encrypted secret payload is too short")
	}
	nonce, encrypted := payload[:gcm.NonceSize()], payload[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, encrypted, []byte(purpose))
	if err != nil {
		return "", fmt.Errorf("decrypt secret: %w", err)
	}
	return string(plaintext), nil
}

func IsEncryptedSecret(value string) bool {
	return strings.HasPrefix(value, encryptedSecretPrefix)
}

func secretGCM(purpose string) (cipher.AEAD, error) {
	if strings.TrimSpace(purpose) == "" {
		return nil, fmt.Errorf("secret purpose is required")
	}
	key := sha256.Sum256([]byte("new-api-secret:v1\x00" + purpose + "\x00" + CryptoSecret))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, fmt.Errorf("create secret cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("create secret cipher mode: %w", err)
	}
	return gcm, nil
}
