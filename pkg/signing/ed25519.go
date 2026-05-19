package signing

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/ethan-mdev/patchline/pkg/manifest"
)

const (
	AlgorithmEd25519 = "ed25519"

	privatePrefix = "PATCHLINE ED25519 PRIVATE KEY"
	publicPrefix  = "PATCHLINE ED25519 PUBLIC KEY"
)

type KeyPair struct {
	PublicKey  ed25519.PublicKey
	PrivateKey ed25519.PrivateKey
	KeyID      string
}

type Signer struct {
	privateKey ed25519.PrivateKey
	keyID      string
}

type Verifier struct {
	publicKey ed25519.PublicKey
	keyID     string
}

func GenerateKeyPair() (*KeyPair, error) {
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, err
	}
	return &KeyPair{
		PublicKey:  publicKey,
		PrivateKey: privateKey,
		KeyID:      KeyID(publicKey),
	}, nil
}

func NewSigner(privateKey ed25519.PrivateKey) (*Signer, error) {
	if len(privateKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid ed25519 private key length %d", len(privateKey))
	}
	publicKey := privateKey.Public().(ed25519.PublicKey)
	return &Signer{privateKey: privateKey, keyID: KeyID(publicKey)}, nil
}

func NewVerifier(publicKey ed25519.PublicKey) (*Verifier, error) {
	if len(publicKey) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid ed25519 public key length %d", len(publicKey))
	}
	return &Verifier{publicKey: publicKey, keyID: KeyID(publicKey)}, nil
}

func (s *Signer) SignManifest(ctx context.Context, m *manifest.Manifest) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s == nil {
		return errors.New("signer is nil")
	}
	payload, err := manifest.CanonicalBytes(m)
	if err != nil {
		return err
	}
	signature := ed25519.Sign(s.privateKey, payload)
	m.Signature = &manifest.Signature{
		Algorithm: AlgorithmEd25519,
		KeyID:     s.keyID,
		Value:     base64.StdEncoding.EncodeToString(signature),
	}
	return nil
}

func (v *Verifier) VerifyManifest(ctx context.Context, data []byte) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if v == nil {
		return errors.New("verifier is nil")
	}

	var m manifest.Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("decode manifest for verification: %w", err)
	}
	if m.Signature == nil {
		return errors.New("manifest is unsigned")
	}
	if m.Signature.Algorithm != AlgorithmEd25519 {
		return fmt.Errorf("unsupported signature algorithm %q", m.Signature.Algorithm)
	}
	if m.Signature.KeyID != "" && m.Signature.KeyID != v.keyID {
		return fmt.Errorf("manifest key_id %q does not match verifier key_id %q", m.Signature.KeyID, v.keyID)
	}
	signature, err := base64.StdEncoding.DecodeString(m.Signature.Value)
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}
	if len(signature) != ed25519.SignatureSize {
		return fmt.Errorf("invalid signature length %d", len(signature))
	}
	payload, err := manifest.CanonicalBytes(&m)
	if err != nil {
		return err
	}
	if !ed25519.Verify(v.publicKey, payload, signature) {
		return errors.New("manifest signature verification failed")
	}
	return nil
}

func EncodePrivateKey(privateKey ed25519.PrivateKey) string {
	return encodePEMish(privatePrefix, privateKey)
}

func EncodePublicKey(publicKey ed25519.PublicKey) string {
	return encodePEMish(publicPrefix, publicKey)
}

func DecodePrivateKey(text string) (ed25519.PrivateKey, error) {
	data, err := decodePEMish(text, privatePrefix)
	if err != nil {
		return nil, err
	}
	if len(data) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("invalid ed25519 private key length %d", len(data))
	}
	return ed25519.PrivateKey(data), nil
}

func DecodePublicKey(text string) (ed25519.PublicKey, error) {
	data, err := decodePEMish(text, publicPrefix)
	if err != nil {
		return nil, err
	}
	if len(data) != ed25519.PublicKeySize {
		return nil, fmt.Errorf("invalid ed25519 public key length %d", len(data))
	}
	return ed25519.PublicKey(data), nil
}

func ReadPrivateKey(path string) (ed25519.PrivateKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return DecodePrivateKey(string(data))
}

func ReadPublicKey(path string) (ed25519.PublicKey, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return DecodePublicKey(string(data))
}

func WriteKeyPair(privatePath string, publicPath string, pair *KeyPair) error {
	if pair == nil {
		return errors.New("key pair is nil")
	}
	if err := os.WriteFile(privatePath, []byte(EncodePrivateKey(pair.PrivateKey)), 0600); err != nil {
		return err
	}
	return os.WriteFile(publicPath, []byte(EncodePublicKey(pair.PublicKey)), 0644)
}

func KeyID(publicKey ed25519.PublicKey) string {
	sum := sha256.Sum256(publicKey)
	return base64.RawURLEncoding.EncodeToString(sum[:8])
}

func encodePEMish(label string, data []byte) string {
	encoded := base64.StdEncoding.EncodeToString(data)
	return fmt.Sprintf("-----BEGIN %s-----\n%s\n-----END %s-----\n", label, encoded, label)
}

func decodePEMish(text string, label string) ([]byte, error) {
	text = strings.TrimSpace(text)
	begin := "-----BEGIN " + label + "-----"
	end := "-----END " + label + "-----"
	text = strings.TrimPrefix(text, begin)
	text = strings.TrimSuffix(text, end)
	text = strings.TrimSpace(text)
	text = strings.ReplaceAll(text, "\r", "")
	text = strings.ReplaceAll(text, "\n", "")
	data, err := base64.StdEncoding.DecodeString(text)
	if err != nil {
		return nil, err
	}
	return data, nil
}
