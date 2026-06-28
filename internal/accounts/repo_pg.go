package accounts

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/huseyn0w/cmstack-go/internal/platform/db/sqlcgen"
)

// querier is the subset of *sqlcgen.Queries the repositories use. Defining it
// locally keeps the repos honest about their data access and testable.
type querier interface {
	GetUserByID(ctx context.Context, id pgtype.UUID) (sqlcgen.User, error)
	GetUserByEmail(ctx context.Context, email string) (sqlcgen.User, error)
	GetUserByUsername(ctx context.Context, username *string) (sqlcgen.User, error)
	CreateUser(ctx context.Context, arg sqlcgen.CreateUserParams) (sqlcgen.User, error)
	SetUserPassword(ctx context.Context, arg sqlcgen.SetUserPasswordParams) error
	MarkEmailVerified(ctx context.Context, id pgtype.UUID) error

	GetRoleByKey(ctx context.Context, key string) (sqlcgen.Role, error)
	GetRoleByID(ctx context.Context, id pgtype.UUID) (sqlcgen.Role, error)
	ListRolePermissions(ctx context.Context) ([]sqlcgen.ListRolePermissionsRow, error)

	CreateEmailVerificationToken(ctx context.Context, arg sqlcgen.CreateEmailVerificationTokenParams) (sqlcgen.EmailVerificationToken, error)
	GetEmailVerificationToken(ctx context.Context, tokenHash string) (sqlcgen.EmailVerificationToken, error)
	ConsumeEmailVerificationToken(ctx context.Context, id pgtype.UUID) (pgtype.UUID, error)
	CreatePasswordResetToken(ctx context.Context, arg sqlcgen.CreatePasswordResetTokenParams) (sqlcgen.PasswordResetToken, error)
	GetPasswordResetToken(ctx context.Context, tokenHash string) (sqlcgen.PasswordResetToken, error)
	ConsumePasswordResetToken(ctx context.Context, id pgtype.UUID) (pgtype.UUID, error)
}

// compile-time assertions that wiring satisfies the domain interfaces.
var (
	_ querier         = (*sqlcgen.Queries)(nil)
	_ UserRepository  = (*UserRepoPG)(nil)
	_ RoleRepository  = (*RoleRepoPG)(nil)
	_ TokenRepository = (*TokenRepoPG)(nil)
)

// UserRepoPG is the sqlc/pgx-backed UserRepository — the ONLY layer touching
// generated SQL for users.
type UserRepoPG struct{ q *sqlcgen.Queries }

// NewUserRepoPG constructs a UserRepoPG. The base Queries carries the pool;
// transactional methods rebind it via WithTx.
func NewUserRepoPG(q *sqlcgen.Queries) *UserRepoPG { return &UserRepoPG{q: q} }

// GetByID loads a user by id, returning ErrNotFound when absent.
func (r *UserRepoPG) GetByID(ctx context.Context, id uuid.UUID) (User, error) {
	row, err := r.q.GetUserByID(ctx, toPgUUID(id))
	return userFromRow(row), mapErr(err)
}

// GetByEmail loads a user by (case-insensitive) email.
func (r *UserRepoPG) GetByEmail(ctx context.Context, email string) (User, error) {
	row, err := r.q.GetUserByEmail(ctx, email)
	return userFromRow(row), mapErr(err)
}

// GetByUsername loads a user by username.
func (r *UserRepoPG) GetByUsername(ctx context.Context, username string) (User, error) {
	row, err := r.q.GetUserByUsername(ctx, &username)
	return userFromRow(row), mapErr(err)
}

// CreateTx inserts a user within tx.
func (r *UserRepoPG) CreateTx(ctx context.Context, tx pgx.Tx, in CreateUserInput) (User, error) {
	row, err := r.q.WithTx(tx).CreateUser(ctx, sqlcgen.CreateUserParams{
		Email:           in.Email,
		Username:        optString(in.Username),
		PasswordHash:    in.PasswordHash,
		Name:            in.Name,
		RoleID:          toPgUUID(in.RoleID),
		EmailVerifiedAt: optTime(in.EmailVerifiedAt),
	})
	return userFromRow(row), mapErr(err)
}

// SetPasswordTx updates a user's password hash within tx.
func (r *UserRepoPG) SetPasswordTx(ctx context.Context, tx pgx.Tx, id uuid.UUID, hash string) error {
	return mapErr(r.q.WithTx(tx).SetUserPassword(ctx, sqlcgen.SetUserPasswordParams{
		ID:           toPgUUID(id),
		PasswordHash: hash,
	}))
}

// MarkEmailVerifiedTx stamps email_verified_at within tx.
func (r *UserRepoPG) MarkEmailVerifiedTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	return mapErr(r.q.WithTx(tx).MarkEmailVerified(ctx, toPgUUID(id)))
}

// RoleRepoPG is the sqlc-backed RoleRepository.
type RoleRepoPG struct{ q *sqlcgen.Queries }

// NewRoleRepoPG constructs a RoleRepoPG.
func NewRoleRepoPG(q *sqlcgen.Queries) *RoleRepoPG { return &RoleRepoPG{q: q} }

// GetByKey loads a role by its key.
func (r *RoleRepoPG) GetByKey(ctx context.Context, key string) (Role, error) {
	row, err := r.q.GetRoleByKey(ctx, key)
	if err != nil {
		return Role{}, mapErr(err)
	}
	return Role{ID: fromPgUUID(row.ID), Key: row.Key, Label: row.Label}, nil
}

// GetByID loads a role by id.
func (r *RoleRepoPG) GetByID(ctx context.Context, id uuid.UUID) (Role, error) {
	row, err := r.q.GetRoleByID(ctx, toPgUUID(id))
	if err != nil {
		return Role{}, mapErr(err)
	}
	return Role{ID: fromPgUUID(row.ID), Key: row.Key, Label: row.Label}, nil
}

