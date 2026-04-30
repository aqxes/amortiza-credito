package main

import (
	"context"
	"fmt"
	"html/template"
	"log"
	"math"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/google/generative-ai-go/genai"
	"github.com/joho/godotenv"
	"google.golang.org/api/option"
)

type Deuda struct {
	Nombre string
	Saldo  float64
	Cuota  float64
	Tasa   float64
	Meses  int
}

type PerfilFinanciero struct {
	Ingresos   float64
	Estrategia string
	Deudas     []Deuda
}

type FilaAmortizacion struct {
	Mes        int
	SaldoIni   float64
	CuotaBase  float64
	AbonoExtra float64
	Interes    float64
	Capital    float64
	SaldoFin   float64
}

type TablaDeuda struct {
	Nombre string
	Filas  []FilaAmortizacion
}

type VistaHTML struct {
	Calculado           bool
	Estrategia          string
	AbonoSugerido       float64
	DeudaObjetivo       string
	MesesBase           int
	MesesOptA           int
	AhorroMeses         int
	AhorroInteresesOptA float64
	AhorroInteresesOptB float64
	AbonoOptB           float64
	NuevaCuota          float64
	Tablas              []TablaDeuda
	TextoIA             template.HTML
}

// NUEVO: Función para poner puntos de miles (Ej: 1500000 -> 1.500.000)
func formatearDinero(monto float64) string {
	if math.IsNaN(monto) || math.IsInf(monto, 0) {
		return "0"
	}
	texto := fmt.Sprintf("%.0f", monto)
	esNegativo := false
	if texto[0] == '-' {
		esNegativo = true
		texto = texto[1:]
	}

	var resultado []byte
	l := len(texto)
	for i := 0; i < l; i++ {
		if i > 0 && (l-i)%3 == 0 {
			resultado = append(resultado, '.')
		}
		resultado = append(resultado, texto[i])
	}
	if esNegativo {
		return "-" + string(resultado)
	}
	return string(resultado)
}

func calcularRebajaCuota(abono, tasaMensual float64, mesesRestantes int) float64 {
	if mesesRestantes <= 0 {
		return 0
	}
	if tasaMensual <= 0 {
		return abono / float64(mesesRestantes)
	}

	tasaDecimal := tasaMensual / 100.0
	return abono * (tasaDecimal / (1 - math.Pow(1+tasaDecimal, -float64(mesesRestantes))))
}

func simularViajeEnElTiempo(deudas []Deuda, abonoMensualExtra float64, usarCascada bool) (int, float64, map[int][]FilaAmortizacion) {
	deudasSim := make([]Deuda, len(deudas))
	copy(deudasSim, deudas)

	tablas := make(map[int][]FilaAmortizacion)
	meses := 0
	interesesPagados := 0.0
	poderAbono := abonoMensualExtra

	for {
		todasPagadas := true
		abonoDisponible := poderAbono
		meses++

		for i := range deudasSim {
			if deudasSim[i].Saldo > 0 {
				todasPagadas = false
				fila := FilaAmortizacion{Mes: meses, SaldoIni: deudasSim[i].Saldo}

				interes := deudasSim[i].Saldo * (deudasSim[i].Tasa / 100.0)
				interesesPagados += interes
				fila.Interes = interes
				deudasSim[i].Saldo += interes

				pagoMinimo := deudasSim[i].Cuota
				if deudasSim[i].Saldo < pagoMinimo {
					pagoMinimo = deudasSim[i].Saldo
				}
				deudasSim[i].Saldo -= pagoMinimo
				fila.CuotaBase = pagoMinimo

				if deudasSim[i].Saldo <= 0.01 {
					deudasSim[i].Saldo = 0
					if usarCascada {
						poderAbono += deudasSim[i].Cuota
					}
				}

				fila.SaldoFin = deudasSim[i].Saldo
				tablas[i] = append(tablas[i], fila)
			}
		}

		if todasPagadas {
			meses--
			break
		}

		for i := range deudasSim {
			if deudasSim[i].Saldo > 0 && abonoDisponible > 0 {
				ultimaFilaIndex := len(tablas[i]) - 1
				filaActual := tablas[i][ultimaFilaIndex]

				if deudasSim[i].Saldo <= abonoDisponible {
					abonoAplicado := deudasSim[i].Saldo
					abonoDisponible -= abonoAplicado
					deudasSim[i].Saldo = 0
					if usarCascada {
						poderAbono += deudasSim[i].Cuota
					}

					filaActual.AbonoExtra = abonoAplicado
					filaActual.SaldoFin = 0
				} else {
					abonoAplicado := abonoDisponible
					deudasSim[i].Saldo -= abonoAplicado
					abonoDisponible = 0

					filaActual.AbonoExtra = abonoAplicado
					filaActual.SaldoFin = deudasSim[i].Saldo
				}

				filaActual.Capital = filaActual.CuotaBase + filaActual.AbonoExtra - filaActual.Interes
				tablas[i][ultimaFilaIndex] = filaActual
			}
		}

		// Seguro contra loops infinitos
		if meses > 1200 {
			break
		}
	}

	return meses, interesesPagados, tablas
}

