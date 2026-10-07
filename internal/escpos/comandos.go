// Package escpos lê o fluxo de bytes ESC/POS como a impressora leria: um
// comando por vez, sabendo o tamanho de cada um. Isso permite responder a um
// pedido de status no meio de um cupom sem confundir com os dados de uma
// imagem, e mostrar no inspetor o que cada trecho de bytes significa.
package escpos

import (
	"fmt"
)

// Tipo agrupa os comandos pelo que eles fazem, para o inspetor colorir.
type Tipo string

const (
	Texto        Tipo = "texto"
	Formatacao   Tipo = "formatacao"
	Avanco       Tipo = "avanco"
	Status       Tipo = "status"
	Imagem       Tipo = "imagem"
	CodigoBarras Tipo = "codigo-barras"
	Corte        Tipo = "corte"
	Gaveta       Tipo = "gaveta"
	Controle     Tipo = "controle"
	Desconhecido Tipo = "desconhecido"
)

// Comando é um trecho do fluxo já interpretado.
type Comando struct {
	Offset    int    `json:"offset"`
	Tamanho   int    `json:"tamanho"`
	Tipo      Tipo   `json:"tipo"`
	Nome      string `json:"nome"`      // como aparece no manual: "ESC a", "GS V"
	Descricao string `json:"descricao"` // o que faz, em português

	// Para quem desenha e responde: os bytes do comando inteiro.
	Bytes []byte `json:"-"`

	// NovoModo diz que este comando troca o conjunto de comandos (GS F9 da
	// Bematech); Grava diz que a troca fica guardada na memória da impressora.
	NovoModo Modo `json:"-"`
	Grava    bool `json:"-"`
}

// Modo é o conjunto de comandos que a impressora está entendendo agora.
type Modo uint8

const (
	ModoESCPOS   Modo = 1 // ESC/POS padrão (Epson e compatíveis)
	ModoBematech Modo = 2 // ESC/Bematech, o modo próprio das Bematech
)

// NomeModo é como o modo aparece para o usuário.
func NomeModo(m Modo) string {
	if m == ModoBematech {
		return "ESC/Bematech"
	}
	return "ESC/POS"
}

// Parametro devolve o byte na posição i depois do prefixo (ESC x, GS x...).
func (c Comando) Parametro(i int) int {
	if i < len(c.Bytes) {
		return int(c.Bytes[i])
	}
	return 0
}

// Ler interpreta o fluxo inteiro no modo ESC/POS.
func Ler(dados []byte) []Comando { return LerNoModo(dados, ModoESCPOS) }

// LerNoModo interpreta o fluxo começando no modo dado e seguindo as trocas de
// modo (GS F9) que aparecerem no meio. Bytes de um comando que ficou pela
// metade no fim viram um comando "incompleto".
func LerNoModo(dados []byte, modo Modo) []Comando {
	var r []Comando
	for pos := 0; pos < len(dados); {
		c, ok := ProximoNoModo(dados[pos:], modo)
		if !ok {
			r = append(r, Comando{Offset: pos, Tamanho: len(dados) - pos, Tipo: Desconhecido,
				Nome: "incompleto", Descricao: "O fluxo terminou no meio de um comando", Bytes: dados[pos:]})
			break
		}
		c.Offset = pos
		if c.NovoModo != 0 {
			modo = c.NovoModo
		}
		r = append(r, c)
		pos += c.Tamanho
	}
	return r
}

// Proximo lê um comando ESC/POS do início de b.
func Proximo(b []byte) (Comando, bool) { return ProximoNoModo(b, ModoESCPOS) }

// ProximoNoModo lê um comando do início de b. Devolve ok=false quando b ainda
// não tem todos os bytes do comando (chegou só um pedaço pela rede ou serial).
func ProximoNoModo(b []byte, modo Modo) (Comando, bool) {
	if len(b) == 0 {
		return Comando{}, false
	}
	if modo == ModoBematech {
		if c, ok, tratou := lerBematech(b); tratou {
			return c, ok
		}
	}
	c := b[0]
	switch {
	case c >= 0x20 || c == 0x09:
		return lerTexto(b), true
	case c == 0x1B:
		return lerESC(b)
	case c == 0x1D:
		return lerGS(b)
	case c == 0x10:
		return lerDLE(b)
	case c == 0x1C:
		return lerFS(b)
	}
	return simples(b, 1, controles[c]), true
}

type info struct {
	tipo Tipo
	nome string
	desc string
}

