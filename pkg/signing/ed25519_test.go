package signing

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ethan-mdev/patchline/pkg/manifest"
)

const testHash = "2cf24dba5fb0a30e26e83b2ac5b9e29e1b161e5c1fa7425e73043362938b9824"

func TestSignAndVerifyManifest(t *testing.T) {
	signer, verifier := testSignerVerifier(t)
	m := testManifest(t)

	if err := signer.SignManifest(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := verifier.VerifyManifest(context.Background(), data); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyRejectsTamperedManifest(t *testing.T) {
	signer, verifier := testSignerVerifier(t)
	m := testManifest(t)
	if err := signer.SignManifest(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	m.Version = "9.9.9"
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}

	if err := verifier.VerifyManifest(context.Background(), data); err == nil {
		t.Fatal("expected tampered manifest to fail verification")
	}
}

func TestVerifyRejectsWrongKey(t *testing.T) {
	signer, _ := testSignerVerifier(t)
	_, verifier := testSignerVerifier(t)
	m := testManifest(t)
	if err := signer.SignManifest(context.Background(), m); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}

	if err := verifier.VerifyManifest(context.Background(), data); err == nil {
		t.Fatal("expected wrong key to fail verification")
	}
}

func TestVerifyRejectsUnsignedManifest(t *testing.T) {
	_, verifier := testSignerVerifier(t)
	data, err := json.Marshal(testManifest(t))
	if err != nil {
		t.Fatal(err)
	}

	if err := verifier.VerifyManifest(context.Background(), data); err == nil {
		t.Fatal("expected unsigned manifest to fail verification")
	}
}

func TestKeyEncodingRoundTrip(t *testing.T) {
	pair, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	privateKey, err := DecodePrivateKey(EncodePrivateKey(pair.PrivateKey))
	if err != nil {
		t.Fatal(err)
	}
	publicKey, err := DecodePublicKey(EncodePublicKey(pair.PublicKey))
	if err != nil {
		t.Fatal(err)
	}
	if string(privateKey) != string(pair.PrivateKey) {
		t.Fatal("private key did not round trip")
	}
	if string(publicKey) != string(pair.PublicKey) {
		t.Fatal("public key did not round trip")
	}
}

func testManifest(t *testing.T) *manifest.Manifest {
	t.Helper()
	key, err := manifest.ObjectKeyForHash(testHash)
	if err != nil {
		t.Fatal(err)
	}
	return &manifest.Manifest{
		FormatVersion:   manifest.FormatVersion,
		AppID:           "com.example.game",
		Version:         "1.0.0",
		Channel:         "beta",
		ReleaseSequence: 1,
		PublishedAt:     time.Unix(100, 0).UTC(),
		Files: []manifest.File{{
			Path:      "Game.bin",
			Size:      5,
			SHA256:    testHash,
			ObjectKey: key,
		}},
	}
}

func testSignerVerifier(t *testing.T) (*Signer, *Verifier) {
	t.Helper()
	pair, err := GenerateKeyPair()
	if err != nil {
		t.Fatal(err)
	}
	signer, err := NewSigner(pair.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	verifier, err := NewVerifier(pair.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return signer, verifier
}
