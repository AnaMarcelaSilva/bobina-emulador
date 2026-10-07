package escpos

import (
	"fmt"
	"html"
	"strings"

	"bobina/internal/codigobarras"
)

// Papel descreve a bobina: quantas colunas cabem na fonte A e quantos pontos
// a cabeça de impressão tem de largura (576 em 80 mm, 384 em 58 mm).
type Papel struct {
	Colunas      int
	Pontos       int
	PaginaCodigo int // página usada até chegar um ESC t
}

// Cupom é o resultado do desenho: o HTML para a tela, o texto puro (para
// testes conferirem o conteúdo) e os avisos para quem está desenvolvendo.
type Cupom struct {
	HTML   string   `json:"html"`
	Texto  string   `json:"texto"`
	Avisos []string `json:"avisos"`
	Cortes int      `json:"cortes"`
	Gaveta bool     `json:"gaveta"`
}

type estilo struct {
	negrito, sublinhado, invertido, fonteB bool
	condensado                             bool // Bematech: 2/3 da largura
	largura, altura                        int
}

type trecho struct {
	texto string
	estilo
}

type desenhista struct {
	papel  Papel
	escala float64 // quantos "ch" (largura de um caractere da fonte A) vale um ponto

	est         estilo
	alinhamento int
	pagina      int
	espacamento int // pontos entre linhas
	espacoCar   int

	linha        []trecho
	alinhaLinha  int
	larguraLinha float64
	textoLinha   strings.Builder

	imagem [][]bool // faixas de ESC * empilhadas, desenhadas juntas

	cbAltura, cbModulo, cbTexto int
	qrModulo                    int
	qrNivel                     byte
	qrDados                     []byte

	html     strings.Builder
	texto    strings.Builder
	avisos   []string
	jaAvisou map[string]bool
	cortes   int
	gaveta   bool
	nLinha   int
	bytesTxt []byte
}

// Desenhar monta o cupom a partir dos comandos.
func Desenhar(cmds []Comando, papel Papel) Cupom {
	if papel.Colunas <= 0 {
		papel.Colunas = 48
	}
	if papel.Pontos <= 0 {
		papel.Pontos = 576
	}
	d := &desenhista{papel: papel, escala: float64(papel.Colunas) / float64(papel.Pontos), jaAvisou: map[string]bool{}}
	d.reiniciar()
	d.pagina = papel.PaginaCodigo

	desconhecidos := 0
	for _, c := range cmds {
		if c.Tipo == Desconhecido {
			desconhecidos++
		}
		d.aplicar(c)
	}
	d.fecharLinha(false)
	d.fecharImagem()

	if PareceUTF8(d.bytesTxt) {
		d.avisar(fmt.Sprintf("O texto chegou em UTF-8, mas a impressora usa a página %s, então os acentos saem trocados (%s). Converta o texto para a página de código da impressora antes de enviar.", NomePaginaCodigo(d.pagina), ExemploAcento(d.bytesTxt, d.pagina)))
	}
	if desconhecidos > 0 {
		d.avisar(fmt.Sprintf("%d comando(s) que o emulador não conhece; veja a aba Comandos.", desconhecidos))
	}

	return Cupom{
		HTML:   fmt.Sprintf(`<div class="cupom" style="--colunas:%d">%s</div>`, papel.Colunas, d.html.String()),
		Texto:  strings.TrimRight(d.texto.String(), "\n"),
		Avisos: d.avisos,
		Cortes: d.cortes,
		Gaveta: d.gaveta,
	}
}

func (d *desenhista) reiniciar() {
	d.est = estilo{largura: 1, altura: 1}
	d.alinhamento = 0
	d.espacamento = 30
	d.espacoCar = 0
	d.pagina = d.papel.PaginaCodigo
	d.cbAltura, d.cbModulo, d.cbTexto = 162, 3, 0
	d.qrModulo, d.qrNivel = 3, 'L'
}

func (d *desenhista) avisar(msg string) {
	if !d.jaAvisou[msg] {
		d.jaAvisou[msg] = true
		d.avisos = append(d.avisos, msg)
	}
}

