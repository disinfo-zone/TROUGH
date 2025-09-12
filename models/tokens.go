package models

import (
	"crypto/rand"
	"errors"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jmoiron/sqlx"
)

// Invite represents an invitation code that can be used to register even when public registration is disabled.
// If MaxUses is nil, uses are unlimited. If ExpiresAt is nil, there is no time limit.
// Security notes:
// - Codes are random base62 strings from 12 random bytes (~96 bits) => ~16 chars
// - Use constant-time comparisons in SQL by letting the DB do exact match on indexed column
// - We track uses and last_used_at atomically via a single UPDATE with constraints
// - We avoid exposing internal IDs; only the code is shared externally
//
// Admins manage invites via admin endpoints only.
type Invite struct {
	ID         uuid.UUID  `db:"id" json:"id"`
	Code       string     `db:"code" json:"code"`
	MaxUses    *int       `db:"max_uses" json:"max_uses"`
	Uses       int        `db:"uses" json:"uses"`
	ExpiresAt  *time.Time `db:"expires_at" json:"expires_at"`
	CreatedBy  *uuid.UUID `db:"created_by" json:"created_by"`
	CreatedAt  time.Time  `db:"created_at" json:"created_at"`
	LastUsedAt *time.Time `db:"last_used_at" json:"last_used_at"`
}

// Invite repository interface is declared in interfaces.go to avoid circular deps

type InviteRepository struct {
	db *sqlx.DB
}

func NewInviteRepository(db *sqlx.DB) *InviteRepository { return &InviteRepository{db: db} }

var base62Alphabet = []byte("0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz")

func encodeBase62(b []byte) string {
	n := new(big.Int).SetBytes(b)
	if n.Sign() == 0 {
		return "0"
	}
	base := big.NewInt(62)
	zero := big.NewInt(0)
	q := new(big.Int)
	r := new(big.Int)
	var out []byte
	for n.Cmp(zero) > 0 {
		q.DivMod(n, base, r)
		out = append(out, base62Alphabet[r.Int64()])
		n.Set(q)
	}
	// reverse
	for l, rr := 0, len(out)-1; l < rr; l, rr = l+1, rr-1 {
		out[l], out[rr] = out[rr], out[l]
	}
	return string(out)
}

func generateInviteCode() (string, error) {
	// 12 random bytes ~ 96 bits entropy => ~16 base62 chars
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return encodeBase62(b), nil
}

func (r *InviteRepository) Create(maxUses *int, expiresAt *time.Time, createdBy *uuid.UUID) (*Invite, error) {
	q := `INSERT INTO invites (code, max_uses, expires_at, created_by) VALUES ($1,$2,$3,$4) RETURNING id, uses, created_at`
	for attempts := 0; attempts < 5; attempts++ {
		code, err := generateInviteCode()
		if err != nil {
			return nil, err
		}
		inv := &Invite{Code: code, MaxUses: maxUses, ExpiresAt: expiresAt, CreatedBy: createdBy}
		if err := r.db.QueryRowx(q, inv.Code, inv.MaxUses, inv.ExpiresAt, inv.CreatedBy).Scan(&inv.ID, &inv.Uses, &inv.CreatedAt); err != nil {
			// Retry on duplicate
			if strings.Contains(strings.ToLower(err.Error()), "duplicate key") {
				continue
			}
			return nil, err
		} else {
			return inv, nil
		}
	}
	return nil, errors.New("failed to generate unique invite code")
}

func (r *InviteRepository) List(page, limit int) ([]Invite, int, error) {
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 200 {
		limit = 50
	}
	offset := (page - 1) * limit
	var total int
	if err := r.db.Get(&total, `SELECT COUNT(*) FROM invites`); err != nil {
		return nil, 0, err
	}
	var out []Invite
	err := r.db.Select(&out, `SELECT * FROM invites ORDER BY created_at DESC LIMIT $1 OFFSET $2`, limit, offset)
	return out, total, err
}

func (r *InviteRepository) GetByCode(code string) (*Invite, error) {
	var inv Invite
	err := r.db.Get(&inv, `SELECT * FROM invites WHERE code=$1`, code)
	if err != nil {
		return nil, err
	}
	return &inv, nil
}

func (r *InviteRepository) GetByCodeWithTx(tx *sqlx.Tx, code string) (*Invite, error) {
	var inv Invite
	err := tx.Get(&inv, `SELECT * FROM invites WHERE code=$1`, code)
	if err != nil {
		return nil, err
	}
	return &inv, nil
}

// Consume validates and consumes one use of the invite atomically.
// Returns updated invite or error if invalid/expired/exhausted.
func (r *InviteRepository) Consume(code string) (*Invite, error) {
	// Atomic update: ensure not expired and not over max uses.
	// If max_uses is NULL => unlimited.
	q := `
        UPDATE invites
        SET uses = uses + 1, last_used_at = NOW()
        WHERE code = $1
          AND (expires_at IS NULL OR NOW() < expires_at)
          AND (max_uses IS NULL OR uses < max_uses)
        RETURNING id, code, max_uses, uses, expires_at, created_by, created_at, last_used_at`
	var inv Invite
	err := r.db.QueryRowx(q, code).Scan(&inv.ID, &inv.Code, &inv.MaxUses, &inv.Uses, &inv.ExpiresAt, &inv.CreatedBy, &inv.CreatedAt, &inv.LastUsedAt)
	if err != nil {
		return nil, errors.New("invalid or expired invite")
	}
	return &inv, nil
}

