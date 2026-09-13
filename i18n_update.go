package main

var updateMessages = map[string]map[string]string{
	"err.updateRequired":         {"en": "Wicket %s is required. Update Wicket to continue using the admin interface.", "de": "Wicket %s ist erforderlich. Aktualisiere Wicket, um den Admin-Bereich weiter zu nutzen.", "es": "Se requiere Wicket %s. Actualiza Wicket para seguir usando el panel de administración."},
	"err.updateEnvOff":           {"en": "The update check is turned off (WICKET_UPDATE_CHECK=off).", "de": "Die Update-Prüfung ist abgeschaltet (WICKET_UPDATE_CHECK=off).", "es": "La comprobación de actualizaciones está desactivada (WICKET_UPDATE_CHECK=off)."},
	"err.updateNoMethod":         {"en": "No updater is set up (WICKET_UPDATE_URL and WICKET_UPDATE_TOKEN).", "de": "Kein Updater eingerichtet (WICKET_UPDATE_URL und WICKET_UPDATE_TOKEN).", "es": "No hay ningún actualizador configurado (WICKET_UPDATE_URL y WICKET_UPDATE_TOKEN)."},
	"err.updateNone":             {"en": "There is no newer version.", "de": "Es gibt keine neuere Version.", "es": "No hay ninguna versión más reciente."},
	"err.updateLockedConfig":     {"en": "The update check cannot be turned off while a required update is pending.", "de": "Die Update-Prüfung lässt sich nicht abschalten, solange ein erforderliches Update aussteht.", "es": "La comprobación no se puede desactivar mientras haya una actualización obligatoria pendiente."},
	"err.updateBackup":           {"en": "The backup before the update failed: %s", "de": "Die Sicherung vor dem Update ist fehlgeschlagen: %s", "es": "La copia de seguridad previa a la actualización falló: %s"},
	"update.notifyTitle":         {"en": "Wicket %s is available", "de": "Wicket %s ist verfügbar", "es": "Wicket %s está disponible"},
	"update.notifyTitleRequired": {"en": "Required update: Wicket %s", "de": "Erforderliches Update: Wicket %s", "es": "Actualización obligatoria: Wicket %s"},
	"update.notifyBody":          {"en": "You are running Wicket %s. Open the admin interface to update.", "de": "Du nutzt Wicket %s. Öffne den Admin-Bereich, um zu aktualisieren.", "es": "Usas Wicket %s. Abre el panel de administración para actualizar."},
}

func init() {
	for k, v := range updateMessages {
		messages[k] = v
	}
}
