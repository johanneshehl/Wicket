package main

var brandingMessages = map[string]map[string]string{
	"err.brandLogo":     {"en": "The logo must be a PNG, JPEG, WebP, GIF or SVG image.", "de": "Das Logo muss ein PNG-, JPEG-, WebP-, GIF- oder SVG-Bild sein.", "es": "El logotipo debe ser una imagen PNG, JPEG, WebP, GIF o SVG."},
	"err.brandLogoSize": {"en": "The logo may be at most %d KB.", "de": "Das Logo darf höchstens %d KB groß sein.", "es": "El logotipo puede tener como máximo %d KB."},
	"err.brandLogoSVG":  {"en": "SVG logos must not contain scripts or event handlers.", "de": "SVG-Logos dürfen keine Skripte oder Event-Handler enthalten.", "es": "Los logotipos SVG no pueden contener scripts ni controladores de eventos."},
	"err.brandAccent":   {"en": "The accent colour must be a hex value like #0070f3.", "de": "Die Akzentfarbe muss ein Hex-Wert wie #0070f3 sein.", "es": "El color de acento debe ser un valor hexadecimal como #0070f3."},
	"err.brandText":     {"en": "Name at most 40 characters, footer at most 120.", "de": "Name höchstens 40 Zeichen, Fußzeile höchstens 120.", "es": "Nombre como máximo 40 caracteres, pie de página como máximo 120."},
}

func init() {
	for k, v := range brandingMessages {
		messages[k] = v
	}
}
