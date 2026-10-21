package web

import (
	"strings"

	"golang.org/x/text/encoding/charmap"
)

// Amostras que o botão "Imprimir teste" manda, montadas byte a byte como um
// sistema de verdade faria.

func pc850(s string) string {
	b, err := charmap.CodePage850.NewEncoder().String(s)
	if err != nil {
		return s
	}
	return b
}

func linha(esq, dir string, colunas int) string {
	espacos := colunas - len([]rune(esq)) - len([]rune(dir))
	if espacos < 1 {
		espacos = 1
	}
	return esq + strings.Repeat(" ", espacos) + dir + "\n"
}

func cupomExemplo(colunas int) []byte {
	const (
		esc = "\x1b"
		gs  = "\x1d"
	)
	sep := strings.Repeat("-", colunas) + "\n"
	var b strings.Builder
	b.WriteString(esc + "@" + esc + "t\x02") // reinicia e escolhe PC850
	b.WriteString(esc + "a\x01" + gs + "!\x11" + esc + "E\x01" + pc850("PADARIA BOA MASSA") + "\n")
	b.WriteString(gs + "!\x00" + esc + "E\x00" + pc850("Rua das Flores, 120 - Centro") + "\n")
	b.WriteString("CNPJ 12.345.678/0001-90\n")
	b.WriteString(esc + "a\x00" + sep)
	b.WriteString(esc + "E\x01" + linha("ITEM", "TOTAL", colunas) + esc + "E\x00")
	itens := []struct {
		nome  string
		qtd   string
		total string
	}{
		{"Pão francês", "6 x 0,90", "5,40"},
		{"Café coado grande", "1 x 7,50", "7,50"},
		{"Pão de queijo", "4 x 3,25", "13,00"},
		{"Suco de laranja 500ml", "1 x 9,90", "9,90"},
	}
	for _, it := range itens {
		b.WriteString(pc850(it.nome) + "\n")
		b.WriteString(esc + "M\x01" + linha("  "+it.qtd, it.total, colunas*4/3) + esc + "M\x00")
	}
	b.WriteString(sep)
	b.WriteString(esc + "E\x01" + gs + "!\x01" + linha("TOTAL R$", "35,80", colunas) + gs + "!\x00" + esc + "E\x00")
	b.WriteString(linha(pc850("Cartão de débito"), "35,80", colunas))
	b.WriteString(sep)
	b.WriteString(esc + "a\x01")
	// QR Code: modelo 2, módulo 5, correção M, dados e imprimir.
	qr := "https://exemplo.com.br/nfce?chave=35241012345678000190650010000012341000012345"
	b.WriteString(gs + "(k\x04\x00\x31\x41\x32\x00")
	b.WriteString(gs + "(k\x03\x00\x31\x43\x05")
	b.WriteString(gs + "(k\x03\x00\x31\x45\x31")
	n := len(qr) + 3
	b.WriteString(gs + "(k" + string([]byte{byte(n % 256), byte(n / 256)}) + "\x31\x50\x30" + qr)
	b.WriteString(gs + "(k\x03\x00\x31\x51\x30\n")
	b.WriteString(pc850("Consulte pela chave de acesso") + "\n\n")
	// Code 128 no conjunto B, altura 60, barra 2, texto embaixo.
	dados := "{BPED-000345"
	b.WriteString(gs + "h\x3c" + gs + "w\x02" + gs + "H\x02" + gs + "k\x49" + string([]byte{byte(len(dados))}) + dados + "\n")
	b.WriteString(esc + "E\x01" + pc850("Obrigado pela preferência!") + esc + "E\x00\n")
	b.WriteString(esc + "a\x00")
	b.WriteString(esc + "d\x03" + gs + "V\x42\x28") // avança e corta
	return []byte(b.String())
}

// cupomComProblemas tem erros comuns de integração para o inspetor apontar.
func cupomComProblemas(colunas int) []byte {
	var b strings.Builder
	b.WriteString("\x1b@")
	b.WriteString("\x1ba\x01\x1bE\x01Pedido para viagem\n\x1bE\x00\x1ba\x00")
	b.WriteString("Observação: sem cebola, maionese à parte\n") // UTF-8 numa impressora PC850
	b.WriteString(strings.Repeat("=", colunas+6) + "\n")        // linha maior que a bobina
	b.WriteString("1 X-Salada especial da casa com batata    R$ 32,90\n")
	b.WriteString("\x1dh\x50\x1dw\x02\x1dH\x02")
	b.WriteString("\x1dk\x49\x0c{C7891234567") // {C com dígitos em texto
	b.WriteString("\n\n\n\x1dV\x00")
	return []byte(b.String())
}

const etiquetaExemplo = `^XA
^CI28
^PW480
^LL320
^FO0,16^FB480,1,0,C^A0N,26,26^FDATENDIMENTO RÁPIDO^FS
^FO0,46^FB480,1,0,C^A0N,22,22^FDCOMANDA^FS
^FO0,74^FB480,1,0,C^A0N,64,64^FD0042^FS
^FO60,146^BY2^BCN,60,Y,N,N^FD100042^FS
^FO20,236^GB440,2,2^FS
^FO24,250^A0N,22,22^FDAtendente: Joana^FS
^FO0,250^FB456,1,0,R^A0N,22,22^FDRodada 3^FS
^FO24,280^A0N,22,22^FD5 itens^FS
^FO0,280^FB456,1,0,R^A0N,26,26^FDR$ 87,40^FS
^FO380,140^BQN,2,3^FDQA,https://exemplo.com.br/c/42^FS
^PQ1
^XZ`

const etiquetaComProblemas = `^XA
^PW480
^LL320
^FO20,20^A0N,40,40^FDCartão nº 0042^FS
^FO20,80^FB300,1,0,L^A0N,36,36^FDTexto comprido demais para o bloco^FS
^FO300,140^BY3^BCN,80,Y,N,N^FD7891234567890^FS
^FO20,240^A0N,30,30^FDEtiqueta sem fim`

// Amostras disponíveis para cada linguagem.
func amostras(linguagem string, colunas int) map[string][]byte {
	if linguagem == "zpl" {
		return map[string][]byte{
			"exemplo":   []byte(etiquetaExemplo),
			"problemas": []byte(etiquetaComProblemas),
		}
	}
	return map[string][]byte{
		"exemplo":   cupomExemplo(colunas),
		"problemas": cupomComProblemas(colunas),
	}
}