// AllRolePermissions returns the role-key -> permissions map for the authorizer.
func (r *RoleRepoPG) AllRolePermissions(ctx context.Context) (map[string][]Permission, error) {
	rows, err := r.q.ListRolePermissions(ctx)
	if err != nil {
		return nil, mapErr(err)
	}
	out := make(map[string][]Permission, 4)
	for _, row := range rows {
		out[row.RoleKey] = append(out[row.RoleKey], Permission{Action: row.Action, Subject: row.Subject})
	}
	return out, nil
}

// TokenRepoPG is the sqlc-backed TokenRepository.
type TokenRepoPG struct{ q *sqlcgen.Queries }

// NewTokenRepoPG constructs a TokenRepoPG.
func NewTokenRepoPG(q *sqlcgen.Queries) *TokenRepoPG { return &TokenRepoPG{q: q} }

// CreateEmailVerificationTx persists a hashed verification token within tx.
func (r *TokenRepoPG) CreateEmailVerificationTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID, hash string, exp time.Time) error {
	_, err := r.q.WithTx(tx).CreateEmailVerificationToken(ctx, sqlcgen.CreateEmailVerificationTokenParams{
		UserID:    toPgUUID(userID),
		TokenHash: hash,
		ExpiresAt: pgtype.Timestamptz{Time: exp, Valid: true},
	})
	return mapErr(err)
}

// GetEmailVerification loads a verification token by its hash.
func (r *TokenRepoPG) GetEmailVerification(ctx context.Context, hash string) (Token, error) {
	row, err := r.q.GetEmailVerificationToken(ctx, hash)
	if err != nil {
		return Token{}, mapErr(err)
	}
	return tokenFromEmail(row), nil
}

// ConsumeEmailVerificationTx atomically marks a verification token consumed
// within tx. The UPDATE matches only a still-unconsumed row, so a concurrent
// double-use yields pgx.ErrNoRows -> ErrNotFound for the loser; this is the
// single-use gate.
func (r *TokenRepoPG) ConsumeEmailVerificationTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	_, err := r.q.WithTx(tx).ConsumeEmailVerificationToken(ctx, toPgUUID(id))
	return mapErr(err)
}

// CreatePasswordResetTx persists a hashed reset token within tx.
func (r *TokenRepoPG) CreatePasswordResetTx(ctx context.Context, tx pgx.Tx, userID uuid.UUID, hash string, exp time.Time) error {
	_, err := r.q.WithTx(tx).CreatePasswordResetToken(ctx, sqlcgen.CreatePasswordResetTokenParams{
		UserID:    toPgUUID(userID),
		TokenHash: hash,
		ExpiresAt: pgtype.Timestamptz{Time: exp, Valid: true},
	})
	return mapErr(err)
}

// GetPasswordReset loads a reset token by its hash.
func (r *TokenRepoPG) GetPasswordReset(ctx context.Context, hash string) (Token, error) {
	row, err := r.q.GetPasswordResetToken(ctx, hash)
	if err != nil {
		return Token{}, mapErr(err)
	}
	return tokenFromReset(row), nil
}

// ConsumePasswordResetTx atomically marks a reset token consumed within tx. The
// UPDATE matches only a still-unconsumed row, so a concurrent double-use yields
// pgx.ErrNoRows -> ErrNotFound for the loser; this is the single-use gate.
func (r *TokenRepoPG) ConsumePasswordResetTx(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	_, err := r.q.WithTx(tx).ConsumePasswordResetToken(ctx, toPgUUID(id))
	return mapErr(err)
}

// --- conversions -------------------------------------------------------------

func mapErr(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func toPgUUID(id uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: id, Valid: true}
}

func fromPgUUID(id pgtype.UUID) uuid.UUID {
	if !id.Valid {
		return uuid.Nil
	}
	return id.Bytes
}

func optString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

func optTime(t *time.Time) pgtype.Timestamptz {
	if t == nil {
		return pgtype.Timestamptz{}
	}
	return pgtype.Timestamptz{Time: *t, Valid: true}
}

func fromTimestamptz(ts pgtype.Timestamptz) *time.Time {
	if !ts.Valid {
		return nil
	}
	t := ts.Time
	return &t
}

func userFromRow(u sqlcgen.User) User {
	username := ""
	if u.Username != nil {
		username = *u.Username
	}
	return User{
		ID:                fromPgUUID(u.ID),
		Email:             u.Email,
		Username:          username,
		PasswordHash:      u.PasswordHash,
		Name:              u.Name,
		EmailVerifiedAt:   fromTimestamptz(u.EmailVerifiedAt),
		RoleID:            fromPgUUID(u.RoleID),
		PasswordChangedAt: u.PasswordChangedAt.Time,
		CreatedAt:         u.CreatedAt.Time,
		UpdatedAt:         u.UpdatedAt.Time,
	}
}

func tokenFromEmail(t sqlcgen.EmailVerificationToken) Token {
	return Token{
		ID:         fromPgUUID(t.ID),
		UserID:     fromPgUUID(t.UserID),
		TokenHash:  t.TokenHash,
		ExpiresAt:  t.ExpiresAt.Time,
		ConsumedAt: fromTimestamptz(t.ConsumedAt),
	}
}

func tokenFromReset(t sqlcgen.PasswordResetToken) Token {
	return Token{
		ID:         fromPgUUID(t.ID),
		UserID:     fromPgUUID(t.UserID),
		TokenHash:  t.TokenHash,
		ExpiresAt:  t.ExpiresAt.Time,
		ConsumedAt: fromTimestamptz(t.ConsumedAt),
	}
}
