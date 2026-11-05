package googleauth

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
)

// Sealed is a refresh token encrypted by a Key: AES-256-GCM, each part in standard base64, kept
// in three columns. Omni and Paper store their tokens in exactly this shape
// (refresh_token_ciphertext_b64, refresh_token_iv_b64, refresh_token_tag_b64), so an app that
// moves to this package opens its existing rows with its existing key.
type Sealed struct {
	Ciphertext string
	// IV is the 12-byte GCM nonce, new for every Seal.
	IV string
	// Tag is the 16-byte GCM authentication tag.
	Tag string
}

// Key seals refresh tokens for the app's store. It is safe for concurrent use.
type Key struct {
	aead cipher.AEAD
}

// ParseKey reads a key of 32 random bytes in standard base64 (openssl rand -base64 32), such as
// the app's token key secret. Surrounding white space is ignored; errors never contain the value.
func ParseKey(b64 string) (*Key, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(b64))
	if err != nil {
		return nil, errors.New("googleauth: token key is not standard base64")
	}
	if len(raw) != 32 {
		return nil, errors.New("googleauth: token key must decode to 32 bytes")
	}
	block, err := aes.NewCipher(raw)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Key{aead: aead}, nil
}

// Seal encrypts a refresh token bound to account, the additional data: the Google account's
// e-mail in lower case (Grant.Email), as Omni and Paper bind theirs. Open then needs the same
// account, so a row copied to another account does not open.
func (k *Key) Seal(refreshToken, account string) Sealed {
	iv := make([]byte, k.aead.NonceSize())
	_, _ = rand.Read(iv) // crypto/rand.Read never fails; it crashes the program instead
	return k.seal(iv, refreshToken, account)
}

func (k *Key) seal(iv []byte, refreshToken, account string) Sealed {
	out := k.aead.Seal(nil, iv, []byte(refreshToken), []byte(account))
	tag := len(out) - k.aead.Overhead()
	return Sealed{
		Ciphertext: base64.StdEncoding.EncodeToString(out[:tag]),
		IV:         base64.StdEncoding.EncodeToString(iv),
		Tag:        base64.StdEncoding.EncodeToString(out[tag:]),
	}
}

// Open decrypts what Seal returned for the same account. A different key or account, or a
// changed part, is an error; the app treats the grant as gone and offers to connect again.
func (k *Key) Open(s Sealed, account string) (string, error) {
	ciphertext, err1 := base64.StdEncoding.DecodeString(s.Ciphertext)
	iv, err2 := base64.StdEncoding.DecodeString(s.IV)
	tag, err3 := base64.StdEncoding.DecodeString(s.Tag)
	if err1 != nil || err2 != nil || err3 != nil || len(iv) != k.aead.NonceSize() || len(tag) != k.aead.Overhead() {
		return "", errors.New("googleauth: sealed token is malformed")
	}
	plain, err := k.aead.Open(nil, iv, append(ciphertext, tag...), []byte(account))
	if err != nil {
		return "", errors.New("googleauth: sealed token does not open with this key and account")
	}
	return string(plain), nil
}
