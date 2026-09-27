package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

type IdentitySubject struct {
	ID         string
	Username   string
	SuperAdmin bool
	Status     string
	ExpiresAt  *time.Time
}

type IdentityRepository interface {
	FindIdentitySubject(ctx context.Context, userID string) (IdentitySubject, bool, error)
	FindIdentitySubjectByTokenHash(ctx context.Context, tokenHash string) (IdentitySubject, bool, error)
}

type IdentityService struct {
	repository IdentityRepository
	now        func() time.Time
}

func NewIdentityService(repository IdentityRepository) (*IdentityService, error) {
	if repository == nil {
		return nil, errors.New("identity repository is required")
	}
	return &IdentityService{
		repository: repository,
		now:        func() time.Time { return time.Now().UTC() },
	}, nil
}

func (s *IdentityService) FindIdentitySubject(
	ctx context.Context,
	userID string,
) (IdentitySubject, bool, error) {
	userID = strings.TrimSpace(userID)
	if userID == "" {
		return IdentitySubject{}, false, nil
	}
	subject, found, err := s.repository.FindIdentitySubject(ctx, userID)
	if err != nil {
		return IdentitySubject{}, false, fmt.Errorf("find identity subject: %w", err)
	}
	return s.activeSubject(subject, found)
}

func (s *IdentityService) FindIdentitySubjectByTokenHash(
	ctx context.Context,
	tokenHash string,
) (IdentitySubject, bool, error) {
	tokenHash = strings.TrimSpace(tokenHash)
	if tokenHash == "" {
		return IdentitySubject{}, false, nil
	}
	subject, found, err := s.repository.FindIdentitySubjectByTokenHash(ctx, tokenHash)
	if err != nil {
		return IdentitySubject{}, false, fmt.Errorf("find identity subject by token hash: %w", err)
	}
	return s.activeSubject(subject, found)
}

func (s *IdentityService) activeSubject(
	subject IdentitySubject,
	found bool,
) (IdentitySubject, bool, error) {
	if !found || subject.Status != "active" {
		return IdentitySubject{}, false, nil
	}
	if !subject.SuperAdmin && subject.ExpiresAt != nil && !subject.ExpiresAt.After(s.now().UTC()) {
		return IdentitySubject{}, false, nil
	}
	return subject, true, nil
}
