package secrets

import "testing"

func TestEncryptDecryptRoundTrip(t *testing.T) {
	box, err := New("test-secret-key")
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	plaintext := "root@pam!ferrum-token-secret-value"
	ciphertext, err := box.Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if ciphertext == plaintext {
		t.Fatal("ciphertext must not equal plaintext")
	}

	decrypted, err := box.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if decrypted != plaintext {
		t.Fatalf("decrypted = %q, want %q", decrypted, plaintext)
	}
}

func TestEncryptEmptyStringRoundTrips(t *testing.T) {
	box, err := New("test-secret-key")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ciphertext, err := box.Encrypt("")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if ciphertext != "" {
		t.Fatalf("Encrypt(\"\") = %q, want empty string", ciphertext)
	}
	decrypted, err := box.Decrypt(ciphertext)
	if err != nil {
		t.Fatalf("Decrypt: %v", err)
	}
	if decrypted != "" {
		t.Fatalf("Decrypt(\"\") = %q, want empty string", decrypted)
	}
}

func TestDecryptWithWrongKeyFails(t *testing.T) {
	boxA, _ := New("key-a")
	boxB, _ := New("key-b")

	ciphertext, err := boxA.Encrypt("sensitive value")
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if _, err := boxB.Decrypt(ciphertext); err == nil {
		t.Fatal("expected Decrypt with wrong key to fail, got nil error")
	}
}

func TestNewRejectsEmptySecret(t *testing.T) {
	if _, err := New(""); err == nil {
		t.Fatal("expected New(\"\") to return an error")
	}
}
