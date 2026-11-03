package workspace

import (
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/config"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/httpapi/input"
	"lewcontext/api/internal/store"
	"net/http"
	"regexp"
)

var idempotencyValue = regexp.MustCompile(`^[A-Za-z0-9._~-]{16,128}$`)

// ApiProjects is the stable v1 project resource, using credential-scoped storage.
type ApiProjects struct{ *httpapi.Server }

type apiProjectInput struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Status      string `json:"status"`
	Color       string `json:"color"`
}

func decodeApiProject(w http.ResponseWriter, r *http.Request) (store.ProjectInput, bool) {
	var body apiProjectInput
	if !httpapi.Decode(w, r, &body) {
		return store.ProjectInput{}, false
	}
	in := store.ProjectInput{Name: body.Name, Description: body.Description, Status: body.Status, Color: body.Color}
	field, message, ok := input.ValidProject(&in)
	if !ok {
		if field == "" {
			httpapi.Fail(w, 422, message)
		} else {
			httpapi.FailField(w, 422, field, message)
		}
	}
	return in, ok
}

func (h ApiProjects) Index(w http.ResponseWriter, r *http.Request) {
	params, ok := input.Query(r, "page", "perPage", "q", "status", "sort")
	if !ok {
		httpapi.Fail(w, 400, "Invalid query parameters.")
		return
	}
	req, ok := input.Page(params)
	if !ok {
		httpapi.Fail(w, 400, "Invalid page.")
		return
	}
	f, ok := input.ProjectFilter(params)
	if !ok {
		httpapi.Fail(w, 400, "Check the search, status, and sort.")
		return
	}
	page, err := h.Repo.IndexApiProjects(r.Context(), httpapi.ApiKey(r), f, req)
	if h.AnswerApiKeyError(w, r, err) {
		return
	}
	httpapi.JSON(w, 200, page)
}

func (h ApiProjects) Show(w http.ResponseWriter, r *http.Request) {
	p, err := h.Repo.FindApiProject(r.Context(), httpapi.ApiKey(r), r.PathValue("project"))
	if h.AnswerApiKeyError(w, r, err) {
		return
	}
	httpapi.JSON(w, 200, p)
}

func (h ApiProjects) Store(w http.ResponseWriter, r *http.Request) {
	in, ok := decodeApiProject(w, r)
	if !ok {
		return
	}
	values := r.Header.Values("Idempotency-Key")
	retry := ""
	if len(values) != 0 {
		if len(values) != 1 || !idempotencyValue.MatchString(values[0]) {
			httpapi.Fail(w, 400, "Use one Idempotency-Key of 16–128 ASCII letters, digits, dot, underscore, tilde or hyphen.")
			return
		}
		retry = auth.Digest(values[0])
	}
	a := httpapi.ApiKey(r)
	limit := store.ApiProjectCreateLimit{}
	if h.Config.RateLimits != config.RateLimitsOff {
		l := httpapi.ApiKeyProjectPerWorkspace
		limit = store.ApiProjectCreateLimit{Key: l.Key(a.WorkspacePublicID()), Maximum: l.Block.At(), Window: l.Window}
	}
	p, err := h.Repo.CreateApiProject(r.Context(), a, in, retry, limit)
	if h.AnswerApiKeyError(w, r, err) {
		return
	}
	httpapi.JSON(w, 201, p)
}

func (h ApiProjects) Update(w http.ResponseWriter, r *http.Request) {
	in, ok := decodeApiProject(w, r)
	if !ok {
		return
	}
	p, err := h.Repo.UpdateApiProject(r.Context(), httpapi.ApiKey(r), r.PathValue("project"), in)
	if h.AnswerApiKeyError(w, r, err) {
		return
	}
	httpapi.JSON(w, 200, p)
}

func (h ApiProjects) Destroy(w http.ResponseWriter, r *http.Request) {
	if h.AnswerApiKeyError(w, r, h.Repo.DeleteApiProject(r.Context(), httpapi.ApiKey(r), r.PathValue("project"))) {
		return
	}
	w.WriteHeader(204)
}