func (d *desenhista) aplicar(c Comando) {
	p := c.Parametro
	switch c.Nome {
	case "texto":
		d.escrever(c.Bytes)
	case "LF":
		d.fecharLinha(true)
	case "FF":
		d.fecharLinha(true)
	case "ESC @":
		d.fecharLinha(false)
		d.reiniciar()
	case "ESC !":
		n := p(2)
		d.est.fonteB = n&1 == 1
		d.est.negrito = n&0x08 != 0
		d.est.altura, d.est.largura = 1, 1
		if n&0x10 != 0 {
			d.est.altura = 2
		}
		if n&0x20 != 0 {
			d.est.largura = 2
		}
		d.est.sublinhado = n&0x80 != 0
	case "ESC E", "ESC G":
		// No modo Bematech o ESC E não tem parâmetro: só liga.
		d.est.negrito = len(c.Bytes) == 2 || p(2)&1 == 1
	case "ESC F":
		d.est.negrito = false
	case "SI", "ESC SI":
		d.est.condensado = true
	case "DC2", "ESC DC2":
		d.est.condensado = false
	case "ESC W":
		if len(c.Bytes) == 3 { // expandido da Bematech; no ESC/POS o ESC W é outra coisa
			d.est.largura = map[bool]int{true: 2, false: 1}[p(2)&1 == 1]
		}
	case "GS k Q":
		d.qrBematech(c)
	case "ESC -":
		d.est.sublinhado = p(2)%48 != 0
	case "ESC a":
		d.alinhamento = p(2) % 48 % 3
	case "ESC M":
		d.est.fonteB = p(2)%48 == 1
	case "ESC SP":
		d.espacoCar = p(2)
	case "ESC 3":
		d.espacamento = p(2)
	case "ESC 2":
		d.espacamento = 30
	case "ESC t":
		d.pagina = p(2)
	case "ESC d":
		d.fecharLinha(true)
		for i := 0; i < p(2); i++ {
			d.fecharLinha(true)
		}
	case "ESC J":
		d.fecharLinha(false)
		d.bloco(fmt.Sprintf(`<div style="height:%.2fch"></div>`, float64(p(2))*d.escala))
	case "GS !":
		n := p(2)
		d.est.largura, d.est.altura = n>>4%8+1, n&7+1
	case "GS B":
		d.est.invertido = p(2)&1 == 1
	case "GS h":
		d.cbAltura = max(p(2), 1)
	case "GS w":
		d.cbModulo = min(max(p(2), 1), 6)
	case "GS H":
		d.cbTexto = p(2) % 48 % 4
	case "GS k":
		d.codigoBarras(c)
	case "GS ( k":
		d.qr(c)
	case "GS v 0":
		d.raster(c)
	case "ESC *":
		d.faixa(c)
	case "FS p":
		d.fecharLinha(false)
		d.marca("imagem", "Logotipo guardado na impressora (FS p)")
	case "GS ( L", "GS 8 L":
		d.avisar("Há gráficos enviados com GS ( L / GS 8 L, que o emulador ainda não desenha.")
	case "GS V", "ESC i", "ESC m", "ESC w":
		d.fecharLinha(false)
		d.cortes++
		d.bloco(fmt.Sprintf(`<div class="corte" title="%s"></div>`, html.EscapeString(c.Descricao)))
	case "ESC p", "DLE DC4 1", "ESC v":
		d.fecharLinha(false)
		d.gaveta = true
		d.marca("gaveta", "Gaveta acionada")
	case "ESC B":
		d.marca("bipe", "Bipe")
	}
}

// ------------------------------------------------------------------ texto

func (d *desenhista) escrever(b []byte) {
	d.fecharImagem()
	d.bytesTxt = append(d.bytesTxt, b...)
	s := Decodificar(b, d.pagina)
	s = strings.ReplaceAll(s, "\t", "        ")
	if len(d.linha) == 0 {
		d.alinhaLinha = d.alinhamento
	}
	d.linha = append(d.linha, trecho{s, d.est})
	fator := 1.0
	if d.est.fonteB {
		fator = 0.75
	}
	if d.est.condensado {
		fator *= 2.0 / 3
	}
	d.larguraLinha += float64(len([]rune(s))*d.est.largura) * fator
	d.textoLinha.WriteString(s)
}

// fecharLinha imprime o que estiver na linha. Com avancar, uma linha vazia
// também ocupa espaço (é um LF sozinho).
func (d *desenhista) fecharLinha(avancar bool) {
	if len(d.linha) == 0 {
		if avancar {
			if d.imagem != nil {
				return // o LF depois de uma faixa de imagem só desce para a próxima faixa
			}
			d.bloco(fmt.Sprintf(`<div class="l" style="height:%.2fem"></div>`, d.alturaLinha(1)))
			d.texto.WriteString("\n")
		}
		return
	}
	d.nLinha++
	if d.larguraLinha > float64(d.papel.Colunas)+0.01 {
		d.avisar(fmt.Sprintf("Linha com %.0f colunas numa bobina de %d. A impressora quebra a linha no meio (ex.: \"%s\").",
			d.larguraLinha, d.papel.Colunas, resumir(d.textoLinha.String(), 40)))
	}

	maiorAltura := 1
	var b strings.Builder
	for _, t := range d.linha {
		maiorAltura = max(maiorAltura, t.altura)
		b.WriteString(d.trechoHTML(t))
	}
	alinha := []string{"left", "center", "right"}[d.alinhaLinha]
	d.bloco(fmt.Sprintf(`<div class="l" style="text-align:%s;line-height:%.2fem">%s</div>`, alinha, d.alturaLinha(maiorAltura), b.String()))
	d.texto.WriteString(d.textoLinha.String())
	d.texto.WriteString("\n")

	d.linha = nil
	d.larguraLinha = 0
	d.textoLinha.Reset()
}

