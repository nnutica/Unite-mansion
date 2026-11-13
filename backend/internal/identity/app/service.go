// Package app holds the identity use cases: login/sessions, tenant
// registration and binding accounts to rooms (context.md §6.5).
package app

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"time"

	"unite-mansion/internal/identity/domain"
	"unite-mansion/internal/shared"
)

var (
	// ErrNotFound is returned by the repository for a missing admin or session.
	ErrNotFound = errors.New("not found")
	// ErrUnauthenticated: unknown, expired or revoked session token.
	ErrUnauthenticated = errors.New("session is not valid")
	// ErrRoomNotFound is returned by RoomDirectory for an unknown room number.
	ErrRoomNotFound = errors.New("room not found")
)

type PasswordHasher interface {
	Hash(password string) (string, error)
	Verify(password, hash string) (bool, error)
}

type Repository interface {
	AdminByUsername(ctx context.Context, username string) (domain.Admin, error)
	Admin(ctx context.Context, id string) (domain.Admin, error)

	InsertTenantAccount(ctx context.Context, a *domain.TenantAccount) error // ErrUsernameTaken
	TenantAccount(ctx context.Context, id string, forUpdate bool) (*domain.TenantAccount, error)
	TenantAccountByUsername(ctx context.Context, username string) (*domain.TenantAccount, error)
	ListTenantAccounts(ctx context.Context, status *domain.AccountStatus) ([]*domain.TenantAccount, error)
	ActiveTenantAccounts(ctx context.Context) ([]*domain.TenantAccount, error)
	UpdateTenantAccount(ctx context.Context, a *domain.TenantAccount) error // ErrRoomAlreadyAssigned

	InsertSession(ctx context.Context, s domain.Session) error
	Session(ctx context.Context, tokenHash []byte) (domain.Session, error)
	UpdateSessionExpiry(ctx context.Context, tokenHash []byte, expiresAt time.Time) error
	DeleteSession(ctx context.Context, tokenHash []byte) error
	DeleteSessionsFor(ctx context.Context, subject domain.SubjectType, subjectID string) error
}

// RoomDirectory is implemented by tenancy; identity never reads its tables.
type RoomDirectory interface {
	RoomIDByNumber(ctx context.Context, roomNumber string) (string, error) // ErrRoomNotFound
	RoomNumbers(ctx context.Context) (map[string]string, error)            // room ID → number
}

type EventDispatcher interface {
	Dispatch(ctx context.Context, events ...shared.Event) error
}

type Service struct {
	repo      Repository
	rooms     RoomDirectory
	hasher    PasswordHasher
	tx        shared.TxManager
	events    EventDispatcher
	now       func() time.Time
	newID     func() string
	dummyHash string
}

func NewService(repo Repository, rooms RoomDirectory, hasher PasswordHasher, tx shared.TxManager,
	events EventDispatcher, now func() time.Time, newID func() string) (*Service, error) {
	// Verifying against a dummy hash when the username is unknown keeps login
	// timing the same, so it does not reveal which usernames exist.
	dummy, err := hasher.Hash("dummy-password-for-timing")
	if err != nil {
		return nil, fmt.Errorf("identity: dummy hash: %w", err)
	}
	return &Service{repo: repo, rooms: rooms, hasher: hasher, tx: tx, events: events, now: now, newID: newID, dummyHash: dummy}, nil
}

// LoginResult carries the new session token for the cookie.
type LoginResult struct {
	Token     string
	ExpiresAt time.Time
	Me        Me
}

// Me describes the logged-in user.
type Me struct {
	ID         string
	Role       domain.SubjectType
	Username   string
	FullName   string
	Status     domain.AccountStatus // tenants only
	RoomID     string               // ACTIVE tenants only
	RoomNumber string               // ACTIVE tenants only
}

func (s *Service) AdminLogin(ctx context.Context, username, password string) (LoginResult, error) {
	admin, err := s.repo.AdminByUsername(ctx, domain.NormalizeUsername(username))
	if errors.Is(err, ErrNotFound) {
		s.burnVerify(password)
		return LoginResult{}, domain.ErrInvalidCredentials
	}
	if err != nil {
		return LoginResult{}, fmt.Errorf("admin login: %w", err)
	}
	if err := s.checkPassword(password, admin.PasswordHash); err != nil {
		return LoginResult{}, err
	}
	return s.startSession(ctx, domain.SubjectAdmin, admin.ID, Me{ID: admin.ID, Role: domain.SubjectAdmin, Username: admin.Username})
}

func (s *Service) TenantLogin(ctx context.Context, username, password string) (LoginResult, error) {
	acc, err := s.repo.TenantAccountByUsername(ctx, domain.NormalizeUsername(username))
	if errors.Is(err, domain.ErrAccountNotFound) {
		s.burnVerify(password)
		return LoginResult{}, domain.ErrInvalidCredentials
	}
	if err != nil {
		return LoginResult{}, fmt.Errorf("tenant login: %w", err)
	}
	if err := s.checkPassword(password, acc.PasswordHash); err != nil {
		return LoginResult{}, err
	}
	me, err := s.tenantMe(ctx, acc)
	if err != nil {
		return LoginResult{}, err
	}
	return s.startSession(ctx, domain.SubjectTenant, acc.ID, me)
}

