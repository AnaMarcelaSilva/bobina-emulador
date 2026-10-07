package escpos

import (
	"os"
	"strings"
	"testing"
)

// Um cupom no formato do driver PRN: reinicia, centraliza em negrito, fonte B
// em largura 1,5x, código de barras e corte com avanço.
func cupomPRN() []byte {
	var b []byte
	b = append(b, "\x1B\x40"...)
	b = append(b, "\x1b\x61\x01\x1b\x45\x01MESA 12\n\x1b\x45\x00\x1b\x61\x00"...)
	b = append(b, "\x1B\x20\x01\x1B\x4D\x01\x1D\x21\x10PEDIDO 345\n"...)
	b = append(b, "\x1B\x40\x1B\x4D\x00"...)
	b = append(b, "1 X-BURGUER                       R$ 25,00\n"...)
	b = append(b, "\x1D\x68\x64\x1D\x77\x02\x1D\x48\x02"...)
	b = append(b, 0x1D, 0x6B, 73, 9, '{', 'B', 'P', 'E', 'D', '-', '3', '4', '5')
	b = append(b, '\n')
	b = append(b, "\x1D\x56\x42\x28"...)
	return b
}

func TestLerSeparaOsComandos(t *testing.T) {
	cmds := Ler(cupomPRN())
	var nomes []string
	for _, c := range cmds {
		nomes = append(nomes, c.Nome)
	}
	esperado := "ESC @,ESC a,ESC E,texto,LF,ESC E,ESC a,ESC SP,ESC M,GS !,texto,LF,ESC @,ESC M,texto,LF,GS h,GS w,GS H,GS k,LF,GS V"
	if got := strings.Join(nomes, ","); got != esperado {
		t.Fatalf("comandos lidos:\n%s\nesperado:\n%s", got, esperado)
	}
}

func TestProximoEsperaOComandoCompleto(t *testing.T) {
	if _, ok := Proximo([]byte{0x10, 0x04}); ok {
		t.Error("DLE EOT sem o número não pode ser considerado completo")
	}
	// Imagem de 1 x 2 bytes com os bytes do DLE EOT nos dados: não é status.
	img := []byte{0x1D, 'v', '0', 0, 1, 0, 2, 0, 0x10, 0x04}
	c, ok := Proximo(img)
	if !ok || c.Nome != "GS v 0" || c.Tamanho != len(img) {
		t.Fatalf("imagem lida errado: %+v ok=%v", c, ok)
	}
}

func TestDesenharCupom(t *testing.T) {
	cupom := Desenhar(Ler(cupomPRN()), Papel{Colunas: 42, Pontos: 576, PaginaCodigo: 2})
	for _, trecho := range []string{"MESA 12", "PEDIDO 345", "X-BURGUER", "[Code 128: PED-345]"} {
		if !strings.Contains(cupom.Texto, trecho) {
			t.Errorf("o texto do cupom deveria ter %q:\n%s", trecho, cupom.Texto)
		}
	}
	if cupom.Cortes != 1 {
		t.Errorf("esperava 1 corte, veio %d", cupom.Cortes)
	}
	if len(cupom.Avisos) != 0 {
		t.Errorf("não esperava avisos: %v", cupom.Avisos)
	}
}

func TestAvisos(t *testing.T) {
	// UTF-8 numa impressora PC850.
	c := Desenhar(Ler([]byte("Pão de queijo\n")), Papel{Colunas: 48, PaginaCodigo: 2})
	if len(c.Avisos) == 0 || !strings.Contains(c.Avisos[0], "UTF-8") {
		t.Errorf("deveria avisar do UTF-8: %v", c.Avisos)
	}
	if c.Texto != "P├úo de queijo" || !strings.Contains(c.Avisos[0], `"ã" sai como "├ú"`) {
		t.Errorf("o texto deveria sair como a impressora imprimiria: %q", c.Texto)
	}
	// Em PC850 o ç é 0x87.
	c = Desenhar(Ler([]byte("Cora\x87\xc6o\n")), Papel{Colunas: 48, PaginaCodigo: 2})
	if c.Texto != "Coração" || len(c.Avisos) != 0 {
		t.Errorf("PC850 decodificado errado: %q %v", c.Texto, c.Avisos)
	}
	// Linha maior que a bobina.
	c = Desenhar(Ler([]byte(strings.Repeat("x", 50)+"\n")), Papel{Colunas: 48})
	if len(c.Avisos) == 0 || !strings.Contains(c.Avisos[0], "50 colunas") {
		t.Errorf("deveria avisar da linha longa: %v", c.Avisos)
	}
	// Code 128 {C com dígitos em texto.
	c = Desenhar(Ler([]byte{0x1D, 0x6B, 73, 6, '{', 'C', '1', '2', '3', '4'}), Papel{})
	if len(c.Avisos) == 0 || !strings.Contains(c.Avisos[0], "{C") {
		t.Errorf("deveria avisar do {C com dígitos em texto: %v", c.Avisos)
	}
}

// Pedido de cozinha real, enviado pelo driver Bematech do serviço de
// impressão: começa com GS F9 20 00 (grava o modo ESC/Bematech) e usa
// ESC E/ESC F sem parâmetro, condensado (ESC SI) e largura dupla.
func TestPedidoBematech(t *testing.T) {
	dados, err := os.ReadFile("testdata/pedido_cozinha_bematech.prn")
	if err != nil {
		t.Fatal(err)
	}
	cmds := LerNoModo(dados, ModoESCPOS)
	if cmds[0].NovoModo != ModoBematech || !cmds[0].Grava {
		t.Fatalf("o GS F9 20 00 deveria trocar para ESC/Bematech gravando: %+v", cmds[0])
	}
	for _, c := range cmds {
		if c.Tipo == Desconhecido {
			t.Errorf("comando desconhecido no offset %d: %s (%s)", c.Offset, c.Nome, c.Descricao)
		}
	}
	// 42 colunas é o padrão do driver Bematech; condensado com largura dupla
	// dá 42 × 1,5 ÷ 2 = 31 colunas, e as linhas do pedido têm 28.
	cupom := Desenhar(cmds, Papel{Colunas: 42, Pontos: 576, PaginaCodigo: 3})
	for _, trecho := range []string{"PEDIDO", "Comanda: 116", "ESPETO DE MORANGO C/CHO 1 un", "SEQUENCIA: 5"} {
		if !strings.Contains(cupom.Texto, trecho) {
			t.Errorf("faltou %q no cupom:\n%s", trecho, cupom.Texto)
		}
	}
	for _, a := range cupom.Avisos {
		t.Errorf("não esperava aviso: %s", a)
	}
	// Lido como ESC/POS, o mesmo pedido perde letras: o ESC E come o "C".
	comoESCPOS := Desenhar(Ler(dados[4:]), Papel{Colunas: 42})
	if strings.Contains(comoESCPOS.Texto, "Comanda: 116") {
		t.Error("sem o modo Bematech o ESC E deveria engolir a primeira letra (prova de que o teste mede algo)")
	}
}
