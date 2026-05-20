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

	envelope := signAndEncode(t, signer, m)
	if err := verifier.VerifyManifest(context.Background(), envelope); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyRejectsTamperedManifest(t *testing.T) {
	signer, verifier := testSignerVerifier(t)
	m := testManifest(t)
	envelope := signAndEncode(t, signer, m)

	// Tamper: decode envelope, swap payload version, re-encode without
	// re-signing. The signature must no longer verify.
	var env manifest.Envelope
	if err := json.Unmarshal(envelope, &env); err != nil {
		t.Fatal(err)
	}
	var inner manifest.Manifest
	if err := json.Unmarshal(env.Payload, &inner); err != nil {
		t.Fatal(err)
	}
	inner.Version = "9.9.9"
	tampered, err := json.Marshal(&inner)
	if err != nil {
		t.Fatal(err)
	}
	env.Payload = tampered
	tamperedEnvelope, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}

	if err := verifier.VerifyManifest(context.Background(), tamperedEnvelope); err == nil {
		t.Fatal("expected tampered manifest to fail verification")
	}
}

func TestVerifyRejectsWrongKey(t *testing.T) {
	signer, _ := testSignerVerifier(t)
	_, verifier := testSignerVerifier(t)
	envelope := signAndEncode(t, signer, testManifest(t))

	if err := verifier.VerifyManifest(context.Background(), envelope); err == nil {
		t.Fatal("expected wrong key to fail verification")
	}
}

func TestVerifyRejectsUnsignedManifest(t *testing.T) {
	_, verifier := testSignerVerifier(t)
	payload, err := manifest.EncodePayload(testManifest(t))
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := manifest.EncodeEnvelope(payload, nil)
	if err != nil {
		t.Fatal(err)
	}

	if err := verifier.VerifyManifest(context.Background(), envelope); err == nil {
		t.Fatal("expected unsigned manifest to fail verification")
	}
}

func TestVerifyPreservesUnknownPayloadFields(t *testing.T) {
	signer, verifier := testSignerVerifier(t)
	payload, err := manifest.EncodePayload(testManifest(t))
	if err != nil {
		t.Fatal(err)
	}

	// Inject an unknown field directly into the payload bytes. The signature
	// is computed over these bytes, so verification must succeed even though
	// the manifest struct doesn't know the field.
	var raw map[string]any
	if err := json.Unmarshal(payload, &raw); err != nil {
		t.Fatal(err)
	}
	raw["future_field"] = "from a newer publisher"
	payloadWithExtra, err := json.Marshal(raw)
	if err != nil {
		t.Fatal(err)
	}

	sig, err := signer.SignPayload(context.Background(), payloadWithExtra)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := manifest.EncodeEnvelope(payloadWithExtra, sig)
	if err != nil {
		t.Fatal(err)
	}

	if err := verifier.VerifyManifest(context.Background(), envelope); err != nil {
		t.Fatalf("unknown payload field broke verification: %v", err)
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

func signAndEncode(t *testing.T, signer *Signer, m *manifest.Manifest) []byte {
	t.Helper()
	payload, err := manifest.EncodePayload(m)
	if err != nil {
		t.Fatal(err)
	}
	sig, err := signer.SignPayload(context.Background(), payload)
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := manifest.EncodeEnvelope(payload, sig)
	if err != nil {
		t.Fatal(err)
	}
	return envelope
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
