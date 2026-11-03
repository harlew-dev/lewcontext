package httpapi

import (
	"database/sql"
	"encoding/json"
	"errors"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/httpapi/input"
	"lewcontext/api/internal/store"
	"net/http"
	"slices"
	"strings"
	"unicode/utf8"
)

type apiKeyManagementInput struct {
	Name          string          `json:"name"`
	Workspace     string          `json:"workspace,omitempty"`
	ProjectAccess string          `json:"projectAccess"`
	Projects      []string        `json:"projects"`
	Scopes        []string        `json:"scopes"`
	ExpiresAt     json.RawMessage `json:"expiresAt"`
	Password      string          `json:"password,omitempty"`
	Code          string          `json:"code,omitempty"`
}

func (s *Server) apiKeyManager(r *http.Request, workspace bool) store.ApiKeyManager {
	m := store.ApiKeyManager{UserID: Session(r).User.ID, SessionHash: s.SessionDigest(r), RequireVerified: s.Config.RequireExisting}
	if workspace {
		m.WorkspaceID = Workspace(r).ID
	}
	return m
}

// IndexManagedApiKeys is shared transport for the account and workspace resources.
func (s *Server) IndexManagedApiKeys(w http.ResponseWriter, r *http.Request, workspace bool) {
	params, ok := input.Query(r, "page", "perPage", "q", "kind")
	if !ok {
		Fail(w, 400, "Invalid query parameters.")
		return
	}
	req, ok := input.Page(params)
	if !ok {
		Fail(w, 400, "Invalid page.")
		return
	}
	search := strings.TrimSpace(params["q"])
	if !input.Search(search) {
		Fail(w, 400, "Invalid search.")
		return
	}
	if params["kind"] != "" && (!workspace || !slices.Contains([]string{"personal", "workspace"}, params["kind"])) {
		Fail(w, 400, "Invalid key kind.")
		return
	}
	f := store.ApiKeyFilter{OwnerID: Session(r).User.ID, Search: search, Kind: params["kind"]}
	if workspace {
		f.WorkspaceID = Workspace(r).ID
	}
	page, err := s.Repo.IndexApiKeys(r.Context(), f, req)
	if s.AnswerApiKeyError(w, r, err) {
		return
	}
	JSON(w, 200, page)
}

// SaveManagedApiKey handles creation/update without caching a creation response.
func (s *Server) SaveManagedApiKey(w http.ResponseWriter, r *http.Request, workspace, creating bool) {
	var body apiKeyManagementInput
	if !Decode(w, r, &body) {
		return
	}
	body.Name = strings.TrimSpace(body.Name)
	if n := utf8.RuneCountInString(body.Name); n < 1 || n > 80 || !input.ValidText(body.Name, false) {
		FailField(w, 422, "name", "Use a name of 1–80 characters without invisible formatting characters.")
		return
	}
	if len(body.ExpiresAt) == 0 {
		FailField(w, 422, "expiresAt", "Choose an expiry, or Never.")
		return
	}
	in := store.ApiKeyInput{Name: body.Name, ProjectAccess: body.ProjectAccess, Projects: body.Projects, Scopes: body.Scopes}
	if err := json.Unmarshal(body.ExpiresAt, &in.ExpiresAt); err != nil {
		FailField(w, 422, "expiresAt", "Choose a valid expiry.")
		return
	}
	if len(in.Projects) > 100 || len(in.Scopes) > 5 {
		FailField(w, 422, "projects", "Select up to 100 projects and supported permissions.")
		return
	}
	for _, id := range in.Projects {
		if !input.PublicID("prj", id) {
			FailField(w, 422, "projects", "Select accessible projects in this workspace.")
			return
		}
	}
	if workspace {
		if body.Workspace != "" {
			Fail(w, 400, "Workspace is determined by the route.")
			return
		}
		body.Workspace = Workspace(r).PublicID
	} else if creating && !input.PublicID("wsp", body.Workspace) {
		FailField(w, 422, "workspace", "Choose a workspace.")
		return
	} else if !creating && body.Workspace != "" {
		Fail(w, 400, "A key's workspace cannot change.")
		return
	}
	if !creating && !input.PublicID("apk", r.PathValue("key")) {
		NotFound(w, r)
		return
	}
	u := Session(r).User
	checked := false
	confirmed := store.PasswordConfirmed{}
	proof := store.ApiKeyProof{ExpectedMFA: u.MFAEnabledAt != nil, Checked: &checked, Confirmed: &confirmed}
	recordProof := func(err error) error {
		if !checked || (err != nil && !errors.Is(err, store.ErrApiKeyProof) && !errors.Is(err, store.ErrWrongCode)) {
			return nil
		}
		if confirmed.MFA {
			return s.RecordMFACheck(r, u, proof.Factor, "api_key", err == nil, confirmed.FactorUsed)
		}
		return s.RecordPasswordCheck(r, u, err == nil, "api_key")
	}
	if creating || body.Password != "" || body.Code != "" {
		release, ok := s.authSlot(w, r)
		if !ok {
			return
		}
		defer release()
		if u.MFAEnabledAt != nil {
			if !s.Allow(w, r, MFAPerAccount, MFAPerAccount.Key(u.PublicID)) {
				return
			}
		} else if !s.AccountLimit(w, r, u.Email) {
			return
		}
		proof.Factor = s.SecondFactor(r.Context(), u, body.Code)
		proof.Password = func(hash string) error {
			if !PasswordInBounds(body.Password) || !auth.CheckPassword(body.Password, hash) {
				return store.ErrApiKeyProof
			}
			return nil
		}
	}
	manager := s.apiKeyManager(r, workspace)
	if creating {
		key, secret, err := s.Repo.CreateApiKey(r.Context(), manager, body.Workspace, in, proof)
		if auditErr := recordProof(err); auditErr != nil {
			InternalError(w, r, auditErr)
			return
		}
		if s.AnswerApiKeyError(w, r, err) {
			return
		}
		JSON(w, 201, struct {
			Key    store.ApiKey `json:"key"`
			Secret string       `json:"secret"`
		}{key, secret})
		return
	}
	key, err := s.Repo.UpdateApiKey(r.Context(), manager, r.PathValue("key"), in, proof)
	if auditErr := recordProof(err); auditErr != nil {
		InternalError(w, r, auditErr)
		return
	}
	if s.AnswerApiKeyError(w, r, err) {
		return
	}
	JSON(w, 200, key)
}

