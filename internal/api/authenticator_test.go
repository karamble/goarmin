// Copyright (c) 2026 karamble
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.

package api

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"testing"

	"github.com/fxamacker/cbor/v2"
)

// softKey is a software passkey: one ES256 credential, as a platform authenticator would
// create it, with "none" attestation.
type softKey struct {
	key    *ecdsa.PrivateKey
	id     []byte
	user   []byte // the user handle the relying party gave at registration
	rpID   string
	origin string
	count  uint32
}

var b64 = base64.RawURLEncoding

func newSoftKey(t *testing.T, rpID, origin string) *softKey {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	id := make([]byte, 16)
	_, _ = rand.Read(id)
	return &softKey{key: k, id: id, rpID: rpID, origin: origin}
}

func (k *softKey) clientData(t *testing.T, typ, challenge string) []byte {
	t.Helper()
	b, err := json.Marshal(map[string]any{"type": typ, "challenge": challenge, "origin": k.origin, "crossOrigin": false})
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func (k *softKey) authData(flags byte, attested []byte) []byte {
	rp := sha256.Sum256([]byte(k.rpID))
	out := append([]byte{}, rp[:]...)
	out = append(out, flags)
	out = binary.BigEndian.AppendUint32(out, k.count)
	return append(out, attested...)
}

// register answers PublicKeyCredentialCreationOptions (the JSON BeginRegistration returns).
func (k *softKey) register(t *testing.T, options []byte) []byte {
	t.Helper()
	var opt struct {
		PublicKey struct {
			Challenge string              `json:"challenge"`
			RP        struct{ ID string } `json:"rp"`
			User      struct{ ID string } `json:"user"`
		} `json:"publicKey"`
	}
	if err := json.Unmarshal(options, &opt); err != nil {
		t.Fatal(err)
	}
	if opt.PublicKey.RP.ID != k.rpID {
		t.Fatalf("relying party %q, want %q", opt.PublicKey.RP.ID, k.rpID)
	}
	user, err := b64.DecodeString(opt.PublicKey.User.ID)
	if err != nil {
		t.Fatal(err)
	}
	k.user = user
	pub := k.key.PublicKey
	x, y := make([]byte, 32), make([]byte, 32)
	pub.X.FillBytes(x)
	pub.Y.FillBytes(y)
	cose, err := cbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: x, -3: y})
	if err != nil {
		t.Fatal(err)
	}
	attested := make([]byte, 16) // AAGUID: none
	attested = binary.BigEndian.AppendUint16(attested, uint16(len(k.id)))
	attested = append(attested, k.id...)
	attested = append(attested, cose...)
	ad := k.authData(0x45, attested) // user present, user verified, attested credential data
	att, err := cbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": ad})
	if err != nil {
		t.Fatal(err)
	}
	cd := k.clientData(t, "webauthn.create", opt.PublicKey.Challenge)
	b, _ := json.Marshal(map[string]any{
		"id": b64.EncodeToString(k.id), "rawId": b64.EncodeToString(k.id), "type": "public-key",
		"response":               map[string]any{"clientDataJSON": b64.EncodeToString(cd), "attestationObject": b64.EncodeToString(att)},
		"clientExtensionResults": map[string]any{},
	})
	return b
}

// login answers PublicKeyCredentialRequestOptions (the JSON BeginDiscoverableLogin returns).
func (k *softKey) login(t *testing.T, options []byte) []byte {
	t.Helper()
	var opt struct {
		PublicKey struct {
			Challenge string `json:"challenge"`
		} `json:"publicKey"`
	}
	if err := json.Unmarshal(options, &opt); err != nil {
		t.Fatal(err)
	}
	k.count++
	ad := k.authData(0x05, nil) // user present, user verified
	cd := k.clientData(t, "webauthn.get", opt.PublicKey.Challenge)
	h := sha256.Sum256(cd)
	digest := sha256.Sum256(append(append([]byte{}, ad...), h[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, k.key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]any{
		"id": b64.EncodeToString(k.id), "rawId": b64.EncodeToString(k.id), "type": "public-key",
		"response": map[string]any{"clientDataJSON": b64.EncodeToString(cd), "authenticatorData": b64.EncodeToString(ad),
			"signature": b64.EncodeToString(sig), "userHandle": b64.EncodeToString(k.user)},
		"clientExtensionResults": map[string]any{},
	})
	return b
}
