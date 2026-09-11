package main

import (
	"database/sql"
	"log"
	"strings"
	"sync"
)

// Groups bundle users; sites and OIDC clients can grant access to whole groups.
// The tables are created on first use so older databases pick them up without a migration step.

const groupsSchema = `
create table if not exists groupdefs (
	id integer primary key,
	name text unique not null collate nocase,
	description text not null default '',
	created_at integer not null
);
create table if not exists user_groups (
	user_id integer not null references users(id) on delete cascade,
	group_id integer not null references groupdefs(id) on delete cascade,
	primary key (user_id, group_id)
);
create table if not exists site_groups (
	site_id integer not null references sites(id) on delete cascade,
	group_id integer not null references groupdefs(id) on delete cascade,
	primary key (site_id, group_id)
);
`

var groupsOnce sync.Once

func (st *Store) ensureGroups() {
	groupsOnce.Do(func() {
		if _, err := st.db.Exec(groupsSchema); err != nil {
			log.Printf("groups schema: %v", err)
		}
	})
}

type Group struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	Members     []int64 `json:"members"`
	CreatedAt   int64   `json:"createdAt"`
}

func (st *Store) ListGroups() ([]*Group, error) {
	st.ensureGroups()
	rows, err := st.db.Query(`select id, name, description, created_at from groupdefs order by name`)
	if err != nil {
		return nil, err
	}
	out := []*Group{}
	byID := map[int64]*Group{}
	for rows.Next() {
		g := &Group{Members: []int64{}}
		if err := rows.Scan(&g.ID, &g.Name, &g.Description, &g.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, g)
		byID[g.ID] = g
	}
	rows.Close()
	mrows, err := st.db.Query(`select group_id, user_id from user_groups`)
	if err != nil {
		return nil, err
	}
	defer mrows.Close()
	for mrows.Next() {
		var gid, uid int64
		if err := mrows.Scan(&gid, &uid); err != nil {
			return nil, err
		}
		if g := byID[gid]; g != nil {
			g.Members = append(g.Members, uid)
		}
	}
	return out, mrows.Err()
}

func (st *Store) CreateGroup(g *Group) (int64, error) {
	st.ensureGroups()
	tx, err := st.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`insert into groupdefs (name, description, created_at) values (?, ?, ?)`, g.Name, g.Description, now())
	if err != nil {
		return 0, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	if err := setGroupMembers(tx, id, g.Members); err != nil {
		return 0, err
	}
	return id, tx.Commit()
}

func (st *Store) UpdateGroup(g *Group) error {
	st.ensureGroups()
	tx, err := st.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`update groupdefs set name = ?, description = ? where id = ?`, g.Name, g.Description, g.ID); err != nil {
		return err
	}
	if err := setGroupMembers(tx, g.ID, g.Members); err != nil {
		return err
	}
	return tx.Commit()
}

func setGroupMembers(tx *sql.Tx, groupID int64, members []int64) error {
	if _, err := tx.Exec(`delete from user_groups where group_id = ?`, groupID); err != nil {
		return err
	}
	for _, uid := range members {
		if _, err := tx.Exec(`insert or ignore into user_groups (user_id, group_id) select id, ? from users where id = ?`, groupID, uid); err != nil {
			return err
		}
	}
	return nil
}

func (st *Store) DeleteGroup(id int64) error {
	st.ensureGroups()
	_, err := st.db.Exec(`delete from groupdefs where id = ?`, id)
	return err
}

// SetUserGroups replaces the group memberships of one user.
func (st *Store) SetUserGroups(userID int64, groups []int64) error {
	st.ensureGroups()
	tx, err := st.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`delete from user_groups where user_id = ?`, userID); err != nil {
		return err
	}
	for _, gid := range groups {
		if _, err := tx.Exec(`insert or ignore into user_groups (user_id, group_id) select ?, id from groupdefs where id = ?`, userID, gid); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (st *Store) UserGroups(userID int64) (ids []int64, names []string, err error) {
	st.ensureGroups()
	rows, err := st.db.Query(`select g.id, g.name from user_groups ug join groupdefs g on g.id = ug.group_id where ug.user_id = ? order by g.name`, userID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	ids, names = []int64{}, []string{}
	for rows.Next() {
		var id int64
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, nil, err
		}
		ids = append(ids, id)
		names = append(names, name)
	}
	return ids, names, rows.Err()
}

// SiteGroups returns site id -> granted group ids.
func (st *Store) SiteGroups() (map[int64][]int64, error) {
	st.ensureGroups()
	rows, err := st.db.Query(`select site_id, group_id from site_groups`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]int64{}
	for rows.Next() {
		var sid, gid int64
		if err := rows.Scan(&sid, &gid); err != nil {
			return nil, err
		}
		out[sid] = append(out[sid], gid)
	}
	return out, rows.Err()
}

func (st *Store) SetSiteGroups(siteID int64, groups []int64) error {
	st.ensureGroups()
	tx, err := st.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`delete from site_groups where site_id = ?`, siteID); err != nil {
		return err
	}
	for _, gid := range groups {
		if _, err := tx.Exec(`insert or ignore into site_groups (site_id, group_id) select ?, id from groupdefs where id = ?`, siteID, gid); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func validGroupName(n string) bool {
	n = strings.TrimSpace(n)
	return len(n) >= 2 && len(n) <= 48 && !strings.ContainsAny(n, "<>\"'")
}

// inAnyGroup reports whether the user is a member of at least one of the groups.
func (st *Store) inAnyGroup(userID int64, groups []int64) bool {
	if len(groups) == 0 {
		return false
	}
	ids, _, err := st.UserGroups(userID)
	if err != nil {
		return false
	}
	for _, a := range ids {
		for _, b := range groups {
			if a == b {
				return true
			}
		}
	}
	return false
}
