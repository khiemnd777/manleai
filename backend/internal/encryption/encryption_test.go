package encryption

import "testing"

func TestTokenCipherRoundTrip(t *testing.T) {
	cipher, err := NewTokenCipher("local-development-token-encryption-key")
	if err != nil {
		t.Fatalf("cipher init failed: %v", err)
	}

	encrypted, err := cipher.Encrypt("square-access-token")
	if err != nil {
		t.Fatalf("encrypt failed: %v", err)
	}
	if encrypted == "square-access-token" {
		t.Fatalf("encrypted token should not match plaintext")
	}

	decrypted, err := cipher.Decrypt(encrypted)
	if err != nil {
		t.Fatalf("decrypt failed: %v", err)
	}
	if decrypted != "square-access-token" {
		t.Fatalf("unexpected plaintext: %s", decrypted)
	}
}
