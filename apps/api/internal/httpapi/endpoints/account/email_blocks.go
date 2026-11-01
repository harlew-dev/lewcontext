package account

import (
	"errors"
	"lewcontext/api/internal/auth"
	"lewcontext/api/internal/httpapi"
	"lewcontext/api/internal/httpapi/input"
	"lewcontext/api/internal/store"
	"net/http"
)

// Blocking an email address (plans/features/implemented/email-blocks.md): a blockable
// email (an invitation) carries a block link, whose page checks it, then
// blocks or unblocks the address it went to. Only an address without an
// account can be blocked, and only with its link, so only that inbox can.
// Nothing else ever says an address is blocked. The routes are
// /api/email-blocks, in this area as the other signed-out links' are.

const (
	blockLinkInvalid = "This link has expired or isn’t valid."
	blockRegistered  = "This address has an account. Delete your account first."
)

type blockLink struct {
	Token string `json:"token"`
}

// EmailBlocks are the block list, reached only through a block link.
type EmailBlocks struct{ *httpapi.Server }

// Check lets the link's page show the address and whether an account uses
// it or it's blocked. POST /api/email-blocks/check.
func (h EmailBlocks) Check(w http.ResponseWriter, r *http.Request) {
	digest, ok := h.blockLink(w, r)
	if !ok {
		return
	}
	link, err := h.Repo.CheckEmailBlockLink(r.Context(), digest)
	if h.failed(w, err) {
		return
	}
	httpapi.JSON(w, 200, link)
}

// Store blocks the link's address. An address already blocked is a 204 too.
func (h EmailBlocks) Store(w http.ResponseWriter, r *http.Request) {
	digest, ok := h.blockLink(w, r)
	if !ok {
		return
	}
	if !h.failed(w, h.Repo.BlockEmail(r.Context(), digest)) {
		w.WriteHeader(204)
	}
}

// Destroy unblocks the link's address. One that isn't blocked is a 204 too.
func (h EmailBlocks) Destroy(w http.ResponseWriter, r *http.Request) {
	digest, ok := h.blockLink(w, r)
	if !ok {
		return
	}
	if !h.failed(w, h.Repo.UnblockEmail(r.Context(), digest)) {
		w.WriteHeader(204)
	}
}

// blockLink reads the body's token, after the IP limit, as the digest the
// store finds the link by. A malformed token is a dead link.
func (h EmailBlocks) blockLink(w http.ResponseWriter, r *http.Request) (string, bool) {
	var in blockLink
	if !httpapi.Decode(w, r, &in) {
		return "", false
	}
	if !h.Allow(w, r, httpapi.AuthPerIP, httpapi.AuthPerIP.IPKey(httpapi.ClientIP(r))) {
		return "", false
	}
	if !input.Token(in.Token) {
		httpapi.FailField(w, 422, "token", blockLinkInvalid)
		return "", false
	}
	return auth.Digest(in.Token), true
}

// failed answers a store error, if there is one: a dead link, an address an
// account uses, or a 500.
func (h EmailBlocks) failed(w http.ResponseWriter, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, store.ErrBlockLinkInvalid):
		httpapi.FailField(w, 422, "token", blockLinkInvalid)
	case errors.Is(err, store.ErrEmailRegistered):
		httpapi.Fail(w, 409, blockRegistered)
	default:
		httpapi.InternalError(w, err)
	}
	return true
}
