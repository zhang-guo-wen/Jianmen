package service

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"testing"
	"time"

	altcha "github.com/altcha-org/altcha-lib-go"
)

func TestLoginCaptchaVerifyIsSingleUse(t *testing.T) {
	captcha := newLoginCaptcha("test-secret", time.Minute, 1000)
	challenge, err := captcha.CreateChallenge()
	if err != nil {
		t.Fatalf("create challenge: %v", err)
	}
	payload := solveLoginCaptchaPayload(t, challenge)

	if err := captcha.Verify(payload); err != nil {
		t.Fatalf("verify payload: %v", err)
	}
	if err := captcha.Verify(payload); !errors.Is(err, ErrLoginCaptchaReplayed) {
		t.Fatalf("replay error = %v, want ErrLoginCaptchaReplayed", err)
	}
}

func TestLoginCaptchaRejectsExpiredChallenge(t *testing.T) {
	now := time.Now().UTC()
	captcha := newLoginCaptcha("test-secret", time.Minute, 1000)
	captcha.clock = func() time.Time { return now }
	challenge, err := captcha.CreateChallenge()
	if err != nil {
		t.Fatalf("create challenge: %v", err)
	}
	payload := solveLoginCaptchaPayload(t, challenge)
	captcha.clock = func() time.Time { return now.Add(2 * time.Minute) }

	if err := captcha.Verify(payload); !errors.Is(err, ErrLoginCaptchaExpired) {
		t.Fatalf("expired error = %v, want ErrLoginCaptchaExpired", err)
	}
}

func TestLoginCaptchaRejectsInvalidPayload(t *testing.T) {
	captcha := newLoginCaptcha("test-secret", time.Minute, 1000)
	challenge, err := captcha.CreateChallenge()
	if err != nil {
		t.Fatalf("create challenge: %v", err)
	}
	payload := solveLoginCaptchaPayload(t, challenge)
	decoded, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	var parsed altcha.Payload
	if err := json.Unmarshal(decoded, &parsed); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	parsed.Number++
	tampered, err := json.Marshal(parsed)
	if err != nil {
		t.Fatalf("marshal tampered payload: %v", err)
	}

	if err := captcha.Verify(base64.StdEncoding.EncodeToString(tampered)); !errors.Is(err, ErrLoginCaptchaInvalid) {
		t.Fatalf("tampered error = %v, want ErrLoginCaptchaInvalid", err)
	}
}

func solveLoginCaptchaPayload(t *testing.T, challenge LoginCaptchaChallenge) string {
	t.Helper()
	solution, err := altcha.SolveChallenge(challenge.Challenge, challenge.Salt, altcha.SHA256, int(challenge.MaxNumber), 0, nil)
	if err != nil {
		t.Fatalf("solve challenge: %v", err)
	}
	payload, err := json.Marshal(altcha.Payload{
		Algorithm: challenge.Algorithm,
		Challenge: challenge.Challenge,
		Number:    int64(solution.Number),
		Salt:      challenge.Salt,
		Signature: challenge.Signature,
	})
	if err != nil {
		t.Fatalf("marshal solution: %v", err)
	}
	return base64.StdEncoding.EncodeToString(payload)
}