func (r *InviteRepository) ConsumeWithTx(tx *sqlx.Tx, code string) (*Invite, error) {
	// Atomic update: ensure not expired and not over max uses.
	// If max_uses is NULL => unlimited.
	q := `
        UPDATE invites
        SET uses = uses + 1, last_used_at = NOW()
        WHERE code = $1
          AND (expires_at IS NULL OR NOW() < expires_at)
          AND (max_uses IS NULL OR uses < max_uses)
        RETURNING id, code, max_uses, uses, expires_at, created_by, created_at, last_used_at`
	var inv Invite
	err := tx.QueryRowx(q, code).Scan(&inv.ID, &inv.Code, &inv.MaxUses, &inv.Uses, &inv.ExpiresAt, &inv.CreatedBy, &inv.CreatedAt, &inv.LastUsedAt)
	if err != nil {
		return nil, errors.New("invalid or expired invite")
	}
	return &inv, nil
}

func (r *InviteRepository) Delete(id uuid.UUID) error {
	_, err := r.db.Exec(`DELETE FROM invites WHERE id=$1`, id)
	return err
}

// DeleteUsedAndExpired removes invites that are either fully used (uses >= max_uses when max_uses is not NULL)
// or expired (expires_at in the past). Unlimited invites (max_uses IS NULL) and non-expired invites are preserved.
func (r *InviteRepository) DeleteUsedAndExpired() (int, error) {
	res, err := r.db.Exec(`
        DELETE FROM invites
        WHERE (expires_at IS NOT NULL AND NOW() >= expires_at)
           OR (max_uses IS NOT NULL AND uses >= max_uses)
    `)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// RevertConsume attempts to undo a previous consumption, used when downstream operations fail after consuming.
// This is best-effort and may not perfectly restore last_used_at for historical accuracy, but keeps uses correct.
func (r *InviteRepository) RevertConsume(id uuid.UUID) error {
	_, err := r.db.Exec(`
        UPDATE invites
        SET uses = CASE WHEN uses > 0 THEN uses - 1 ELSE 0 END,
            last_used_at = CASE WHEN uses <= 1 THEN NULL ELSE last_used_at END
        WHERE id = $1`, id)
	return err
}

func (r *InviteRepository) RevertConsumeWithTx(tx *sqlx.Tx, id uuid.UUID) error {
	_, err := tx.Exec(`
        UPDATE invites
        SET uses = CASE WHEN uses > 0 THEN uses - 1 ELSE 0 END,
            last_used_at = CASE WHEN uses <= 1 THEN NULL ELSE last_used_at END
        WHERE id = $1`, id)
	return err
}

func CreatePasswordReset(userID uuid.UUID, tokenHash string, expires time.Time) error {
	_, err := DB().Exec(`INSERT INTO password_resets (user_id, token, expires_at) VALUES ($1,$2,$3)`, userID, tokenHash, expires)
	return err
}

func GetPasswordReset(tokenHash string) (uuid.UUID, time.Time, error) {
	var uid uuid.UUID
	var exp time.Time
	err := DB().QueryRowx(`SELECT user_id, expires_at FROM password_resets WHERE token=$1`, tokenHash).Scan(&uid, &exp)
	return uid, exp, err
}

func DeletePasswordReset(tokenHash string) error {
	_, err := DB().Exec(`DELETE FROM password_resets WHERE token=$1`, tokenHash)
	return err
}

func CreateEmailVerification(userID uuid.UUID, tokenHash string, expires time.Time) error {
	_, err := DB().Exec(`INSERT INTO email_verifications (user_id, token, expires_at) VALUES ($1,$2,$3)`, userID, tokenHash, expires)
	return err
}

func GetEmailVerification(tokenHash string) (uuid.UUID, time.Time, error) {
	var uid uuid.UUID
	var exp time.Time
	err := DB().QueryRowx(`SELECT user_id, expires_at FROM email_verifications WHERE token=$1`, tokenHash).Scan(&uid, &exp)
	return uid, exp, err
}

func DeleteEmailVerification(tokenHash string) error {
	_, err := DB().Exec(`DELETE FROM email_verifications WHERE token=$1`, tokenHash)
	return err
}

func SetEmailVerified(id uuid.UUID, v bool) error {
	_, err := DB().Exec(`UPDATE users SET email_verified=$1 WHERE id=$2`, v, id)
	return err
}

func LastPasswordResetSentAt(userID uuid.UUID) (time.Time, error) {
	var t time.Time
	err := DB().Get(&t, `SELECT COALESCE(MAX(created_at), to_timestamp(0)) FROM password_resets WHERE user_id=$1`, userID)
	return t, err
}

func LastVerificationSentAt(userID uuid.UUID) (time.Time, error) {
	var t time.Time
	err := DB().Get(&t, `SELECT COALESCE(MAX(created_at), to_timestamp(0)) FROM email_verifications WHERE user_id=$1`, userID)
	return t, err
}
