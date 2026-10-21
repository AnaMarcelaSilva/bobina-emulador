package escpos

import (
	"fmt"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
)

// Páginas de código que o ESC t seleciona. A impressora não sabe nada de
// UTF-8: cada byte acima de 127 vira o caractere daquela posição na tabela.
var paginas = map[int]struct {
	nome   string
	tabela *charmap.Charmap
}{
	0:  {"PC437 (EUA)", charmap.CodePage437},
	2:  {"PC850 (Multilíngue)", charmap.CodePage850},
	3:  {"PC860 (Português)", charmap.CodePage860},
	4:  {"PC863 (Francês canadense)", charmap.CodePage863},
	5:  {"PC865 (Nórdico)", charmap.CodePage865},
	16: {"WPC1252 (Windows Latin 1)", charmap.Windows1252},
	17: {"PC866 (Cirílico)", charmap.CodePage866},
	19: {"PC858 (Euro)", charmap.CodePage858},
}

// NomePaginaCodigo descreve o número usado no ESC t.
func NomePaginaCodigo(n int) string {
	if p, ok := paginas[n]; ok {
		return fmt.Sprintf("%d: %s", n, p.nome)
	}
	return fmt.Sprintf("%d (desconhecida, o emulador usa PC850)", n)
}

// PaginasDisponiveis lista as páginas para a tela de configuração.
func PaginasDisponiveis() map[int]string {
	r := map[int]string{}
	for n, p := range paginas {
		r[n] = p.nome
	}
	return r
}

// Decodificar transforma os bytes de texto no que a impressora imprimiria.
func Decodificar(b []byte, pagina int) string {
	p, ok := paginas[pagina]
	if !ok {
		p = paginas[2]
	}
	r := make([]rune, len(b))
	for i, c := range b {
		if c < 0x80 {
			r[i] = rune(c)
		} else {
			r[i] = p.tabela.DecodeByte(c)
		}
	}
	return string(r)
}

// PareceUTF8 indica texto com acentos enviado em UTF-8. Numa impressora de
// cupom isso sai como "Ã§" no lugar de "ç".
func PareceUTF8(b []byte) bool {
	multibyte := false
	for _, c := range b {
		if c >= 0x80 {
			multibyte = true
			break
		}
	}
	return multibyte && utf8.Valid(b)
}

// ExemploAcento pega o primeiro caractere acentuado do texto em UTF-8 e
// mostra como ele sai na impressora: "ã" vira "├ú" em PC850, por exemplo.
func ExemploAcento(b []byte, pagina int) string {
	for i := 0; i < len(b); {
		r, n := utf8.DecodeRune(b[i:])
		if n > 1 {
			return fmt.Sprintf("\"%c\" sai como \"%s\"", r, Decodificar(b[i:i+n], pagina))
		}
		i += n
	}
	return ""
}
