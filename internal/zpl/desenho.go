package zpl

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"html"
	"io"
	"math"
	"strconv"
	"strings"

	"bobina/internal/codigobarras"
	"bobina/internal/escpos"
)

// Midia é a etiqueta configurada na impressora; ^PW e ^LL podem mudar.
type Midia struct {
	Dpmm      int // pontos por milímetro: 8 (203 dpi), 12 (300 dpi), 24 (600 dpi)
	LarguraMM float64
	AlturaMM  float64
}

// Etiqueta é o resultado do desenho.
type Etiqueta struct {
	SVG        string   `json:"svg"`
	Texto      string   `json:"texto"`
	Avisos     []string `json:"avisos"`
	Quantidade int      `json:"quantidade"`
	Largura    int      `json:"largura"` // em pontos
	Altura     int      `json:"altura"`
}

type fonte struct {
	nome    string
	orient  byte
	altura  int
	largura int
}

// Tamanho base (altura x largura em pontos) das fontes bitmap da Zebra.
var fontesBitmap = map[string][2]int{
	"A": {9, 5}, "B": {11, 7}, "C": {18, 10}, "D": {18, 10}, "E": {28, 15},
	"F": {26, 13}, "G": {60, 40}, "H": {21, 13},
}

type campo struct {
	x, y     int
	ft       bool
	fonte    fonte
	dados    []byte
	temDados bool
	reverso  bool
	hex      byte

	// ^FB
	bloco                  bool
	blocoLarg, blocoLinhas int
	blocoEspaco            int
	blocoAlinha            byte

	// código de barras ou gráfico pendente
	tipo   string
	params []string
}

type desenhistaZPL struct {
	midia        Midia
	largura      int
	altura       int
	origemX      int
	origemY      int
	fontePadrao  fonte
	orientPadrao byte
	byModulo     int
	byRazao      float64
	byAltura     int
	utf8         bool
	invertida    bool
	reversaTudo  bool

	c          campo
	svg        strings.Builder
	textos     []string
	avisos     []string
	vistos     map[string]bool
	qtd        int
	bytesSemCI []byte
}

// Desenhar desenha uma etiqueta (de ^XA a ^XZ).
func Desenhar(dados []byte, midia Midia) Etiqueta {
	if midia.Dpmm <= 0 {
		midia.Dpmm = 8
	}
	if midia.LarguraMM <= 0 {
		midia.LarguraMM = 100
	}
	if midia.AlturaMM <= 0 {
		midia.AlturaMM = 150
	}
	d := &desenhistaZPL{
		midia:        midia,
		largura:      int(math.Round(midia.LarguraMM * float64(midia.Dpmm))),
		altura:       int(math.Round(midia.AlturaMM * float64(midia.Dpmm))),
		fontePadrao:  fonte{"A", 'N', 9, 5},
		orientPadrao: 'N',
		byModulo:     2,
		byRazao:      3,
		byAltura:     10,
		vistos:       map[string]bool{},
		qtd:          1,
	}
	d.novoCampo()

	cmds := Ler(dados)
	desconhecidos := map[string]bool{}
	temXZ := false
	for _, c := range cmds {
		if c.Tipo == "desconhecido" && c.Nome != "texto solto" {
			desconhecidos[c.Nome] = true
		}
		if c.Nome == "^XZ" {
			temXZ = true
		}
		d.aplicar(c)
	}
	if d.c.temDados || d.c.tipo != "" {
		d.fecharCampo()
	}

	if !temXZ {
		d.avisar("A etiqueta não terminou com ^XZ. Uma impressora de verdade fica esperando e não imprime nada.")
	}
	if len(desconhecidos) > 0 {
		var nomes []string
		for n := range desconhecidos {
			nomes = append(nomes, n)
		}
		d.avisar("Comandos que o emulador não conhece: " + strings.Join(nomes, ", ") + ".")
	}
	if !d.utf8 && escpos.PareceUTF8(d.bytesSemCI) {
		d.avisar("O texto está em UTF-8, mas falta ^CI28 na etiqueta, então os acentos saem trocados (" +
			escpos.ExemploAcento(d.bytesSemCI, 2) + "). Coloque ^CI28 logo depois do ^XA.")
	}

	conteudo := d.svg.String()
	if d.invertida {
		conteudo = fmt.Sprintf(`<g transform="rotate(180 %d %d)">%s</g>`, d.largura/2, d.altura/2, conteudo)
	}
	svg := fmt.Sprintf(`<svg class="etiqueta" xmlns="http://www.w3.org/2000/svg" viewBox="0 0 %d %d" data-mm="%.1f x %.1f">`+
		`<rect width="%d" height="%d" fill="#fff"/><g fill="#000">%s</g></svg>`,
		d.largura, d.altura, float64(d.largura)/float64(midia.Dpmm), float64(d.altura)/float64(midia.Dpmm),
		d.largura, d.altura, conteudo)
	return Etiqueta{SVG: svg, Texto: strings.Join(d.textos, "\n"), Avisos: d.avisos, Quantidade: d.qtd, Largura: d.largura, Altura: d.altura}
}

