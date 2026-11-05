package googleauth

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"
)

// testKey is the key of the vectors below (bytes 0x00..0x1f).
const testKey = "AAECAwQFBgcICQoLDA0ODxAREhMUFRYXGBkaGxwdHh8="

// Rows as the apps store them today. Moving to this package must open them unchanged, and what
// Seal writes must open in the apps' own code, or every person would have to consent again.
var vectors = []struct {
	name    string
	sealed  Sealed
	account string
	plain   string
}{
	{
		// Sealed by Omni's encryptToken (internal/google/crypto.go, which Paper copied).
		name:    "omni",
		sealed:  Sealed{Ciphertext: "cY05CmQ6FzMNmp7L/858YS3f/kB/UqFXkK5ctByqLA==", IV: "nQOZAVPmVDQCQjcY", Tag: "IrT220r8ayIQojE9L8MpBA=="},
		account: "alice@example.org",
		plain:   "1//refresh-token-sealed-by-omni",
	},
	{
		// Sealed by Node's crypto.createCipheriv('aes-256-gcm') with setAAD, the implementation
		// Omni's rows date from (Omni's TestDecryptsNodeAESGCMFixture).
		name:    "node",
		sealed:  Sealed{Ciphertext: "qOOWwSoUIzMGD6R5V+rrRU13wTA=", IV: "ABEiM0RVZneImaq7", Tag: "C9dLergIm+ADrOnAbS/zhg=="},
		account: "me@b.com",
		plain:   "refresh-node-fixture",
	},
}

func TestOpensAndWritesTheAppsStoredFormat(t *testing.T) {
	key, err := ParseKey(testKey)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range vectors {
		t.Run(v.name, func(t *testing.T) {
			got, err := key.Open(v.sealed, v.account)
			if err != nil || got != v.plain {
				t.Fatalf("Open = %q, %v; want %q", got, err, v.plain)
			}
			// With the same nonce Seal writes the same three values byte for byte, so the
			// apps' own code opens what this package stores.
			iv, _ := base64.StdEncoding.DecodeString(v.sealed.IV)
			if again := key.seal(iv, v.plain, v.account); again != v.sealed {
				t.Fatalf("seal = %+v; want %+v", again, v.sealed)
			}
		})
	}
}

func TestSealOpenRoundTripAndFailures(t *testing.T) {
	key, err := ParseKey(testKey)
	if err != nil {
		t.Fatal(err)
	}
	other, err := ParseKey(base64.StdEncoding.EncodeToString([]byte(strings.Repeat("k", 32))))
	if err != nil {
		t.Fatal(err)
	}
	sealed := key.Seal("1//refresh", "alice@example.org")
	if again := key.Seal("1//refresh", "alice@example.org"); again.IV == sealed.IV || again.Ciphertext == sealed.Ciphertext {
		t.Fatal("two seals share a nonce")
	}
	if got, err := key.Open(sealed, "alice@example.org"); err != nil || got != "1//refresh" {
		t.Fatalf("round trip = %q, %v", got, err)
	}

	flip := func(b64 string) string {
		raw, _ := base64.StdEncoding.DecodeString(b64)
		raw[0] ^= 1
		return base64.StdEncoding.EncodeToString(raw)
	}
	cases := []struct {
		name    string
		key     *Key
		sealed  Sealed
		account string
	}{
		{"other account", key, sealed, "bob@example.org"},
		{"account case", key, sealed, "Alice@example.org"},
		{"other key", other, sealed, "alice@example.org"},
		{"changed ciphertext", key, Sealed{flip(sealed.Ciphertext), sealed.IV, sealed.Tag}, "alice@example.org"},
		{"changed nonce", key, Sealed{sealed.Ciphertext, flip(sealed.IV), sealed.Tag}, "alice@example.org"},
		{"changed tag", key, Sealed{sealed.Ciphertext, sealed.IV, flip(sealed.Tag)}, "alice@example.org"},
		{"short tag", key, Sealed{sealed.Ciphertext, sealed.IV, "AAAA"}, "alice@example.org"},
		{"not base64", key, Sealed{"%%%", sealed.IV, sealed.Tag}, "alice@example.org"},
		{"empty", key, Sealed{}, "alice@example.org"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got, err := tc.key.Open(tc.sealed, tc.account); err == nil {
				t.Fatalf("opened %q", got)
			}
		})
	}
}

func TestParseKey(t *testing.T) {
	cases := []struct {
		name string
		in   string
		ok   bool
	}{
		{"32 bytes", testKey, true},
		{"trailing newline", testKey + "\n", true},
		{"16 bytes", base64.StdEncoding.EncodeToString(make([]byte, 16)), false},
		{"33 bytes", base64.StdEncoding.EncodeToString(make([]byte, 33)), false},
		{"url alphabet", base64.URLEncoding.EncodeToString(bytes.Repeat([]byte{0xff}, 32)), false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseKey(tc.in)
			if (err == nil) != tc.ok {
				t.Fatalf("ParseKey error = %v, want ok %v", err, tc.ok)
			}
			if err != nil && tc.in != "" && strings.Contains(err.Error(), strings.TrimSpace(tc.in)) {
				t.Fatalf("error leaks the key: %v", err)
			}
		})
	}
}