// alturaLinha converte o espaçamento em pontos para em, sabendo que a fonte A
// tem 24 pontos de altura e o espaçamento padrão (30 pontos) vira 1,3em.
func (d *desenhista) alturaLinha(alturaCaractere int) float64 {
	pontos := max(d.espacamento, 24*alturaCaractere)
	return float64(pontos) / 30 * 1.3
}

func (d *desenhista) trechoHTML(t trecho) string {
	var classes []string
	if t.negrito {
		classes = append(classes, "n")
	}
	if t.sublinhado {
		classes = append(classes, "s")
	}
	if t.invertido {
		classes = append(classes, "i")
	}
	if t.fonteB {
		classes = append(classes, "b")
	}
	txt := html.EscapeString(t.texto)
	estiloExtra := ""
	if d.espacoCar > 0 {
		estiloExtra = fmt.Sprintf(` style="letter-spacing:%.3fch"`, float64(d.espacoCar)*d.escala)
	}
	if t.largura == 1 && t.altura == 1 && !t.condensado {
		return fmt.Sprintf(`<span class="%s"%s>%s</span>`, strings.Join(classes, " "), estiloExtra, txt)
	}
	// Caractere ampliado ou condensado: a caixa externa reserva o espaço e a
	// interna estica o texto. O condensado da Bematech mantém a altura e usa
	// 2/3 da largura.
	largura := float64(t.largura)
	if t.condensado {
		largura *= 2.0 / 3
	}
	// Um bloco por caractere, para a linha quebrar no meio como na impressora
	// quando passa da largura da bobina.
	var b strings.Builder
	classe := strings.Join(classes, " ")
	for _, r := range t.texto {
		fmt.Fprintf(&b, `<span class="x %s" style="--w:%.4g;--h:%d;--n:1"><span>%s</span></span>`,
			classe, largura, t.altura, html.EscapeString(string(r)))
	}
	return b.String()
}

// ------------------------------------------------------------------ blocos

func (d *desenhista) bloco(s string) {
	d.fecharImagem()
	d.html.WriteString(s)
}

func (d *desenhista) marca(classe, texto string) {
	d.bloco(fmt.Sprintf(`<div class="marca %s">%s</div>`, classe, html.EscapeString(texto)))
	d.texto.WriteString("[" + texto + "]\n")
}

func (d *desenhista) alinhar() string {
	return []string{"left", "center", "right"}[d.alinhamento]
}

func (d *desenhista) imagemHTML(m [][]bool, escalaX, escalaY int) string {
	if len(m) == 0 {
		return ""
	}
	w := float64(len(m[0])*escalaX) * d.escala
	h := float64(len(m)*escalaY) * d.escala
	return fmt.Sprintf(`<div class="img" style="text-align:%s"><img src="%s" style="width:%.2fch;height:%.2fch" alt=""></div>`,
		d.alinhar(), codigobarras.PNG(m), w, h)
}

// raster desenha o GS v 0, o jeito mais comum de mandar logotipo e QR em imagem.
func (d *desenhista) raster(c Comando) {
	d.fecharLinha(false)
	m := c.Parametro(3)
	x := c.Parametro(4) + c.Parametro(5)*256
	matriz := codigobarras.MatrizDeBytes(c.Bytes[8:], x)
	ex, ey := 1, 1
	if m&1 == 1 {
		ex = 2
	}
	if m&2 == 2 {
		ey = 2
	}
	d.bloco(d.imagemHTML(matriz, ex, ey))
	d.texto.WriteString("[imagem]\n")
}