func (d *desenhistaZPL) avisar(msg string) {
	if !d.vistos[msg] {
		d.vistos[msg] = true
		d.avisos = append(d.avisos, msg)
	}
}

func (d *desenhistaZPL) novoCampo() {
	f := d.fontePadrao
	f.orient = d.orientPadrao
	d.c = campo{fonte: f, reverso: d.reversaTudo}
}

func params(s string) []string { return strings.Split(s, ",") }

// num lê o parâmetro i como inteiro, ou usa o padrão quando está vazio.
func num(p []string, i, padrao int) int {
	if i < len(p) {
		if v, err := strconv.Atoi(strings.TrimSpace(p[i])); err == nil {
			return v
		}
	}
	return padrao
}

func letra(p []string, i int, padrao byte) byte {
	if i < len(p) {
		if s := strings.TrimSpace(p[i]); s != "" {
			return strings.ToUpper(s)[0]
		}
	}
	return padrao
}

func (d *desenhistaZPL) aplicar(c Comando) {
	p := params(c.Params)
	switch {
	case c.Nome == "^XA":
		d.novoCampo()
	case c.Nome == "^FO" || c.Nome == "^FT":
		if d.c.temDados || d.c.tipo != "" {
			d.fecharCampo()
		}
		d.c.x, d.c.y, d.c.ft = num(p, 0, 0)+d.origemX, num(p, 1, 0)+d.origemY, c.Nome == "^FT"
	case c.Nome == "^FD" || c.Nome == "^FV":
		d.c.dados = append(d.c.dados, c.Params...)
		d.c.temDados = true
	case c.Nome == "^SN":
		d.c.dados = []byte(strings.TrimSpace(p[0]))
		d.c.temDados = true
	case c.Nome == "^FS":
		d.fecharCampo()
	case c.Nome == "^FH":
		d.c.hex = '_'
		if s := strings.TrimSpace(c.Params); s != "" {
			d.c.hex = s[0]
		}
	case c.Nome == "^FR":
		d.c.reverso = true
	case c.Nome == "^FB":
		d.c.bloco = true
		d.c.blocoLarg = num(p, 0, 0)
		d.c.blocoLinhas = max(num(p, 1, 1), 1)
		d.c.blocoEspaco = num(p, 2, 0)
		d.c.blocoAlinha = letra(p, 3, 'L')
	case c.Nome == "^FW":
		d.orientPadrao = letra(p, 0, 'N')
		d.c.fonte.orient = d.orientPadrao
	case c.Nome == "^CF":
		f := d.fontePadrao
		if s := strings.TrimSpace(p[0]); s != "" {
			f.nome = strings.ToUpper(s[:1])
		}
		f.altura, f.largura = d.tamanhoFonte(f.nome, num(p, 1, 0), num(p, 2, 0))
		d.fontePadrao = f
		f.orient = d.c.fonte.orient
		d.c.fonte = f
	case c.Nome == "^CI":
		d.utf8 = num(p, 0, 0) == 28
	case c.Nome == "^LH":
		d.origemX, d.origemY = num(p, 0, 0), num(p, 1, 0)
	case c.Nome == "^PW":
		d.largura = num(p, 0, d.largura)
	case c.Nome == "^LL":
		d.altura = num(p, 0, d.altura)
	case c.Nome == "^PO":
		d.invertida = letra(p, 0, 'N') == 'I'
	case c.Nome == "^LR":
		d.reversaTudo = letra(p, 0, 'N') == 'Y'
		d.c.reverso = d.reversaTudo
	case c.Nome == "^PQ":
		d.qtd = max(num(p, 0, 1), 1)
	case c.Nome == "^BY":
		d.byModulo = min(max(num(p, 0, d.byModulo), 1), 10)
		if len(p) > 1 {
			if r, err := strconv.ParseFloat(strings.TrimSpace(p[1]), 64); err == nil {
				d.byRazao = r
			}
		}
		d.byAltura = num(p, 2, d.byAltura)
	case c.Nome == "^A@":
		d.c.fonte = fonte{"0", letra(p, 0, d.orientPadrao), 0, 0}
		d.c.fonte.altura, d.c.fonte.largura = d.tamanhoFonte("0", num(p, 1, 0), num(p, 2, 0))
	case strings.HasPrefix(c.Nome, "^A") && len(c.Nome) == 3:
		nome := c.Nome[2:]
		f := fonte{nome: nome, orient: letra(p, 0, d.orientPadrao)}
		f.altura, f.largura = d.tamanhoFonte(nome, num(p, 1, 0), num(p, 2, 0))
		d.c.fonte = f
	case c.Nome == "^GB" || c.Nome == "^GC" || c.Nome == "^GE" || c.Nome == "^GD" || c.Nome == "^GF" ||
		c.Nome == "^BC" || c.Nome == "^BE" || c.Nome == "^B8" || c.Nome == "^BU" || c.Nome == "^B3" ||
		c.Nome == "^B2" || c.Nome == "^BQ":
		d.c.tipo = c.Nome
		d.c.params = p
		if c.Nome == "^GF" {
			// O ^GF já traz os dados; desenha mesmo sem ^FS.
			d.fecharCampo()
		}
	case c.Nome == "^FN" || c.Nome == "^DF" || c.Nome == "^XF":
		d.avisar("Modelos guardados na impressora (^DF, ^XF, ^FN) não são suportados pelo emulador.")
	}
}

