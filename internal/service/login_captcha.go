package service

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"sync"
	"time"

	altcha "github.com/altcha-org/altcha-lib-go"
)

const (
	loginCaptchaTTL       = 2 * time.Minute
	loginCaptchaMaxNumber = 100_000
)

var (
	ErrLoginCaptchaMissing  = errors.New("login captcha is missing")
	ErrLoginCaptchaInvalid  = errors.New("login captcha is invalid")
	ErrLoginCaptchaExpired  = errors.New("login captcha is expired")
	ErrLoginCaptchaReplayed = errors.New("login captcha was already used")
)

// LoginCaptchaChallenge is the public challenge shape consumed by the ALTCHA widget.
type LoginCaptchaChallenge struct {
	Algorithm string `json:"algorithm"`
	Challenge string `json:"challenge"`
	MaxNumber int64  `json:"maxNumber"`
	Salt      string `json:"salt"`
	Signature string `json:"signature"`
}

type LoginCaptcha struct {
	mu        sync.Mutex
	secret    string
	ttl       time.Duration
	maxNumber int64
	used      map[string]time.Time
	clock     func() time.Time
}

func NewLoginCaptcha() (*LoginCaptcha, error) {
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		return nil, fmt.Errorf("generate login captcha secret: %w", err)
	}
	return newLoginCaptcha(base64.RawURLEncoding.EncodeToString(secretBytes), loginCaptchaTTL, loginCaptchaMaxNumber), nil
}

func newLoginCaptcha(secret string, ttl time.Duration, maxNumber int64) *LoginCaptcha {
	return &LoginCaptcha{
		secret:    secret,
		ttl:       ttl,
		maxNumber: maxNumber,
		used:      make(map[string]time.Time),
		clock:     time.Now,
	}
}

func (c *LoginCaptcha) CreateChallenge() (LoginCaptchaChallenge, error) {
	if c == nil || c.secret == "" {
		return LoginCaptchaChallenge{}, errors.New("login captcha is unavailable")
	}

	now := c.clock().UTC()
	expiresAt := now.Add(c.ttl)
	challenge, err := altcha.CreateChallenge(altcha.ChallengeOptions{
		Algorithm: altcha.SHA256,
		MaxNumber: c.maxNumber,
		Expires:   &expiresAt,
		HMACKey:   c.secret,
		Params:    url.Values{},
	})
	if err != nil {
		return LoginCaptchaChallenge{}, fmt.Errorf("create login captcha challenge: %w", err)
	}

	return LoginCaptchaChallenge{
		Algorithm: challenge.Algorithm,
		Challenge: challenge.Challenge,
		MaxNumber: challenge.MaxNumber,
		Salt:      challenge.Salt,
		Signature: challenge.Signature,
	}, nil
}

func (c *LoginCaptcha) Verify(payload string) error {
	if c == nil || c.secret == "" {
		return ErrLoginCaptchaInvalid
	}
	if payload == "" {
		return ErrLoginCaptchaMissing
	}
	if len(payload) > 16*1024 {
		return ErrLoginCaptchaInvalid
	}

	decoded, err := decodeLoginCaptchaPayload(payload)
	if err != nil {
		return fmt.Errorf("%w: decode payload", ErrLoginCaptchaInvalid)
	}

	var parsed altcha.Payload
	if err := json.Unmarshal(decoded, &parsed); err != nil || parsed.Signature == "" {
		return fmt.Errorf("%w: parse payload", ErrLoginCaptchaInvalid)
	}

	expiresAt, err := loginCaptchaExpiresAt(parsed)
	if err != nil {
		return fmt.Errorf("%w: parse expiry", ErrLoginCaptchaInvalid)
	}
	now := c.clock().UTC()
	if !expiresAt.After(now) {
		return ErrLoginCaptchaExpired
	}

	valid, err := altcha.VerifySolutionSafe(decoded, c.secret, true)
	if err != nil {
		return fmt.Errorf("%w: verify payload", ErrLoginCaptchaInvalid)
	}
	if !valid {
		return ErrLoginCaptchaInvalid
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.cleanupLocked(now)
	if _, ok := c.used[parsed.Signature]; ok {
		return ErrLoginCaptchaReplayed
	}
	c.used[parsed.Signature] = expiresAt
	return nil
}

func (c *LoginCaptcha) cleanupLocked(now time.Time) {
	for signature, expiresAt := range c.used {
		if !expiresAt.After(now) {
			delete(c.used, signature)
		}
	}
}

func loginCaptchaExpiresAt(payload altcha.Payload) (time.Time, error) {
	raw := altcha.ExtractParams(payload).Get("expires")
	if raw == "" {
		return time.Time{}, errors.New("expiry is missing")
	}
	seconds, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse expiry: %w", err)
	}
	return time.Unix(seconds, 0).UTC(), nil
}

func decodeLoginCaptchaPayload(payload string) ([]byte, error) {
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err == nil {
		return decoded, nil
	}
	return base64.RawStdEncoding.DecodeString(payload)
}
