package main

import (
	"database/sql"
	"errors"
	"log"
	"sync"
)

// Single-use links sent by e-mail (invitations, password resets). Only a hash of the token is stored.

const tokenSchema = `
create table if not exists user_tokens (
	id integer primary key,
	user_id integer not null references users(id) on delete cascade,
	purpose text not null,
	token_hash text unique not null,
	created_at integer not null,
	expires_at integer not null,
	used_at integer not null default 0
);
`

var tokensOnce sync.Once

func (st *Store) ensureTokens() {
	tokensOnce.Do(func() {
		if _, err := st.db.Exec(tokenSchema); err != nil {
			log.Printf("tokens schema: %v", err)
		}
	})
}

// CreateUserToken issues a new link token; older unused tokens of the same purpose stop working.
func (st *Store) CreateUserToken(userID int64, purpose string, ttl int64) (string, error) {
	st.ensureTokens()
	token := newToken()
	tx, err := st.db.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`delete from user_tokens where user_id = ? and purpose = ? and used_at = 0`, userID, purpose); err != nil {
		return "", err
	}
	if _, err := tx.Exec(`insert into user_tokens (user_id, purpose, token_hash, created_at, expires_at) values (?, ?, ?, ?, ?)`,
		userID, purpose, sha(token), now(), now()+ttl); err != nil {
		return "", err
	}
	return token, tx.Commit()
}

// PeekUserToken returns the user of a valid token without using it up.
func (st *Store) PeekUserToken(token, purpose string) (*User, error) {
	st.ensureTokens()
	var uid int64
	err := st.db.QueryRow(`select user_id from user_tokens where token_hash = ? and purpose = ? and used_at = 0 and expires_at > ?`,
		sha(token), purpose, now()).Scan(&uid)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	u, err := st.UserByID(uid)
	if err != nil || u == nil || u.Disabled {
		return nil, err
	}
	return u, nil
}

// UseUserToken consumes a token exactly once.
func (st *Store) UseUserToken(token, purpose string) (*User, error) {
	u, err := st.PeekUserToken(token, purpose)
	if err != nil || u == nil {
		return nil, err
	}
	res, err := st.db.Exec(`update user_tokens set used_at = ? where token_hash = ? and purpose = ? and used_at = 0`, now(), sha(token), purpose)
	if err != nil {
		return nil, err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return nil, nil // used concurrently
	}
	return u, nil
}

func (st *Store) CleanupUserTokens() {
	st.ensureTokens()
	_, _ = st.db.Exec(`delete from user_tokens where expires_at < ? or (used_at > 0 and used_at < ?)`, now(), now()-86400)
}