func manejadorCalculadora(w http.ResponseWriter, r *http.Request) {
	// NUEVO: Conectamos la función formatearDinero al HTML
	funcMap := template.FuncMap{
		"dinero": formatearDinero,
	}
	tmpl, err := template.New("interfaz.html").Funcs(funcMap).ParseFiles("interfaz.html")
	if err != nil {
		http.Error(w, "Error cargando la interfaz: "+err.Error(), http.StatusInternalServerError)
		return
	}

	if r.Method == http.MethodGet {
		datos := VistaHTML{Calculado: false}
		tmpl.Execute(w, datos)
		return
	}

	r.ParseForm()

	ingresos, _ := strconv.ParseFloat(r.FormValue("ingresos"), 64)
	estrategiaElegida := r.FormValue("estrategia")

	nombres := r.Form["nombre_deuda[]"]
	saldos := r.Form["saldo_deuda[]"]
	cuotas := r.Form["cuota_deuda[]"]
	tasas := r.Form["tasa_deuda[]"]
	mesesFaltantes := r.Form["meses_deuda[]"]

	var listaDeudas []Deuda
	var totalCuotas float64

	for i := 0; i < len(nombres); i++ {
		saldoActual, _ := strconv.ParseFloat(saldos[i], 64)
		cuotaMensual, _ := strconv.ParseFloat(cuotas[i], 64)
		tasaMensual, _ := strconv.ParseFloat(tasas[i], 64)
		mesesDeuda, _ := strconv.Atoi(mesesFaltantes[i])

		// ¡EL SALVAVIDAS MATEMÁTICO!
		// Si la cuota que puso el usuario no alcanza ni para pagar los intereses, la deuda crecería al infinito.
		// Aquí forzamos la cuota real usando la fórmula de amortización bancaria.
		tasaDecimal := tasaMensual / 100.0
		interesMinimo := saldoActual * tasaDecimal
		if cuotaMensual <= interesMinimo {
			if tasaDecimal > 0 && mesesDeuda > 0 {
				cuotaMensual = saldoActual * (tasaDecimal / (1 - math.Pow(1+tasaDecimal, -float64(mesesDeuda))))
			} else {
				cuotaMensual = interesMinimo + 1 // Para destrabar el cálculo
			}
		}

		listaDeudas = append(listaDeudas, Deuda{
			Nombre: nombres[i], Saldo: saldoActual, Cuota: cuotaMensual, Tasa: tasaMensual, Meses: mesesDeuda,
		})
		totalCuotas += cuotaMensual
	}

	perfil := PerfilFinanciero{Ingresos: ingresos, Estrategia: estrategiaElegida, Deudas: listaDeudas}

	if perfil.Estrategia == "avalancha" {
		sort.Slice(perfil.Deudas, func(i, j int) bool { return perfil.Deudas[i].Tasa > perfil.Deudas[j].Tasa })
	} else {
		sort.Slice(perfil.Deudas, func(i, j int) bool { return perfil.Deudas[i].Saldo < perfil.Deudas[j].Saldo })
	}

	dineroLibre := perfil.Ingresos - totalCuotas
	abonoSugerido := 0.0

	if dineroLibre > 0 {
		nivelEndeudamiento := totalCuotas / perfil.Ingresos
		porcentajeBase := 0.70
		if nivelEndeudamiento >= 0.40 {
			porcentajeBase = 0.30
		} else if nivelEndeudamiento >= 0.20 {
			porcentajeBase = 0.50
		}
		if perfil.Estrategia == "avalancha" {
			porcentajeBase += 0.10
		} else {
			porcentajeBase -= 0.10
		}
		if porcentajeBase > 0.90 {
			porcentajeBase = 0.90
		}
		if porcentajeBase < 0.10 {
			porcentajeBase = 0.10
		}
		abonoSugerido = dineroLibre * porcentajeBase
	}

	deudaObjetivo := perfil.Deudas[0]

	// 1. UNIVERSO BASE
	mesesBase, interesesBase, _ := simularViajeEnElTiempo(perfil.Deudas, 0.0, false)

	// 2. OPCIÓN A
	mesesOptA, interesesOptA, tablasAmort := simularViajeEnElTiempo(perfil.Deudas, abonoSugerido, true)

	// 3. OPCIÓN B
	abonoOptB := abonoSugerido
	if abonoOptB > deudaObjetivo.Saldo {
		abonoOptB = deudaObjetivo.Saldo
	}

	rebajaCuota := calcularRebajaCuota(abonoOptB, deudaObjetivo.Tasa, deudaObjetivo.Meses)
	nuevaCuota := deudaObjetivo.Cuota - rebajaCuota
	if nuevaCuota < 0 {
		nuevaCuota = 0
	}

	deudasOptB := make([]Deuda, len(perfil.Deudas))
	copy(deudasOptB, perfil.Deudas)
	deudasOptB[0].Saldo -= abonoOptB
	deudasOptB[0].Cuota = nuevaCuota

	_, interesesOptB, _ := simularViajeEnElTiempo(deudasOptB, 0.0, false)

	var listaTablasHTML []TablaDeuda
	for i, d := range perfil.Deudas {
		listaTablasHTML = append(listaTablasHTML, TablaDeuda{
			Nombre: d.Nombre,
			Filas:  tablasAmort[i],
		})
	}

	estrategiaTexto := "Avalancha (Prioridad: Intereses Altos)"
	if estrategiaElegida == "bolanieve" {
		estrategiaTexto = "Bola de Nieve (Prioridad: Saldos Pequeños)"
	}

	datosVista := VistaHTML{
		Calculado:           true,
		Estrategia:          estrategiaTexto,
		AbonoSugerido:       abonoSugerido,
		DeudaObjetivo:       deudaObjetivo.Nombre,
		MesesBase:           mesesBase, // ¡CORREGIDO!
		MesesOptA:           mesesOptA,
		AhorroMeses:         mesesBase - mesesOptA, // ¡CORREGIDO!
		AhorroInteresesOptA: interesesBase - interesesOptA,
		AhorroInteresesOptB: interesesBase - interesesOptB,
		AbonoOptB:           abonoOptB,
		NuevaCuota:          nuevaCuota,
		Tablas:              listaTablasHTML,
	}

	tmpl.Execute(w, datosVista)
}
func main() {
	// 1. Ruta principal (la calculadora de siempre)
	err := godotenv.Load()

	if err != nil {
		fmt.Println("Aviso: No se encontró el archivo .env")
	}
	http.HandleFunc("/", manejadorCalculadora)

	// 2. Ruta DINÁMICA para SEO Programático
	http.HandleFunc("/simulador/", manejadorSEO)

	// 3. Ruta para el archivo ads.txt (importante para AdSense)
	http.HandleFunc("/ads.txt", func(w http.ResponseWriter, r *http.Request) {
		http.ServeFile(w, r, "ads.txt")
	})

	puerto := os.Getenv("PORT")
	if puerto == "" {
		puerto = "8080"
	}

	fmt.Println("Servidor activo en puerto:", puerto)
	log.Fatal(http.ListenAndServe(":"+puerto, nil))
}

