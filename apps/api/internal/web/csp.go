package web

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"strings"

	"golang.org/x/net/html"
)

// policy is the Content-Security-Policy for one HTML file. Scripts load only
// from this origin, plus the file's own inline scripts by hash: React Router
// writes a few (hydration context, module bootstrap, scroll restoration) into
// the HTML it builds. Styles allow inline CSS because BaseUI's Styletron
// writes <style> tags.
func policy(hashes []string) string {
	script := "script-src 'self'"
	for _, hash := range hashes {
		script += " 'sha256-" + hash + "'"
	}
	return strings.Join([]string{
		"default-src 'self'",
		script,
		"style-src 'self' 'unsafe-inline'",
		"img-src 'self' data:",
		"font-src 'self' data:",
		"connect-src 'self'",
		"object-src 'none'",
		"base-uri 'self'",
		"form-action 'self'",
		"frame-ancestors 'none'",
	}, "; ")
}

// scriptHashes returns the base64 SHA-256 of every inline <script> in an HTML
// document, in order, as a browser computes them for CSP: over the element's
// text after HTML tokenizing (which normalises newlines). Scripts with a src
// attribute load from this origin and need no hash.
func scriptHashes(document []byte) ([]string, error) {
	var hashes []string
	z := html.NewTokenizer(bytes.NewReader(document))
	for {
		switch z.Next() {
		case html.ErrorToken:
			if errors.Is(z.Err(), io.EOF) {
				return hashes, nil
			}
			return nil, z.Err()
		case html.StartTagToken:
			name, hasAttributes := z.TagName()
			if string(name) != "script" || hasSource(z, hasAttributes) {
				continue
			}
			// Script contents are raw text: the next token is all of it, or the
			// end tag when the script is empty.
			text := ""
			if z.Next() == html.TextToken {
				text = string(z.Text())
			}
			sum := sha256.Sum256([]byte(text))
			hashes = append(hashes, base64.StdEncoding.EncodeToString(sum[:]))
		}
	}
}

// hasSource reports whether the start tag being read has a src attribute.
func hasSource(z *html.Tokenizer, more bool) bool {
	for more {
		var key []byte
		key, _, more = z.TagAttr()
		if string(key) == "src" {
			return true
		}
	}
	return false
}
