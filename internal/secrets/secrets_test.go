package secrets

import (
	"encoding/json"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	if _, err := Init("passphrase-one", "", nil); err != nil {
		t.Fatal(err)
	}
	ct, err := Encrypt("hunter2")
	if err != nil || !IsEncrypted(ct) {
		t.Fatalf("encrypt: %v %q", err, ct)
	}
	pt, err := Decrypt(ct)
	if err != nil || pt != "hunter2" {
		t.Fatalf("decrypt: %v %q", err, pt)
	}
	if p, _ := Decrypt("legacy-plain"); p != "legacy-plain" {
		t.Fatal("plaintext must pass through")
	}
	if !NeedsReencrypt("legacy-plain") || NeedsReencrypt(ct) {
		t.Fatal("NeedsReencrypt wrong")
	}
	// Rotate: new current key, old key as previous.
	if _, err := Init("passphrase-two", "", []string{"passphrase-one"}); err != nil {
		t.Fatal(err)
	}
	if pt, err := Decrypt(ct); err != nil || pt != "hunter2" {
		t.Fatalf("decrypt with previous key: %v", err)
	}
	if !NeedsReencrypt(ct) {
		t.Fatal("old-key value should need re-encryption")
	}
	if _, err := Init("passphrase-three", "", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := Decrypt(ct); err == nil {
		t.Fatal("unknown key must fail")
	}
}

func TestPaths(t *testing.T) {
	Init("k", "", nil)
	raw := json.RawMessage(`{"proxy":{"remoteUrl":"https://x","password":"pw"},"apt":{"signingKey":"KEY"},"maven":{"layoutPolicy":"STRICT"}}`)
	enc, err := EncryptPaths(raw, RepositoryPaths)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]map[string]any
	json.Unmarshal(enc, &m)
	if !IsEncrypted(m["proxy"]["password"].(string)) || !IsEncrypted(m["apt"]["signingKey"].(string)) || m["proxy"]["remoteUrl"] != "https://x" {
		t.Fatalf("bad encrypt: %s", enc)
	}
	dec, _ := DecryptPaths(enc, RepositoryPaths)
	json.Unmarshal(dec, &m)
	if m["proxy"]["password"] != "pw" || m["apt"]["signingKey"] != "KEY" {
		t.Fatalf("bad decrypt: %s", dec)
	}
	red := RedactPaths(dec, RepositoryPaths)
	json.Unmarshal(red, &m)
	if m["proxy"]["password"] != "***" || m["maven"]["layoutPolicy"] != "STRICT" {
		t.Fatalf("bad redact: %s", red)
	}
}