// tamanhoFonte resolve altura e largura: na fonte 0 elas escalam livremente;
// nas bitmap viram múltiplos do tamanho base.
func (d *desenhistaZPL) tamanhoFonte(nome string, h, w int) (int, int) {
	if base, ok := fontesBitmap[nome]; ok {
		mh, mw := 1, 1
		if h > 0 {
			mh = max(int(math.Round(float64(h)/float64(base[0]))), 1)
		}
		if w > 0 {
			mw = max(int(math.Round(float64(w)/float64(base[1]))), 1)
		} else {
			mw = mh
		}
		if h <= 0 {
			mh = mw
		}
		return base[0] * mh, base[1] * mw
	}
	if h <= 0 && w <= 0 {
		h = 9
	}
	if h <= 0 {
		h = w
	}
	if w <= 0 {
		w = h
	}
	return h, w
}

// ------------------------------------------------------------------ campos

func (d *desenhistaZPL) fecharCampo() {
	defer d.novoCampo()
	switch {
	case d.c.tipo == "^GB":
		d.caixa()
	case d.c.tipo == "^GC" || d.c.tipo == "^GE":
		d.elipse()
	case d.c.tipo == "^GD":
		d.diagonal()
	case d.c.tipo == "^GF":
		d.grafico()
	case strings.HasPrefix(d.c.tipo, "^B"):
		d.codigo()
	case d.c.temDados:
		d.texto()
	}
}

// colocar posiciona um elemento desenhado em (0,0) com tamanho w x h,
// girando conforme a orientação (N, R, I, B).
func (d *desenhistaZPL) colocar(conteudo string, w, h float64, orient byte, deslocBase float64) {
	x, y := float64(d.c.x), float64(d.c.y)
	if d.c.ft {
		y -= deslocBase
	}
	var t string
	switch orient {
	case 'R':
		t = fmt.Sprintf("translate(%g %g) rotate(90)", x+h, y)
	case 'I':
		t = fmt.Sprintf("translate(%g %g) rotate(180)", x+w, y+h)
	case 'B':
		t = fmt.Sprintf("translate(%g %g) rotate(270)", x, y+w)
	default:
		t = fmt.Sprintf("translate(%g %g)", x, y)
	}
	estilo := ""
	if d.c.reverso {
		estilo = ` fill="#fff" stroke="#fff" style="mix-blend-mode:difference"`
	}
	fmt.Fprintf(&d.svg, `<g transform="%s"%s>%s</g>`, t, estilo, conteudo)

	// Confere se o campo cabe na etiqueta (considerando a rotação).
	larg, alt := w, h
	if orient == 'R' || orient == 'B' {
		larg, alt = h, w
	}
	if x < -1 || y < -1 || x+larg > float64(d.largura)+1 || y+alt > float64(d.altura)+1 {
		desc := d.c.tipo
		if desc == "" {
			desc = fmt.Sprintf("texto \"%s\"", resumir(d.textoCampo(), 30))
		}
		d.avisar(fmt.Sprintf("O campo %s em (%d, %d) passa da borda da etiqueta de %d x %d pontos e vai sair cortado.",
			desc, d.c.x, d.c.y, d.largura, d.altura))
	}
}

