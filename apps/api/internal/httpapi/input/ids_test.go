package input

import "testing"

func TestValidPublicID(t *testing.T) {
	for _, tc := range []struct {
		id   string
		want bool
	}{
		{"prj_j292s2S2j7Qx4B", true},
		{"prj_00000000000000", true},
		{"wsp_j292s2S2j7Qx4B", false},
		{"prj_j292s2S2j7Qx4", false},
		{"prj_j292s2S2j7Qx4BB", false},
		{"prj_j292s2S2j7Qx-B", false},
		{"prj_j292s2S2j7Qxé", false},
		{"prj-j292s2S2j7Qx4B", false},
		{"42", false},
		{"", false},
	} {
		if got := PublicID("prj", tc.id); got != tc.want {
			t.Errorf("PublicID(prj, %q) = %v", tc.id, got)
		}
	}
}