var controles = map[byte]info{
	0x0A: {Avanco, "LF", "Imprime a linha e avança"},
	0x0D: {Avanco, "CR", "Retorno de carro (a maioria das impressoras ignora)"},
	0x0C: {Avanco, "FF", "Imprime e avança a página"},
	0x05: {Status, "ENQ", "Pedido de status no modo próprio da Bematech"},
	0x18: {Controle, "CAN", "Cancela os dados da linha"},
	0x00: {Controle, "NUL", "Byte nulo"},
}

func simples(b []byte, n int, i info) Comando {
	if i.nome == "" {
		i = info{Controle, fmt.Sprintf("0x%02X", b[0]), "Byte de controle sem efeito"}
	}
	return Comando{Tamanho: n, Tipo: i.tipo, Nome: i.nome, Descricao: i.desc, Bytes: b[:n]}
}

func lerTexto(b []byte) Comando {
	n := 0
	for n < len(b) && (b[n] >= 0x20 || b[n] == 0x09) {
		n++
	}
	return Comando{Tamanho: n, Tipo: Texto, Nome: "texto", Descricao: fmt.Sprintf("%d caracteres", n), Bytes: b[:n]}
}

// cmd monta um comando de tamanho fixo, ou avisa que faltam bytes.
func cmd(b []byte, n int, tipo Tipo, nome, desc string) (Comando, bool) {
	if len(b) < n {
		return Comando{}, false
	}
	return Comando{Tamanho: n, Tipo: tipo, Nome: nome, Descricao: desc, Bytes: b[:n]}, true
}

func ligado(n byte) string {
	if n&1 == 1 {
		return "liga"
	}
	return "desliga"
}

