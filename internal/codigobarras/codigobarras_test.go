package codigobarras

import (
	"image"
	"image/color"
	"testing"

	"github.com/makiuchi-d/gozxing"
	"github.com/makiuchi-d/gozxing/oned"
	qrleitor "github.com/makiuchi-d/gozxing/qrcode"
)

// ler desenha o símbolo numa imagem e pede para um leitor de verdade decodificar.
func ler(t *testing.T, s Simbolo, leitor gozxing.Reader) string {
	t.Helper()
	const escala, margem, altura = 3, 15, 80
	img := image.NewGray(image.Rect(0, 0, (len(s.Modulos)+2*margem)*escala, altura))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	for i, preto := range s.Modulos {
		if !preto {
			continue
		}
		for x := 0; x < escala; x++ {
			for y := 0; y < altura; y++ {
				img.SetGray((margem+i)*escala+x, y, color.Gray{})
			}
		}
	}
	bmp, _ := gozxing.NewBinaryBitmapFromImage(img)
	r, err := leitor.Decode(bmp, nil)
	if err != nil {
		t.Fatalf("o leitor não conseguiu ler o código: %v", err)
	}
	return r.GetText()
}

func TestCode128(t *testing.T) {
	for _, dados := range []string{"PED-123", "7891234567890", "12345678", "AB12345678CD", "a", "00"} {
		s, err := Code128Auto(dados)
		if err != nil {
			t.Fatal(err)
		}
		if got := ler(t, s, oned.NewCode128Reader()); got != dados {
			t.Errorf("Code128Auto(%q) foi lido como %q", dados, got)
		}
		s, _ = Code128Conjunto(dados, 'B')
		if got := ler(t, s, oned.NewCode128Reader()); got != dados {
			t.Errorf("Code128Conjunto(%q, B) foi lido como %q", dados, got)
		}
	}
}

func TestEAN(t *testing.T) {
	s, err := EAN13("789123456789")
	if err != nil {
		t.Fatal(err)
	}
	if got := ler(t, s, oned.NewEAN13Reader()); got != "7891234567895" {
		t.Errorf("EAN-13 lido como %q", got)
	}
	if _, err := EAN13("7891234567890"); err == nil {
		t.Error("EAN-13 com dígito verificador errado deveria dar erro")
	}
	s, _ = EAN8("1234567")
	if got := ler(t, s, oned.NewEAN8Reader()); got != "12345670" {
		t.Errorf("EAN-8 lido como %q", got)
	}
	s, _ = UPCA("03600029145")
	if got := ler(t, s, oned.NewUPCAReader()); got != "036000291452" {
		t.Errorf("UPC-A lido como %q", got)
	}
}

func TestCode39EITF(t *testing.T) {
	s, err := Code39("COMANDA-42", 3)
	if err != nil {
		t.Fatal(err)
	}
	if got := ler(t, s, oned.NewCode39Reader()); got != "COMANDA-42" {
		t.Errorf("Code 39 lido como %q", got)
	}
	s, _ = ITF("12345678", 3)
	if got := ler(t, s, oned.NewITFReader()); got != "12345678" {
		t.Errorf("ITF lido como %q", got)
	}
}

func TestQR(t *testing.T) {
	m, err := QR([]byte("https://exemplo.com.br/mesa/12"), 'M')
	if err != nil {
		t.Fatal(err)
	}
	const escala, margem = 4, 4
	lado := (len(m) + 2*margem) * escala
	img := image.NewGray(image.Rect(0, 0, lado, lado))
	for i := range img.Pix {
		img.Pix[i] = 255
	}
	for y, linha := range m {
		for x, preto := range linha {
			if preto {
				for dx := 0; dx < escala; dx++ {
					for dy := 0; dy < escala; dy++ {
						img.SetGray((margem+x)*escala+dx, (margem+y)*escala+dy, color.Gray{})
					}
				}
			}
		}
	}
	bmp, _ := gozxing.NewBinaryBitmapFromImage(img)
	r, err := qrleitor.NewQRCodeReader().Decode(bmp, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r.GetText() != "https://exemplo.com.br/mesa/12" {
		t.Errorf("QR lido como %q", r.GetText())
	}
}
