package password

import "testing"

func TestHashVerify(t *testing.T) {
	plain := "  密码 with spaces  "
	a, err := Hash(plain)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Hash(plain)
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("salt was reused")
	}
	for _, tc := range []struct {
		plain string
		want  bool
	}{
		{plain, true},
		{"wrong password", false},
		{"密码 with spaces", false},
	} {
		ok, err := Verify(tc.plain, a)
		if err != nil || ok != tc.want {
			t.Fatalf("verify result %v, %v", ok, err)
		}
	}
	for _, bad := range []string{"", "$argon2id$v=19$m=999999999,t=3,p=2$AA$AA", prefix + "AA$AA"} {
		if _, err := Verify(plain, bad); err == nil {
			t.Fatal("malformed hash accepted")
		}
	}
}
