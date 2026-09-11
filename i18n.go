package main

import (
	"errors"
	"fmt"
	"html"
	"html/template"
	"net/http"
	"strconv"
	"strings"
)

// Languages Wicket ships. "auto" in the settings picks one of these from the
// browser's Accept-Language header and falls back to English.
var languages = []string{"en", "de", "es"}

var loginTemplates = []string{"centered", "split", "light", "dock", "terminal", "glass"}

func supportedLang(l string) bool {
	for _, s := range languages {
		if s == l {
			return true
		}
	}
	return false
}

func validTemplate(t string) bool {
	for _, s := range loginTemplates {
		if s == t {
			return true
		}
	}
	return false
}

// trKey marks a format argument that is itself a message key and gets translated too.
type trKey string

func tr(lang, key string, args ...any) string {
	m, ok := messages[key]
	if !ok {
		return key
	}
	s := m[lang]
	if s == "" {
		s = m["en"]
	}
	if len(args) == 0 {
		return s
	}
	vals := make([]any, len(args))
	for i, a := range args {
		if k, ok := a.(trKey); ok {
			vals[i] = tr(lang, string(k))
		} else {
			vals[i] = a
		}
	}
	return fmt.Sprintf(s, vals...)
}

// negotiateLang uses the visitor's most preferred language; anything Wicket
// does not ship falls back to English.
func negotiateLang(header string) string {
	best, bestQ := "", -1.0
	for _, part := range strings.Split(header, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		tag, q := part, 1.0
		if i := strings.Index(part, ";"); i >= 0 {
			tag = strings.TrimSpace(part[:i])
			for _, p := range strings.Split(part[i+1:], ";") {
				if p = strings.TrimSpace(p); strings.HasPrefix(p, "q=") {
					if v, err := strconv.ParseFloat(p[2:], 64); err == nil {
						q = v
					}
				}
			}
		}
		base := strings.ToLower(tag)
		if i := strings.IndexAny(base, "-_"); i >= 0 {
			base = base[:i]
		}
		if q > bestQ {
			best, bestQ = base, q
		}
	}
	if supportedLang(best) {
		return best
	}
	return "en"
}

func (a *App) langFor(r *http.Request) string {
	if l := a.settings().Language; supportedLang(l) {
		return l
	}
	return negotiateLang(r.Header.Get("Accept-Language"))
}

// ---------------------------------------------------------------- template helpers

func (p *page) T(key string, args ...any) string { return tr(p.Lang, key, args...) }

// TB formats a message whose arguments are shown in bold (HTML-escaped).
func (p *page) TB(key string, args ...string) template.HTML {
	vals := make([]any, len(args))
	for i, a := range args {
		vals[i] = "<b>" + html.EscapeString(a) + "</b>"
	}
	return template.HTML(tr(p.Lang, key, vals...))
}

// ---------------------------------------------------------------- user-facing errors

type userError struct {
	key  string
	args []any
}

func (e userError) Error() string           { return tr("en", e.key, e.args...) }
func userErr(key string, args ...any) error { return userError{key, args} }

// msgFor translates a userError; other errors keep their (English) text.
func msgFor(lang string, err error) string {
	var ue userError
	if errors.As(err, &ue) {
		return tr(lang, ue.key, ue.args...)
	}
	return err.Error()
}

// ---------------------------------------------------------------- messages

