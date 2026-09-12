package auth

import (
	"crypto/sha512"
	"encoding/base64"
	"testing"

	"github.com/holiaokho/holiaokho/internal/model"
)

func TestArgon2RoundTrip(t *testing.T) {
	h, err := HashPassword("s3cret")
	if err != nil {
		t.Fatal(err)
	}
	ok, rehash, err := VerifyPassword(h, "s3cret")
	if err != nil || !ok || rehash {
		t.Fatalf("verify: ok=%v rehash=%v err=%v", ok, rehash, err)
	}
	if ok, _, _ := VerifyPassword(h, "wrong"); ok {
		t.Fatal("wrong password accepted")
	}
}

// Build a Shiro1 hash the way Nexus does and verify it.
func TestShiro1(t *testing.T) {
	salt := []byte("0123456789abcdef")
	pw := "nexus-import-fixture"
	h := sha512.New()
	h.Write(salt)
	h.Write([]byte(pw))
	sum := h.Sum(nil)
	for i := 1; i < 1024; i++ {
		s := sha512.Sum512(sum)
		sum = s[:]
	}
	hash := "$shiro1$SHA-512$1024$" + base64.StdEncoding.EncodeToString(salt) + "$" + base64.StdEncoding.EncodeToString(sum)
	ok, rehash, err := VerifyPassword(hash, pw)
	if err != nil || !ok || !rehash {
		t.Fatalf("shiro verify: ok=%v rehash=%v err=%v", ok, rehash, err)
	}
	if ok, _, _ := VerifyPassword(hash, "nope"); ok {
		t.Fatal("wrong shiro password accepted")
	}
}

func TestRBAC(t *testing.T) {
	p := &Principal{Privileges: []model.Privilege{
		{Target: "repo:maven-public", Actions: []string{Read}},
		{Target: "format:npm", Actions: []string{Write}},
		{Target: "app:users", Actions: []string{Admin}},
	}}
	if !p.CanRepo("maven-public", "maven", Read) || p.CanRepo("maven-public", "maven", Write) {
		t.Fatal("repo read/write wrong")
	}
	if !p.CanRepo("npm-hosted", "npm", Read) || !p.CanRepo("npm-hosted", "npm", Write) || p.CanRepo("npm-hosted", "npm", Delete) {
		t.Fatal("format write implies read, not delete")
	}
	if !p.Can("app:users", Delete) || p.Can("app:roles", Read) {
		t.Fatal("admin implies all on target only")
	}
	admin := &Principal{Privileges: []model.Privilege{{Target: "*", Actions: []string{"*"}}}}
	if !admin.CanRepo("anything", "x", Delete) || !admin.Can("app:system", Admin) {
		t.Fatal("wildcard wrong")
	}
	anon := &Principal{Privileges: []model.Privilege{{Target: "repo:*", Actions: []string{Read}}}}
	if !anon.CanRepo("x", "y", Read) || anon.Can("app:users", Read) {
		t.Fatal("repo:* wrong")
	}
}
