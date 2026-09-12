package repo

import "testing"

func TestCleanPath(t *testing.T) {
	for _, ok := range []string{"a/b/c.jar", "/a", "@s/n/-/n-1.tgz"} {
		if _, err := CleanPath(ok); err != nil {
			t.Errorf("%q should be valid: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "/", "a/../b", "./a", "a//b", "a\x00b"} {
		if _, err := CleanPath(bad); err == nil {
			t.Errorf("%q should be invalid", bad)
		}
	}
}
