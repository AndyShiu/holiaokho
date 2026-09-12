package pypi

import "testing"

func TestNormalizeAndFilename(t *testing.T) {
	if Normalize("Django_REST.framework") != "django-rest-framework" {
		t.Fatal("normalize")
	}
	n, v := parseFilename("requests-2.32.3-py3-none-any.whl")
	if n != "requests" || v != "2.32.3" {
		t.Fatalf("wheel: %s %s", n, v)
	}
	n, v = parseFilename("my-pkg-1.0.0.tar.gz")
	if n != "my-pkg" || v != "1.0.0" {
		t.Fatalf("sdist: %s %s", n, v)
	}
}

func TestPEP440(t *testing.T) {
	cases := [][2]string{{"1.0", "1.0.1"}, {"1.0a1", "1.0"}, {"1.0a1", "1.0b1"}, {"1.0rc1", "1.0"}, {"1.0", "1.0.post1"},
		{"1.0.dev1", "1.0a1"}, {"1.9", "1.10"}, {"2.0", "1!1.0"}, {"1.0", "1.0.0.1"}}
	for _, c := range cases {
		if !pep440Less(c[0], c[1]) || pep440Less(c[1], c[0]) {
			t.Errorf("expected %s < %s", c[0], c[1])
		}
	}
	if pep440Less("1.0", "1.0.0") || pep440Less("1.0.0", "1.0") {
		t.Error("1.0 == 1.0.0")
	}
}

func TestParseSimple(t *testing.T) {
	ls := parseSimple([]byte(`<a href="https://files.pythonhosted.org/packages/ab/cd/requests-2.32.3-py3-none-any.whl#sha256=abc" data-requires-python="&gt;=3.8">requests-2.32.3-py3-none-any.whl</a><br/>`))
	if len(ls) != 1 || ls[0].File != "requests-2.32.3-py3-none-any.whl" || ls[0].Hash != "sha256=abc" || ls[0].Requires != ">=3.8" || ls[0].URL != "https://files.pythonhosted.org/packages/ab/cd/requests-2.32.3-py3-none-any.whl" {
		t.Fatalf("parse: %+v", ls)
	}
}
