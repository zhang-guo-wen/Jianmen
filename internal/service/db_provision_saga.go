package service

import (
	"context"
	cryptorand "crypto/rand"
	"errors"
	"io"
	"log/slog"
	"math/big"
	"strings"
	"time"

	"jianmen/internal/model"
)

const (
	ProvisioningStageReserved             = "reserved"
	ProvisioningStageCreateStarted        = "create_started"
	ProvisioningStageCreateUncertain      = "create_uncertain"
	ProvisioningStageUpstreamCreated      = "upstream_created"
	ProvisioningStageGrantStarted         = "grant_started"
	ProvisioningStageActivationPending    = "activation_pending"
	ProvisioningStageCleanupRequired      = "cleanup_required"
	ProvisioningStageCleanupInProgress    = "cleanup_in_progress"
	ProvisioningStageNotCreated           = "not_created"
	ProvisioningStageActiveManaged        = "active_managed"
	ProvisioningStageDeprovisionRequested = "deprovision_requested"
	ProvisioningStageDropStarted          = "drop_started"
	ProvisioningStageDropUncertain        = "drop_uncertain"

	ProvisioningCleanupNone       = "none"
	ProvisioningCleanupRequired   = "required"
	ProvisioningCleanupInProgress = "in_progress"
	ProvisioningCleanupFailed     = "failed"

	ProvisioningErrorCreateUncertain  = "create_outcome_uncertain"
	ProvisioningErrorGrantFailed      = "grant_failed"
	ProvisioningErrorActivationFailed = "activation_failed"
	ProvisioningErrorCleanupFailed    = "cleanup_failed"
)

var (
	ErrDatabaseProvisioningFailed              = errors.New("database account provisioning failed")
	ErrDatabaseProvisioningCleanupRequired     = errors.New("database account provisioning cleanup is required")
	ErrInvalidDatabaseProvisioningRequest      = errors.New("invalid database provisioning request")
	ErrDatabaseProvisioningIdempotencyConflict = errors.New("database account provisioning idempotency conflict")
	ErrDatabaseProvisioningInProgress          = errors.New("database account provisioning is in progress")
	ErrDatabaseAccountNotManaged               = errors.New("database account is not managed")
	ErrDatabaseDeprovisionFailed               = errors.New("database account deprovision failed")
	ErrDatabaseDeprovisionInProgress           = errors.New("database account deprovision is in progress")
)

type DatabaseAccountCreateDisposition string

const (
	DatabaseAccountCreateNotSent      DatabaseAccountCreateDisposition = "not_sent"
	DatabaseAccountCreateNotCreated   DatabaseAccountCreateDisposition = "not_created"
	DatabaseAccountCreateMayBeApplied DatabaseAccountCreateDisposition = "may_be_applied"
	DatabaseAccountCreateApplied      DatabaseAccountCreateDisposition = "applied"
)

type DatabaseAccountCreateResult struct {
	Disposition DatabaseAccountCreateDisposition
}

type DatabaseProvisioningOperationInput struct {
	ID             string
	InstanceID     string
	AdminAccountID string
	Username       string
	Password       string `json:"-"`
	Host           string
	GrantsJSON     string
	Group          string
	Remark         string
	ExpiresAt      *time.Time
	ActorID        string
	IdempotencyKey string
	RequestHash    string
	Lease          DatabaseProvisioningLease
	// Administrator credentials are used only to prove that the transaction
	// creating an operation still owns the exact credential validated by the
	// caller. They are never persisted in the operation.
	AdministratorUsername string
	AdministratorPassword string
	// InstanceProof is a SHA-256 digest of every connection-affecting instance
	// field. It is checked under the same database transaction as the
	// administrator credential proof and is never persisted.
	InstanceProof string
}

