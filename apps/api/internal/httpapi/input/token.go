package input

import "encoding/base64"

// Token checks a token has the shape auth.Token makes: 32 bytes as
// 43 base64url characters.
func Token(token string) bool {
	if len(token) != 43 {
		return false
	}
	_, err := base64.RawURLEncoding.DecodeString(token)
	return err == nil
}
