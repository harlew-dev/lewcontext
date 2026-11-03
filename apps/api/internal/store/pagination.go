package store

import (
	"context"
	"github.com/uptrace/bun"
)

// Page size bounds. MaxPage caps OFFSET so a request can skip at most
// (MaxPage-1)*MaxPerPage rows.
const (
	DefaultPerPage = 20
	MaxPerPage     = 100
	MaxPage        = 10_000
)

// PageRequest is a validated page position; httpapi builds it from the query string.
type PageRequest struct {
	Page    int
	PerPage int
}

// Pages describes where a page sits in the full result set.
type Pages struct {
	Current int `json:"current"`
	Total   int `json:"total"`
	PerPage int `json:"perPage"`
	Items   int `json:"items"`
}

// Page is one page of results plus its position.
type Page[T any] struct {
	Data  []T   `json:"data"`
	Pages Pages `json:"pages"`
}

// Paginate scans one page of q and counts every row q matches.
//
// q must already be scoped to the caller and carry a deterministic ORDER BY
// (ending in a unique column); Paginate only adds LIMIT and OFFSET. A page past
// the end returns no rows with the real totals.
func Paginate[T any](ctx context.Context, q *bun.SelectQuery, req PageRequest) (Page[T], error) {
	rows := []T{}
	count, err := q.
		Limit(int64(req.PerPage)).
		Offset(int64(req.Page-1)*int64(req.PerPage)).
		ScanAndCount(ctx, &rows)
	if err != nil {
		return Page[T]{}, err
	}
	return Page[T]{Data: rows, Pages: pagesFor(req, int(count))}, nil
}

// pagesFor reports at least one page, so an empty result is "page 1 of 1".
func pagesFor(req PageRequest, items int) Pages {
	total := max(1, (items+req.PerPage-1)/req.PerPage)
	return Pages{Current: req.Page, Total: total, PerPage: req.PerPage, Items: items}
}
