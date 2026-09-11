package main

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type Store struct{ db *sql.DB }

const schema = `
create table if not exists users (
	id integer primary key,
	username text unique not null collate nocase,
	email text not null default '',
	role text not null default 'user',
	password_hash text not null,
	totp_secret text not null default '',
	totp_enabled integer not null default 0,
	totp_last_step integer not null default 0,
	created_at integer not null,
	last_seen integer not null default 0,
	disabled integer not null default 0
);
create table if not exists recovery_codes (
	id integer primary key,
	user_id integer not null references users(id) on delete cascade,
	code_hash text not null,
	used_at integer not null default 0
);
create table if not exists sessions (
	id integer primary key,
	user_id integer not null references users(id) on delete cascade,
	token_hash text unique not null,
	state text not null,
	mfa integer not null default 0,
	remember integer not null default 0,
	created_at integer not null,
	last_seen integer not null,
	expires_at integer not null,
	ip text not null default '',
	ua text not null default ''
);
create table if not exists sites (
	id integer primary key,
	domain text unique not null,
	target text not null default '',
	access text not null default 'all',
	require_2fa integer not null default 0,
	bypass text not null default '',
	enabled integer not null default 1,
	managed integer not null default 1,
	created_at integer not null
);
create table if not exists site_users (
	site_id integer not null references sites(id) on delete cascade,
	user_id integer not null references users(id) on delete cascade,
	primary key (site_id, user_id)
);
create table if not exists events (
	id integer primary key,
	at integer not null,
	kind text not null,
	username text not null default '',
	site text not null default '',
	ip text not null default '',
	ua text not null default '',
	detail text not null default ''
);
create index if not exists events_at on events(at);
create table if not exists settings (key text primary key, value text not null);
`

func OpenStore(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	dsn := "file:" + filepath.Join(dir, "wicket.db") +
		"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// SQLite allows one writer; a single connection avoids "database is locked".
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schema); err != nil {
		return nil, err
	}
	st := &Store{db: db}
	if err := st.migrate(); err != nil {
		return nil, err
	}
	return st, nil
}

func now() int64 { return time.Now().Unix() }

type scanner interface{ Scan(...any) error }

// ---------------------------------------------------------------- users

type User struct {
	ID           int64  `json:"id"`
	Username     string `json:"username"`
	Email        string `json:"email"`
	Role         string `json:"role"`
	PasswordHash string `json:"-"`
	TOTPSecret   string `json:"-"`
	TOTPEnabled  bool   `json:"totpEnabled"`
	TOTPLastStep int64  `json:"-"`
	CreatedAt    int64  `json:"createdAt"`
	LastSeen     int64  `json:"lastSeen"`
	Disabled     bool   `json:"disabled"`
}

const userCols = `id, username, email, role, password_hash, totp_secret, totp_enabled, totp_last_step, created_at, last_seen, disabled`

func scanUser(row scanner) (*User, error) {
	u := &User{}
	err := row.Scan(&u.ID, &u.Username, &u.Email, &u.Role, &u.PasswordHash, &u.TOTPSecret,
		&u.TOTPEnabled, &u.TOTPLastStep, &u.CreatedAt, &u.LastSeen, &u.Disabled)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return u, err
}

func (st *Store) UserByName(name string) (*User, error) {
	return scanUser(st.db.QueryRow(`select `+userCols+` from users where username = ?`, name))
}

func (st *Store) UserByID(id int64) (*User, error) {
	return scanUser(st.db.QueryRow(`select `+userCols+` from users where id = ?`, id))
}

// UserByEmail finds the one user with this email address (case-insensitive); none or several -> nil.
func (st *Store) UserByEmail(email string) (*User, error) {
	email = strings.TrimSpace(email)
	if email == "" {
		return nil, nil
	}
	rows, err := st.db.Query(`select `+userCols+` from users where lower(email) = lower(?)`, email)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var found []*User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		found = append(found, u)
	}
	if len(found) != 1 {
		return nil, rows.Err()
	}
	return found[0], rows.Err()
}

