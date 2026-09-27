package sshserver

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"jianmen/internal/config"
	"jianmen/internal/model"
	"jianmen/internal/proxy/sshproxy"
	"jianmen/internal/rbac"
)

type authorizationCall struct {
	userID       string
	actions      []string
	resourceType string
	resourceID   string
}

type captureConnectionAuthorizer struct {
	calls  []authorizationCall
	allow  map[string]bool
	errFor map[string]error
}

func (a *captureConnectionAuthorizer) AuthorizeConnection(
	ctx context.Context,
	userID string,
	actions []string,
	resourceType string,
	resourceID string,
) (bool, error) {
	a.calls = append(a.calls, authorizationCall{
		userID:       userID,
		actions:      append([]string(nil), actions...),
		resourceType: resourceType,
		resourceID:   resourceID,
	})
	if err := a.errFor[actions[0]]; err != nil {
		return false, err
	}
	if err := ctx.Err(); err != nil {
		return false, err
	}
	return a.allow[actions[0]], nil
}

func TestAuthorizeTargetBuildsProtocolAccessFromUnifiedAuthorizer(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		allow map[string]bool
		want  sshproxy.Access
	}{
		{
			name: "SSH only",
			allow: map[string]bool{
				rbac.ActionSessionConnect: true,
				rbac.ActionSFTPConnect:    false,
			},
			want: sshproxy.Access{SSH: true},
		},
		{
			name: "SFTP only",
			allow: map[string]bool{
				rbac.ActionSessionConnect: false,
				rbac.ActionSFTPConnect:    true,
			},
			want: sshproxy.Access{SFTP: true},
		},
		{
			name: "both protocols",
			allow: map[string]bool{
				rbac.ActionSessionConnect: true,
				rbac.ActionSFTPConnect:    true,
			},
			want: sshproxy.Access{SSH: true, SFTP: true},
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			authorizer := &captureConnectionAuthorizer{allow: tc.allow}
			server := &Server{authorizer: authorizer}

			got, err := server.authorizeTarget(context.Background(), "user-1", "host-account-1")
			if err != nil {
				t.Fatalf("authorize target: %v", err)
			}
			if got != tc.want {
				t.Fatalf("access = %#v, want %#v", got, tc.want)
			}
			wantCalls := []authorizationCall{
				{userID: "user-1", actions: []string{rbac.ActionSessionConnect}, resourceType: model.ResourceTypeHostAccount, resourceID: "host-account-1"},
				{userID: "user-1", actions: []string{rbac.ActionSFTPConnect}, resourceType: model.ResourceTypeHostAccount, resourceID: "host-account-1"},
			}
			if !reflect.DeepEqual(authorizer.calls, wantCalls) {
				t.Fatalf("authorization calls = %#v, want %#v", authorizer.calls, wantCalls)
			}
		})
	}
}

func TestAuthorizeTargetRejectsWhenAllProtocolsAreDenied(t *testing.T) {
	t.Parallel()
	server := &Server{authorizer: &captureConnectionAuthorizer{allow: map[string]bool{}}}

	_, err := server.authorizeTarget(context.Background(), "user-1", "host-account-1")
	if err == nil {
		t.Fatal("authorize target unexpectedly succeeded")
	}
}

func TestAuthorizeTargetUsesUnifiedServiceForSuperAdministrator(t *testing.T) {
	t.Parallel()
	// Super-admin bypass is owned by the injected authorization service. The SSH
	// server still calls it for both protocol decisions instead of keeping a local map.
	authorizer := &captureConnectionAuthorizer{allow: map[string]bool{
		rbac.ActionSessionConnect: true,
		rbac.ActionSFTPConnect:    true,
	}}
	server := &Server{authorizer: authorizer}

	access, err := server.authorizeTarget(context.Background(), "super-admin-1", "host-account-1")
	if err != nil {
		t.Fatalf("authorize target: %v", err)
	}
	if access != (sshproxy.Access{SSH: true, SFTP: true}) {
		t.Fatalf("access = %#v, want full access", access)
	}
	if len(authorizer.calls) != 2 {
		t.Fatalf("authorization call count = %d, want 2", len(authorizer.calls))
	}
}

func TestAuthorizeTargetFailsClosed(t *testing.T) {
	t.Parallel()
	t.Run("authorizer error", func(t *testing.T) {
		t.Parallel()
		wantErr := errors.New("authorization backend unavailable")
		server := &Server{authorizer: &captureConnectionAuthorizer{
			allow:  map[string]bool{},
			errFor: map[string]error{rbac.ActionSessionConnect: wantErr},
		}}

		_, err := server.authorizeTarget(context.Background(), "user-1", "host-account-1")
		if !errors.Is(err, wantErr) {
			t.Fatalf("error = %v, want wrapped %v", err, wantErr)
		}
	})

	t.Run("cancelled context", func(t *testing.T) {
		t.Parallel()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		server := &Server{authorizer: &captureConnectionAuthorizer{allow: map[string]bool{
			rbac.ActionSessionConnect: true,
			rbac.ActionSFTPConnect:    true,
		}}}

		_, err := server.authorizeTarget(ctx, "user-1", "host-account-1")
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("error = %v, want context cancellation", err)
		}
	})

	t.Run("missing authorizer", func(t *testing.T) {
		t.Parallel()
		_, err := (&Server{}).authorizeTarget(context.Background(), "user-1", "host-account-1")
		if err == nil {
			t.Fatal("authorize target unexpectedly succeeded without authorizer")
		}
	})
}

func TestNewRequiresUnifiedAuthorizer(t *testing.T) {
	t.Parallel()
	_, err := New(&config.Config{}, nil, nil, nil, nil)
	if err == nil || !strings.Contains(err.Error(), "authorization service") {
		t.Fatalf("New error = %v, want missing authorization service", err)
	}
}