type DatabaseProvisioningOperation struct {
	ID             string
	InstanceID     string
	AdminAccountID string
	Username       string
	Password       string `json:"-"`
	Host           string
	GrantsJSON     string
	Group          string
	Remark         string
	ExpiresAt      *time.Time
	ActorID        string
	IdempotencyKey string
	RequestHash    string
	Stage          string
	CleanupStatus  string
	LastError      string
	AttemptCount   int
	LastAttemptAt  *time.Time
	Revision       int64
	LeaseOwner     string
	LeaseToken     string
	LeaseExpiresAt *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type DatabaseProvisioningFence struct {
	ID            string
	Stage         string
	CleanupStatus string
	Revision      int64
	LeaseOwner    string
	LeaseToken    string
}

func (o DatabaseProvisioningOperation) Fence() DatabaseProvisioningFence {
	return DatabaseProvisioningFence{
		ID: o.ID, Stage: o.Stage, CleanupStatus: o.CleanupStatus,
		Revision: o.Revision, LeaseOwner: o.LeaseOwner, LeaseToken: o.LeaseToken,
	}
}

type DatabaseProvisioningTransition struct {
	Stage            string
	CleanupStatus    string
	LastError        string
	IncrementAttempt bool
	ReleaseLease     bool
}

type DatabaseProvisioningLease struct {
	Owner    string
	Token    string
	Duration time.Duration
}

type DatabaseProvisioningLeaseWindow struct {
	Remaining time.Duration
}

type ProvisionedDatabaseAccount struct {
	ID                      string     `json:"id"`
	InstanceID              string     `json:"instance_id"`
	UniqueName              string     `json:"unique_name"`
	Username                string     `json:"username"`
	Group                   string     `json:"group,omitempty"`
	Remark                  string     `json:"remark,omitempty"`
	ExpiresAt               *time.Time `json:"expires_at,omitempty"`
	Status                  string     `json:"status"`
	ResourceID              string     `json:"resource_id,omitempty"`
	ResourceSeq             int        `json:"resource_seq,omitempty"`
	CreatedAt               string     `json:"created_at,omitempty"`
	UpdatedAt               string     `json:"updated_at,omitempty"`
	Managed                 bool       `json:"-"`
	UpstreamHost            string     `json:"-"`
	ProvisioningOperationID string     `json:"-"`
}

type ProvisionDatabaseAccountRequest struct {
	InstanceID     string
	AdminAccountID string
	Host           string
	Grants         []DBGrant
	Group          string
	Remark         string
	ExpiresAt      *time.Time
	Actor          DatabaseProvisioningActor
	IdempotencyKey string
}

type ProvisionDatabaseAccountResult struct {
	Account     ProvisionedDatabaseAccount `json:"account"`
	OperationID string                     `json:"operation_id"`
}

type DatabaseProvisioningOptions struct {
	Random              io.Reader
	CleanupTimeout      time.Duration
	LeaseDuration       time.Duration
	ReconcileStaleAfter time.Duration
	WorkerID            string
	Now                 func() time.Time
	Logger              *slog.Logger
}

type DatabaseProvisioningRepository interface {
	DatabaseProvisioningAdmin(
		context.Context,
		string,
		string,
	) (model.DatabaseInstance, model.DatabaseAccount, error)
	CreateDatabaseProvisioningOperation(
		context.Context,
		DatabaseProvisioningOperationInput,
	) (DatabaseProvisioningOperation, DatabaseProvisioningLeaseWindow, error)
	CreateOrGetDatabaseProvisioningOperation(
		context.Context,
		DatabaseProvisioningOperationInput,
	) (DatabaseProvisioningOperation, DatabaseProvisioningLeaseWindow, bool, error)
	DatabaseProvisioningOperationByIdempotency(
		context.Context,
		string,
		string,
	) (DatabaseProvisioningOperation, bool, error)
	ProvisionedDatabaseAccountByOperation(
		context.Context,
		string,
	) (ProvisionedDatabaseAccount, bool, error)
	DatabaseProvisioningOperation(
		context.Context,
		string,
	) (DatabaseProvisioningOperation, error)
	TransitionDatabaseProvisioningOperation(
		context.Context,
		DatabaseProvisioningFence,
		DatabaseProvisioningTransition,
	) (DatabaseProvisioningOperation, bool, error)
	ListExecutableDatabaseProvisioningOperations(
		context.Context,
		time.Duration,
		int,
	) ([]DatabaseProvisioningOperation, error)
	ClaimDatabaseProvisioningOperation(
		context.Context,
		DatabaseProvisioningFence,
		DatabaseProvisioningLease,
	) (DatabaseProvisioningOperation, DatabaseProvisioningLeaseWindow, bool, error)
	RenewDatabaseProvisioningOperation(
		context.Context,
		DatabaseProvisioningFence,
		DatabaseProvisioningLease,
	) (DatabaseProvisioningOperation, DatabaseProvisioningLeaseWindow, bool, error)
	ActivateDatabaseProvisioningOperation(
		context.Context,
		DatabaseProvisioningFence,
	) (ProvisionedDatabaseAccount, bool, error)
	DeleteDatabaseProvisioningOperation(
		context.Context,
		DatabaseProvisioningFence,
	) (bool, error)
	BeginDatabaseDeprovision(
		context.Context,
		string,
		DatabaseProvisioningLease,
	) (DatabaseProvisioningOperation, bool, error)
	CompleteDatabaseDeprovision(
		context.Context,
		DatabaseProvisioningFence,
		string,
	) (bool, error)
	BeginDatabaseProvisioningAudit(
		context.Context,
		DatabaseProvisioningAudit,
	) (string, error)
	CompleteDatabaseProvisioningAudit(context.Context, string, string) error
}

type DatabaseAccountProvisioner interface {
	ListDatabases(
		context.Context,
		model.DatabaseInstance,
		model.DatabaseAccount,
	) ([]string, error)
	CreateAccount(
		context.Context,
		model.DatabaseInstance,
		model.DatabaseAccount,
		string,
		string,
		string,
	) (DatabaseAccountCreateResult, error)
	GrantAccount(
		context.Context,
		model.DatabaseInstance,
		model.DatabaseAccount,
		string,
		string,
		[]DBGrant,
	) error
	DropAccount(
		context.Context,
		model.DatabaseInstance,
		model.DatabaseAccount,
		string,
		string,
	) error
}

type DatabaseProvisioningService struct {
	repository          DatabaseProvisioningRepository
	provisioner         DatabaseAccountProvisioner
	random              io.Reader
	cleanupTimeout      time.Duration
	leaseDuration       time.Duration
	reconcileStaleAfter time.Duration
	workerID            string
	now                 func() time.Time
	logger              *slog.Logger
}

func NewDatabaseProvisioningService(
	repository DatabaseProvisioningRepository,
	provisioner DatabaseAccountProvisioner,
	options DatabaseProvisioningOptions,
) (*DatabaseProvisioningService, error) {
	if repository == nil {
		return nil, errors.New("database provisioning repository is required")
	}
	if provisioner == nil {
		return nil, errors.New("database account provisioner is required")
	}
	if options.Random == nil {
		options.Random = cryptorand.Reader
	}
	if options.CleanupTimeout <= 0 {
		options.CleanupTimeout = 10 * time.Second
	}
	if options.LeaseDuration <= options.CleanupTimeout {
		options.LeaseDuration = 3 * options.CleanupTimeout
	}
	if options.ReconcileStaleAfter <= 0 {
		options.ReconcileStaleAfter = 30 * time.Second
	}
	if options.WorkerID == "" {
		options.WorkerID = "database-provisioner"
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.Logger == nil {
		options.Logger = slog.Default()
	}
	return &DatabaseProvisioningService{
		repository: repository, provisioner: provisioner, random: options.Random,
		cleanupTimeout: options.CleanupTimeout, leaseDuration: options.LeaseDuration,
		reconcileStaleAfter: options.ReconcileStaleAfter, workerID: options.WorkerID,
		now: options.Now, logger: options.Logger,
	}, nil
}

const (
	provisioningOperationAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"
	provisioningPasswordAlphabet  = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789!#$%&()*+,-./:;<=>?@[]^_"
)

func randomProvisioningString(reader io.Reader, length int, alphabet string) (string, error) {
	if reader == nil || length <= 0 || alphabet == "" {
		return "", errors.New("invalid random string input")
	}
	result := make([]byte, length)
	upperBound := big.NewInt(int64(len(alphabet)))
	for index := range result {
		value, err := cryptorand.Int(reader, upperBound)
		if err != nil {
			return "", err
		}
		result[index] = alphabet[value.Int64()]
	}
	return string(result), nil
}

func validateProvisioningAdministratorForNewUse(
	instance model.DatabaseInstance,
	admin model.DatabaseAccount,
	now time.Time,
) error {
	if instance.Status != "active" || admin.Status != "active" {
		return errors.New("database provisioning administrator is unavailable")
	}
	if instance.Protocol != "mysql" {
		return errors.New("database provisioning protocol is unsupported")
	}
	if admin.ExpiresAt != nil && !now.Before(*admin.ExpiresAt) {
		return errors.New("database provisioning administrator is unavailable")
	}
	if admin.Password.GetPlaintext() == "" {
		return errors.New("database provisioning administrator is unavailable")
	}
	return nil
}

func validateProvisioningAdministratorForRecovery(
	instance model.DatabaseInstance,
	admin model.DatabaseAccount,
) error {
	if instance.Status != "active" || admin.Status != "active" {
		return errors.New("database provisioning administrator is unavailable")
	}
	if instance.Protocol != "mysql" || admin.Password.GetPlaintext() == "" {
		return errors.New("database provisioning administrator is unavailable")
	}
	return nil
}

func operationOwnsUpstreamIdentity(operation DatabaseProvisioningOperation) bool {
	token := strings.TrimPrefix(operation.ID, "jmo_")
	return token != operation.ID && len(token) == 20 &&
		operation.Username == "jm_"+token
}
