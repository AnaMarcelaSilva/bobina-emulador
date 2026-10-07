package codigobarras

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"strings"

	qrcode "github.com/skip2/go-qrcode"
)

// Barra é um trecho contínuo de módulos pretos: começa em Inicio e ocupa
// Largura módulos.
type Barra struct{ Inicio, Largura int }

// Barras junta módulos pretos vizinhos, para desenhar um retângulo por barra.
func (s Simbolo) Barras() []Barra {
	var r []Barra
	for i := 0; i < len(s.Modulos); i++ {
		if !s.Modulos[i] {
			continue
		}
		j := i
		for j < len(s.Modulos) && s.Modulos[j] {
			j++
		}
		r = append(r, Barra{i, j - i})
		i = j
	}
	return r
}

// SVGBarras desenha as barras como retângulos SVG a partir de (x, y), com
// cada módulo medindo `modulo` unidades.
func SVGBarras(s Simbolo, x, y, modulo, altura float64) string {
	var b strings.Builder
	for _, br := range s.Barras() {
		fmt.Fprintf(&b, `<rect x="%g" y="%g" width="%g" height="%g"/>`,
			x+float64(br.Inicio)*modulo, y, float64(br.Largura)*modulo, altura)
	}
	return b.String()
}

// QR gera a matriz de módulos do QR Code, sem a margem branca.
// nivel é L, M, Q ou H (correção de erro).
func QR(dados []byte, nivel byte) ([][]bool, error) {
	nv := map[byte]qrcode.RecoveryLevel{'L': qrcode.Low, 'M': qrcode.Medium, 'Q': qrcode.High, 'H': qrcode.Highest}[nivel]
	if nivel == 0 {
		nv = qrcode.Medium
	}
	q, err := qrcode.New(string(dados), nv)
	if err != nil {
		return nil, fmt.Errorf("QR Code: %w", err)
	}
	q.DisableBorder = true
	return q.Bitmap(), nil
}

// SVGMatriz desenha uma matriz de pontos (QR Code, imagem) como retângulos,
// juntando os pontos pretos vizinhos de cada linha.
func SVGMatriz(m [][]bool, x, y, ponto float64) string {
	var b strings.Builder
	for l, linha := range m {
		for c := 0; c < len(linha); c++ {
			if !linha[c] {
				continue
			}
			f := c
			for f < len(linha) && linha[f] {
				f++
			}
			fmt.Fprintf(&b, `<rect x="%g" y="%g" width="%g" height="%g"/>`,
				x+float64(c)*ponto, y+float64(l)*ponto, float64(f-c)*ponto, ponto)
			c = f
		}
	}
	return b.String()
}

// PNG transforma uma matriz de pontos (true = preto) numa imagem PNG em data
// URI, pronta para ir num <img> ou <image>.
func PNG(m [][]bool) string {
	if len(m) == 0 || len(m[0]) == 0 {
		return ""
	}
	paleta := color.Palette{color.White, color.Black}
	img := image.NewPaletted(image.Rect(0, 0, len(m[0]), len(m)), paleta)
	for y, linha := range m {
		for x, preto := range linha {
			if preto {
				img.SetColorIndex(x, y, 1)
			}
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

// MatrizDeBytes lê uma imagem monocromática em que cada linha ocupa
// bytesPorLinha bytes e cada bit é um ponto (bit mais alto à esquerda).
func MatrizDeBytes(dados []byte, bytesPorLinha int) [][]bool {
	if bytesPorLinha <= 0 {
		return nil
	}
	var m [][]bool
	for i := 0; i+bytesPorLinha <= len(dados); i += bytesPorLinha {
		linha := make([]bool, bytesPorLinha*8)
		for j := 0; j < bytesPorLinha; j++ {
			for b := 0; b < 8; b++ {
				linha[j*8+b] = dados[i+j]&(0x80>>b) != 0
			}
		}
		m = append(m, linha)
	}
	return m
}
