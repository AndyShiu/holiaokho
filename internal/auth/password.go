package auth

import (
	"crypto/rand"
	"crypto/sha512"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

// HashPassword returns an argon2id PHC string.
func HashPassword(pw string) (string, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	const t, m, p, kl = 2, 64 * 1024, 1, 32
	key := argon2.IDKey([]byte(pw), salt, t, m, p, kl)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", m, t, p,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// VerifyPassword accepts argon2id (ours) and Shiro1 SHA-512 (imported from
// Nexus) hashes. It returns needsRehash=true for legacy formats so the caller
// can upgrade the stored hash after a successful login.
func VerifyPassword(hash, pw string) (ok bool, needsRehash bool, err error) {
	switch {
	case strings.HasPrefix(hash, "$argon2id$"):
		ok, err = verifyArgon2(hash, pw)
		return ok, false, err
	case strings.HasPrefix(hash, "$shiro1$"):
		ok, err = verifyShiro1(hash, pw)
		return ok, true, err
	case hash == "":
		return false, false, nil
	default:
		return false, false, errors.New("unknown password hash format")
	}
}

func verifyArgon2(hash, pw string) (bool, error) {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 {
		return false, errors.New("malformed argon2 hash")
	}
	var m, t uint32
	var p uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &p); err != nil {
		return false, err
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, err
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, err
	}
	got := argon2.IDKey([]byte(pw), salt, t, m, p, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

// verifyShiro1 implements Apache Shiro's Shiro1CryptFormat as used by Nexus:
// $shiro1$SHA-512$<iterations>$<base64 salt>$<base64 hash>
// hash0 = SHA512(salt || password); hash_i = SHA512(hash_{i-1}) for i < iterations.
func verifyShiro1(hash, pw string) (bool, error) {
	parts := strings.Split(hash, "$")
	if len(parts) != 6 || parts[2] != "SHA-512" {
		return false, errors.New("unsupported shiro hash")
	}
	iter, err := strconv.Atoi(parts[3])
	if err != nil || iter < 1 {
		return false, errors.New("malformed shiro hash")
	}
	salt, err := base64.StdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, err
	}
	want, err := base64.StdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, err
	}
	h := sha512.New()
	h.Write(salt)
	h.Write([]byte(pw))
	sum := h.Sum(nil)
	for i := 1; i < iter; i++ {
		s := sha512.Sum512(sum)
		sum = s[:]
	}
	return subtle.ConstantTimeCompare(sum, want) == 1, nil
}
