package httpapi

import (
	"net/http"
	"strings"
	"unicode"
	"unicode/utf8"
)

const invalidProfile = "Use a name of 1–80 characters."

// profileInput is what a user can change about their account. The email
// address can't change: it's how they sign in.
type profileInput struct {
	Name string `json:"name"`
}

// validProfile trims the name and requires 1–80 characters without control
// characters, matching the column's CHECK.
func validProfile(in *profileInput) bool {
	in.Name = strings.TrimSpace(in.Name)
	n := utf8.RuneCountInString(in.Name)
	return n >= 1 && n <= 80 && !strings.ContainsFunc(in.Name, unicode.IsControl)
}
func (s *Server) updateProfile(w http.ResponseWriter, r *http.Request) {
	in, ok := decodeValid(w, r, validProfile, invalidProfile)
	if !ok {
		return
	}
	user, err := s.Store.UpdateProfile(r.Context(), current(r).User, in.Name)
	if err != nil {
		internalError(w, err)
		return
	}
	jsonResponse(w, 200, user)
}
