package httpapi

// Emails and passwords: their shapes, and checking the signed-in user's.

import (
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/events"
	"lewcontext/api/internal/store"
	"net/http"
	"net/mail"
	"strings"
	"unicode/utf8"
)

type Credentials struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

// ValidCredentials normalises the email and checks the bounds login and
// register share. The 12-character minimum is a sign-up rule (StrongPassword),
// so accounts created another way, such as the development seed, can sign in.
func ValidCredentials(in *Credentials) bool {
	in.Name = strings.TrimSpace(in.Name)
	return ValidEmail(&in.Email) && PasswordInBounds(in.Password)
}

// ValidEmail trims and lowercases an email, then checks it is a bare address
// of at most 254 bytes.
func ValidEmail(email *string) bool {
	*email = strings.ToLower(strings.TrimSpace(*email))
	address, err := mail.ParseAddress(*email)
	return err == nil && address.Address == *email && len(*email) <= 254
}

// PasswordInBounds is the shape any password a request sends must have: not
// empty and at most 128 bytes, which also caps the hashing work.
func PasswordInBounds(password string) bool {
	return password != "" && len(password) <= 128
}

func StrongPassword(password string) bool {
	return utf8.RuneCountInString(password) >= 12
}

const WrongCurrentPassword = "Current password is incorrect."

// PasswordConfirmed checks that password is the signed-in user's, for the
// request step, counting against the account's sign-in limit and recording
// auth.password_checked. When it isn't, it answers 422 with field
// "password"; it returns false whenever it has answered. Routes using it run
// behind LimitAuth.
func (s *Server) PasswordConfirmed(w http.ResponseWriter, r *http.Request, password, step string) bool {
	if !PasswordInBounds(password) {
		FailField(w, 422, "password", WrongCurrentPassword)
		return false
	}
	user := Session(r).User
	if !s.AccountLimit(w, r, user.Email) {
		return false
	}
	hash, err := s.Repo.FindPasswordHash(r.Context(), user.ID)
	if err != nil {
		InternalError(w, r, err)
		return false
	}
	ok := auth.CheckPassword(password, hash)
	if err := s.RecordPasswordCheck(r, user, ok, step); err != nil {
		InternalError(w, r, err)
		return false
	}
	if !ok {
		FailField(w, 422, "password", WrongCurrentPassword)
	}
	return ok
}

// RecordPasswordCheck records auth.password_checked.
func (s *Server) RecordPasswordCheck(r *http.Request, user store.User, ok bool, step string) error {
	e := events.PasswordChecked{User: user.PublicID, OK: ok, Step: step}
	return s.Repo.Record(r.Context(), e, store.EventScope{UserID: user.ID})
}