// manejadorSEO es el "cerebro" que decide si llamar a la IA o usar el caché
func manejadorSEO(w http.ResponseWriter, r *http.Request) {
	termino := strings.TrimPrefix(r.URL.Path, "/simulador/")
	termino = strings.ReplaceAll(termino, "-", " ")

	if termino == "" {
		http.Redirect(w, r, "/", http.StatusSeeOther)
		return
	}

	// 1. BUSCAR EN CACHÉ
	nombreArchivo := fmt.Sprintf("cache/%s.txt", strings.ReplaceAll(termino, " ", "_"))
	contenido, err := os.ReadFile(nombreArchivo)

	var textoFinal string

	if err == nil {
		textoFinal = string(contenido)
		fmt.Println("Cargando desde caché:", termino)
	} else {
		fmt.Println("Generando nuevo contenido con IA para:", termino)
		textoFinal = generarTextoConIA(termino)

		os.MkdirAll("cache", os.ModePerm)
		os.WriteFile(nombreArchivo, []byte(textoFinal), 0644)
	}

	// 2. RENDERIZAR LA PÁGINA
	funcMap := template.FuncMap{
		"dinero": func(v float64) string {
			s := strconv.FormatFloat(v, 'f', 0, 64)
			for i := len(s) - 3; i > 0; i -= 3 {
				s = s[:i] + "." + s[i:]
			}
			return s
		},
	}

	tmpl, err := template.New("interfaz.html").Funcs(funcMap).ParseFiles("interfaz.html")
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// ESTA ES LA PARTE QUE TE FALTA PARA USAR LA VARIABLE 'tmpl'
	data := struct {
		Calculado bool
		TextoIA   template.HTML
	}{
		Calculado: false,
		TextoIA:   template.HTML(textoFinal), // <-- Así enviamos el texto de Gemini
	}

	err = tmpl.Execute(w, data)
	if err != nil {
		fmt.Println("Error ejecutando template:", err)
	}
}

