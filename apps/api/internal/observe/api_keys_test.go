package observe

import (
	"strings"
	"testing"
)

func TestWholeApiCredentialsAreScrubbedBeforeIdentifiers(t *testing.T) {
	for _, kind := range []string{"wk", "pk"} {
		for _, secret := range []string{strings.Repeat("a", 43), strings.Repeat("0", 43), strings.Repeat("_", 43), "usr_0123456789abcd" + strings.Repeat("z", 25)} {
			value := "lc_" + kind + "_apk_0123456789AbCd_" + secret
			got := Scrub("rejected " + value + " after authentication")
			if got != "rejected <token> after authentication" {
				t.Fatalf("credential was fragmented: %s", got)
			}
		}
	}
	if got := Scrub("credential apk_0123456789AbCd"); got != "credential <id>" {
		t.Fatal("public credential ID not recognized")
	}
}
