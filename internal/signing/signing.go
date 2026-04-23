package signing

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"strconv"
	"strings"
	"time"
)

type Signer struct {
	Secret []byte
	TTL    time.Duration
}

func New(secret string, ttl time.Duration) *Signer {
	return &Signer{Secret: []byte(secret), TTL: ttl}
}

func (s *Signer) Sign(path string, now time.Time) (exp string, sig string) {
	e := strconv.FormatInt(now.Add(s.TTL).Unix(), 10)
	return e, s.mac(path, e)
}

func (s *Signer) Verify(path, exp, sig string, now time.Time) error {
	if exp == "" || sig == "" {
		return errors.New("missing signature")
	}
	ts, err := strconv.ParseInt(exp, 10, 64)
	if err != nil {
		return errors.New("bad expiry")
	}
	if now.Unix() > ts {
		return errors.New("expired")
	}
	expected := s.mac(path, exp)
	a, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		return errors.New("bad signature")
	}
	b, _ := base64.RawURLEncoding.DecodeString(expected)
	if !hmac.Equal(a, b) {
		return errors.New("invalid signature")
	}
	return nil
}

func (s *Signer) mac(path, exp string) string {
	h := hmac.New(sha256.New, s.Secret)
	h.Write([]byte(path))
	h.Write([]byte{'|'})
	h.Write([]byte(exp))
	return base64.RawURLEncoding.EncodeToString(h.Sum(nil))
}

// RewriteSegments rewrites .ts lines in an HLS playlist by appending ?exp=..&sig=..
// urlPathFor builds the full URL path the client will request so the signature
// binds to that exact path.
func (s *Signer) RewriteSegments(playlist []byte, urlPathFor func(segment string) string, now time.Time) []byte {
	lines := strings.Split(string(playlist), "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if !strings.HasSuffix(trimmed, ".ts") {
			continue
		}
		full := urlPathFor(trimmed)
		exp, sig := s.Sign(full, now)
		lines[i] = trimmed + "?exp=" + exp + "&sig=" + sig
	}
	return []byte(strings.Join(lines, "\n"))
}
