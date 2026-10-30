package httpapi

import "net/http"

// Routes is the single registry of API endpoints and their authorization requirements.
func (s *Server) Routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/health", s.health)
	mux.Handle("POST /api/auth/register", s.limitAuth(http.HandlerFunc(s.register)))
	mux.Handle("POST /api/auth/login", s.limitAuth(http.HandlerFunc(s.login)))
	mux.Handle("GET /api/auth/me", s.requireSession(http.HandlerFunc(s.me)))
	mux.Handle("POST /api/auth/logout", s.requireSession(http.HandlerFunc(s.logout)))
	mux.Handle("DELETE /api/auth/sessions", s.requireSession(http.HandlerFunc(s.revokeOthers)))
	mux.Handle("GET /api/projects", s.requireSession(http.HandlerFunc(s.projects)))
	mux.Handle("POST /api/projects", s.requireSession(http.HandlerFunc(s.createProject)))
	mux.Handle("PATCH /api/projects/{id}", s.requireSession(http.HandlerFunc(s.updateProject)))
	return s.security(mux)
}
func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	if err := s.Store.DB.PingContext(r.Context()); err != nil {
		fail(w, 503, "Database unavailable")
		return
	}
	jsonResponse(w, 200, map[string]string{"status": "ok"})
}
