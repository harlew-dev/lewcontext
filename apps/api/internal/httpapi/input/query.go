// Package input checks what a client sends in a request's URL: query
// parameters, paging, public IDs, search terms and the shape of an emailed
// link's token. Each check answers ok or not; the caller picks the response.
package input

import (
	"lewcontext/api/internal/store"
	"net/http"
	"net/url"
	"slices"
	"strconv"
)

// Query returns the single value of each allowed key in the query
// string. Malformed encoding, unknown keys and repeated keys are rejected.
func Query(r *http.Request, allowed ...string) (map[string]string, bool) {
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return nil, false
	}
	params := make(map[string]string, len(values))
	for key, list := range values {
		if !slices.Contains(allowed, key) || len(list) != 1 {
			return nil, false
		}
		params[key] = list[0]
	}
	return params, true
}

// Page reads "page" and "perPage", defaulting to the first page.
func Page(params map[string]string) (store.PageRequest, bool) {
	page, ok := Int(params["page"], 1, store.MaxPage)
	if !ok {
		return store.PageRequest{}, false
	}
	perPage, ok := Int(params["perPage"], store.DefaultPerPage, store.MaxPerPage)
	if !ok {
		return store.PageRequest{}, false
	}
	return store.PageRequest{Page: page, PerPage: perPage}, true
}

// Int parses plain decimal digits in [1, limit]; "" means fallback.
// Signs, spaces and exponents are rejected rather than coerced.
func Int(raw string, fallback, limit int) (int, bool) {
	if raw == "" {
		return fallback, true
	}
	if len(raw) > len(strconv.Itoa(limit)) {
		return 0, false
	}
	for _, c := range raw {
		if c < '0' || c > '9' {
			return 0, false
		}
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > limit {
		return 0, false
	}
	return n, true
}