// textoCampo aplica o ^FH e a codificação para obter o texto que sai na etiqueta.
func (d *desenhistaZPL) textoCampo() string {
	b := d.c.dados
	if d.c.hex != 0 {
		var out []byte
		for i := 0; i < len(b); i++ {
			if b[i] == d.c.hex && i+2 < len(b) {
				if v, err := hex.DecodeString(string(b[i+1 : i+3])); err == nil {
					out = append(out, v[0])
					i += 2
					continue
				}
			}
			out = append(out, b[i])
		}
		b = out
	}
	if d.utf8 {
		return string(b)
	}
	d.bytesSemCI = append(d.bytesSemCI, b...)
	return escpos.Decodificar(b, 2)
}

// larguraCaractere estima a largura média de um caractere em pontos.
func larguraCaractere(f fonte) float64 {
	if _, ok := fontesBitmap[f.nome]; ok {
		return float64(f.largura) + float64(f.largura)/5 // a fonte bitmap tem espaço entre caracteres
	}
	return float64(f.largura) * 0.58
}

func (d *desenhistaZPL) texto() {
	f := d.c.fonte
	txt := d.textoCampo()
	d.textos = append(d.textos, strings.ReplaceAll(txt, `\&`, "\n"))
	cw := larguraCaractere(f)
	h := float64(f.altura)
	familia := `font-family="'Roboto Condensed','Arial Narrow','Liberation Sans Narrow','Nimbus Sans Narrow','Helvetica Neue',Arial,sans-serif" font-weight="700"`
	escalaX := float64(f.largura) / float64(f.altura)
	_, bitmap := fontesBitmap[f.nome]
	if bitmap {
		familia = `font-family="'DejaVu Sans Mono',Consolas,'Courier New',monospace"`
		escalaX = 1
	}

	linhas := []string{txt}
	larguraBloco := 0.0
	if d.c.bloco {
		larguraBloco = float64(d.c.blocoLarg)
		linhas = quebrarTexto(txt, larguraBloco, cw)
		if len(linhas) > d.c.blocoLinhas {
			d.avisar(fmt.Sprintf("O texto \"%s\" precisa de %d linhas, mas o ^FB permite %d. A impressora escreve o que sobra por cima da última linha.",
				resumir(txt, 30), len(linhas), d.c.blocoLinhas))
			linhas = linhas[:d.c.blocoLinhas]
		}
	}

	var b strings.Builder
	maiorLargura := 0.0
	for i, linha := range linhas {
		largura := float64(len([]rune(linha))) * cw
		maiorLargura = math.Max(maiorLargura, largura)
		yBase := float64(i)*(h+float64(d.c.blocoEspaco)) + h*0.8
		x, ancora := 0.0, "start"
		if d.c.bloco {
			switch d.c.blocoAlinha {
			case 'C':
				x, ancora = larguraBloco/2, "middle"
			case 'R':
				x, ancora = larguraBloco, "end"
			}
		}
		attrs := ""
		if bitmap {
			attrs = fmt.Sprintf(` textLength="%g" lengthAdjust="spacingAndGlyphs"`, largura)
		}
		fmt.Fprintf(&b, `<text x="%g" y="%g" text-anchor="%s" font-size="%g"%s xml:space="preserve" transform="scale(%g 1)">%s</text>`,
			x/escalaX, yBase, ancora, fonteSVG(h, bitmap), attrs, escalaX, html.EscapeString(linha))
	}
	w := maiorLargura
	if d.c.bloco {
		w = larguraBloco
		if maiorLargura > larguraBloco+1 && d.c.blocoLinhas == 1 {
			d.avisar(fmt.Sprintf("O texto \"%s\" é mais largo que o ^FB de %d pontos (estimado).", resumir(txt, 30), d.c.blocoLarg))
		}
	}
	alt := float64(len(linhas))*h + float64(max(len(linhas)-1, 0)*d.c.blocoEspaco)
	d.colocar(fmt.Sprintf(`<g %s>%s</g>`, familia, b.String()), w, alt, f.orient, h*0.8)
}

