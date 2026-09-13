package auth

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/png"
	"time"

	"github.com/google/uuid"
	"github.com/pquerna/otp/totp"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrTOTPAlreadyEnabled = errors.New("two-factor authentication is already enabled")
	ErrTOTPNotEnabled     = errors.New("two-factor authentication is not enabled")
	ErrInvalidTOTPCode    = errors.New("invalid authentication code")
)

const recoveryCodeCount = 10

// TOTPEnrollment is the pending secret + QR code shown during setup, before
// the user has proven possession of it by entering a valid code.
type TOTPEnrollment struct {
	Secret     string
	OTPAuthURL string
	QRCodePNG  []byte // PNG image bytes; caller base64-encodes for the API response
}

// encryptStoredSecret/decryptStoredSecret wrap the secrets box around the
// users.totp_secret column. A nil box means legacy plaintext storage, and
// readers also fall back to the raw value when decryption fails, so rows
// written before encryption was introduced keep working until the next
// enrollment re-encrypts them.
func (s *Service) encryptStoredSecret(v string) (string, error) {
	if s.secrets == nil {
		return v, nil
	}
	return s.secrets.Encrypt(v)
}

func (s *Service) decryptStoredSecret(v string) string {
	if s.secrets == nil || v == "" {
		return v
	}
	if plain, err := s.secrets.Decrypt(v); err == nil {
		return plain
	}
	return v
}

// EnrollTOTP generates a new secret for the user and stores it (disabled)
// pending confirmation via ConfirmTOTP. Re-enrolling overwrites any prior
// unconfirmed secret.
func (s *Service) EnrollTOTP(ctx context.Context, userID, username string) (*TOTPEnrollment, error) {
	var enabled int
	if err := s.db.QueryRowContext(ctx, `SELECT totp_enabled FROM users WHERE id = ?`, userID).Scan(&enabled); err != nil {
		return nil, err
	}
	if enabled == 1 {
		return nil, ErrTOTPAlreadyEnabled
	}

	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      "Ferrum",
		AccountName: username,
	})
	if err != nil {
		return nil, err
	}

	secretEnc, err := s.encryptStoredSecret(key.Secret())
	if err != nil {
		return nil, err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE users SET totp_secret = ? WHERE id = ?`, secretEnc, userID); err != nil {
		return nil, err
	}

	img, err := key.Image(240, 240)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}

	return &TOTPEnrollment{Secret: key.Secret(), OTPAuthURL: key.URL(), QRCodePNG: buf.Bytes()}, nil
}

// ConfirmTOTP verifies the user entered a valid code for their pending
// secret, enables TOTP, and generates one-time recovery codes (returned in
// cleartext exactly once — only their bcrypt hash is persisted).
func (s *Service) ConfirmTOTP(ctx context.Context, userID, code string) ([]string, error) {
	var stored string
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(totp_secret, '') FROM users WHERE id = ?`, userID).Scan(&stored); err != nil {
		return nil, err
	}
	secret := s.decryptStoredSecret(stored)
	if secret == "" || !totp.Validate(code, secret) {
		return nil, ErrInvalidTOTPCode
	}

	// bcrypt deliberately runs before the transaction begins: hashing ten
	// recovery codes holds the CPU long enough to starve SQLite's single
	// connection if done while the tx holds it.
	codes := make([]string, recoveryCodeCount)
	hashes := make([]string, recoveryCodeCount)
	for i := range codes {
		raw := randomToken()[:10]
		codes[i] = fmt.Sprintf("%s-%s", raw[:5], raw[5:])
		hash, err := bcrypt.GenerateFromPassword([]byte(codes[i]), bcrypt.DefaultCost)
		if err != nil {
			return nil, err
		}
		hashes[i] = string(hash)
	}

	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`UPDATE users SET totp_enabled = 1 WHERE id = ?`, userID); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(`DELETE FROM user_totp_recovery_codes WHERE user_id = ?`, userID); err != nil {
		return nil, err
	}
	for i := range codes {
		if _, err := tx.Exec(
			`INSERT INTO user_totp_recovery_codes (id, user_id, code_hash) VALUES (?, ?, ?)`,
			uuid.NewString(), userID, hashes[i],
		); err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return codes, nil
}

// DisableTOTP turns off two-factor auth and removes recovery codes.
func (s *Service) DisableTOTP(ctx context.Context, userID string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE users SET totp_enabled = 0, totp_secret = NULL WHERE id = ?`, userID)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM user_totp_recovery_codes WHERE user_id = ?`, userID)
	return err
}

// VerifyTOTPStep checks a 6-digit TOTP code or an unused recovery code for
// the given (already password-verified) user during login.
func (s *Service) VerifyTOTPStep(ctx context.Context, userID, code string) error {
	var stored string
	if err := s.db.QueryRowContext(ctx, `SELECT COALESCE(totp_secret, '') FROM users WHERE id = ?`, userID).Scan(&stored); err != nil {
		return err
	}
	if secret := s.decryptStoredSecret(stored); secret != "" && totp.Validate(code, secret) {
		return nil
	}
	return s.consumeRecoveryCode(ctx, userID, code)
}

func (s *Service) consumeRecoveryCode(ctx context.Context, userID, code string) error {
	rows, err := s.db.QueryContext(ctx, `SELECT id, code_hash FROM user_totp_recovery_codes WHERE user_id = ? AND used_at IS NULL`, userID)
	if err != nil {
		return err
	}
	type candidate struct{ id, hash string }
	var candidates []candidate
	for rows.Next() {
		var c candidate
		if err := rows.Scan(&c.id, &c.hash); err != nil {
			rows.Close()
			return err
		}
		candidates = append(candidates, c)
	}
	rows.Close()

	for _, c := range candidates {
		if bcrypt.CompareHashAndPassword([]byte(c.hash), []byte(code)) == nil {
			// The used_at IS NULL guard makes consumption safe against a
			// double submit: two requests can both read the unused code, but
			// the loser's UPDATE matches zero rows and must not report success.
			res, err := s.db.ExecContext(ctx, `UPDATE user_totp_recovery_codes SET used_at = ? WHERE id = ? AND used_at IS NULL`, time.Now().UTC().Format(time.RFC3339), c.id)
			if err != nil {
				return err
			}
			if n, _ := res.RowsAffected(); n == 0 {
				return ErrInvalidTOTPCode
			}
			return nil
		}
	}
	return ErrInvalidTOTPCode
}

// TOTPStatus reports whether TOTP is enabled and how many unused recovery codes remain.
func (s *Service) TOTPStatus(ctx context.Context, userID string) (enabled bool, remainingCodes int, err error) {
	var e int
	if err = s.db.QueryRowContext(ctx, `SELECT totp_enabled FROM users WHERE id = ?`, userID).Scan(&e); err != nil {
		return false, 0, err
	}
	enabled = e == 1
	if !enabled {
		return false, 0, nil
	}
	err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM user_totp_recovery_codes WHERE user_id = ? AND used_at IS NULL`, userID).Scan(&remainingCodes)
	return enabled, remainingCodes, err
}
