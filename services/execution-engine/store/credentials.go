package store

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"time"
)

// Credential storage with encryption at rest.
//
// Connector secrets (Slack tokens, SendGrid keys, etc.) are stored AES-256-GCM
// encrypted using a key derived from KNOTT_SECRET_KEY. The API layer never returns
// decrypted values — only key names and a "configured" flag — so secrets cannot
// leak back through the UI. The executor decrypts on demand at runtime.

var encryptionKey [32]byte

// SetEncryptionKey derives the 32-byte AES key from the operator-supplied secret.
// An empty secret still yields a deterministic key (obfuscation), but callers
// should warn the operator to set KNOTT_SECRET_KEY for real protection.
func SetEncryptionKey(secret string) {
	encryptionKey = sha256.Sum256([]byte("knott:credentials:v1:" + secret))
}

func encryptSecret(plaintext string) (string, error) {
	block, err := aes.NewCipher(encryptionKey[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ct := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ct), nil
}

func decryptSecret(encoded string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(encryptionKey[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", fmt.Errorf("ciphertext too short")
	}
	nonce, ct := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	pt, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(pt), nil
}

// Credential is the public (non-secret) view of a stored credential.
type Credential struct {
	Name       string    `json:"name"`
	Configured bool      `json:"configured"`
	UpdatedAt  time.Time `json:"updated_at"`
}

func (s *DB) migrateCredentials() error {
	_, err := s.db.Exec(`
		CREATE TABLE IF NOT EXISTS credentials (
			name TEXT PRIMARY KEY,
			value_encrypted TEXT NOT NULL,
			updated_at TEXT DEFAULT (datetime('now'))
		);
	`)
	return err
}

// SetCredential stores (or replaces) an encrypted secret by name.
func (s *DB) SetCredential(name, plaintext string) error {
	enc, err := encryptSecret(plaintext)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(
		`INSERT INTO credentials (name, value_encrypted, updated_at) VALUES (?,?,datetime('now'))
		 ON CONFLICT(name) DO UPDATE SET value_encrypted=excluded.value_encrypted, updated_at=datetime('now')`,
		name, enc)
	return err
}

// GetCredential returns the decrypted secret for a name, if present.
func (s *DB) GetCredential(name string) (string, bool) {
	var enc string
	if err := s.db.QueryRow(`SELECT value_encrypted FROM credentials WHERE name=?`, name).Scan(&enc); err != nil {
		return "", false
	}
	pt, err := decryptSecret(enc)
	if err != nil {
		return "", false
	}
	return pt, true
}

// ListCredentials returns names + configured flag only (never values).
func (s *DB) ListCredentials() ([]*Credential, error) {
	rows, err := s.db.Query(`SELECT name, updated_at FROM credentials ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Credential
	for rows.Next() {
		c := &Credential{Configured: true}
		var ts string
		if err := rows.Scan(&c.Name, &ts); err != nil {
			continue
		}
		c.UpdatedAt, _ = time.Parse(time.DateTime, ts)
		out = append(out, c)
	}
	return out, nil
}

// DeleteCredential removes a stored secret.
func (s *DB) DeleteCredential(name string) error {
	_, err := s.db.Exec(`DELETE FROM credentials WHERE name=?`, name)
	return err
}