// A altura da fonte no ZPL é a da célula inteira; no SVG o tamanho é o "em".
func fonteSVG(h float64, bitmap bool) float64 {
	if bitmap {
		return h
	}
	return h * 1.05
}

// quebrarTexto quebra nas palavras para caber em `largura` pontos; "\&" força
// uma quebra, como na impressora.
func quebrarTexto(txt string, largura, cw float64) []string {
	maxCar := int(largura / cw)
	if maxCar < 1 {
		maxCar = 1
	}
	var linhas []string
	for _, paragrafo := range strings.Split(txt, `\&`) {
		atual := ""
		for _, palavra := range strings.Fields(paragrafo) {
			switch {
			case atual == "":
				atual = palavra
			case len([]rune(atual))+1+len([]rune(palavra)) <= maxCar:
				atual += " " + palavra
			default:
				linhas = append(linhas, atual)
				atual = palavra
			}
		}
		linhas = append(linhas, atual)
	}
	return linhas
}

func (d *desenhistaZPL) caixa() {
	p := d.c.params
	t := max(num(p, 2, 1), 1)
	w := max(num(p, 0, t), t)
	h := max(num(p, 1, t), t)
	cor := "#000"
	if letra(p, 3, 'B') == 'W' {
		cor = "#fff"
	}
	r := float64(min(num(p, 4, 0), 8)) / 8 * float64(min(w, h)) / 2
	var s string
	if 2*t >= min(w, h) && r == 0 {
		s = fmt.Sprintf(`<rect width="%d" height="%d" fill="%s"/>`, w, h, cor)
	} else {
		s = fmt.Sprintf(`<rect x="%g" y="%g" width="%g" height="%g" rx="%g" fill="none" stroke="%s" stroke-width="%d"/>`,
			float64(t)/2, float64(t)/2, float64(w-t), float64(h-t), r, cor, t)
	}
	if d.c.reverso {
		s = strings.ReplaceAll(strings.ReplaceAll(s, `fill="`+cor+`"`, `fill="#fff"`), `stroke="`+cor+`"`, `stroke="#fff"`)
	}
	d.colocar(s, float64(w), float64(h), 'N', float64(h))
}

func (d *desenhistaZPL) elipse() {
	p := d.c.params
	var w, h, t int
	var cor byte
	if d.c.tipo == "^GC" {
		w = max(num(p, 0, 3), 3)
		h = w
		t = max(num(p, 1, 1), 1)
		cor = letra(p, 2, 'B')
	} else {
		w, h = max(num(p, 0, 3), 3), max(num(p, 1, 3), 3)
		t = max(num(p, 2, 1), 1)
		cor = letra(p, 3, 'B')
	}
	c := "#000"
	if cor == 'W' || d.c.reverso {
		c = "#fff"
	}
	var s string
	if 2*t >= min(w, h) {
		s = fmt.Sprintf(`<ellipse cx="%g" cy="%g" rx="%g" ry="%g" fill="%s"/>`, float64(w)/2, float64(h)/2, float64(w)/2, float64(h)/2, c)
	} else {
		s = fmt.Sprintf(`<ellipse cx="%g" cy="%g" rx="%g" ry="%g" fill="none" stroke="%s" stroke-width="%d"/>`,
			float64(w)/2, float64(h)/2, float64(w-t)/2, float64(h-t)/2, c, t)
	}
	d.colocar(s, float64(w), float64(h), 'N', float64(h))
}

func (d *desenhistaZPL) diagonal() {
	p := d.c.params
	t := max(num(p, 2, 1), 1)
	w, h := max(num(p, 0, t), t), max(num(p, 1, t), t)
	var s string
	if letra(p, 4, 'R') == 'L' {
		s = fmt.Sprintf(`<polygon points="0,0 %d,0 %d,%d %d,%d"/>`, t, w, h, w-t, h)
	} else {
		s = fmt.Sprintf(`<polygon points="0,%d %d,0 %d,0 %d,%d"/>`, h, w-t, w, t, h)
	}
	d.colocar(s, float64(w), float64(h), 'N', float64(h))
}

// ------------------------------------------------------------------ imagem ^GF

