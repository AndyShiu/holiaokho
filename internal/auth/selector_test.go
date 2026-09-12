package auth

import "testing"

func TestSelector(t *testing.T) {
	c := &ContentSelector{Expression: `format == "maven" and (path =^ "/com/acme/" or path =~ "^/org/acme/.*") and not coordinate.groupId == "x"`}
	if err := c.Compile(); err != nil {
		t.Fatal(err)
	}
	if !c.Match(SelectorInput{Format: "maven", Path: "/com/acme/lib/1.0/lib-1.0.jar"}) {
		t.Fatal("should match prefix")
	}
	if !c.Match(SelectorInput{Format: "maven", Path: "org/acme/x"}) {
		t.Fatal("should match regex (and add leading slash)")
	}
	if c.Match(SelectorInput{Format: "npm", Path: "/com/acme/x"}) || c.Match(SelectorInput{Format: "maven", Path: "/net/x"}) {
		t.Fatal("should not match")
	}
	if c.Match(SelectorInput{Format: "maven", Path: "/com/acme/x", Coord: map[string]string{"groupId": "x"}}) {
		t.Fatal("not clause")
	}
	for _, bad := range []string{`path = "x"`, `path == x`, `(path == "x"`, `foo`} {
		if _, err := parseExpr(bad); err == nil {
			t.Errorf("%q should fail", bad)
		}
	}
}
