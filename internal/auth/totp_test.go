package auth

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"

	"ferrum/internal/config"
	"ferrum/internal/secrets"
	"ferrum/internal/store"
)

func bootstrapTestUser(t *testing.T, svc *Service) (userID string) {
	t.Helper()
	user, err := svc.Bootstrap(context.Background(), "admin", "admin@example.com", "correct-password")
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	return user.ID
}

// newTestServiceWithBox is newTestService with a real secrets box, plus the
// opened DB so tests can inspect what was actually persisted.
func newTestServiceWithBox(t *testing.T, box *secrets.Box) (*Service, *store.DB) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "ferrum.db")
	db, err := store.Open(config.DBConfig{Driver: "sqlite", Path: dbPath})
	if err != nil {
		t.Fatalf("store.Open: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	return NewService(db, box), db
}

func TestTOTPEnrollConfirmAndVerify(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	userID := bootstrapTestUser(t, svc)

	enrollment, err := svc.EnrollTOTP(ctx, userID, "admin")
	if err != nil {
		t.Fatalf("EnrollTOTP: %v", err)
	}
	if enrollment.Secret == "" || enrollment.OTPAuthURL == "" || len(enrollment.QRCodePNG) == 0 {
		t.Fatal("EnrollTOTP returned an incomplete enrollment")
	}

	// A second enrollment before confirming should succeed (overwrites the
	// pending secret) since TOTP isn't enabled yet.
	if _, err := svc.EnrollTOTP(ctx, userID, "admin"); err != nil {
		t.Fatalf("re-enrolling before confirmation should succeed: %v", err)
	}
	enrollment, err = svc.EnrollTOTP(ctx, userID, "admin")
	if err != nil {
		t.Fatalf("EnrollTOTP (final): %v", err)
	}

	if _, err := svc.ConfirmTOTP(ctx, userID, "000000"); err != ErrInvalidTOTPCode {
		t.Fatalf("ConfirmTOTP with a bogus code: got %v, want ErrInvalidTOTPCode", err)
	}

	code, err := totp.GenerateCode(enrollment.Secret, time.Now())
	if err != nil {
		t.Fatalf("generating a valid code: %v", err)
	}
	codes, err := svc.ConfirmTOTP(ctx, userID, code)
	if err != nil {
		t.Fatalf("ConfirmTOTP with a valid code: %v", err)
	}
	if len(codes) != recoveryCodeCount {
		t.Fatalf("got %d recovery codes, want %d", len(codes), recoveryCodeCount)
	}

	enabled, remaining, err := svc.TOTPStatus(ctx, userID)
	if err != nil {
		t.Fatalf("TOTPStatus: %v", err)
	}
	if !enabled {
		t.Fatal("expected TOTP to be enabled after ConfirmTOTP")
	}
	if remaining != recoveryCodeCount {
		t.Fatalf("remaining recovery codes = %d, want %d", remaining, recoveryCodeCount)
	}

	// A second enrollment attempt should now be rejected — TOTP is enabled.
	if _, err := svc.EnrollTOTP(ctx, userID, "admin"); err != ErrTOTPAlreadyEnabled {
		t.Fatalf("EnrollTOTP while enabled: got %v, want ErrTOTPAlreadyEnabled", err)
	}

	// Verifying login: a fresh code from the same secret must pass.
	loginCode, err := totp.GenerateCode(enrollment.Secret, time.Now())
	if err != nil {
		t.Fatalf("generating login code: %v", err)
	}
	if err := svc.VerifyTOTPStep(ctx, userID, loginCode); err != nil {
		t.Fatalf("VerifyTOTPStep with a valid TOTP code: %v", err)
	}

	// A recovery code should also work, and only once.
	recoveryCode := codes[0]
	if err := svc.VerifyTOTPStep(ctx, userID, recoveryCode); err != nil {
		t.Fatalf("VerifyTOTPStep with a valid recovery code: %v", err)
	}
	if err := svc.VerifyTOTPStep(ctx, userID, recoveryCode); err != ErrInvalidTOTPCode {
		t.Fatalf("reusing a recovery code: got %v, want ErrInvalidTOTPCode", err)
	}

	remaining--
	_, remaining2, err := svc.TOTPStatus(ctx, userID)
	if err != nil {
		t.Fatalf("TOTPStatus after consuming a recovery code: %v", err)
	}
	if remaining2 != remaining {
		t.Fatalf("remaining recovery codes after use = %d, want %d", remaining2, remaining)
	}

	if err := svc.DisableTOTP(ctx, userID); err != nil {
		t.Fatalf("DisableTOTP: %v", err)
	}
	enabled, remaining, err = svc.TOTPStatus(ctx, userID)
	if err != nil {
		t.Fatalf("TOTPStatus after disable: %v", err)
	}
	if enabled || remaining != 0 {
		t.Fatalf("after DisableTOTP: enabled=%v remaining=%d, want false/0", enabled, remaining)
	}
}

func TestVerifyTOTPStepRejectsGarbage(t *testing.T) {
	svc := newTestService(t)
	ctx := context.Background()
	userID := bootstrapTestUser(t, svc)

	enrollment, err := svc.EnrollTOTP(ctx, userID, "admin")
	if err != nil {
		t.Fatalf("EnrollTOTP: %v", err)
	}
	code, _ := totp.GenerateCode(enrollment.Secret, time.Now())
	if _, err := svc.ConfirmTOTP(ctx, userID, code); err != nil {
		t.Fatalf("ConfirmTOTP: %v", err)
	}

	if err := svc.VerifyTOTPStep(ctx, userID, "not-a-code"); err != ErrInvalidTOTPCode {
		t.Fatalf("VerifyTOTPStep(garbage): got %v, want ErrInvalidTOTPCode", err)
	}
}

// TestTOTPSecretRoundTripAndLegacyPlaintextFallback covers both directions of
// the at-rest migration: a secret enrolled with a secrets box must not sit in
// the column in plaintext (yet still verify on read), and a row written by an
// older plaintext build must keep working once a box exists.
func TestTOTPSecretRoundTripAndLegacyPlaintextFallback(t *testing.T) {
	box, err := secrets.New("totp-roundtrip-test-secret")
	if err != nil {
		t.Fatalf("secrets.New: %v", err)
	}
	svc, db := newTestServiceWithBox(t, box)
	ctx := context.Background()
	userID := bootstrapTestUser(t, svc)

	enrollment, err := svc.EnrollTOTP(ctx, userID, "admin")
	if err != nil {
		t.Fatalf("EnrollTOTP: %v", err)
	}
	code, err := totp.GenerateCode(enrollment.Secret, time.Now())
	if err != nil {
		t.Fatalf("generating code: %v", err)
	}
	if _, err := svc.ConfirmTOTP(ctx, userID, code); err != nil {
		t.Fatalf("ConfirmTOTP: %v", err)
	}

	var stored string
	if err := db.QueryRow(`SELECT totp_secret FROM users WHERE id = ?`, userID).Scan(&stored); err != nil {
		t.Fatalf("reading stored secret: %v", err)
	}
	if stored == enrollment.Secret {
		t.Fatal("TOTP secret must not be stored in plaintext when a secrets box is configured")
	}
	loginCode, err := totp.GenerateCode(enrollment.Secret, time.Now())
	if err != nil {
		t.Fatalf("generating login code: %v", err)
	}
	if err := svc.VerifyTOTPStep(ctx, userID, loginCode); err != nil {
		t.Fatalf("VerifyTOTPStep with an encrypted-at-rest secret: %v", err)
	}

	// A row written before encryption stored the plaintext secret.
	legacySecret := "JBSWY3DPEHPK3PXP"
	if _, err := db.Exec(`UPDATE users SET totp_secret = ? WHERE id = ?`, legacySecret, userID); err != nil {
		t.Fatalf("seeding legacy plaintext secret: %v", err)
	}
	legacyCode, err := totp.GenerateCode(legacySecret, time.Now())
	if err != nil {
		t.Fatalf("generating legacy code: %v", err)
	}
	if err := svc.VerifyTOTPStep(ctx, userID, legacyCode); err != nil {
		t.Fatalf("VerifyTOTPStep with a legacy plaintext secret: %v", err)
	}
}
