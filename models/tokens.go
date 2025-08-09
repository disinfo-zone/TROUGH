package models

import (
	"time"

	"github.com/google/uuid"
)

func CreatePasswordReset(userID uuid.UUID, token string, expires time.Time) error {
	_, err := DB().Exec(`INSERT INTO password_resets (user_id, token, expires_at) VALUES ($1,$2,$3)`, userID, token, expires)
	return err
}

func GetPasswordReset(token string) (uuid.UUID, time.Time, error) {
	var uid uuid.UUID
	var exp time.Time
	err := DB().QueryRowx(`SELECT user_id, expires_at FROM password_resets WHERE token=$1`, token).Scan(&uid, &exp)
	return uid, exp, err
}

func DeletePasswordReset(token string) error {
	_, err := DB().Exec(`DELETE FROM password_resets WHERE token=$1`, token)
	return err
}

func CreateEmailVerification(userID uuid.UUID, token string, expires time.Time) error {
	_, err := DB().Exec(`INSERT INTO email_verifications (user_id, token, expires_at) VALUES ($1,$2,$3)`, userID, token, expires)
	return err
}

func GetEmailVerification(token string) (uuid.UUID, time.Time, error) {
	var uid uuid.UUID
	var exp time.Time
	err := DB().QueryRowx(`SELECT user_id, expires_at FROM email_verifications WHERE token=$1`, token).Scan(&uid, &exp)
	return uid, exp, err
}

func DeleteEmailVerification(token string) error {
	_, err := DB().Exec(`DELETE FROM email_verifications WHERE token=$1`, token)
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