func (s *Service) Logout(ctx context.Context, tokenHash []byte) error {
	if err := s.repo.DeleteSession(ctx, tokenHash); err != nil {
		return fmt.Errorf("logout: %w", err)
	}
	return nil
}

// Authenticate resolves a session token to the current user, sliding tenant
// sessions forward. Account state is read fresh on every request.
func (s *Service) Authenticate(ctx context.Context, token string) (Me, []byte, error) {
	hash := HashToken(token)
	sess, err := s.repo.Session(ctx, hash)
	if errors.Is(err, ErrNotFound) {
		return Me{}, nil, ErrUnauthenticated
	}
	if err != nil {
		return Me{}, nil, fmt.Errorf("authenticate: %w", err)
	}
	now := s.now()
	if sess.Expired(now) {
		return Me{}, nil, ErrUnauthenticated
	}
	me, err := s.Current(ctx, sess.SubjectType, sess.SubjectID)
	if err != nil {
		return Me{}, nil, err
	}
	if exp, ok := sess.RenewedExpiry(now); ok {
		if err := s.repo.UpdateSessionExpiry(ctx, hash, exp); err != nil {
			return Me{}, nil, fmt.Errorf("renew session: %w", err)
		}
	}
	return me, hash, nil
}

// Current loads the user behind a session. A deleted user is ErrUnauthenticated.
func (s *Service) Current(ctx context.Context, subject domain.SubjectType, id string) (Me, error) {
	switch subject {
	case domain.SubjectAdmin:
		admin, err := s.repo.Admin(ctx, id)
		if errors.Is(err, ErrNotFound) {
			return Me{}, ErrUnauthenticated
		}
		if err != nil {
			return Me{}, fmt.Errorf("current admin: %w", err)
		}
		return Me{ID: admin.ID, Role: domain.SubjectAdmin, Username: admin.Username}, nil
	case domain.SubjectTenant:
		acc, err := s.repo.TenantAccount(ctx, id, false)
		if errors.Is(err, domain.ErrAccountNotFound) {
			return Me{}, ErrUnauthenticated
		}
		if err != nil {
			return Me{}, fmt.Errorf("current tenant: %w", err)
		}
		return s.tenantMe(ctx, acc)
	}
	return Me{}, ErrUnauthenticated
}

// Register creates a PENDING tenant account (self sign-up).
func (s *Service) Register(ctx context.Context, username, password, fullName, phone string) (*domain.TenantAccount, error) {
	username = domain.NormalizeUsername(username)
	if err := domain.ValidateUsername(username); err != nil {
		return nil, err
	}
	if err := domain.ValidatePassword(password); err != nil {
		return nil, err
	}
	hash, err := s.hasher.Hash(password)
	if err != nil {
		return nil, fmt.Errorf("register: %w", err)
	}
	acc, err := domain.NewTenantAccount(s.newID(), username, hash, fullName, phone, s.now())
	if err != nil {
		return nil, err
	}
	if err := s.repo.InsertTenantAccount(ctx, acc); err != nil {
		return nil, fmt.Errorf("register %s: %w", username, err)
	}
	return acc, nil
}

func (s *Service) ListTenantAccounts(ctx context.Context, status *domain.AccountStatus) ([]*domain.TenantAccount, error) {
	accounts, err := s.repo.ListTenantAccounts(ctx, status)
	if err != nil {
		return nil, fmt.Errorf("list tenant accounts: %w", err)
	}
	if err := s.fillRoomNumbers(ctx, accounts); err != nil {
		return nil, err
	}
	return accounts, nil
}

// ActiveAccountsByRoom returns the ACTIVE account of each room, keyed by room ID.
func (s *Service) ActiveAccountsByRoom(ctx context.Context) (map[string]*domain.TenantAccount, error) {
	accounts, err := s.repo.ActiveTenantAccounts(ctx)
	if err != nil {
		return nil, fmt.Errorf("active tenant accounts: %w", err)
	}
	byRoom := make(map[string]*domain.TenantAccount, len(accounts))
	for _, a := range accounts {
		if a.RoomID != nil {
			byRoom[*a.RoomID] = a
		}
	}
	return byRoom, nil
}

