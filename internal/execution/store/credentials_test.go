// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package store

import "testing"

func TestEncryptRoundTrip(t *testing.T) {
	SetEncryptionKey("test-master-key")
	secrets := []string{"sk-ant-12345", "", "xoxb-token-with-üñïçødé", "a very long secret " + string(make([]byte, 500))}
	for _, s := range secrets {
		enc, err := encryptSecret(s)
		if err != nil {
			t.Fatalf("encrypt %q: %v", s, err)
		}
		if enc == s && s != "" {
			t.Fatalf("ciphertext equals plaintext for %q", s)
		}
		dec, err := decryptSecret(enc)
		if err != nil {
			t.Fatalf("decrypt: %v", err)
		}
		if dec != s {
			t.Fatalf("roundtrip mismatch: got %q want %q", dec, s)
		}
	}
}

func TestEncryptNonceUnique(t *testing.T) {
	SetEncryptionKey("k")
	a, _ := encryptSecret("same")
	b, _ := encryptSecret("same")
	if a == b {
		t.Fatal("expected different ciphertexts for same plaintext (random nonce)")
	}
}

func TestDecryptWrongKeyFails(t *testing.T) {
	SetEncryptionKey("key-one")
	enc, _ := encryptSecret("secret")
	SetEncryptionKey("key-two")
	if _, err := decryptSecret(enc); err == nil {
		t.Fatal("expected decryption to fail with a different key")
	}
}
