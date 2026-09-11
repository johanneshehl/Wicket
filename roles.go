package main

// Roles:
//   admin    full access to Wicket and to every protected site
//   auditor  may sign in to the Wicket admin interface but only read (sites, users, log, settings)
//   user     signs in to protected sites according to their rules

var roles = []string{"admin", "auditor", "user"}

func validRole(role string) bool {
	for _, r := range roles {
		if r == role {
			return true
		}
	}
	return false
}

// canAdmin reports whether the user may open the Wicket admin interface.
func canAdmin(u *User) bool { return u != nil && (u.Role == "admin" || u.Role == "auditor") }