var messages = map[string]map[string]string{
	// page titles
	"title.signin":   {"en": "Sign in", "de": "Anmelden", "es": "Iniciar sesión"},
	"title.confirm":  {"en": "Confirm", "de": "Bestätigen", "es": "Confirmar"},
	"title.denied":   {"en": "Access denied", "de": "Kein Zugriff", "es": "Acceso denegado"},
	"title.locked":   {"en": "Temporarily locked", "de": "Vorübergehend gesperrt", "es": "Bloqueado temporalmente"},
	"title.setup2fa": {"en": "Set up two-factor", "de": "Zwei-Faktor einrichten", "es": "Configurar la verificación en dos pasos"},
	"title.setup":    {"en": "Set up Wicket", "de": "Wicket einrichten", "es": "Configurar Wicket"},
	"title.signedin": {"en": "Signed in", "de": "Angemeldet", "es": "Sesión iniciada"},

	"footer.protected": {"en": "Protected by Wicket", "de": "Geschützt von Wicket", "es": "Protegido por Wicket"},

	// sign-in
	"login.continueTo":     {"en": "continue to", "de": "weiter zu", "es": "continuar a"},
	"login.username":       {"en": "Username", "de": "Benutzername", "es": "Nombre de usuario"},
	"login.password":       {"en": "Password", "de": "Passwort", "es": "Contraseña"},
	"login.continue":       {"en": "Continue", "de": "Weiter", "es": "Continuar"},
	"login.remember":       {"en": "Stay signed in", "de": "Angemeldet bleiben", "es": "Mantener la sesión"},
	"login.days":           {"en": "%d days", "de": "%d Tage", "es": "%d días"},
	"login.rememberDevice": {"en": "Stay signed in on this device", "de": "Auf diesem Gerät angemeldet bleiben", "es": "Mantener la sesión en este dispositivo"},

	// Wicket's own login
	"admin.eyebrow":       {"en": "Wicket Admin", "de": "Wicket Admin", "es": "Wicket Admin"},
	"admin.welcome":       {"en": "Welcome back", "de": "Willkommen zurück", "es": "Bienvenido de nuevo"},
	"admin.claim1":        {"en": "One login.", "de": "Ein Login.", "es": "Un solo acceso."},
	"admin.claim2":        {"en": "All your services.", "de": "Alle deine Dienste.", "es": "Todos tus servicios."},
	"admin.claimSub":      {"en": "Manage who can access which site under %s.", "de": "Verwalte, wer auf welche Seite unter %s darf.", "es": "Gestiona quién puede acceder a cada sitio de %s."},
	"admin.claimSubPlain": {"en": "Manage who can access which site.", "de": "Verwalte, wer auf welche Seite darf.", "es": "Gestiona quién puede acceder a cada sitio."},
	"admin.foot":          {"en": "Access is logged", "de": "Zugriff wird protokolliert", "es": "Los accesos quedan registrados"},
	"admin.signin":        {"en": "Sign in", "de": "Anmelden", "es": "Iniciar sesión"},

	// second factor
	"steps.password":    {"en": "Password", "de": "Passwort", "es": "Contraseña"},
	"steps.confirm":     {"en": "Confirm", "de": "Bestätigen", "es": "Confirmar"},
	"otp.digit":         {"en": "Digit %d", "de": "Ziffer %d", "es": "Dígito %d"},
	"2fa.titleCode":     {"en": "Verification code", "de": "Bestätigungscode", "es": "Código de verificación"},
	"2fa.titleRecovery": {"en": "Recovery code", "de": "Wiederherstellungscode", "es": "Código de recuperación"},
	"2fa.signedInAs":    {"en": "Signed in as %s.", "de": "Angemeldet als %s.", "es": "Sesión iniciada como %s."},
	"2fa.enterCode":     {"en": "Enter the code from your authenticator app.", "de": "Gib den Code aus deiner Authenticator-App ein.", "es": "Introduce el código de tu app de autenticación."},
	"2fa.enterRecovery": {"en": "Enter one of your recovery codes.", "de": "Gib einen deiner Wiederherstellungscodes ein.", "es": "Introduce uno de tus códigos de recuperación."},
	"2fa.adminCode":     {"en": "6-digit code from your authenticator app.", "de": "6-stelliger Code aus deiner Authenticator-App.", "es": "Código de 6 dígitos de tu app de autenticación."},
	"2fa.adminRecovery": {"en": "One of your ten recovery codes.", "de": "Einer deiner zehn Wiederherstellungscodes.", "es": "Uno de tus diez códigos de recuperación."},
	"2fa.submit":        {"en": "Verify", "de": "Bestätigen", "es": "Verificar"},
	"2fa.otherUser":     {"en": "← Different user", "de": "← Anderer Benutzer", "es": "← Otro usuario"},
	"2fa.back":          {"en": "← Back", "de": "← Zurück", "es": "← Volver"},
	"2fa.useRecovery":   {"en": "Use a recovery code", "de": "Wiederherstellungscode nutzen", "es": "Usar un código de recuperación"},
	"2fa.useApp":        {"en": "Use the code from the app", "de": "Code aus der App nutzen", "es": "Usar el código de la app"},

	// notices
	"notice.lockedText":   {"en": "Too many failed sign-in attempts from your IP address. Please try again later.", "de": "Zu viele fehlgeschlagene Anmeldungen von deiner IP. Versuch es später noch einmal.", "es": "Demasiados intentos fallidos desde tu dirección IP. Vuelve a intentarlo más tarde."},
	"notice.untilNext":    {"en": "until the next attempt", "de": "bis zum nächsten Versuch", "es": "hasta el próximo intento"},
	"notice.denied":       {"en": "You are signed in as %s, but %s is not enabled for your account.", "de": "Du bist als %s angemeldet, aber %s ist für dein Konto nicht freigegeben.", "es": "Has iniciado sesión como %s, pero %s no está habilitado para tu cuenta."},
	"notice.thisSite":     {"en": "this site", "de": "diese Seite", "es": "este sitio"},
	"notice.otherAccount": {"en": "Different account", "de": "Anderes Konto", "es": "Otra cuenta"},
	"notice.back":         {"en": "Back", "de": "Zurück", "es": "Volver"},
	"notice.signedIn":     {"en": "You are signed in as %s and can open every site you have access to.", "de": "Du bist als %s angemeldet und kannst alle freigegebenen Seiten öffnen.", "es": "Has iniciado sesión como %s y puedes abrir todos los sitios a los que tienes acceso."},
	"notice.setup2fa":     {"en": "Set up two-factor", "de": "Zwei-Faktor einrichten", "es": "Configurar dos pasos"},
	"notice.signout":      {"en": "Sign out", "de": "Abmelden", "es": "Cerrar sesión"},

	// 2FA enrollment
	"setup2fa.required":    {"en": "Two-factor authentication is required for your account.", "de": "Für dein Konto ist Zwei-Faktor Pflicht.", "es": "La verificación en dos pasos es obligatoria para tu cuenta."},
	"setup2fa.scan":        {"en": "Scan the code with an authenticator app, e.g. Aegis, 2FAS or Google Authenticator.", "de": "Scanne den Code mit einer Authenticator-App, z. B. Aegis, 2FAS oder Google Authenticator.", "es": "Escanea el código con una app de autenticación, p. ej. Aegis, 2FAS o Google Authenticator."},
	"setup2fa.qrAlt":       {"en": "QR code for the authenticator app", "de": "QR-Code für die Authenticator-App", "es": "Código QR para la app de autenticación"},
	"setup2fa.manual":      {"en": "Or enter the key manually", "de": "Oder Schlüssel manuell eingeben", "es": "O introduce la clave manualmente"},
	"setup2fa.copy":        {"en": "Copy", "de": "Kopieren", "es": "Copiar"},
	"setup2fa.copied":      {"en": "Copied", "de": "Kopiert", "es": "Copiado"},
	"setup2fa.confirmCode": {"en": "Code from the app to confirm", "de": "Code aus der App zum Bestätigen", "es": "Código de la app para confirmar"},
	"setup2fa.signedInAs":  {"en": "Signed in as %s", "de": "Angemeldet als %s", "es": "Sesión iniciada como %s"},
	"setup2fa.later":       {"en": "Later", "de": "Später", "es": "Más tarde"},
	"setup2fa.activate":    {"en": "Activate", "de": "Aktivieren", "es": "Activar"},
	"setup2fa.activeTitle": {"en": "Two-factor is active", "de": "Zwei-Faktor ist aktiv", "es": "La verificación en dos pasos está activa"},
	"setup2fa.codesText":   {"en": "Save these recovery codes, e.g. in a password manager, in case you lose your phone. Each code works exactly once and is only shown now.", "de": "Speichere diese Wiederherstellungscodes, z. B. in einem Passwort-Manager – falls du dein Handy verlierst. Jeder Code funktioniert genau einmal und wird nur jetzt angezeigt.", "es": "Guarda estos códigos de recuperación, p. ej. en un gestor de contraseñas, por si pierdes el móvil. Cada código funciona una sola vez y solo se muestra ahora."},
	"setup2fa.copyAll":     {"en": "Copy all", "de": "Alle kopieren", "es": "Copiar todos"},
	"setup2fa.download":    {"en": "Download", "de": "Herunterladen", "es": "Descargar"},
	"setup2fa.saved":       {"en": "I saved the codes", "de": "Codes gespeichert", "es": "He guardado los códigos"},
	"setup2fa.continue":    {"en": "Continue", "de": "Weiter", "es": "Continuar"},
	"setup2fa.fileTitle":   {"en": "Wicket – recovery codes for %s", "de": "Wicket – Wiederherstellungscodes für %s", "es": "Wicket – códigos de recuperación de %s"},

	// first-run setup
	"setup.lead":           {"en": "Only once on the first start – afterwards you sign in as usual.", "de": "Einmalig beim ersten Start – danach meldest du dich ganz normal an.", "es": "Solo una vez en el primer inicio; después inicias sesión con normalidad."},
	"setup.tabAccount":     {"en": "Admin account", "de": "Admin-Konto", "es": "Cuenta de administrador"},
	"setup.tabDomain":      {"en": "Domain", "de": "Domain", "es": "Dominio"},
	"setup.tab2fa":         {"en": "Two-factor", "de": "Zwei-Faktor", "es": "Dos pasos"},
	"setup.code":           {"en": "Setup code", "de": "Setup-Code", "es": "Código de configuración"},
	"setup.codeHint":       {"en": "shown in the container log:", "de": "steht im Container-Log:", "es": "aparece en el registro del contenedor:"},
	"setup.repeat":         {"en": "Repeat", "de": "Wiederholen", "es": "Repetir"},
	"setup.mainDomain":     {"en": "Main domain", "de": "Hauptdomain", "es": "Dominio principal"},
	"setup.mainDomainHint": {"en": "The login is valid for this domain and all its subdomains.", "de": "Das Login gilt für diese Domain und alle Subdomains.", "es": "El inicio de sesión vale para este dominio y todos sus subdominios."},
	"setup.loginHost":      {"en": "Login address", "de": "Login-Adresse", "es": "Dirección de inicio de sesión"},
	"setup.adminHost":      {"en": "Admin address", "de": "Admin-Adresse", "es": "Dirección de administración"},
	"setup.dnsInfo":        {"en": "Both addresses need a DNS record pointing to this server. Wicket creates the Caddy entries itself as soon as your Caddyfile contains", "de": "Beide Adressen brauchen einen DNS-Eintrag auf diesen Server. Wicket legt die Caddy-Einträge selbst an, sobald dein Caddyfile Folgendes enthält:", "es": "Ambas direcciones necesitan un registro DNS que apunte a este servidor. Wicket crea las entradas de Caddy en cuanto tu Caddyfile contiene"},
	"setup.step":           {"en": "Step %d of 3", "de": "Schritt %d von 3", "es": "Paso %d de 3"},
	"setup.next":           {"en": "Continue", "de": "Weiter", "es": "Continuar"},
	"strength.labels":      {"en": "At least 10 characters|Weak|Weak|Okay|Good|Strong", "de": "Mindestens 10 Zeichen|Schwach|Schwach|Okay|Gut|Stark", "es": "Al menos 10 caracteres|Débil|Débil|Aceptable|Buena|Fuerte"},

	// login templates
	"tpl.headingTo": {"en": "You are on your way to", "de": "Du bist auf dem Weg zu", "es": "Vas de camino a"},
	"tpl.secure":    {"en": "Encrypted connection · approved users only", "de": "Verbindung verschlüsselt · nur für freigegebene Benutzer", "es": "Conexión cifrada · solo usuarios autorizados"},
	"tpl.logged":    {"en": "Access is logged", "de": "Zugriff wird protokolliert", "es": "Los accesos quedan registrados"},
	"tpl.preview":   {"en": "Preview – sign-in is disabled", "de": "Vorschau – Anmeldung deaktiviert", "es": "Vista previa: el inicio de sesión está desactivado"},

	// errors
	"err.internal":        {"en": "Internal error", "de": "Interner Fehler", "es": "Error interno"},
	"err.csrf":            {"en": "The page was open for too long – please try again.", "de": "Die Seite war zu lange offen – bitte noch einmal versuchen.", "es": "La página estuvo abierta demasiado tiempo; vuelve a intentarlo."},
	"err.credentials":     {"en": "Wrong username or password.", "de": "Benutzername oder Passwort ist falsch.", "es": "Usuario o contraseña incorrectos."},
	"err.noAdminAccess":   {"en": "This account has no access to Wicket.", "de": "Dieses Konto hat keinen Zugriff auf Wicket.", "es": "Esta cuenta no tiene acceso a Wicket."},
	"err.signedInNoAdmin": {"en": "You are signed in as %s – this account has no access to Wicket.", "de": "Du bist als %s angemeldet – dieses Konto hat keinen Zugriff auf Wicket.", "es": "Has iniciado sesión como %s; esta cuenta no tiene acceso a Wicket."},
	"err.code":            {"en": "The code is not correct.", "de": "Der Code stimmt nicht.", "es": "El código no es correcto."},
	"err.codeClock":       {"en": "The code is not correct. Check that the time on your phone is set automatically.", "de": "Der Code stimmt nicht. Prüfe, ob die Uhrzeit auf deinem Handy automatisch gestellt wird.", "es": "El código no es correcto. Comprueba que la hora de tu móvil se ajuste automáticamente."},
	"err.setupCode":       {"en": "The setup code is not correct. You find it in the container log (docker logs wicket).", "de": "Der Setup-Code stimmt nicht. Du findest ihn im Container-Log (docker logs wicket).", "es": "El código de configuración no es correcto. Lo encontrarás en el registro del contenedor (docker logs wicket)."},
	"err.username":        {"en": "Username: 3–32 characters, only letters, digits, dot, underscore and hyphen.", "de": "Benutzername: 3–32 Zeichen, nur Buchstaben, Ziffern, Punkt, Unterstrich und Bindestrich.", "es": "Nombre de usuario: 3–32 caracteres, solo letras, números, punto, guion bajo y guion."},
	"err.password10":      {"en": "The password needs at least 10 characters.", "de": "Das Passwort braucht mindestens 10 Zeichen.", "es": "La contraseña necesita al menos 10 caracteres."},
	"err.passwordMatch":   {"en": "The passwords do not match.", "de": "Die Passwörter stimmen nicht überein.", "es": "Las contraseñas no coinciden."},
	"err.domain":          {"en": "The main domain is invalid, e.g. example.com.", "de": "Die Hauptdomain ist ungültig, z. B. example.com.", "es": "El dominio principal no es válido, p. ej. example.com."},
	"err.subdomain":       {"en": "%q must be a subdomain of %s.", "de": "%q muss eine Subdomain von %s sein.", "es": "%q debe ser un subdominio de %s."},
	"err.hostsDiffer":     {"en": "Login and admin address must be different.", "de": "Login- und Admin-Adresse müssen verschieden sein.", "es": "Las direcciones de inicio de sesión y de administración deben ser distintas."},
	"err.badRequest":      {"en": "Invalid request", "de": "Ungültige Anfrage", "es": "Solicitud no válida"},
	"err.badID":           {"en": "Invalid ID", "de": "Ungültige ID", "es": "ID no válido"},
	"err.notSignedIn":     {"en": "Not signed in", "de": "Nicht angemeldet", "es": "No has iniciado sesión"},
	"err.rejected":        {"en": "Request rejected", "de": "Anfrage abgelehnt", "es": "Solicitud rechazada"},
	"err.siteDomain":      {"en": "Invalid domain, e.g. app.example.com", "de": "Ungültige Domain, z. B. app.example.com", "es": "Dominio no válido, p. ej. app.example.com"},
	"err.siteOwnHost":     {"en": "The login and admin address of Wicket cannot be a protected site.", "de": "Die Login- und Admin-Adresse von Wicket kann keine geschützte Seite sein.", "es": "Las direcciones de inicio de sesión y de administración de Wicket no pueden ser un sitio protegido."},
	"err.wildcard":        {"en": "Wildcard domains only work without Caddy management (certificates need a DNS challenge).", "de": "Wildcard-Domains gehen nur ohne Caddy-Verwaltung (Zertifikate brauchen eine DNS-Challenge).", "es": "Los dominios comodín solo funcionan sin gestión de Caddy (los certificados necesitan un desafío DNS)."},
	"err.target":          {"en": "Target e.g. 127.0.0.1:8080 or http://container:80", "de": "Ziel z. B. 127.0.0.1:8080 oder http://container:80", "es": "Destino, p. ej. 127.0.0.1:8080 o http://container:80"},
	"err.access":          {"en": "Invalid access rule", "de": "Ungültige Zugriffsregel", "es": "Regla de acceso no válida"},
	"err.bypass":          {"en": "Invalid public path %q – paths start with / and may only have * at the end.", "de": "Ungültige Ausnahme %q – Pfade beginnen mit / und dürfen * nur am Ende haben.", "es": "Ruta pública no válida %q: las rutas empiezan por / y solo pueden tener * al final."},
	"err.domainExists":    {"en": "This domain is already set up.", "de": "Diese Domain ist schon eingerichtet.", "es": "Este dominio ya está configurado."},
	"err.caddyRejected":   {"en": "Caddy rejected the configuration: %s", "de": "Caddy hat die Konfiguration abgelehnt: %s", "es": "Caddy rechazó la configuración: %s"},
	"err.siteNotFound":    {"en": "Site not found", "de": "Seite nicht gefunden", "es": "Sitio no encontrado"},
	"warn.caddyReload":    {"en": "Caddy could not be reloaded: %s", "de": "Caddy konnte nicht neu geladen werden: %s", "es": "No se pudo recargar Caddy: %s"},
	"err.role":            {"en": "Invalid role", "de": "Ungültige Rolle", "es": "Rol no válido"},
	"err.userExists":      {"en": "This username already exists.", "de": "Diesen Benutzernamen gibt es schon.", "es": "Este nombre de usuario ya existe."},
	"err.userNotFound":    {"en": "User not found", "de": "Benutzer nicht gefunden", "es": "Usuario no encontrado"},
	"err.selfDemote":      {"en": "You cannot remove your own admin rights.", "de": "Du kannst dir nicht selbst die Admin-Rechte nehmen.", "es": "No puedes quitarte tus propios permisos de administrador."},
	"err.lastAdmin":       {"en": "At least one active admin must remain.", "de": "Es muss mindestens ein aktiver Admin bleiben.", "es": "Debe quedar al menos un administrador activo."},
	"err.selfDelete":      {"en": "You cannot delete your own account.", "de": "Du kannst dein eigenes Konto nicht löschen.", "es": "No puedes eliminar tu propia cuenta."},
	"err.selfReset2fa":    {"en": "Change your own two-factor under Settings → My account.", "de": "Deine eigene 2FA änderst du unter Einstellungen → Mein Konto.", "es": "Cambia tu propia verificación en dos pasos en Ajustes → Mi cuenta."},
	"err.between":         {"en": "%s must be between %d and %d.", "de": "%s muss zwischen %d und %d liegen.", "es": "%s debe estar entre %d y %d."},
	"err.currentPassword": {"en": "The current password is not correct.", "de": "Das aktuelle Passwort stimmt nicht.", "es": "La contraseña actual no es correcta."},
	"err.newPassword10":   {"en": "The new password needs at least 10 characters.", "de": "Das neue Passwort braucht mindestens 10 Zeichen.", "es": "La nueva contraseña necesita al menos 10 caracteres."},
	"err.2faActive":       {"en": "Two-factor is already active.", "de": "Zwei-Faktor ist schon aktiv.", "es": "La verificación en dos pasos ya está activa."},
	"err.passwordWrong":   {"en": "The password is not correct.", "de": "Das Passwort stimmt nicht.", "es": "La contraseña no es correcta."},
	"err.2faInactive":     {"en": "Two-factor is not active.", "de": "Zwei-Faktor ist nicht aktiv.", "es": "La verificación en dos pasos no está activa."},
	"err.2faEnforced":     {"en": "Two-factor is required for admins (Settings → Security).", "de": "Für Admins ist Zwei-Faktor Pflicht (Einstellungen → Sicherheit).", "es": "La verificación en dos pasos es obligatoria para los administradores (Ajustes → Seguridad)."},
	"err.language":        {"en": "Invalid language", "de": "Ungültige Sprache", "es": "Idioma no válido"},
	"err.template":        {"en": "Unknown template", "de": "Unbekanntes Template", "es": "Plantilla desconocida"},

	"f.lockAttempts": {"en": "Failed attempts", "de": "Fehlversuche", "es": "Intentos fallidos"},
	"f.lockWindow":   {"en": "Time window", "de": "Zeitfenster", "es": "Intervalo"},
	"f.lockDuration": {"en": "Lock duration", "de": "Sperrdauer", "es": "Duración del bloqueo"},
	"f.sessionHours": {"en": "Session length", "de": "Sitzungsdauer", "es": "Duración de la sesión"},
	"f.rememberDays": {"en": "Stay signed in", "de": "Angemeldet bleiben", "es": "Mantener la sesión"},
	"f.retention":    {"en": "Log retention", "de": "Aufbewahrung", "es": "Conservación del registro"},

	"verify.internal": {"en": "Wicket: internal error", "de": "Wicket: interner Fehler", "es": "Wicket: error interno"},
	"verify.unknown":  {"en": "Wicket: this domain is not set up", "de": "Wicket: diese Domain ist nicht eingerichtet", "es": "Wicket: este dominio no está configurado"},

	"csv.header":   {"en": "Time;Event;User;Site;IP;Device;Details", "de": "Zeit;Ereignis;Benutzer;Seite;IP;Gerät;Details", "es": "Hora;Evento;Usuario;Sitio;IP;Dispositivo;Detalles"},
	"csv.filename": {"en": "wicket-log.csv", "de": "wicket-protokoll.csv", "es": "wicket-registro.csv"},

	// sign-in providers
	"oauth.or":          {"en": "or", "de": "oder", "es": "o"},
	"oauth.continue":    {"en": "Continue with %s", "de": "Weiter mit %s", "es": "Continuar con %s"},
	"oauth.testOk":      {"en": "%s accepted the credentials.", "de": "%s hat die Zugangsdaten akzeptiert.", "es": "%s aceptó las credenciales."},
	"err.oauthState":    {"en": "The sign-in took too long or was started in another browser – please try again.", "de": "Die Anmeldung hat zu lange gedauert oder wurde in einem anderen Browser gestartet – bitte noch einmal versuchen.", "es": "El inicio de sesión tardó demasiado o se inició en otro navegador; vuelve a intentarlo."},
	"err.oauthFailed":   {"en": "Sign-in with %s failed. Please try again.", "de": "Die Anmeldung mit %s ist fehlgeschlagen. Bitte noch einmal versuchen.", "es": "El inicio de sesión con %s falló. Vuelve a intentarlo."},
	"err.oauthEmail":    {"en": "%s did not return a verified email address.", "de": "%s hat keine verifizierte E-Mail-Adresse geliefert.", "es": "%s no devolvió ninguna dirección de correo verificada."},
	"err.oauthTenant":   {"en": "Only personal Microsoft accounts can sign in. Ask the admin to enter your organisation's tenant ID.", "de": "Nur private Microsoft-Konten können sich anmelden. Bitte den Admin, die Tenant-ID deiner Organisation einzutragen.", "es": "Solo pueden iniciar sesión las cuentas personales de Microsoft. Pide al administrador que introduzca el ID de inquilino de tu organización."},
	"err.oauthNoUser":   {"en": "No Wicket account uses the email address of this account.", "de": "Kein Wicket-Konto nutzt die E-Mail-Adresse dieses Kontos.", "es": "Ninguna cuenta de Wicket usa la dirección de correo de esta cuenta."},
	"err.oauthConfig":   {"en": "Client ID and client secret are required.", "de": "Client-ID und Client-Secret sind Pflicht.", "es": "El ID de cliente y el secreto de cliente son obligatorios."},
	"err.oauthNoHost":   {"en": "Set the login address first (Settings → General).", "de": "Trage zuerst die Login-Adresse ein (Einstellungen → Allgemein).", "es": "Primero configura la dirección de inicio de sesión (Ajustes → General)."},
	"err.oauthTest":     {"en": "%s rejected the credentials: %s", "de": "%s hat die Zugangsdaten abgelehnt: %s", "es": "%s rechazó las credenciales: %s"},
	"err.oauthProvider": {"en": "Unknown sign-in provider", "de": "Unbekannter Anmelde-Anbieter", "es": "Proveedor de inicio de sesión desconocido"},
}
