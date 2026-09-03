package auth

import (
	"context"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

func bootstrapTestUser(t *testing.T, svc *Service) (userID string) {
	t.Helper()
	user, err := svc.Bootstrap(context.Background(), "admin", "admin@example.com", "correct-password")
	if err != nil {
		t.Fatalf("Bootstrap: %v", err)
	}
	return user.ID
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
