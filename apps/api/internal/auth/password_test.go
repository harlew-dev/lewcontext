package auth

import (
	"net/http"
	"testing"
)

func TestPasswordHash(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword("correct horse battery staple", hash) {
		t.Fatal("valid password rejected")
	}
	if CheckPassword("incorrect horse battery staple", hash) {
		t.Fatal("invalid password accepted")
	}
	another, _ := HashPassword("correct horse battery staple")
	if hash == another {
		t.Fatal("password salts must be random")
	}
	for _, malformed := range []string{"", "$argon2id$v=19$m=1,t=1,p=1$a$b", "$argon2id$v=19$m=19456,t=2,p=1$bad$bad"} {
		if CheckPassword("password", malformed) {
			t.Fatal("malformed hash accepted")
		}
	}
}
func TestTokenAndCookie(t *testing.T) {
	a, err := Token()
	if err != nil {
		t.Fatal(err)
	}
	b, _ := Token()
	if len(a) != 43 || a == b || Digest(a) == a {
		t.Fatal("unsafe token")
	}
	cookie := Cookie(a, true)
	if !cookie.HttpOnly || !cookie.Secure || cookie.SameSite != http.SameSiteLaxMode || cookie.Name != "__Host-forma_session" || cookie.Path != "/" || cookie.Domain != "" || cookie.MaxAge != 86400 {
		t.Fatalf("unsafe cookie: %+v", cookie)
	}
	if Cookie(a, false).Secure {
		t.Fatal("local HTTP cookie must be usable")
	}
}
