// Package codigobarras transforma o conteúdo de um código de barras na
// sequência de módulos (barra ou espaço) que a impressora desenharia.
//
// Cada função devolve um []bool em que true é barra e false é espaço, com um
// módulo por posição. Quem desenha decide a largura de cada módulo.
package codigobarras

import (
	"fmt"
	"strings"
)

// Simbolo é o resultado da codificação: os módulos e o texto legível
// (o que aparece embaixo das barras).
type Simbolo struct {
	Modulos []bool
	Texto   string
}

// ---------------------------------------------------------------- Code 128

// Larguras de barra e espaço de cada valor do Code 128 (a parada tem 7).
var padroes128 = [...]string{
	"212222", "222122", "222221", "121223", "121322", "131222", "122213", "122312", "132212", "221213",
	"221312", "231212", "112232", "122132", "122231", "113222", "123122", "123221", "223211", "221132",
	"221231", "213212", "223112", "312131", "311222", "321122", "321221", "312212", "322112", "322211",
	"212123", "212321", "232121", "111323", "131123", "131321", "112313", "132113", "132311", "211313",
	"231113", "231311", "112133", "112331", "132131", "113123", "113321", "133121", "313121", "211331",
	"231131", "213113", "213311", "213131", "311123", "311321", "331121", "312113", "312311", "332111",
	"314111", "221411", "431111", "111224", "111422", "121124", "121421", "141122", "141221", "112214",
	"112412", "122114", "122411", "142112", "142211", "241211", "221114", "413111", "241112", "134111",
	"111242", "121142", "121241", "114212", "124112", "124211", "411212", "421112", "421211", "212141",
	"214121", "412121", "111143", "111341", "131141", "114113", "114311", "411113", "411311", "113141",
	"114131", "311141", "411131", "211412", "211214", "211232", "2331112",
}

const (
	inicioA128 = 103
	inicioB128 = 104
	inicioC128 = 105
	parada128  = 106
	codigoA    = 101 // troca para o conjunto A (estando em B ou C)
	codigoB    = 100 // troca para o conjunto B (estando em A ou C)
	codigoC    = 99  // troca para o conjunto C (estando em A ou B)
	fnc1       = 102
)

// Valores128 monta o símbolo a partir de valores já calculados (início,
// dados e trocas de conjunto), acrescentando o dígito verificador e a parada.
func Valores128(valores []int, texto string) Simbolo {
	soma := valores[0]
	for i, v := range valores[1:] {
		soma += v * (i + 1)
	}
	completo := append(append([]int{}, valores...), soma%103, parada128)
	var modulos []bool
	for _, v := range completo {
		modulos = append(modulos, larguras(padroes128[v])...)
	}
	return Simbolo{Modulos: modulos, Texto: texto}
}

// Code128Auto escolhe sozinho o conjunto mais curto: C para trechos de 4 ou
// mais dígitos, B para o resto. É o que fazem as bibliotecas e o modo
// automático do ZPL.
func Code128Auto(dados string) (Simbolo, error) {
	for _, c := range dados {
		if c > 127 {
			return Simbolo{}, fmt.Errorf("o Code 128 só aceita ASCII; %q não pode ser codificado", c)
		}
	}
	var valores []int
	conjunto := 0
	i := 0
	for i < len(dados) {
		digitos := contarDigitos(dados[i:])
		usarC := digitos >= 4 || (digitos >= 2 && digitos == len(dados)-i && digitos%2 == 0)
		if usarC {
			if conjunto != 'C' {
				valores = append(valores, trocaPara('C', conjunto))
				conjunto = 'C'
			}
			for digitos >= 2 {
				valores = append(valores, int(dados[i]-'0')*10+int(dados[i+1]-'0'))
				i += 2
				digitos -= 2
			}
			continue
		}
		c := dados[i]
		if c < 32 {
			if conjunto != 'A' {
				valores = append(valores, trocaPara('A', conjunto))
				conjunto = 'A'
			}
			valores = append(valores, int(c)+64)
		} else {
			if conjunto != 'B' {
				valores = append(valores, trocaPara('B', conjunto))
				conjunto = 'B'
			}
			valores = append(valores, int(c)-32)
		}
		i++
	}
	if len(valores) == 0 {
		return Simbolo{}, fmt.Errorf("código de barras sem dados")
	}
	return Valores128(valores, dados), nil
}

// Code128Conjunto codifica tudo num conjunto fixo, como o ZPL faz quando não
// há código de troca (^BC começa no conjunto B).
func Code128Conjunto(dados string, conjunto byte) (Simbolo, error) {
	valores := []int{trocaPara(conjunto, 0)}
	switch conjunto {
	case 'C':
		if len(dados)%2 != 0 || contarDigitos(dados) != len(dados) {
			return Code128Auto(dados)
		}
		for i := 0; i < len(dados); i += 2 {
			valores = append(valores, int(dados[i]-'0')*10+int(dados[i+1]-'0'))
		}
	default:
		for _, c := range []byte(dados) {
			switch {
			case c >= 32 && c < 128:
				valores = append(valores, int(c)-32)
			default:
				return Code128Auto(dados)
			}
		}
	}
	return Valores128(valores, dados), nil
}