func (d *desenhistaZPL) grafico() {
	p := d.c.params
	if len(p) < 5 {
		d.avisar("^GF com parâmetros faltando.")
		return
	}
	formato := letra(p, 0, 'A')
	porLinha := num(p, 3, 0)
	dadosTxt := strings.Join(p[4:], ",")
	var dados []byte
	var err error
	switch {
	case strings.HasPrefix(dadosTxt, ":Z64:") || strings.HasPrefix(dadosTxt, ":B64:"):
		dados, err = decodificarB64(dadosTxt)
	case formato == 'A':
		dados, err = descomprimirHex(dadosTxt, porLinha)
	default:
		err = fmt.Errorf("formato %c (binário) não suportado", formato)
	}
	if err != nil || porLinha <= 0 {
		d.avisar(fmt.Sprintf("Não foi possível ler a imagem ^GF: %v", err))
		return
	}
	m := codigobarras.MatrizDeBytes(dados, porLinha)
	if len(m) == 0 {
		return
	}
	w, h := len(m[0]), len(m)
	img := fmt.Sprintf(`<image width="%d" height="%d" href="%s" style="image-rendering:pixelated"/>`, w, h, codigobarras.PNG(m))
	d.colocar(img, float64(w), float64(h), 'N', float64(h))
}

func decodificarB64(s string) ([]byte, error) {
	z64 := strings.HasPrefix(s, ":Z64:")
	s = s[5:]
	if i := strings.Index(s, ":"); i >= 0 {
		s = s[:i] // tira o CRC do fim
	}
	b, err := base64.StdEncoding.DecodeString(strings.TrimSpace(s))
	if err != nil || !z64 {
		return b, err
	}
	r, err := zlib.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	return io.ReadAll(r)
}

// descomprimirHex entende a compressão do ZPL: G..Y e g..z repetem o próximo
// dígito, "," completa a linha com 0, "!" com 1 e ":" repete a linha anterior.
func descomprimirHex(s string, porLinha int) ([]byte, error) {
	if porLinha <= 0 {
		return nil, fmt.Errorf("bytes por linha inválido")
	}
	tamLinha := porLinha * 2
	var out, linha []byte
	anterior := strings.Repeat("0", tamLinha)
	fecharLinha := func(preencher byte) {
		for len(linha) < tamLinha {
			linha = append(linha, preencher)
		}
		anterior = string(linha)
		out = append(out, linha...)
		linha = nil
	}
	rep := 0
	for _, c := range []byte(s) {
		switch {
		case c >= 'G' && c <= 'Y':
			rep += int(c-'G') + 1
		case c >= 'g' && c <= 'z':
			rep += (int(c-'g') + 1) * 20
		case c == ',':
			fecharLinha('0')
		case c == '!':
			fecharLinha('F')
		case c == ':':
			linha = []byte(anterior)
			fecharLinha('0')
		case (c >= '0' && c <= '9') || (c >= 'A' && c <= 'F') || (c >= 'a' && c <= 'f'):
			n := max(rep, 1)
			rep = 0
			for k := 0; k < n; k++ {
				linha = append(linha, c)
				if len(linha) == tamLinha {
					fecharLinha('0')
				}
			}
		}
	}
	if len(linha) > 0 {
		fecharLinha('0')
	}
	return hex.DecodeString(string(out))
}

// ------------------------------------------------------------------ códigos

