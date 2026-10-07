package zpl

import (
	"strings"
	"testing"
)

// Etiqueta no formato do template de comanda (60 x 40 mm, 203 dpi).
const etiquetaComanda = "^XA^CI28^PW480^LL320\n" +
	"^FO0,12^FB480,1,0,C^A0N,30,30^FDCOMANDA 0042^FS\n" +
	"^FO40,60^BY2^BCN,80,Y,N,N^FD10042^FS\n" +
	"^FO20,228^GB440,2,2^FS\n" +
	"^FO20,240^A0N,22,22^FDAtendente: João^FS\n" +
	"^FO0,240^FB460,1,0,R^A0N,22,22^FDR$ 35,00^FS\n" +
	"^FO0,278^FB480,1,0,C^A0N,20,20^FDApresente no caixa^FS\n" +
	"^PQ1^XZ"

func TestEtiquetaComanda(t *testing.T) {
	e := Desenhar([]byte(etiquetaComanda), Midia{Dpmm: 8, LarguraMM: 60, AlturaMM: 40})
	if len(e.Avisos) != 0 {
		t.Errorf("não esperava avisos: %v", e.Avisos)
	}
	for _, trecho := range []string{"COMANDA 0042", "[Code 128: 10042]", "Atendente: João", "R$ 35,00", "Apresente no caixa"} {
		if !strings.Contains(e.Texto, trecho) {
			t.Errorf("faltou %q no texto:\n%s", trecho, e.Texto)
		}
	}
	if e.Largura != 480 || e.Altura != 320 {
		t.Errorf("tamanho %dx%d, esperado 480x320", e.Largura, e.Altura)
	}
	if !strings.Contains(e.SVG, `text-anchor="middle"`) || !strings.Contains(e.SVG, `text-anchor="end"`) {
		t.Error("o ^FB centralizado e à direita deveria virar text-anchor middle/end")
	}
}

func TestAvisosZPL(t *testing.T) {
	e := Desenhar([]byte("^XA^FO10,10^A0N,30,30^FDPão^FS"), Midia{})
	var juntos = strings.Join(e.Avisos, " | ")
	if !strings.Contains(juntos, "^XZ") || !strings.Contains(juntos, "^CI28") {
		t.Errorf("deveria avisar de ^XZ e ^CI28: %s", juntos)
	}
	e = Desenhar([]byte("^XA^PW400^FO300,10^BY3^BCN,50,Y^FD123456789012^FS^XZ"), Midia{})
	if len(e.Avisos) == 0 || !strings.Contains(e.Avisos[0], "passa da borda") {
		t.Errorf("deveria avisar que o código passa da borda: %v", e.Avisos)
	}
	e = Desenhar([]byte("^XA^FO0,0^FB100,1,0,L^A0N,30,30^FDTexto comprido demais para caber^FS^XZ"), Midia{})
	if len(e.Avisos) == 0 || !strings.Contains(e.Avisos[0], "^FB permite 1") {
		t.Errorf("deveria avisar do ^FB: %v", e.Avisos)
	}
}

func TestDescomprimirHex(t *testing.T) {
	// "I0" = 3x "0"; "8," é 8 e completa a linha com 0; ":" repete a linha anterior; "!" completa com F.
	b, err := descomprimirHex("FI08,:!", 2)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.ToUpper(string(hexStr(b))); got != "F00080008000FFFF" {
		t.Errorf("descompressão errada: %s", got)
	}
}

func hexStr(b []byte) []byte {
	const h = "0123456789ABCDEF"
	var r []byte
	for _, c := range b {
		r = append(r, h[c>>4], h[c&15])
	}
	return r
}