func generarTextoConIA(tema string) string {
	ctx := context.Background()
	client, err := genai.NewClient(ctx, option.WithAPIKey(os.Getenv("GEMINI_API_KEY")))
	if err != nil {
		return "Error conectando con el experto financiero."
	}
	defer client.Close()

	model := client.GenerativeModel("gemini-2.5-flash")

	prompt := fmt.Sprintf(`Actúa como un experto en finanzas personales. 
	Escribe un artículo corto (3 párrafos) en formato HTML sobre: Cómo hacer abonos a capital para %s.
	Usa etiquetas <h3> para subtítulos. Menciona el beneficio de reducir el tiempo y el método avalancha.
	No incluyas etiquetas <html> ni <body>, solo el contenido.recuerda priorizar la optimizacion del SEO`, tema)

	resp, err := model.GenerateContent(ctx, genai.Text(prompt))
	if err != nil {
		fmt.Println("CRÍTICO - Error de Gemini:", err)
		return "Contenido en desarrollo, vuelve pronto."
	}

	if len(resp.Candidates) > 0 {
		c := resp.Candidates[0]
		if c.Content != nil && len(c.Content.Parts) > 0 {
			return fmt.Sprintf("%v", c.Content.Parts[0])
		}
	}
	return "Optimiza tus deudas hoy mismo con nuestra calculadora."
}
