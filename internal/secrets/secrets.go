// Package secrets encrypts configuration secrets (upstream passwords,
// signing keys, LDAP/OIDC/SMTP credentials, S3 keys) before they reach the
// database, using AES-256-GCM. Values are stored as
// "enc:v1:<keyid>:<base64(nonce||ciphertext)>"; anything without the prefix
// is treated as legacy plaintext so existing rows keep working until the
// re-encrypt task rewrites them.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

const prefix = "enc:v1:"

type Keyring struct {
	current   []byte
	keys      map[string][]byte // keyid -> key (current + previous)
	KeyID     string
	Generated bool // true when the key was created on first start
}

var (
	mu     sync.RWMutex
	active *Keyring
)

func keyID(k []byte) string {
	s := sha256.Sum256(k)
	return hex.EncodeToString(s[:4])
}

func decodeKey(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, errors.New("empty key")
	}
	if b, err := base64.StdEncoding.DecodeString(s); err == nil && len(b) == 32 {
		return b, nil
	}
	if b, err := hex.DecodeString(s); err == nil && len(b) == 32 {
		return b, nil
	}
	// Any other string: derive a 32-byte key so operators can use a passphrase.
	d := sha256.Sum256([]byte(s))
	return d[:], nil
}

// Init installs the process keyring. key may be empty: then keyFile is
// read, or a new random key is generated and written there.
func Init(key, keyFile string, previous []string) (*Keyring, error) {
	kr := &Keyring{keys: map[string][]byte{}}
	if key == "" && keyFile != "" {
		if b, err := os.ReadFile(keyFile); err == nil {
			key = strings.TrimSpace(string(b))
		} else {
			raw := make([]byte, 32)
			if _, err := rand.Read(raw); err != nil {
				return nil, err
			}
			key = base64.StdEncoding.EncodeToString(raw)
			if err := os.MkdirAll(filepath.Dir(keyFile), 0o755); err != nil {
				return nil, err
			}
			if err := os.WriteFile(keyFile, []byte(key+"\n"), 0o600); err != nil {
				return nil, fmt.Errorf("write secret key file: %w", err)
			}
			kr.Generated = true
		}
	}
	if key == "" {
		return nil, errors.New("no secret key configured")
	}
	k, err := decodeKey(key)
	if err != nil {
		return nil, err
	}
	kr.current = k
	kr.KeyID = keyID(k)
	kr.keys[kr.KeyID] = k
	for _, p := range previous {
		if pk, err := decodeKey(p); err == nil {
			kr.keys[keyID(pk)] = pk
		}
	}
	mu.Lock()
	active = kr
	mu.Unlock()
	return kr, nil
}

func ring() *Keyring {
	mu.RLock()
	defer mu.RUnlock()
	return active
}

// IsEncrypted reports whether s carries the ciphertext prefix.
func IsEncrypted(s string) bool { return strings.HasPrefix(s, prefix) }

// Encrypt returns the ciphertext form of plain ("" stays "").
func Encrypt(plain string) (string, error) {
	kr := ring()
	if plain == "" || kr == nil {
		return plain, nil
	}
	if IsEncrypted(plain) {
		return plain, nil
	}
	block, err := aes.NewCipher(kr.current)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	ct := gcm.Seal(nil, nonce, []byte(plain), []byte(kr.KeyID))
	return prefix + kr.KeyID + ":" + base64.StdEncoding.EncodeToString(append(nonce, ct...)), nil
}

// Decrypt returns the plaintext of s; legacy plaintext passes through.
func Decrypt(s string) (string, error) {
	if !IsEncrypted(s) {
		return s, nil
	}
	kr := ring()
	if kr == nil {
		return "", errors.New("secrets: keyring not initialised")
	}
	rest := strings.TrimPrefix(s, prefix)
	kid, b64, ok := strings.Cut(rest, ":")
	if !ok {
		return "", errors.New("secrets: malformed value")
	}
	k, ok := kr.keys[kid]
	if !ok {
		return "", fmt.Errorf("secrets: value encrypted with unknown key %s (add it to secrets.previous_keys)", kid)
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(k)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("secrets: value too short")
	}
	pt, err := gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], []byte(kid))
	if err != nil {
		return "", fmt.Errorf("secrets: decrypt failed: %w", err)
	}
	return string(pt), nil
}

// NeedsReencrypt reports whether s is plaintext or encrypted with a
// non-current key.
func NeedsReencrypt(s string) bool {
	if s == "" {
		return false
	}
	kr := ring()
	if kr == nil {
		return false
	}
	if !IsEncrypted(s) {
		return true
	}
	rest := strings.TrimPrefix(s, prefix)
	kid, _, _ := strings.Cut(rest, ":")
	return kid != kr.KeyID
}

// ------------------------------------------------- JSON field helpers

// Paths lists the dotted JSON paths inside repository attributes that hold
// secrets.
var RepositoryPaths = []string{"proxy.password", "apt.signingKey", "apt.passphrase", "yum.signingKey", "yum.passphrase", "alpine.signingKey"}

// StorageConfigPaths lists secret fields in storage configs.
var StorageConfigPaths = []string{"secretKey"}

// Transform applies fn to every string at the given dotted paths of a JSON
// object and returns the re-encoded document. Missing paths are ignored.
func Transform(raw json.RawMessage, paths []string, fn func(string) (string, error)) (json.RawMessage, error) {
	if len(raw) == 0 {
		return raw, nil
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		return raw, nil // not an object; leave alone
	}
	changed := false
	for _, p := range paths {
		segs := strings.Split(p, ".")
		cur := doc
		ok := true
		for _, s := range segs[:len(segs)-1] {
			next, isMap := cur[s].(map[string]any)
			if !isMap {
				ok = false
				break
			}
			cur = next
		}
		if !ok {
			continue
		}
		last := segs[len(segs)-1]
		v, isStr := cur[last].(string)
		if !isStr || v == "" {
			continue
		}
		nv, err := fn(v)
		if err != nil {
			return nil, err
		}
		if nv != v {
			cur[last] = nv
			changed = true
		}
	}
	if !changed {
		return raw, nil
	}
	return json.Marshal(doc)
}

// EncryptPaths / DecryptPaths are Transform with Encrypt / Decrypt.
func EncryptPaths(raw json.RawMessage, paths []string) (json.RawMessage, error) {
	return Transform(raw, paths, Encrypt)
}
func DecryptPaths(raw json.RawMessage, paths []string) (json.RawMessage, error) {
	return Transform(raw, paths, Decrypt)
}

// RedactPaths replaces secret fields with "***" for API responses.
func RedactPaths(raw json.RawMessage, paths []string) json.RawMessage {
	out, _ := Transform(raw, paths, func(string) (string, error) { return "***", nil })
	return out
}

// Redacted is the placeholder clients send back to mean "keep the stored value".
const Redacted = "***"