func trocaPara(destino byte, atual int) int {
	if atual == 0 {
		switch destino {
		case 'A':
			return inicioA128
		case 'C':
			return inicioC128
		}
		return inicioB128
	}
	switch destino {
	case 'A':
		return codigoA
	case 'C':
		return codigoC
	}
	return codigoB
}

func contarDigitos(s string) int {
	n := 0
	for n < len(s) && s[n] >= '0' && s[n] <= '9' {
		n++
	}
	return n
}

// ---------------------------------------------------------------- EAN / UPC

var (
	eanL = [10]string{"0001101", "0011001", "0010011", "0111101", "0100011", "0110001", "0101111", "0111011", "0110111", "0001011"}
	eanG = [10]string{"0100111", "0110011", "0011011", "0100001", "0011101", "0111001", "0000101", "0010001", "0001001", "0010111"}
	eanR = [10]string{"1110010", "1100110", "1101100", "1000010", "1011100", "1001110", "1010000", "1000100", "1001000", "1110100"}
	// Paridade dos 6 primeiros dígitos, escolhida pelo primeiro dígito do EAN-13.
	eanParidade = [10]string{"LLLLLL", "LLGLGG", "LLGGLG", "LLGGGL", "LGLLGG", "LGGLLG", "LGGGLL", "LGLGLG", "LGLGGL", "LGGLGL"}
)

// DigitoEAN calcula o dígito verificador de EAN-8, EAN-13 e UPC-A.
func DigitoEAN(base string) int {
	soma := 0
	for i := len(base) - 1; i >= 0; i-- {
		d := int(base[i] - '0')
		if (len(base)-1-i)%2 == 0 {
			d *= 3
		}
		soma += d
	}
	return (10 - soma%10) % 10
}

// EAN13 aceita 12 dígitos (calcula o verificador) ou 13 (confere o verificador).
func EAN13(dados string) (Simbolo, error) {
	if err := soDigitos(dados, 12, 13); err != nil {
		return Simbolo{}, fmt.Errorf("EAN-13: %w", err)
	}
	dv := DigitoEAN(dados[:12])
	if len(dados) == 13 && int(dados[12]-'0') != dv {
		return Simbolo{}, fmt.Errorf("EAN-13: dígito verificador %c não confere (o certo é %d)", dados[12], dv)
	}
	dados = dados[:12] + fmt.Sprint(dv)

	var b strings.Builder
	b.WriteString("101")
	paridade := eanParidade[dados[0]-'0']
	for i := 1; i <= 6; i++ {
		d := dados[i] - '0'
		if paridade[i-1] == 'L' {
			b.WriteString(eanL[d])
		} else {
			b.WriteString(eanG[d])
		}
	}
	b.WriteString("01010")
	for i := 7; i <= 12; i++ {
		b.WriteString(eanR[dados[i]-'0'])
	}
	b.WriteString("101")
	return Simbolo{Modulos: bits(b.String()), Texto: dados}, nil
}

// EAN8 aceita 7 dígitos (calcula o verificador) ou 8.
func EAN8(dados string) (Simbolo, error) {
	if err := soDigitos(dados, 7, 8); err != nil {
		return Simbolo{}, fmt.Errorf("EAN-8: %w", err)
	}
	dv := DigitoEAN(dados[:7])
	if len(dados) == 8 && int(dados[7]-'0') != dv {
		return Simbolo{}, fmt.Errorf("EAN-8: dígito verificador %c não confere (o certo é %d)", dados[7], dv)
	}
	dados = dados[:7] + fmt.Sprint(dv)
	var b strings.Builder
	b.WriteString("101")
	for i := 0; i < 4; i++ {
		b.WriteString(eanL[dados[i]-'0'])
	}
	b.WriteString("01010")
	for i := 4; i < 8; i++ {
		b.WriteString(eanR[dados[i]-'0'])
	}
	b.WriteString("101")
	return Simbolo{Modulos: bits(b.String()), Texto: dados}, nil
}

// UPCA é um EAN-13 que começa com zero.
func UPCA(dados string) (Simbolo, error) {
	if err := soDigitos(dados, 11, 12); err != nil {
		return Simbolo{}, fmt.Errorf("UPC-A: %w", err)
	}
	s, err := EAN13("0" + dados)
	if err != nil {
		return Simbolo{}, fmt.Errorf("UPC-A: %w", err)
	}
	s.Texto = s.Texto[1:]
	return s, nil
}

// ---------------------------------------------------------------- Code 39

