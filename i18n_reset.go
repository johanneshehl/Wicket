package main

// Texts for password reset and invitations (pages and mails).

var resetMessages = map[string]map[string]string{
	"login.forgot": {"en": "Forgot password?", "de": "Passwort vergessen?", "es": "¿Olvidaste la contraseña?"},

	"reset.title":        {"en": "Reset password", "de": "Passwort zurücksetzen", "es": "Restablecer contraseña"},
	"reset.lead":         {"en": "Enter your username or email address. If your account has an email address, you will receive a link to set a new password.", "de": "Gib deinen Benutzernamen oder deine E-Mail-Adresse ein. Hat dein Konto eine E-Mail-Adresse, bekommst du einen Link für ein neues Passwort.", "es": "Introduce tu nombre de usuario o tu correo. Si tu cuenta tiene una dirección de correo, recibirás un enlace para crear una contraseña nueva."},
	"reset.loginField":   {"en": "Username or email", "de": "Benutzername oder E-Mail", "es": "Usuario o correo"},
	"reset.send":         {"en": "Send link", "de": "Link senden", "es": "Enviar enlace"},
	"reset.back":         {"en": "Back to sign-in", "de": "Zurück zur Anmeldung", "es": "Volver al inicio de sesión"},
	"reset.sentTitle":    {"en": "Check your inbox", "de": "Schau in dein Postfach", "es": "Revisa tu correo"},
	"reset.sentLead":     {"en": "If an account with this name or address exists, we have sent a link. It is valid for one hour.", "de": "Gibt es ein Konto mit diesem Namen oder dieser Adresse, haben wir einen Link geschickt. Er ist eine Stunde gültig.", "es": "Si existe una cuenta con este nombre o dirección, te hemos enviado un enlace. Es válido durante una hora."},
	"reset.newTitle":     {"en": "Set a new password", "de": "Neues Passwort festlegen", "es": "Crear una contraseña nueva"},
	"reset.newLead":      {"en": "New password for %s. You will be signed out on all devices.", "de": "Neues Passwort für %s. Du wirst auf allen Geräten abgemeldet.", "es": "Contraseña nueva para %s. Se cerrará la sesión en todos los dispositivos."},
	"reset.save":         {"en": "Save password", "de": "Passwort speichern", "es": "Guardar contraseña"},
	"reset.invalidTitle": {"en": "Link no longer valid", "de": "Link nicht mehr gültig", "es": "El enlace ya no es válido"},
	"reset.invalidLead":  {"en": "This link is invalid, has expired or was already used.", "de": "Dieser Link ist ungültig, abgelaufen oder wurde schon benutzt.", "es": "Este enlace no es válido, ha caducado o ya se ha usado."},
	"reset.again":        {"en": "Request a new link", "de": "Neuen Link anfordern", "es": "Solicitar un enlace nuevo"},
	"reset.doneTitle":    {"en": "Password saved", "de": "Passwort gespeichert", "es": "Contraseña guardada"},
	"reset.doneLead":     {"en": "You can now sign in with your new password.", "de": "Du kannst dich jetzt mit dem neuen Passwort anmelden.", "es": "Ya puedes iniciar sesión con tu contraseña nueva."},
	"reset.toLogin":      {"en": "Go to sign-in", "de": "Zur Anmeldung", "es": "Ir al inicio de sesión"},
	"invite.title":       {"en": "Welcome", "de": "Willkommen", "es": "Bienvenido"},
	"invite.lead":        {"en": "Choose a password for your account %s.", "de": "Wähle ein Passwort für dein Konto %s.", "es": "Elige una contraseña para tu cuenta %s."},

	"mail.validHours":   {"en": "%d hour(s)", "de": "%d Stunde(n)", "es": "%d hora(s)"},
	"mail.validDays":    {"en": "%d days", "de": "%d Tage", "es": "%d días"},
	"mail.resetSubject": {"en": "Reset your password for %s", "de": "Passwort zurücksetzen für %s", "es": "Restablece tu contraseña de %s"},
	"mail.resetBody": {
		"en": "Hello %s,\n\nsomeone asked to reset the password of your account. Open this link to set a new one:\n\n%s\n\nThe link is valid for %s and works once. If this was not you, you can ignore this mail.",
		"de": "Hallo %s,\n\njemand möchte das Passwort deines Kontos zurücksetzen. Öffne diesen Link, um ein neues festzulegen:\n\n%s\n\nDer Link ist %s gültig und funktioniert einmal. Warst du das nicht, kannst du diese Mail ignorieren.",
		"es": "Hola %s:\n\nalguien ha solicitado restablecer la contraseña de tu cuenta. Abre este enlace para crear una nueva:\n\n%s\n\nEl enlace es válido durante %s y funciona una sola vez. Si no has sido tú, puedes ignorar este correo.",
	},
	"mail.resetButton":   {"en": "Set a new password", "de": "Neues Passwort festlegen", "es": "Crear contraseña nueva"},
	"mail.inviteSubject": {"en": "Your account for %s", "de": "Dein Konto für %s", "es": "Tu cuenta para %s"},
	"mail.inviteBody": {
		"en": "Hello %s,\n\nan administrator created an account for you. Open this link to choose your password:\n\n%s\n\nThe link is valid for %s and works once.",
		"de": "Hallo %s,\n\nein Administrator hat ein Konto für dich angelegt. Öffne diesen Link, um dein Passwort festzulegen:\n\n%s\n\nDer Link ist %s gültig und funktioniert einmal.",
		"es": "Hola %s:\n\nun administrador ha creado una cuenta para ti. Abre este enlace para elegir tu contraseña:\n\n%s\n\nEl enlace es válido durante %s y funciona una sola vez.",
	},
	"mail.inviteButton": {"en": "Choose password", "de": "Passwort festlegen", "es": "Elegir contraseña"},
	"mail.inviteSent":   {"en": "Invitation sent to %s.", "de": "Einladung an %s gesendet.", "es": "Invitación enviada a %s."},
	"mail.resetSent":    {"en": "Link sent to %s.", "de": "Link an %s gesendet.", "es": "Enlace enviado a %s."},
	"err.userNoEmail":   {"en": "This user has no email address.", "de": "Dieser Benutzer hat keine E-Mail-Adresse.", "es": "Este usuario no tiene dirección de correo."},
}

func init() {
	for k, v := range resetMessages {
		messages[k] = v
	}
}
