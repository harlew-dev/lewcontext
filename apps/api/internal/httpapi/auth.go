package httpapi

import (
	"database/sql"
	"errors"
	"forma/api/internal/auth"
	"forma/api/internal/store"
	"github.com/jackc/pgx/v5/pgconn"
	"net/http"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"
)

type credentials struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

func validCredentials(in *credentials) bool {
	in.Email = strings.ToLower(strings.TrimSpace(in.Email))
	in.Name = strings.TrimSpace(in.Name)
	address, err := mail.ParseAddress(in.Email)
	return err == nil && address.Address == in.Email && len(in.Email) <= 254 && utf8.RuneCountInString(in.Password) >= 12 && len(in.Password) <= 128
}
func (s *Server) accountLimit(w http.ResponseWriter, r *http.Request, email string) bool {
	allowed, err := s.Store.AllowAuth(r.Context(), "account:"+auth.Digest(email), 10)
	if err != nil {
		internalError(w, err)
		return false
	}
	if !allowed {
		w.Header().Set("Retry-After", "900")
		fail(w, 429, "Too many attempts. Try again in 15 minutes.")
		return false
	}
	return true
}
func (s *Server) register(w http.ResponseWriter, r *http.Request) {
	var in credentials
	if !decode(w, r, &in) {
		return
	}
	if !validCredentials(&in) || utf8.RuneCountInString(in.Name) < 1 || utf8.RuneCountInString(in.Name) > 80 {
		fail(w, 422, "Use a name, a valid email, and a password of 12–128 characters.")
		return
	}
	if !s.accountLimit(w, r, in.Email) {
		return
	}
	hash, err := auth.HashPassword(in.Password)
	if err != nil {
		internalError(w, err)
		return
	}
	user, err := s.Store.CreateUser(r.Context(), in.Name, in.Email, hash)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		fail(w, 409, "Unable to create this account. Try signing in or use a different email.")
		return
	}
	if err != nil {
		internalError(w, err)
		return
	}
	s.startSession(w, r, user, 201)
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var in credentials
	if !decode(w, r, &in) {
		return
	}
	if !validCredentials(&in) {
		fail(w, 401, "Email or password is incorrect.")
		return
	}
	if !s.accountLimit(w, r, in.Email) {
		return
	}
	user, hash, err := s.Store.FindUser(r.Context(), in.Email)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		internalError(w, err)
		return
	}
	if errors.Is(err, sql.ErrNoRows) {
		hash = s.dummyHash
	}
	valid := auth.CheckPassword(in.Password, hash)
	if !valid || err != nil {
		fail(w, 401, "Email or password is incorrect.")
		return
	}
	s.startSession(w, r, user, 200)
}
func (s *Server) startSession(w http.ResponseWriter, r *http.Request, u store.User, status int) {
	token, err := auth.Token()
	if err != nil {
		internalError(w, err)
		return
	}
	csrf, err := auth.Token()
	if err != nil {
		internalError(w, err)
		return
	}
	old := ""
	if cookie, err := r.Cookie(auth.CookieName(s.Config.Production)); err == nil {
		old = auth.Digest(cookie.Value)
	}
	if err = s.Store.CreateSession(r.Context(), u.ID, auth.Digest(token), csrf, old, time.Now().Add(auth.SessionLifetime)); err != nil {
		internalError(w, err)
		return
	}
	http.SetCookie(w, auth.Cookie(token, s.Config.Production))
	jsonResponse(w, status, map[string]any{"user": u, "csrfToken": csrf})
}
func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	session := current(r)
	jsonResponse(w, 200, map[string]any{"user": session.User, "csrfToken": session.CSRF, "expiresAt": session.ExpiresAt, "signedInAt": session.CreatedAt})
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	cookie, _ := r.Cookie(auth.CookieName(s.Config.Production))
	if err := s.Store.DeleteSession(r.Context(), auth.Digest(cookie.Value)); err != nil {
		internalError(w, err)
		return
	}
	expired := auth.Cookie("", s.Config.Production)
	expired.MaxAge = -1
	expired.Expires = time.Unix(0, 0)
	http.SetCookie(w, expired)
	w.WriteHeader(204)
}
func (s *Server) revokeOthers(w http.ResponseWriter, r *http.Request) {
	cookie, _ := r.Cookie(auth.CookieName(s.Config.Production))
	if err := s.Store.DeleteOtherSessions(r.Context(), current(r).User.ID, auth.Digest(cookie.Value)); err != nil {
		internalError(w, err)
		return
	}
	w.WriteHeader(204)
}
