package admin

import (
	"context"

	"jianmen/internal/service"
)

type loginCaptchaVerifier interface {
	CreateChallenge() (service.LoginCaptchaChallenge, error)
	Verify(payload string) error
}

type authorizationService interface {
	AuthorizeConnection(
		ctx context.Context,
		userID string,
		actions []string,
		resourceType string,
		resourceID string,
	) (bool, error)
	AuthorizeBatch(context.Context, string, []service.AuthorizationRequest) ([]service.AuthorizationDecision, error)
}