func lerESC(b []byte) (Comando, bool) {
	if len(b) < 2 {
		return Comando{}, false
	}
	p := func(i int) byte {
		if i < len(b) {
			return b[i]
		}
		return 0
	}
	n := p(2)
	switch b[1] {
	case '@':
		return cmd(b, 2, Formatacao, "ESC @", "Reinicia a impressora (volta à formatação padrão)")
	case '!':
		return cmd(b, 3, Formatacao, "ESC !", descModo(n))
	case 'E':
		return cmd(b, 3, Formatacao, "ESC E", ligado(n)+" negrito")
	case 'G':
		return cmd(b, 3, Formatacao, "ESC G", ligado(n)+" passada dupla (parece negrito)")
	case '-':
		return cmd(b, 3, Formatacao, "ESC -", fmt.Sprintf("Sublinhado %d (0 desliga)", n))
	case 'a':
		return cmd(b, 3, Formatacao, "ESC a", "Alinha "+[]string{"à esquerda", "ao centro", "à direita"}[n%48%3])
	case 'M':
		return cmd(b, 3, Formatacao, "ESC M", "Fonte "+string(rune('A'+n%48%3)))
	case ' ':
		return cmd(b, 3, Formatacao, "ESC SP", fmt.Sprintf("Espaço de %d pontos à direita de cada caractere", n))
	case '3':
		return cmd(b, 3, Formatacao, "ESC 3", fmt.Sprintf("Espaçamento entre linhas de %d pontos", n))
	case '2':
		return cmd(b, 2, Formatacao, "ESC 2", "Espaçamento entre linhas padrão")
	case 'd':
		return cmd(b, 3, Avanco, "ESC d", fmt.Sprintf("Imprime e avança %d linhas", n))
	case 'e':
		return cmd(b, 3, Avanco, "ESC e", fmt.Sprintf("Recua %d linhas", n))
	case 'J':
		return cmd(b, 3, Avanco, "ESC J", fmt.Sprintf("Imprime e avança %d pontos", n))
	case 't':
		return cmd(b, 3, Formatacao, "ESC t", "Página de código "+NomePaginaCodigo(int(n)))
	case 'R':
		return cmd(b, 3, Formatacao, "ESC R", fmt.Sprintf("Conjunto de caracteres internacional %d", n))
	case '{':
		return cmd(b, 3, Formatacao, "ESC {", ligado(n)+" impressão de cabeça para baixo")
	case 'V':
		return cmd(b, 3, Formatacao, "ESC V", ligado(n)+" rotação de 90°")
	case 'r':
		return cmd(b, 3, Formatacao, "ESC r", fmt.Sprintf("Cor %d", n))
	case 'U':
		return cmd(b, 3, Formatacao, "ESC U", ligado(n)+" impressão unidirecional")
	case 'c':
		return cmd(b, 4, Controle, "ESC c", "Configura sensores e botões do painel")
	case '=':
		return cmd(b, 3, Controle, "ESC =", "Seleciona o dispositivo")
	case 'p':
		return cmd(b, 5, Gaveta, "ESC p", fmt.Sprintf("Abre a gaveta (pino %d)", 2+n%48))
	case 'i':
		return cmd(b, 2, Corte, "ESC i", "Corte total do papel")
	case 'm':
		return cmd(b, 2, Corte, "ESC m", "Corte parcial do papel")
	case 'w':
		return cmd(b, 2, Corte, "ESC w", "Corte total do papel (Bematech)")
	case '$':
		return cmd(b, 4, Formatacao, "ESC $", "Posição horizontal absoluta")
	case '\\':
		return cmd(b, 4, Formatacao, "ESC \\", "Posição horizontal relativa")
	case 'B':
		return cmd(b, 4, Controle, "ESC B", "Bipe")
	case '%':
		return cmd(b, 3, Formatacao, "ESC %", ligado(n)+" caracteres definidos pelo usuário")
	case '?':
		return cmd(b, 3, Formatacao, "ESC ?", "Apaga caractere definido pelo usuário")
	case 'L':
		return cmd(b, 2, Controle, "ESC L", "Entra no modo página")
	case 'S':
		return cmd(b, 2, Controle, "ESC S", "Volta ao modo padrão")
	case 'T':
		return cmd(b, 3, Controle, "ESC T", "Direção de impressão no modo página")
	case 'W':
		return cmd(b, 10, Controle, "ESC W", "Área de impressão no modo página")
	case 'D':
		for i := 2; i < len(b); i++ {
			if b[i] == 0 || i > 34 {
				return cmd(b, i+1, Formatacao, "ESC D", "Define as posições de tabulação")
			}
		}
		return Comando{}, false
	case '*':
		if len(b) < 5 {
			return Comando{}, false
		}
		colunas := int(b[3]) + int(b[4])*256
		porColuna := 1
		if b[2] >= 32 {
			porColuna = 3
		}
		return cmd(b, 5+colunas*porColuna, Imagem, "ESC *",
			fmt.Sprintf("Faixa de imagem de %d pontos de altura e %d colunas", porColuna*8, colunas))
	case '&':
		// ESC & y c1 c2 [x d1...d(y*x)]...
		if len(b) < 5 {
			return Comando{}, false
		}
		y, c1, c2 := int(b[2]), int(b[3]), int(b[4])
		pos := 5
		for k := c1; k <= c2; k++ {
			if pos >= len(b) {
				return Comando{}, false
			}
			pos += 1 + y*int(b[pos])
		}
		return cmd(b, pos, Formatacao, "ESC &", "Define caracteres do usuário")
	}
	return cmd(b, 2, Desconhecido, fmt.Sprintf("ESC 0x%02X", b[1]), "Comando ESC que o emulador não conhece")
}

func descModo(n byte) string {
	s := "Modo de impressão: fonte "
	if n&1 == 1 {
		s += "B"
	} else {
		s += "A"
	}
	if n&0x08 != 0 {
		s += ", negrito"
	}
	if n&0x10 != 0 {
		s += ", altura dupla"
	}
	if n&0x20 != 0 {
		s += ", largura dupla"
	}
	if n&0x80 != 0 {
		s += ", sublinhado"
	}
	return s
}