// faixa empilha uma faixa do ESC * (8 ou 24 pontos de altura). Programas
// antigos mandam o logotipo assim, uma faixa por linha.
func (d *desenhista) faixa(c Comando) {
	m := c.Parametro(2)
	colunas := c.Parametro(3) + c.Parametro(4)*256
	altura, ex, ey := 8, 2, 3
	switch m {
	case 1:
		ex = 1
	case 32:
		altura, ey = 24, 1
	case 33:
		altura, ex, ey = 24, 1, 1
	}
	porColuna := altura / 8
	dados := c.Bytes[5:]
	linhas := make([][]bool, altura*ey)
	for i := range linhas {
		linhas[i] = make([]bool, colunas*ex)
	}
	for col := 0; col < colunas; col++ {
		for k := 0; k < porColuna; k++ {
			byt := dados[col*porColuna+k]
			for bit := 0; bit < 8; bit++ {
				if byt&(0x80>>bit) == 0 {
					continue
				}
				y := k*8 + bit
				for dy := 0; dy < ey; dy++ {
					for dx := 0; dx < ex; dx++ {
						linhas[y*ey+dy][col*ex+dx] = true
					}
				}
			}
		}
	}
	if d.imagem == nil {
		d.fecharLinha(false)
		d.texto.WriteString("[imagem]\n")
	}
	largura := colunas * ex
	if d.imagem != nil && len(d.imagem[0]) > largura {
		largura = len(d.imagem[0])
	}
	d.imagem = append(ajustarLargura(d.imagem, largura), ajustarLargura(linhas, largura)...)
}

func ajustarLargura(m [][]bool, w int) [][]bool {
	for i := range m {
		for len(m[i]) < w {
			m[i] = append(m[i], false)
		}
	}
	return m
}

func (d *desenhista) fecharImagem() {
	if d.imagem == nil {
		return
	}
	img := d.imagem
	d.imagem = nil
	d.html.WriteString(d.imagemHTML(img, 1, 1))
}

// ------------------------------------------------------------------ códigos

func (d *desenhista) codigoBarras(c Comando) {
	d.fecharLinha(false)
	m, dados := DadosCodigoBarras(c)
	var s codigobarras.Simbolo
	var err error
	tipo := simbologias[m]
	txt := string(dados)
	switch m {
	case 0, 65:
		s, err = codigobarras.UPCA(txt)
	case 2, 67:
		s, err = codigobarras.EAN13(txt)
	case 3, 68:
		s, err = codigobarras.EAN8(txt)
	case 4, 69:
		s, err = codigobarras.Code39(txt, 3)
	case 5, 70:
		s, err = codigobarras.ITF(txt, 3)
	case 73:
		s, err = d.code128(dados)
	default:
		d.bloco(fmt.Sprintf(`<div class="marca erro">%s: %s (o emulador não desenha essa simbologia)</div>`, html.EscapeString(tipo), html.EscapeString(txt)))
		d.texto.WriteString("[" + tipo + ": " + txt + "]\n")
		return
	}
	if err != nil {
		d.avisar("Código de barras inválido, a impressora não imprime: " + err.Error())
		d.bloco(fmt.Sprintf(`<div class="marca erro">%s</div>`, html.EscapeString(err.Error())))
		return
	}
	larg := float64(len(s.Modulos)*d.cbModulo) * d.escala
	alt := float64(d.cbAltura) * d.escala
	svg := fmt.Sprintf(`<svg viewBox="0 0 %d %d" preserveAspectRatio="none" style="width:%.2fch;height:%.2fch">%s</svg>`,
		len(s.Modulos)*d.cbModulo, d.cbAltura, larg, alt, codigobarras.SVGBarras(s, 0, 0, float64(d.cbModulo), float64(d.cbAltura)))
	hri := fmt.Sprintf(`<div class="hri">%s</div>`, html.EscapeString(s.Texto))
	conteudo := svg
	switch d.cbTexto {
	case 1:
		conteudo = hri + svg
	case 2:
		conteudo = svg + hri
	case 3:
		conteudo = hri + svg + hri
	}
	d.bloco(fmt.Sprintf(`<div class="cb" style="text-align:%s"><div class="cb-in">%s</div></div>`, d.alinhar(), conteudo))
	d.texto.WriteString("[" + tipo + ": " + s.Texto + "]\n")
}

