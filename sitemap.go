package main

import (
	"fmt"
	"net/http"
)

// Generador del Sitemap para Google Search Console
func manejadorSitemap(w http.ResponseWriter, r *http.Request) {
	// Le decimos al navegador y a Google que este archivo es un XML
	w.Header().Set("Content-Type", "application/xml")

	// Tu dominio oficial en producción
	dominio := "https://amortizacredito.com"

	// TU MINA DE ORO SEO: Agrega aquí todas las palabras que quieras indexar
	palabrasClave := []string{
		"bancolombia",
		"davivienda",
		"tarjeta-nu",
		"banco-de-bogota",
		"tarjeta-exito",
		"banco-falabella",
		"scotiabank-colpatria",
		"credito-libre-inversion",
		"tarjeta-credito-tuya",
		"banco-caja-social",
		"banco-de-occidente",
		"tarjeta-alkosto",
	}

	// Armamos la cabecera del XML y agregamos tu página principal
	xmlStr := `<?xml version="1.0" encoding="UTF-8"?>
<urlset xmlns="http://www.sitemaps.org/schemas/sitemap/0.9">
	<url>
		<loc>` + dominio + `/</loc>
		<changefreq>weekly</changefreq>
		<priority>1.0</priority>
	</url>`

	// Generamos un bloque XML por cada palabra clave de tu lista
	for _, palabra := range palabrasClave {
		xmlStr += `
	<url>
		<loc>` + dominio + `/simulador/` + palabra + `</loc>
		<changefreq>monthly</changefreq>
		<priority>0.8</priority>
	</url>`
	}

	// Cerramos el documento XML
	xmlStr += `
</urlset>`

	// Imprimimos el resultado en la pantalla
	fmt.Fprint(w, xmlStr)
}