func (st *Store) ListUsers() ([]*User, error) {
	rows, err := st.db.Query(`select ` + userCols + ` from users order by role = 'admin' desc, username`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (st *Store) CountUsers() (int, error) {
	var n int
	err := st.db.QueryRow(`select count(*) from users`).Scan(&n)
	return n, err
}

func (st *Store) CountAdmins() (int, error) {
	var n int
	err := st.db.QueryRow(`select count(*) from users where role = 'admin' and disabled = 0`).Scan(&n)
	return n, err
}

func (st *Store) CreateUser(u *User) (int64, error) {
	res, err := st.db.Exec(`insert into users (username, email, role, password_hash, created_at) values (?, ?, ?, ?, ?)`,
		u.Username, u.Email, u.Role, u.PasswordHash, now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (st *Store) UpdateUser(u *User) error {
	_, err := st.db.Exec(`update users set email = ?, role = ?, disabled = ? where id = ?`, u.Email, u.Role, u.Disabled, u.ID)
	return err
}

func (st *Store) SetPassword(id int64, hash string) error {
	_, err := st.db.Exec(`update users set password_hash = ? where id = ?`, hash, id)
	return err
}

func (st *Store) EnableTOTP(id int64, secret string, step int64) error {
	_, err := st.db.Exec(`update users set totp_secret = ?, totp_enabled = 1, totp_last_step = ? where id = ?`, secret, step, id)
	return err
}

func (st *Store) DisableTOTP(id int64) error {
	if _, err := st.db.Exec(`update users set totp_secret = '', totp_enabled = 0, totp_last_step = 0 where id = ?`, id); err != nil {
		return err
	}
	_, err := st.db.Exec(`delete from recovery_codes where user_id = ?`, id)
	return err
}

func (st *Store) SetTOTPStep(id, step int64) error {
	_, err := st.db.Exec(`update users set totp_last_step = ? where id = ?`, step, id)
	return err
}

func (st *Store) TouchUser(id int64) error {
	_, err := st.db.Exec(`update users set last_seen = ? where id = ?`, now(), id)
	return err
}

func (st *Store) DeleteUser(id int64) error {
	_, err := st.db.Exec(`delete from users where id = ?`, id)
	return err
}

// ---------------------------------------------------------------- recovery codes

func (st *Store) ReplaceRecoveryCodes(userID int64, hashes []string) error {
	tx, err := st.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`delete from recovery_codes where user_id = ?`, userID); err != nil {
		return err
	}
	for _, h := range hashes {
		if _, err := tx.Exec(`insert into recovery_codes (user_id, code_hash) values (?, ?)`, userID, h); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (st *Store) UseRecoveryCode(userID int64, hash string) (bool, error) {
	res, err := st.db.Exec(`update recovery_codes set used_at = ? where user_id = ? and code_hash = ? and used_at = 0`, now(), userID, hash)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n == 1, err
}

func (st *Store) RecoveryLeft(userID int64) (int, error) {
	var n int
	err := st.db.QueryRow(`select count(*) from recovery_codes where user_id = ? and used_at = 0`, userID).Scan(&n)
	return n, err
}

// ---------------------------------------------------------------- sessions

type Session struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"userId"`
	State     string `json:"state"` // "pending" (password ok, 2FA missing) or "active"
	MFA       bool   `json:"mfa"`
	Remember  bool   `json:"remember"`
	CreatedAt int64  `json:"createdAt"`
	LastSeen  int64  `json:"lastSeen"`
	ExpiresAt int64  `json:"expiresAt"`
	IP        string `json:"ip"`
	UA        string `json:"ua"`
}

const sessionCols = `id, user_id, state, mfa, remember, created_at, last_seen, expires_at, ip, ua`

func scanSession(row scanner) (*Session, error) {
	s := &Session{}
	err := row.Scan(&s.ID, &s.UserID, &s.State, &s.MFA, &s.Remember, &s.CreatedAt, &s.LastSeen, &s.ExpiresAt, &s.IP, &s.UA)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return s, err
}

func (st *Store) CreateSession(s *Session, tokenHash string) (int64, error) {
	t := now()
	res, err := st.db.Exec(`insert into sessions (user_id, token_hash, state, mfa, remember, created_at, last_seen, expires_at, ip, ua)
		values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, s.UserID, tokenHash, s.State, s.MFA, s.Remember, t, t, s.ExpiresAt, s.IP, s.UA)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (st *Store) SessionByTokenHash(hash string) (*Session, error) {
	return scanSession(st.db.QueryRow(`select `+sessionCols+` from sessions where token_hash = ?`, hash))
}

func (st *Store) ActivateSession(id int64, tokenHash string, mfa bool, expires int64) error {
	_, err := st.db.Exec(`update sessions set token_hash = ?, state = 'active', mfa = ?, expires_at = ?, last_seen = ? where id = ?`,
		tokenHash, mfa, expires, now(), id)
	return err
}

func (st *Store) SetSessionMFA(id int64) error {
	_, err := st.db.Exec(`update sessions set mfa = 1 where id = ?`, id)
	return err
}

func (st *Store) TouchSession(id int64) error {
	_, err := st.db.Exec(`update sessions set last_seen = ? where id = ?`, now(), id)
	return err
}

func (st *Store) DeleteSession(id int64) error {
	_, err := st.db.Exec(`delete from sessions where id = ?`, id)
	return err
}

func (st *Store) DeleteUserSessions(userID int64) error {
	_, err := st.db.Exec(`delete from sessions where user_id = ?`, userID)
	return err
}

func (st *Store) DeleteAllSessions() error {
	_, err := st.db.Exec(`delete from sessions`)
	return err
}

func (st *Store) ListUserSessions(userID int64) ([]*Session, error) {
	rows, err := st.db.Query(`select `+sessionCols+` from sessions where user_id = ? and state = 'active' and expires_at > ? order by last_seen desc`, userID, now())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Session
	for rows.Next() {
		s, err := scanSession(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (st *Store) SessionCounts() (map[int64]int, error) {
	rows, err := st.db.Query(`select user_id, count(*) from sessions where state = 'active' and expires_at > ? group by user_id`, now())
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]int{}
	for rows.Next() {
		var id int64
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}

func (st *Store) CountActiveSessions() (int, error) {
	var n int
	err := st.db.QueryRow(`select count(*) from sessions where state = 'active' and expires_at > ?`, now()).Scan(&n)
	return n, err
}

func (st *Store) CleanupSessions() error {
	_, err := st.db.Exec(`delete from sessions where expires_at < ?`, now())
	return err
}

// ---------------------------------------------------------------- sites

type Site struct {
	ID              int64    `json:"id"`
	Domain          string   `json:"domain"`
	Target          string   `json:"target"`
	Access          string   `json:"access"` // all | admins | users (selected users and groups)
	Require2FA      bool     `json:"require2fa"`
	Bypass          []string `json:"bypass"`
	Enabled         bool     `json:"enabled"`
	Managed         bool     `json:"managed"` // Wicket writes the Caddy site block
	Users           []int64  `json:"users"`
	Groups          []int64  `json:"groups"`
	AllowIPs        []string `json:"allowIps"`        // networks that pass without login
	DenyIPs         []string `json:"denyIps"`         // networks that are always blocked
	MaxSessionHours int      `json:"maxSessionHours"` // 0 = no extra limit; older sign-ins must sign in again
	CreatedAt       int64    `json:"createdAt"`
}

const siteCols = `id, domain, target, access, require_2fa, bypass, enabled, managed, created_at, allow_ips, deny_ips, max_session_hours`

func scanSite(row scanner) (*Site, error) {
	s := &Site{}
	var bypass, allow, deny string
	err := row.Scan(&s.ID, &s.Domain, &s.Target, &s.Access, &s.Require2FA, &bypass, &s.Enabled, &s.Managed, &s.CreatedAt,
		&allow, &deny, &s.MaxSessionHours)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	s.Bypass = splitLines(bypass)
	s.AllowIPs = splitLines(allow)
	s.DenyIPs = splitLines(deny)
	s.Users = []int64{}
	s.Groups = []int64{}
	return s, err
}

// migrations add columns introduced after the first release; existing databases are upgraded on start.
var migrations = []struct{ table, column, ddl string }{
	{"sites", "allow_ips", `alter table sites add column allow_ips text not null default ''`},
	{"sites", "deny_ips", `alter table sites add column deny_ips text not null default ''`},
	{"sites", "max_session_hours", `alter table sites add column max_session_hours integer not null default 0`},
}

func (st *Store) migrate() error {
	for _, m := range migrations {
		var n int
		if err := st.db.QueryRow(`select count(*) from pragma_table_info(?) where name = ?`, m.table, m.column).Scan(&n); err != nil {
			return err
		}
		if n == 0 {
			if _, err := st.db.Exec(m.ddl); err != nil {
				return err
			}
		}
	}
	st.ensureGroups()
	return nil
}

func splitLines(s string) []string {
	out := []string{}
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

func (st *Store) ListSites() ([]*Site, error) {
	rows, err := st.db.Query(`select ` + siteCols + ` from sites order by domain`)
	if err != nil {
		return nil, err
	}
	var out []*Site
	byID := map[int64]*Site{}
	for rows.Next() {
		s, err := scanSite(rows)
		if err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, s)
		byID[s.ID] = s
	}
	rows.Close()
	urows, err := st.db.Query(`select site_id, user_id from site_users`)
	if err != nil {
		return nil, err
	}
	defer urows.Close()
	for urows.Next() {
		var sid, uid int64
		if err := urows.Scan(&sid, &uid); err != nil {
			return nil, err
		}
		if s := byID[sid]; s != nil {
			s.Users = append(s.Users, uid)
		}
	}
	if err := urows.Err(); err != nil {
		return nil, err
	}
	groups, err := st.SiteGroups()
	if err != nil {
		return nil, err
	}
	for _, s := range out {
		if g := groups[s.ID]; g != nil {
			s.Groups = g
		}
	}
	return out, nil
}

func (st *Store) SiteByID(id int64) (*Site, error) {
	sites, err := st.ListSites()
	if err != nil {
		return nil, err
	}
	for _, s := range sites {
		if s.ID == id {
			return s, nil
		}
	}
	return nil, nil
}

// SiteForHost finds the site for a request host: exact domain first, then "*.parent" wildcards.
func (st *Store) SiteForHost(host string) (*Site, error) {
	sites, err := st.ListSites()
	if err != nil {
		return nil, err
	}
	var wildcard *Site
	for _, s := range sites {
		if s.Domain == host {
			return s, nil
		}
		if strings.HasPrefix(s.Domain, "*.") && strings.HasSuffix(host, s.Domain[1:]) {
			if wildcard == nil || len(s.Domain) > len(wildcard.Domain) {
				wildcard = s
			}
		}
	}
	return wildcard, nil
}

func (st *Store) CreateSite(s *Site) (int64, error) {
	tx, err := st.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`insert into sites (domain, target, access, require_2fa, bypass, enabled, managed, created_at, allow_ips, deny_ips, max_session_hours)
		values (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		s.Domain, s.Target, s.Access, s.Require2FA, strings.Join(s.Bypass, "\n"), s.Enabled, s.Managed, now(),
		strings.Join(s.AllowIPs, "\n"), strings.Join(s.DenyIPs, "\n"), s.MaxSessionHours)
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err := setSiteUsers(tx, id, s.Users); err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, st.SetSiteGroups(id, s.Groups)
}

func (st *Store) UpdateSite(s *Site) error {
	tx, err := st.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`update sites set domain = ?, target = ?, access = ?, require_2fa = ?, bypass = ?, enabled = ?, managed = ?,
		allow_ips = ?, deny_ips = ?, max_session_hours = ? where id = ?`,
		s.Domain, s.Target, s.Access, s.Require2FA, strings.Join(s.Bypass, "\n"), s.Enabled, s.Managed,
		strings.Join(s.AllowIPs, "\n"), strings.Join(s.DenyIPs, "\n"), s.MaxSessionHours, s.ID); err != nil {
		return err
	}
	if err := setSiteUsers(tx, s.ID, s.Users); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	return st.SetSiteGroups(s.ID, s.Groups)
}

func setSiteUsers(tx *sql.Tx, siteID int64, users []int64) error {
	if _, err := tx.Exec(`delete from site_users where site_id = ?`, siteID); err != nil {
		return err
	}
	for _, uid := range users {
		if _, err := tx.Exec(`insert or ignore into site_users (site_id, user_id) select ?, id from users where id = ?`, siteID, uid); err != nil {
			return err
		}
	}
	return nil
}

func (st *Store) DeleteSite(id int64) error {
	_, err := st.db.Exec(`delete from sites where id = ?`, id)
	return err
}

// ---------------------------------------------------------------- events

type Event struct {
	ID       int64  `json:"id"`
	At       int64  `json:"at"`
	Kind     string `json:"kind"`
	Username string `json:"username"`
	Site     string `json:"site"`
	IP       string `json:"ip"`
	UA       string `json:"ua"`
	Detail   string `json:"detail"`
}

func (st *Store) AddEvent(e Event) error {
	_, err := st.db.Exec(`insert into events (at, kind, username, site, ip, ua, detail) values (?, ?, ?, ?, ?, ?, ?)`,
		e.At, e.Kind, e.Username, e.Site, e.IP, e.UA, e.Detail)
	return err
}

type EventQuery struct {
	Kinds  []string
	Since  int64
	Site   string
	Limit  int
	Offset int
}

func (q EventQuery) where() (string, []any) {
	var conds []string
	var args []any
	if len(q.Kinds) > 0 {
		conds = append(conds, "kind in (?"+strings.Repeat(",?", len(q.Kinds)-1)+")")
		for _, k := range q.Kinds {
			args = append(args, k)
		}
	}
	if q.Since > 0 {
		conds = append(conds, "at >= ?")
		args = append(args, q.Since)
	}
	if q.Site != "" {
		conds = append(conds, "site = ?")
		args = append(args, q.Site)
	}
	if len(conds) == 0 {
		return "", nil
	}
	return " where " + strings.Join(conds, " and "), args
}

func (st *Store) ListEvents(q EventQuery) ([]*Event, int, error) {
	where, args := q.where()
	var total int
	if err := st.db.QueryRow(`select count(*) from events`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if q.Limit <= 0 {
		q.Limit = 50
	}
	rows, err := st.db.Query(`select id, at, kind, username, site, ip, ua, detail from events`+where+` order by id desc limit ? offset ?`,
		append(args, q.Limit, q.Offset)...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []*Event{}
	for rows.Next() {
		e := &Event{}
		if err := rows.Scan(&e.ID, &e.At, &e.Kind, &e.Username, &e.Site, &e.IP, &e.UA, &e.Detail); err != nil {
			return nil, 0, err
		}
		out = append(out, e)
	}
	return out, total, rows.Err()
}

func (st *Store) CountEvents(kinds []string, since, until int64) (int, error) {
	q := EventQuery{Kinds: kinds, Since: since}
	where, args := q.where()
	where += " and at < ?"
	args = append(args, until)
	var n int
	err := st.db.QueryRow(`select count(*) from events`+where, args...).Scan(&n)
	return n, err
}

func (st *Store) CleanupEvents(before int64) error {
	_, err := st.db.Exec(`delete from events where at < ?`, before)
	return err
}

// ---------------------------------------------------------------- settings

func (st *Store) GetSetting(key string) (string, bool, error) {
	var v string
	err := st.db.QueryRow(`select value from settings where key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	return v, err == nil, err
}

func (st *Store) SetSetting(key, value string) error {
	_, err := st.db.Exec(`insert into settings (key, value) values (?, ?) on conflict(key) do update set value = excluded.value`, key, value)
	return err
}