// code128 segue o manual Epson: os dados começam com {A, {B ou {C e, no
// conjunto C, cada byte já é um número de 0 a 99.
func (d *desenhista) code128(b []byte) (codigobarras.Simbolo, error) {
	if len(b) < 2 || b[0] != '{' || (b[1] != 'A' && b[1] != 'B' && b[1] != 'C') {
		d.avisar("Code 128 sem {A, {B ou {C no começo. Pelo manual Epson a impressora ignora o comando; o emulador desenhou como se fosse {B.")
		b = append([]byte("{B"), b...)
	}
	var valores []int
	var texto strings.Builder
	conjunto := byte(0)
	digitosASCII, bytesC := 0, 0
	for i := 0; i < len(b); i++ {
		if b[i] == '{' && i+1 < len(b) && b[i+1] != '{' {
			i++
			switch b[i] {
			case 'A', 'B', 'C':
				if conjunto == 0 {
					valores = append(valores, map[byte]int{'A': 103, 'B': 104, 'C': 105}[b[i]])
				} else {
					valores = append(valores, map[byte]int{'A': 101, 'B': 100, 'C': 99}[b[i]])
				}
				conjunto = b[i]
			case 'S':
				valores = append(valores, 98)
			case '1':
				valores = append(valores, 102)
			case '2':
				valores = append(valores, 97)
			case '3':
				valores = append(valores, 96)
			case '4':
				valores = append(valores, map[byte]int{'A': 101, 'B': 100}[conjunto])
			}
			continue
		}
		if b[i] == '{' {
			i++ // "{{" é a própria chave
		}
		c := b[i]
		switch conjunto {
		case 'C':
			if c > 99 {
				return codigobarras.Simbolo{}, fmt.Errorf("Code 128: no conjunto C cada byte vai de 0 a 99, veio %d", c)
			}
			bytesC++
			if c >= '0' && c <= '9' {
				digitosASCII++
			}
			valores = append(valores, int(c))
			fmt.Fprintf(&texto, "%02d", c)
		case 'A':
			if c < 32 {
				valores = append(valores, int(c)+64)
			} else {
				valores = append(valores, int(c)-32)
			}
			texto.WriteByte(c)
		default:
			valores = append(valores, int(c)-32)
			texto.WriteByte(c)
		}
	}
	if bytesC >= 2 && digitosASCII == bytesC {
		d.avisar("Code 128 no conjunto {C com dígitos em texto (\"1\", \"2\"...). Nesse conjunto cada byte é um número de 0 a 99, então \"12\" vira 4950 e o leitor lê outro código. Mande os pares como bytes (12 → 0x0C) ou use o conjunto {B.")
	}
	if len(valores) < 2 {
		return codigobarras.Simbolo{}, fmt.Errorf("Code 128 sem dados")
	}
	return codigobarras.Valores128(valores, texto.String()), nil
}

func (d *desenhista) qr(c Comando) {
	b := c.Bytes
	if len(b) < 7 {
		return
	}
	cn, fn := b[5], b[6]
	if cn != 49 {
		if fn == 81 {
			d.fecharLinha(false)
			d.marca("erro", "Símbolo 2D (PDF417 ou outro) que o emulador não desenha")
		}
		return
	}
	switch fn {
	case 67:
		d.qrModulo = min(max(c.Parametro(7), 1), 16)
	case 69:
		d.qrNivel = "LMQH"[c.Parametro(7)%48%4]
	case 80:
		d.qrDados = append([]byte{}, b[8:]...)
	case 81:
		d.fecharLinha(false)
		m, err := codigobarras.QR(d.qrDados, d.qrNivel)
		if err != nil || len(d.qrDados) == 0 {
			d.avisar("QR Code mandado imprimir sem dados guardados antes (GS ( k ... 80).")
			return
		}
		lado := float64(len(m)*d.qrModulo) * d.escala
		d.bloco(fmt.Sprintf(`<div class="img" style="text-align:%s"><img src="%s" style="width:%.2fch;height:%.2fch" alt=""></div>`,
			d.alinhar(), codigobarras.PNG(m), lado, lado))
		d.texto.WriteString("[QR Code: " + string(d.qrDados) + "]\n")
	}
}

func resumir(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}

// qrBematech desenha o GS k Q da Bematech: quatro parâmetros (o segundo é o
// tamanho), depois o tamanho dos dados em dois bytes e os dados.
func (d *desenhista) qrBematech(c Comando) {
	d.fecharLinha(false)
	dados := c.Bytes[9:]
	m, err := codigobarras.QR(dados, 'M')
	if err != nil || len(dados) == 0 {
		d.avisar("QR Code (Bematech) sem dados.")
		return
	}
	modulo := min(max(c.Parametro(4)/2, 2), 8)
	lado := float64(len(m)*modulo) * d.escala
	d.bloco(fmt.Sprintf(`<div class="img" style="text-align:%s"><img src="%s" style="width:%.2fch;height:%.2fch" alt=""></div>`,
		d.alinhar(), codigobarras.PNG(m), lado, lado))
	d.texto.WriteString("[QR Code: " + string(dados) + "]\n")
}
