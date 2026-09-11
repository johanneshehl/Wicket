package main

// Messages for the features added in 1.3 (groups, IP rules, OIDC, mail, notifications, roles).
// Merged into messages at start so i18n.go stays focused on the core.

var extMessages = map[string]map[string]string{
	"verify.blocked": {"en": "Wicket: access from your IP address is blocked", "de": "Wicket: Zugriff von deiner IP-Adresse ist gesperrt", "es": "Wicket: el acceso desde tu dirección IP está bloqueado"},

	"err.readOnly":            {"en": "Your role only allows reading.", "de": "Deine Rolle erlaubt nur Lesezugriff.", "es": "Tu rol solo permite lectura."},
	"err.ipRule":              {"en": "IP rule: %s", "de": "IP-Regel: %s", "es": "Regla de IP: %s"},
	"f.maxSession":            {"en": "Session limit", "de": "Sitzungsgrenze", "es": "Límite de sesión"},
	"err.groupName":           {"en": "Group name: 2–48 characters.", "de": "Gruppenname: 2–48 Zeichen.", "es": "Nombre del grupo: 2–48 caracteres."},
	"err.groupExists":         {"en": "A group with this name already exists.", "de": "Eine Gruppe mit diesem Namen gibt es schon.", "es": "Ya existe un grupo con este nombre."},
	"err.groupNotFound":       {"en": "Group not found", "de": "Gruppe nicht gefunden", "es": "Grupo no encontrado"},
	"err.oidcName":            {"en": "Application name: 2–48 characters.", "de": "Name der App: 2–48 Zeichen.", "es": "Nombre de la aplicación: 2–48 caracteres."},
	"err.oidcRedirect":        {"en": "Invalid redirect URL %q – use https:// (http:// only for localhost).", "de": "Ungültige Weiterleitungs-URL %q – nutze https:// (http:// nur für localhost).", "es": "URL de redirección no válida %q: usa https:// (http:// solo para localhost)."},
	"err.oidcRedirectMissing": {"en": "Enter at least one redirect URL.", "de": "Trage mindestens eine Weiterleitungs-URL ein.", "es": "Introduce al menos una URL de redirección."},
	"err.oidcNotFound":        {"en": "Application not found", "de": "App nicht gefunden", "es": "Aplicación no encontrada"},
	"err.oidcPublic":          {"en": "Public applications have no secret.", "de": "Öffentliche Apps haben kein Secret.", "es": "Las aplicaciones públicas no tienen secreto."},
	"err.smtp":                {"en": "Mail server: %s", "de": "Mailserver: %s", "es": "Servidor de correo: %s"},
	"err.smtpMissing":         {"en": "Set up a mail server first (Settings → Mail).", "de": "Richte zuerst einen Mailserver ein (Einstellungen → E-Mail).", "es": "Configura primero un servidor de correo (Ajustes → Correo)."},
	"err.mailTo":              {"en": "Enter a recipient address.", "de": "Gib eine Empfängeradresse an.", "es": "Introduce una dirección de destinatario."},
	"err.mailTest":            {"en": "The test mail could not be sent: %s", "de": "Die Testmail konnte nicht gesendet werden: %s", "es": "No se pudo enviar el correo de prueba: %s"},
	"err.notifyChannel":       {"en": "Channel %q: %s", "de": "Kanal %q: %s", "es": "Canal %q: %s"},
	"err.notifyNotFound":      {"en": "Channel not found – save it first.", "de": "Kanal nicht gefunden – speichere ihn zuerst.", "es": "Canal no encontrado: guárdalo primero."},
	"err.notifyTest":          {"en": "The test notification failed: %s", "de": "Die Testbenachrichtigung ist fehlgeschlagen: %s", "es": "La notificación de prueba falló: %s"},

	"mail.testSubject": {"en": "Wicket test mail", "de": "Wicket-Testmail", "es": "Correo de prueba de Wicket"},
	"mail.testBody":    {"en": "Your mail server works. Wicket can send invitations, password resets and notifications.", "de": "Dein Mailserver funktioniert. Wicket kann Einladungen, Passwort-Links und Benachrichtigungen senden.", "es": "Tu servidor de correo funciona. Wicket puede enviar invitaciones, restablecimientos de contraseña y notificaciones."},
	"mail.testSent":    {"en": "Test mail sent to %s.", "de": "Testmail an %s gesendet.", "es": "Correo de prueba enviado a %s."},

	"notify.testTitle":   {"en": "Test notification", "de": "Testbenachrichtigung", "es": "Notificación de prueba"},
	"notify.testBody":    {"en": "This channel works.", "de": "Dieser Kanal funktioniert.", "es": "Este canal funciona."},
	"notify.testSent":    {"en": "Test sent to %s.", "de": "Test an %s gesendet.", "es": "Prueba enviada a %s."},
	"notify.adminTitle":  {"en": "Admin sign-in: %s", "de": "Admin-Anmeldung: %s", "es": "Inicio de sesión de administrador: %s"},
	"notify.adminBody":   {"en": "%s signed in to Wicket from %s (%s).", "de": "%s hat sich von %s (%s) bei Wicket angemeldet.", "es": "%s inició sesión en Wicket desde %s (%s)."},
	"notify.deviceTitle": {"en": "New device for %s", "de": "Neues Gerät für %s", "es": "Nuevo dispositivo para %s"},
	"notify.deviceBody":  {"en": "%s signed in from an IP address not seen before: %s (%s). If this was not you, end the sessions in Wicket.", "de": "%s hat sich von einer bisher unbekannten IP-Adresse angemeldet: %s (%s). Warst du das nicht, beende die Sitzungen in Wicket.", "es": "%s inició sesión desde una dirección IP desconocida: %s (%s). Si no fuiste tú, cierra las sesiones en Wicket."},
	"notify.lockedTitle": {"en": "IP address locked: %s", "de": "IP-Adresse gesperrt: %s", "es": "Dirección IP bloqueada: %s"},
	"notify.lockedBody":  {"en": "Brute-force protection locked %s after repeated failed sign-ins (site: %s).", "de": "Der Schutz vor Passwort-Raten hat %s nach wiederholten Fehlversuchen gesperrt (Seite: %s).", "es": "La protección contra fuerza bruta bloqueó %s tras varios intentos fallidos (sitio: %s)."},
	"notify.changeTitle": {"en": "Wicket configuration changed", "de": "Wicket-Konfiguration geändert", "es": "Configuración de Wicket modificada"},
	"notify.changeBody":  {"en": "%s: %s %s", "de": "%s: %s %s", "es": "%s: %s %s"},
}

func init() {
	for k, v := range extMessages {
		messages[k] = v
	}
}
