package main

var passkeyMessages = map[string]map[string]string{
	"passkey.signin":       {"en": "Sign in with a passkey", "de": "Mit Passkey anmelden", "es": "Iniciar sesión con una llave de acceso"},
	"passkey.add":          {"en": "Add a passkey", "de": "Passkey hinzufügen", "es": "Añadir una llave de acceso"},
	"passkey.added":        {"en": "Passkey added. Next time you can sign in with it.", "de": "Passkey hinzugefügt. Beim nächsten Mal kannst du dich damit anmelden.", "es": "Llave de acceso añadida. La próxima vez podrás iniciar sesión con ella."},
	"passkey.failed":       {"en": "The passkey could not be used.", "de": "Der Passkey konnte nicht verwendet werden.", "es": "No se pudo usar la llave de acceso."},
	"passkey.cancelled":    {"en": "Cancelled or no passkey found for this site.", "de": "Abgebrochen oder kein Passkey für diese Seite gefunden.", "es": "Cancelado o no se encontró ninguna llave de acceso para este sitio."},
	"err.passkey":          {"en": "Passkey: %s", "de": "Passkey: %s", "es": "Llave de acceso: %s"},
	"err.passkeySetup":     {"en": "Passkeys need the main domain and the login address (Settings → General).", "de": "Passkeys brauchen die Hauptdomain und die Login-Adresse (Einstellungen → Allgemein).", "es": "Las llaves de acceso necesitan el dominio principal y la dirección de inicio de sesión (Ajustes → General)."},
	"err.passkeyLocked":    {"en": "Too many failed attempts. Try again in %d min.", "de": "Zu viele Fehlversuche. Versuch es in %d Min. wieder.", "es": "Demasiados intentos fallidos. Vuelve a intentarlo en %d min."},
	"err.passkeyNotFound":  {"en": "Passkey not found", "de": "Passkey nicht gefunden", "es": "Llave de acceso no encontrada"},
	"notice.passkeyIntro":  {"en": "Sign in faster next time with your fingerprint, face or device PIN.", "de": "Melde dich beim nächsten Mal schneller an – mit Fingerabdruck, Gesicht oder Geräte-PIN.", "es": "Inicia sesión más rápido la próxima vez con tu huella, tu cara o el PIN del dispositivo."},
}

func init() {
	for k, v := range passkeyMessages {
		messages[k] = v
	}
}