// AssignRoom binds an account to a room (M-06).
func (s *Service) AssignRoom(ctx context.Context, accountID, roomNumber string) (*domain.TenantAccount, error) {
	var acc *domain.TenantAccount
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		roomID, err := s.rooms.RoomIDByNumber(ctx, roomNumber)
		if err != nil {
			return err
		}
		if acc, err = s.repo.TenantAccount(ctx, accountID, true); err != nil {
			return err
		}
		if err := acc.AssignRoom(roomID, roomNumber, s.now()); err != nil {
			return err
		}
		if err := s.repo.UpdateTenantAccount(ctx, acc); err != nil {
			return err
		}
		return s.events.Dispatch(ctx, acc.PullEvents()...)
	})
	if err != nil {
		return nil, fmt.Errorf("assign account %s to room %s: %w", accountID, roomNumber, err)
	}
	return acc, nil
}

// Unassign unbinds an account when the tenant moves out and ends its sessions.
func (s *Service) Unassign(ctx context.Context, accountID string) (*domain.TenantAccount, error) {
	var acc *domain.TenantAccount
	err := s.tx.WithinTx(ctx, func(ctx context.Context) error {
		var err error
		if acc, err = s.repo.TenantAccount(ctx, accountID, true); err != nil {
			return err
		}
		if err := s.fillRoomNumbers(ctx, []*domain.TenantAccount{acc}); err != nil {
			return err
		}
		if err := acc.Unassign(s.now()); err != nil {
			return err
		}
		if err := s.repo.UpdateTenantAccount(ctx, acc); err != nil {
			return err
		}
		if err := s.repo.DeleteSessionsFor(ctx, domain.SubjectTenant, acc.ID); err != nil {
			return err
		}
		return s.events.Dispatch(ctx, acc.PullEvents()...)
	})
	if err != nil {
		return nil, fmt.Errorf("unassign account %s: %w", accountID, err)
	}
	return acc, nil
}

// ResetPassword lets the admin set a new password for a tenant (no self-service
// reset, §2) and ends the tenant's sessions.
func (s *Service) ResetPassword(ctx context.Context, accountID, password string) error {
	if err := domain.ValidatePassword(password); err != nil {
		return err
	}
	hash, err := s.hasher.Hash(password)
	if err != nil {
		return fmt.Errorf("reset password: %w", err)
	}
	err = s.tx.WithinTx(ctx, func(ctx context.Context) error {
		acc, err := s.repo.TenantAccount(ctx, accountID, true)
		if err != nil {
			return err
		}
		acc.PasswordHash = hash
		if err := s.repo.UpdateTenantAccount(ctx, acc); err != nil {
			return err
		}
		return s.repo.DeleteSessionsFor(ctx, domain.SubjectTenant, acc.ID)
	})
	if err != nil {
		return fmt.Errorf("reset password of %s: %w", accountID, err)
	}
	return nil
}

func (s *Service) tenantMe(ctx context.Context, acc *domain.TenantAccount) (Me, error) {
	me := Me{ID: acc.ID, Role: domain.SubjectTenant, Username: acc.Username, FullName: acc.FullName, Status: acc.Status}
	if acc.Status == domain.StatusActive && acc.RoomID != nil {
		if err := s.fillRoomNumbers(ctx, []*domain.TenantAccount{acc}); err != nil {
			return Me{}, err
		}
		me.RoomID = *acc.RoomID
		if acc.RoomNumber != nil {
			me.RoomNumber = *acc.RoomNumber
		}
	}
	return me, nil
}

func (s *Service) fillRoomNumbers(ctx context.Context, accounts []*domain.TenantAccount) error {
	needed := false
	for _, a := range accounts {
		needed = needed || a.RoomID != nil
	}
	if !needed {
		return nil
	}
	numbers, err := s.rooms.RoomNumbers(ctx)
	if err != nil {
		return fmt.Errorf("room numbers: %w", err)
	}
	for _, a := range accounts {
		if a.RoomID != nil {
			if n, ok := numbers[*a.RoomID]; ok {
				a.RoomNumber = &n
			}
		}
	}
	return nil
}

func (s *Service) startSession(ctx context.Context, subject domain.SubjectType, subjectID string, me Me) (LoginResult, error) {
	token, err := newToken()
	if err != nil {
		return LoginResult{}, err
	}
	sess := domain.NewSession(HashToken(token), subject, subjectID, s.now())
	if err := s.repo.InsertSession(ctx, sess); err != nil {
		return LoginResult{}, fmt.Errorf("create session: %w", err)
	}
	return LoginResult{Token: token, ExpiresAt: sess.ExpiresAt, Me: me}, nil
}

func (s *Service) checkPassword(password, hash string) error {
	ok, err := s.hasher.Verify(password, hash)
	if err != nil {
		return fmt.Errorf("verify password: %w", err)
	}
	if !ok {
		return domain.ErrInvalidCredentials
	}
	return nil
}

// burnVerify spends the same time as a real check; the result is irrelevant.
func (s *Service) burnVerify(password string) {
	if _, err := s.hasher.Verify(password, s.dummyHash); err != nil {
		return // dummy hash is generated by us; an error here only skips the delay
	}
}

// HashToken is how tokens are stored: only the SHA-256 digest is persisted.
func HashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

func newToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("session token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