// Cada caractere tem 9 elementos alternando barra e espaço; w é largo.
var padroes39 = map[byte]string{
	'0': "nnnwwnwnn", '1': "wnnwnnnnw", '2': "nnwwnnnnw", '3': "wnwwnnnnn", '4': "nnnwwnnnw",
	'5': "wnnwwnnnn", '6': "nnwwwnnnn", '7': "nnnwnnwnw", '8': "wnnwnnwnn", '9': "nnwwnnwnn",
	'A': "wnnnnwnnw", 'B': "nnwnnwnnw", 'C': "wnwnnwnnn", 'D': "nnnnwwnnw", 'E': "wnnnwwnnn",
	'F': "nnwnwwnnn", 'G': "nnnnnwwnw", 'H': "wnnnnwwnn", 'I': "nnwnnwwnn", 'J': "nnnnwwwnn",
	'K': "wnnnnnnww", 'L': "nnwnnnnww", 'M': "wnwnnnnwn", 'N': "nnnnwnnww", 'O': "wnnnwnnwn",
	'P': "nnwnwnnwn", 'Q': "nnnnnnwww", 'R': "wnnnnnwwn", 'S': "nnwnnnwwn", 'T': "nnnnwnwwn",
	'U': "wwnnnnnnw", 'V': "nwwnnnnnw", 'W': "wwwnnnnnn", 'X': "nwnnwnnnw", 'Y': "wwnnwnnnn",
	'Z': "nwwnwnnnn", '-': "nwnnnnwnw", '.': "wwnnnnwnn", ' ': "nwwnnnwnn", '$': "nwnwnwnnn",
	'/': "nwnwnnnwn", '+': "nwnnnwnwn", '%': "nnnwnwnwn", '*': "nwnnwnwnn",
}

// Code39 codifica com asteriscos de início e fim; razao é a largura do
// elemento largo em módulos (2 a 3).
func Code39(dados string, razao int) (Simbolo, error) {
	if razao < 2 {
		razao = 3
	}
	dados = strings.ToUpper(strings.Trim(dados, "*"))
	var modulos []bool
	for i, c := range []byte("*" + dados + "*") {
		p, ok := padroes39[c]
		if !ok {
			return Simbolo{}, fmt.Errorf("Code 39: o caractere %q não existe nessa simbologia", c)
		}
		if i > 0 {
			modulos = append(modulos, false) // espaço entre caracteres
		}
		for j := 0; j < 9; j++ {
			n := 1
			if p[j] == 'w' {
				n = razao
			}
			for k := 0; k < n; k++ {
				modulos = append(modulos, j%2 == 0)
			}
		}
	}
	return Simbolo{Modulos: modulos, Texto: dados}, nil
}

// ---------------------------------------------------------------- ITF (2 de 5 intercalado)

var padroesITF = [10]string{"nnwwn", "wnnnw", "nwnnw", "wwnnn", "nnwnw", "wnwnn", "nwwnn", "nnnww", "wnnwn", "nwnwn"}

// ITF codifica pares de dígitos; com quantidade ímpar, põe um zero na frente.
func ITF(dados string, razao int) (Simbolo, error) {
	if razao < 2 {
		razao = 3
	}
	if err := soDigitos(dados, 1, 1000); err != nil {
		return Simbolo{}, fmt.Errorf("ITF: %w", err)
	}
	if len(dados)%2 != 0 {
		dados = "0" + dados
	}
	var modulos []bool
	add := func(barra bool, n int) {
		for k := 0; k < n; k++ {
			modulos = append(modulos, barra)
		}
	}
	add(true, 1)
	add(false, 1)
	add(true, 1)
	add(false, 1)
	for i := 0; i < len(dados); i += 2 {
		pb, ps := padroesITF[dados[i]-'0'], padroesITF[dados[i+1]-'0']
		for j := 0; j < 5; j++ {
			add(true, larguraITF(pb[j], razao))
			add(false, larguraITF(ps[j], razao))
		}
	}
	add(true, razao)
	add(false, 1)
	add(true, 1)
	return Simbolo{Modulos: modulos, Texto: dados}, nil
}

func larguraITF(c byte, razao int) int {
	if c == 'w' {
		return razao
	}
	return 1
}

// ---------------------------------------------------------------- auxiliares

func larguras(p string) []bool {
	var m []bool
	for i, c := range p {
		for k := 0; k < int(c-'0'); k++ {
			m = append(m, i%2 == 0)
		}
	}
	return m
}

func bits(s string) []bool {
	m := make([]bool, len(s))
	for i := range s {
		m[i] = s[i] == '1'
	}
	return m
}

func soDigitos(s string, min, max int) error {
	if len(s) < min || len(s) > max {
		if min == max {
			return fmt.Errorf("precisa de %d dígitos, veio %d", min, len(s))
		}
		return fmt.Errorf("precisa de %d a %d dígitos, veio %d", min, max, len(s))
	}
	if contarDigitos(s) != len(s) {
		return fmt.Errorf("só aceita dígitos, veio %q", s)
	}
	return nil
}
