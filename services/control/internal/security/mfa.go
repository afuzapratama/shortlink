package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"fmt"
	"io"
	"strconv"
	"time"
)

func MFASecret() string {
	raw := make([]byte, 20)
	if _, err := rand.Read(raw); err != nil {
		panic(err)
	}
	return base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(raw)
}
func TOTP(secret string, step int64) string {
	key, err := base32.StdEncoding.WithPadding(base32.NoPadding).DecodeString(secret)
	if err != nil || step < 0 {
		return ""
	}
	var raw [8]byte
	binary.BigEndian.PutUint64(raw[:], uint64(step))
	h := hmac.New(sha1.New, key)
	h.Write(raw[:])
	sum := h.Sum(nil)
	off := sum[len(sum)-1] & 15
	number := binary.BigEndian.Uint32(sum[off:off+4]) & 0x7fffffff
	return fmt.Sprintf("%06d", number%1000000)
}
func MatchTOTP(secret, code string, last int64, now time.Time) (int64, bool) {
	if len(code) != 6 {
		return 0, false
	}
	if _, err := strconv.Atoi(code); err != nil {
		return 0, false
	}
	step := now.Unix() / 30
	for _, n := range []int64{step, step - 1, step + 1} {
		if n > last && subtle.ConstantTimeCompare([]byte(TOTP(secret, n)), []byte(code)) == 1 {
			return n, true
		}
	}
	return 0, false
}
func Seal(seed []byte, purpose string, raw []byte) ([]byte, error) {
	h := hmac.New(sha256.New, seed)
	h.Write([]byte("shortlink-secret-v1:" + purpose))
	block, err := aes.NewCipher(h.Sum(nil))
	if err != nil {
		return nil, err
	}
	a, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, a.NonceSize())
	if _, err = io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return a.Seal(nonce, nonce, raw, []byte(purpose)), nil
}
func OpenSecret(seed []byte, purpose string, raw []byte) ([]byte, error) {
	h := hmac.New(sha256.New, seed)
	h.Write([]byte("shortlink-secret-v1:" + purpose))
	block, err := aes.NewCipher(h.Sum(nil))
	if err != nil {
		return nil, err
	}
	a, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(raw) < a.NonceSize() {
		return nil, fmt.Errorf("secret invalid")
	}
	return a.Open(nil, raw[:a.NonceSize()], raw[a.NonceSize():], []byte(purpose))
}