func (s *Server) RevokeManagedApiKey(w http.ResponseWriter, r *http.Request, workspace bool) {
	if !input.PublicID("apk", r.PathValue("key")) {
		NotFound(w, r)
		return
	}
	if s.AnswerApiKeyError(w, r, s.Repo.RevokeApiKey(r.Context(), s.apiKeyManager(r, workspace), r.PathValue("key"))) {
		return
	}
	w.WriteHeader(204)
}

// AnswerApiKeyError shares transport errors without treating a bad factor as expiry.
func (s *Server) AnswerApiKeyError(w http.ResponseWriter, r *http.Request, err error) bool {
	var quota store.QuotaError
	var limited store.ApiProjectLimited
	switch {
	case err == nil:
		return false
	case errors.Is(err, store.ErrApiKeyInvalid):
		if _, ok := OptionalApiKey(r); ok {
			denyApiKey(w)
		} else {
			Fail(w, 401, SessionExpired)
		}
	case errors.Is(err, store.ErrApiKeyForbidden):
		Fail(w, 403, "You don’t have permission to do that in this workspace.")
	case errors.Is(err, sql.ErrNoRows):
		NotFound(w, r)
	case errors.Is(err, store.ErrApiKeyEnded):
		Fail(w, 409, "This key has expired or been revoked. Create a replacement.")
	case errors.Is(err, store.ErrApiKeyGrants):
		FailField(w, 422, "scopes", "Choose permitted actions and accessible projects in this workspace. Selected-project keys cannot create projects.")
	case errors.Is(err, store.ErrApiKeyExpiry):
		FailField(w, 422, "expiresAt", "Use Never or a finite expiry within 1 hour–365 days of creation. Existing expiry cannot be extended.")
	case errors.Is(err, store.ErrApiKeyProof), errors.Is(err, store.ErrWrongCode):
		if Session(r).User.MFAEnabledAt != nil {
			FailField(w, 422, "code", WrongMFACode)
		} else {
			FailField(w, 422, "password", WrongCurrentPassword)
		}
	case errors.As(err, &quota):
		Fail(w, 422, "Limit reached: "+quota.Error()+". Revoke a key to free a slot before replacing it; this may interrupt its integrations.")
	case errors.Is(err, store.ErrApiReplayConflict):
		Fail(w, 409, "This retry conflicts with the original project creation.")
	case errors.Is(err, store.ErrApiReplayCapacity):
		w.Header().Set("Retry-After", "60")
		Fail(w, 503, "Project retry storage is busy. Try again shortly with the same Idempotency-Key.")
	case errors.As(err, &limited):
		RefuseLimit(w, ApiKeyProjectPerWorkspace, limited.ResetsAt)
	default:
		InternalError(w, r, err)
	}
	return true
}
