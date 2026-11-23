package adminapi

import (
	"errors"
	"regexp"
	"unicode"
	"unicode/utf8"
)

// PasswordReset is transient credential material. Never log or persist this
// request. The game stores only its fingerprint and a non-secret receipt.
type PasswordReset struct {
	RequestID string `json:"requestId"`
	Account   string `json:"account"`
	Password  string `json:"password"`
}

type PasswordResetResult struct {
	RequestID string `json:"requestId"`
	AccountID string `json:"accountId"`
	Account   string `json:"account"`
	ResetAt   int64  `json:"resetAt"`
}

var resetRequestPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{16,96}$`)
var resetAccountPattern = regexp.MustCompile(`^[A-Za-z0-9_]{3,20}$`)

// ValidatePassword mirrors the registration/change-password rules: a valid
// UTF-8 secret of 6~64 bytes with no control characters. This matches the game
// login paths (web accounts: <=64 bytes, local accounts: 8~72 bytes) so a
// custom password set here can always be used to log in.
func ValidatePassword(password string) error {
	if !utf8.ValidString(password) || len(password) < 6 || len(password) > 64 {
		return errors.New("invalid password length")
	}
	for _, c := range password {
		if unicode.IsControl(c) {
			return errors.New("password contains control character")
		}
	}
	return nil
}

func (r PasswordReset) Validate() error {
	if !resetRequestPattern.MatchString(r.RequestID) || !resetAccountPattern.MatchString(r.Account) {
		return errors.New("invalid password reset request")
	}
	return ValidatePassword(r.Password)
}
