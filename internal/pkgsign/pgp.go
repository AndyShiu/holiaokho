// Package pkgsign provides OpenPGP signing for Linux package repositories
// (APT InRelease/Release.gpg, YUM repomd.xml.asc) and key generation.
package pkgsign

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/clearsign"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
)

// Signer holds a decrypted private key.
type Signer struct {
	entity *openpgp.Entity
}

// LoadSigner parses an armored private key and unlocks it.
func LoadSigner(armoredKey, passphrase string) (*Signer, error) {
	if strings.TrimSpace(armoredKey) == "" {
		return nil, errors.New("no signing key configured")
	}
	el, err := openpgp.ReadArmoredKeyRing(strings.NewReader(armoredKey))
	if err != nil {
		return nil, fmt.Errorf("parse signing key: %w", err)
	}
	if len(el) == 0 || el[0].PrivateKey == nil {
		return nil, errors.New("signing key has no private key")
	}
	e := el[0]
	if e.PrivateKey.Encrypted {
		if err := e.PrivateKey.Decrypt([]byte(passphrase)); err != nil {
			return nil, fmt.Errorf("unlock signing key: %w", err)
		}
	}
	for _, sk := range e.Subkeys {
		if sk.PrivateKey != nil && sk.PrivateKey.Encrypted {
			sk.PrivateKey.Decrypt([]byte(passphrase))
		}
	}
	return &Signer{entity: e}, nil
}

// PublicKey returns the armored public key (for clients to trust).
func (s *Signer) PublicKey() (string, error) {
	var buf bytes.Buffer
	w, err := armor.Encode(&buf, openpgp.PublicKeyType, nil)
	if err != nil {
		return "", err
	}
	if err := s.entity.Serialize(w); err != nil {
		return "", err
	}
	w.Close()
	return buf.String(), nil
}

// KeyID returns the long hex key id.
func (s *Signer) KeyID() string { return s.entity.PrimaryKey.KeyIdString() }

// DetachSign returns an armored detached signature (Release.gpg, repomd.xml.asc).
func (s *Signer) DetachSign(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	cfg := signConfig()
	if err := openpgp.ArmoredDetachSign(&buf, s.entity, bytes.NewReader(data), cfg); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// ClearSign returns a clearsigned document (InRelease).
func (s *Signer) ClearSign(data []byte) ([]byte, error) {
	var buf bytes.Buffer
	cfg := signConfig()
	w, err := clearsign.Encode(&buf, s.entity.PrivateKey, cfg)
	if err != nil {
		return nil, err
	}
	if _, err := io.Copy(w, bytes.NewReader(data)); err != nil {
		return nil, err
	}
	if err := w.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// signConfig produces plain v4 RSA/SHA-256 signatures without the random
// salt notation go-crypto adds by default: rpm's OpenPGP parser
// (rpm-sequoia on EL9) rejects signatures carrying unknown notations.
func signConfig() *packet.Config {
	no := false
	return &packet.Config{DefaultHash: cryptoSHA256(), Time: time.Now, NonDeterministicSignaturesViaNotation: &no}
}

// GenerateKey creates a new armored RSA key pair for repository signing.
func GenerateKey(name, email string) (private, public string, err error) {
	no := false
	e, err := openpgp.NewEntity(name, "Holiaokho repository signing key", email, &packet.Config{RSABits: 3072, Time: time.Now, NonDeterministicSignaturesViaNotation: &no})
	if err != nil {
		return "", "", err
	}
	var priv, pub bytes.Buffer
	w, err := armor.Encode(&priv, openpgp.PrivateKeyType, nil)
	if err != nil {
		return "", "", err
	}
	if err := e.SerializePrivate(w, nil); err != nil {
		return "", "", err
	}
	w.Close()
	w2, err := armor.Encode(&pub, openpgp.PublicKeyType, nil)
	if err != nil {
		return "", "", err
	}
	if err := e.Serialize(w2); err != nil {
		return "", "", err
	}
	w2.Close()
	return priv.String(), pub.String(), nil
}