func (d *desenhistaZPL) codigo() {
	p := d.c.params
	txt := d.textoCampo()
	orient := letra(p, 0, d.c.fonte.orient)
	if d.c.tipo == "^BQ" {
		d.qr(txt, p, orient)
		return
	}

	altura := d.byAltura
	mostrarTexto, textoAcima := true, false
	var s codigobarras.Simbolo
	var err error
	razao := int(math.Round(d.byRazao))
	switch d.c.tipo {
	case "^BC":
		altura = num(p, 1, altura)
		mostrarTexto, textoAcima = letra(p, 2, 'Y') == 'Y', letra(p, 3, 'N') == 'Y'
		s, err = code128ZPL(txt, letra(p, 5, 'N'))
	case "^BE", "^B8", "^BU":
		altura = num(p, 1, altura)
		mostrarTexto, textoAcima = letra(p, 2, 'Y') == 'Y', letra(p, 3, 'N') == 'Y'
		switch d.c.tipo {
		case "^BE":
			s, err = codigobarras.EAN13(primeiros(txt, 12))
		case "^B8":
			s, err = codigobarras.EAN8(primeiros(txt, 7))
		default:
			s, err = codigobarras.UPCA(primeiros(txt, 11))
		}
	case "^B3":
		altura = num(p, 2, altura)
		mostrarTexto, textoAcima = letra(p, 3, 'Y') == 'Y', letra(p, 4, 'N') == 'Y'
		s, err = codigobarras.Code39(txt, razao)
	case "^B2":
		altura = num(p, 1, altura)
		mostrarTexto, textoAcima = letra(p, 2, 'Y') == 'Y', letra(p, 3, 'N') == 'Y'
		s, err = codigobarras.ITF(txt, razao)
	}
	if err != nil {
		d.avisar("Código de barras inválido: " + err.Error())
		erro := fmt.Sprintf(`<rect width="200" height="%d" fill="none" stroke="#c00" stroke-dasharray="6 4" stroke-width="2"/>`, altura)
		d.colocar(erro, 200, float64(altura), orient, float64(altura))
		return
	}
	d.textos = append(d.textos, "["+nomeCodigo[d.c.tipo]+": "+s.Texto+"]")

	m := float64(d.byModulo)
	w := float64(len(s.Modulos)) * m
	fonteHRI := math.Max(float64(d.byModulo)*9, 14)
	var b strings.Builder
	y := 0.0
	if mostrarTexto && textoAcima {
		fmt.Fprintf(&b, `<text x="%g" y="%g" text-anchor="middle" font-size="%g" font-family="'DejaVu Sans Mono',Consolas,monospace">%s</text>`,
			w/2, fonteHRI*0.85, fonteHRI, html.EscapeString(s.Texto))
		y = fonteHRI + 2
	}
	b.WriteString(codigobarras.SVGBarras(s, 0, y, m, float64(altura)))
	total := y + float64(altura)
	if mostrarTexto && !textoAcima {
		fmt.Fprintf(&b, `<text x="%g" y="%g" text-anchor="middle" font-size="%g" font-family="'DejaVu Sans Mono',Consolas,monospace">%s</text>`,
			w/2, total+fonteHRI*0.95, fonteHRI, html.EscapeString(s.Texto))
		total += fonteHRI + 2
	}
	d.colocar(b.String(), w, total, orient, float64(altura))
}

var nomeCodigo = map[string]string{"^BC": "Code 128", "^BE": "EAN-13", "^B8": "EAN-8", "^BU": "UPC-A", "^B3": "Code 39", "^B2": "ITF", "^BQ": "QR Code"}

func primeiros(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// code128ZPL segue o ^BC: sem código de troca começa no conjunto B; os
// códigos >: >; >9 escolhem B, C e A; no modo A (automático) a impressora
// otimiza sozinha.
func code128ZPL(txt string, modo byte) (codigobarras.Simbolo, error) {
	if modo == 'A' {
		return codigobarras.Code128Auto(txt)
	}
	if strings.HasPrefix(txt, ">;") {
		return codigobarras.Code128Conjunto(txt[2:], 'C')
	}
	if strings.HasPrefix(txt, ">:") {
		return codigobarras.Code128Conjunto(txt[2:], 'B')
	}
	if strings.HasPrefix(txt, ">9") {
		return codigobarras.Code128Auto(txt[2:])
	}
	return codigobarras.Code128Conjunto(txt, 'B')
}

func (d *desenhistaZPL) qr(txt string, p []string, orient byte) {
	ampliacao := min(max(num(p, 2, 2), 1), 10)
	nivel := byte('Q')
	dados := txt
	// O ^FD do QR começa com o nível de correção e o modo: "QA,conteúdo".
	if len(txt) >= 3 && txt[2] == ',' {
		if strings.ContainsRune("HQML", rune(txt[0])) {
			nivel = txt[0]
		}
		dados = txt[3:]
	}
	m, err := codigobarras.QR([]byte(dados), nivel)
	if err != nil {
		d.avisar(err.Error())
		return
	}
	d.textos = append(d.textos, "[QR Code: "+dados+"]")
	lado := float64(len(m) * ampliacao)
	// A Zebra deixa uma margem de 10 pontos acima do QR posicionado com ^FO.
	conteudo := codigobarras.SVGMatriz(m, 0, 10, float64(ampliacao))
	d.colocar(conteudo, lado, lado+10, orient, lado+10)
}

func resumir(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}