func lerGS(b []byte) (Comando, bool) {
	if len(b) < 2 {
		return Comando{}, false
	}
	n := byte(0)
	if len(b) > 2 {
		n = b[2]
	}
	switch b[1] {
	case '!':
		return cmd(b, 3, Formatacao, "GS !", fmt.Sprintf("Tamanho do caractere: %dx largura, %dx altura", n>>4+1, n&15+1))
	case 'B':
		return cmd(b, 3, Formatacao, "GS B", ligado(n)+" impressão invertida (branco no preto)")
	case 'V':
		if len(b) < 3 {
			return Comando{}, false
		}
		if n == 0 || n == 1 || n == 48 || n == 49 {
			return cmd(b, 3, Corte, "GS V", tipoCorte(n))
		}
		if len(b) < 4 {
			return Comando{}, false
		}
		return cmd(b, 4, Corte, "GS V", fmt.Sprintf("%s depois de avançar %d pontos", tipoCorte(n), b[3]))
	case 'h':
		return cmd(b, 3, CodigoBarras, "GS h", fmt.Sprintf("Altura do código de barras: %d pontos", n))
	case 'w':
		return cmd(b, 3, CodigoBarras, "GS w", fmt.Sprintf("Largura da barra do código de barras: %d", n))
	case 'H':
		return cmd(b, 3, CodigoBarras, "GS H", "Texto do código de barras: "+[]string{"não imprime", "acima", "abaixo", "acima e abaixo"}[n%48%4])
	case 'f':
		return cmd(b, 3, CodigoBarras, "GS f", "Fonte do texto do código de barras")
	case 'L':
		return cmd(b, 4, Formatacao, "GS L", "Margem esquerda")
	case 'W':
		return cmd(b, 4, Formatacao, "GS W", "Largura da área de impressão")
	case 'P':
		return cmd(b, 4, Controle, "GS P", "Unidades de movimento")
	case 'b':
		return cmd(b, 3, Formatacao, "GS b", ligado(n)+" suavização")
	case 'r':
		return cmd(b, 3, Status, "GS r", map[byte]string{1: "Pede o status do sensor de papel", 49: "Pede o status do sensor de papel",
			2: "Pede o status da gaveta", 50: "Pede o status da gaveta"}[n])
	case 'I':
		return cmd(b, 3, Status, "GS I", "Pede a identificação da impressora")
	case 'a':
		return cmd(b, 3, Status, "GS a", "Liga ou desliga o envio automático de status")
	case '$', '\\':
		return cmd(b, 4, Formatacao, "GS "+string(b[1]), "Posição vertical no modo página")
	case '/':
		return cmd(b, 3, Imagem, "GS /", "Imprime a imagem carregada antes")
	case 0xF8:
		return cmd(b, 3, Status, "GS 0xF8", "Pede o status estendido (Bematech)")
	case 0xF9:
		return lerTrocaModo(b)
	case 'k':
		return lerCodigoBarras(b)
	case 'v':
		if len(b) < 8 {
			return Comando{}, false
		}
		x, y := int(b[4])+int(b[5])*256, int(b[6])+int(b[7])*256
		return cmd(b, 8+x*y, Imagem, "GS v 0", fmt.Sprintf("Imagem de %d x %d pontos", x*8, y))
	case '*':
		if len(b) < 4 {
			return Comando{}, false
		}
		return cmd(b, 4+int(b[2])*int(b[3])*8, Imagem, "GS *", "Carrega uma imagem na memória")
	case '(':
		if len(b) < 5 {
			return Comando{}, false
		}
		tam := 5 + int(b[3]) + int(b[4])*256
		switch b[2] {
		case 'k':
			if len(b) < 7 {
				return Comando{}, false
			}
			return cmd(b, tam, CodigoBarras, "GS ( k", descQR(b))
		case 'L':
			return cmd(b, tam, Imagem, "GS ( L", "Gráfico (não desenhado pelo emulador)")
		}
		return cmd(b, tam, Controle, "GS ( "+string(b[2]), "Configuração estendida")
	case '8':
		if len(b) < 7 {
			return Comando{}, false
		}
		tam := 7 + int(b[3]) + int(b[4])<<8 + int(b[5])<<16 + int(b[6])<<24
		return cmd(b, tam, Imagem, "GS 8 L", "Gráfico grande (não desenhado pelo emulador)")
	}
	return cmd(b, 2, Desconhecido, fmt.Sprintf("GS 0x%02X", b[1]), "Comando GS que o emulador não conhece")
}

func tipoCorte(m byte) string {
	switch m {
	case 0, 48, 65, 97, 103:
		return "Corte total"
	}
	return "Corte parcial"
}

func descQR(b []byte) string {
	if b[5] != 49 { // cn 49 = QR Code
		return "Símbolo 2D (PDF417 ou outro)"
	}
	switch b[6] {
	case 65:
		return "QR Code: modelo"
	case 67:
		return fmt.Sprintf("QR Code: tamanho do módulo %d pontos", argQR(b))
	case 69:
		return "QR Code: nível de correção " + string("LMQH"[argQR(b)%48%4])
	case 80:
		return fmt.Sprintf("QR Code: guarda %d bytes de dados", len(b)-8)
	case 81:
		return "QR Code: imprime"
	}
	return "QR Code: configuração"
}

func argQR(b []byte) int {
	if len(b) > 7 {
		return int(b[7])
	}
	return 0
}

