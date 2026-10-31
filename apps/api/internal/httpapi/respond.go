package httpapi

// The JSON answers and request bodies every controller uses.

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
)

func JSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func Fail(w http.ResponseWriter, status int, message string) {
	JSON(w, status, map[string]string{"error": message})
}

// FailField is Fail for an error about one field of the request body, so the
// web app can show it next to that field.
func FailField(w http.ResponseWriter, status int, field, message string) {
	JSON(w, status, map[string]string{"error": message, "field": field})
}

func InternalError(w http.ResponseWriter, err error) {
	slog.Error("request failed", "error", err)
	Fail(w, 500, "Something went wrong. Please try again.")
}

// DecodeValid decodes the body into a T and checks it with valid (which may
// also normalise it, e.g. trim), answering 400 or 422 with message when either
// fails.
func DecodeValid[T any](w http.ResponseWriter, r *http.Request, valid func(*T) bool, message string) (T, bool) {
	var in T
	if !Decode(w, r, &in) {
		return in, false
	}
	if !valid(&in) {
		Fail(w, 422, message)
		return in, false
	}
	return in, true
}

func Decode(w http.ResponseWriter, r *http.Request, target any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if err := d.Decode(target); err != nil {
		Fail(w, 400, "Invalid request body.")
		return false
	}
	if err := d.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		Fail(w, 400, "Expected one JSON object.")
		return false
	}
	return true
}

// NotFound is the answer to an unknown API route, and to every admin request
// without the right key, so the two can't be told apart.
func NotFound(w http.ResponseWriter, r *http.Request) {
	Fail(w, 404, "Not found.")
}