// Nomes das simbologias do GS k. Até 6 os dados terminam em NUL; de 65 em
// diante vem o tamanho antes dos dados.
var simbologias = map[byte]string{
	0: "UPC-A", 1: "UPC-E", 2: "EAN-13", 3: "EAN-8", 4: "Code 39", 5: "ITF", 6: "Codabar",
	65: "UPC-A", 66: "UPC-E", 67: "EAN-13", 68: "EAN-8", 69: "Code 39", 70: "ITF", 71: "Codabar",
	72: "Code 93", 73: "Code 128",
}

func lerCodigoBarras(b []byte) (Comando, bool) {
	if len(b) < 3 {
		return Comando{}, false
	}
	m := b[2]
	nome := simbologias[m]
	if nome == "" {
		nome = fmt.Sprintf("tipo %d", m)
	}
	if m <= 6 {
		for i := 3; i < len(b); i++ {
			if b[i] == 0 {
				return cmd(b, i+1, CodigoBarras, "GS k", "Código de barras "+nome)
			}
		}
		return Comando{}, false
	}
	if len(b) < 4 {
		return Comando{}, false
	}
	return cmd(b, 4+int(b[3]), CodigoBarras, "GS k", "Código de barras "+nome)
}

// DadosCodigoBarras separa a simbologia e os dados de um GS k.
func DadosCodigoBarras(c Comando) (m byte, dados []byte) {
	m = c.Bytes[2]
	if m <= 6 {
		return m, c.Bytes[3 : len(c.Bytes)-1]
	}
	return m, c.Bytes[4:]
}

func lerDLE(b []byte) (Comando, bool) {
	if len(b) < 2 {
		return Comando{}, false
	}
	switch b[1] {
	case 0x04:
		if len(b) < 3 {
			return Comando{}, false
		}
		desc := map[byte]string{1: "Pede o status da impressora", 2: "Pede a causa de estar offline",
			3: "Pede a causa do erro", 4: "Pede o status do sensor de papel"}[b[2]]
		if desc == "" {
			desc = "Pede status"
		}
		return cmd(b, 3, Status, fmt.Sprintf("DLE EOT %d", b[2]), desc)
	case 0x05:
		return cmd(b, 3, Controle, "DLE ENQ", "Pedido em tempo real (sem resposta)")
	case 0x14:
		if len(b) < 3 {
			return Comando{}, false
		}
		if b[2] == 8 {
			return cmd(b, 10, Controle, "DLE DC4 8", "Limpa o buffer")
		}
		if b[2] == 1 {
			return cmd(b, 5, Gaveta, "DLE DC4 1", "Abre a gaveta em tempo real")
		}
		return cmd(b, 5, Controle, fmt.Sprintf("DLE DC4 %d", b[2]), "Comando em tempo real")
	}
	return cmd(b, 1, Controle, "DLE", "Byte DLE solto")
}

func lerFS(b []byte) (Comando, bool) {
	if len(b) < 2 {
		return Comando{}, false
	}
	switch b[1] {
	case 'p':
		return cmd(b, 4, Imagem, "FS p", "Imprime a imagem guardada na memória da impressora (logotipo)")
	case '&', '.':
		return cmd(b, 2, Formatacao, "FS "+string(b[1]), "Modo de caracteres asiáticos")
	case '!', '-', 'C', 'W':
		return cmd(b, 3, Formatacao, "FS "+string(b[1]), "Formatação de caracteres asiáticos")
	case 'S':
		return cmd(b, 4, Formatacao, "FS S", "Espaçamento de caracteres asiáticos")
	case '(':
		if len(b) < 5 {
			return Comando{}, false
		}
		return cmd(b, 5+int(b[3])+int(b[4])*256, Controle, "FS ( "+string(b[2]), "Configuração estendida")
	case 'q':
		// FS q n [xL xH yL yH d1...dk]1...[ ]n
		if len(b) < 3 {
			return Comando{}, false
		}
		pos := 3
		for i := 0; i < int(b[2]); i++ {
			if len(b) < pos+4 {
				return Comando{}, false
			}
			x, y := int(b[pos])+int(b[pos+1])*256, int(b[pos+2])+int(b[pos+3])*256
			pos += 4 + x*y*8
		}
		return cmd(b, pos, Imagem, "FS q", "Grava imagens na memória da impressora")
	}
	return cmd(b, 2, Desconhecido, fmt.Sprintf("FS 0x%02X", b[1]), "Comando FS que o emulador não conhece")
}
