// Package impressora é o coração do emulador: cada Impressora escuta numa
// conexão (rede, serial ou pasta), lê o que chega, responde aos pedidos de
// status conforme o Estado configurado e guarda os trabalhos impressos.
package impressora

import (
	"fmt"
	"strings"
	"time"

	"bobina/internal/escpos"
)

// Linguagem de comandos que a impressora entende.
type Linguagem string

const (
	ESCPOS Linguagem = "escpos"
	ZPL    Linguagem = "zpl"
)

// Conexao é por onde os trabalhos chegam.
type Conexao string

const (
	Rede   Conexao = "rede"   // TCP, como uma impressora de rede (porta 9100)
	Serial Conexao = "serial" // porta serial real ou virtual
	Pasta  Conexao = "pasta"  // arquivos .prn largados numa pasta
)

// Config é tudo o que se configura numa impressora; é o que vai para o
// arquivo de configuração.
type Config struct {
	ID        string    `json:"id"`
	Nome      string    `json:"nome"`
	Cor       string    `json:"cor"`
	Linguagem Linguagem `json:"linguagem"`
	Conexao   Conexao   `json:"conexao"`

	// Rede
	Host  string `json:"host,omitempty"`
	Porta int    `json:"porta,omitempty"`

	// Serial: vazio cria uma porta virtual (Linux); senão abre a porta indicada.
	Serial string `json:"serial,omitempty"`
	Baud   int    `json:"baud,omitempty"`

	// Pasta PRN
	Pasta string `json:"pasta,omitempty"`

	// Cupom (ESC/POS)
	LarguraPapel int `json:"larguraPapel,omitempty"` // mm: 80 ou 58
	// ModoComandos é o conjunto que a impressora entende ao ligar: "escpos"
	// (padrão) ou "bematech" (ESC/Bematech). O sistema pode trocar com GS F9.
	ModoComandos string `json:"modoComandos,omitempty"`
	Colunas      int    `json:"colunas,omitempty"`
	PaginaCodigo int    `json:"paginaCodigo"`

	// Etiqueta (ZPL)
	Dpmm      int     `json:"dpmm,omitempty"`
	LarguraMM float64 `json:"larguraMM,omitempty"`
	AlturaMM  float64 `json:"alturaMM,omitempty"`

	Estado Estado `json:"estado"`
}

// Respostas diz a quais pedidos de status a impressora responde.
type Respostas string

const (
	RespondeTodos    Respostas = "todas"    // ESC/POS padrão e Bematech (ENQ)
	RespondeESCPOS   Respostas = "escpos"   // só DLE EOT / GS r: como uma Bematech no modo ESC/POS
	RespondeBematech Respostas = "bematech" // só ENQ: Bematech no modo próprio
	RespondeNenhum   Respostas = "nenhuma"  // fica calada, como impressora sem retorno
)

// Estado é o que se liga e desliga no painel para testar como o sistema
// reage. Tudo aqui muda a resposta aos pedidos de status.
type Estado struct {
	Ligada         bool      `json:"ligada"`
	Offline        bool      `json:"offline"`
	SemPapel       bool      `json:"semPapel"`
	PoucoPapel     bool      `json:"poucoPapel"`
	TampaAberta    bool      `json:"tampaAberta"`
	ErroGuilhotina bool      `json:"erroGuilhotina"`
	GavetaAberta   bool      `json:"gavetaAberta"`
	SemRibbon      bool      `json:"semRibbon"`
	Respostas      Respostas `json:"respostas"`
	AtrasoMs       int       `json:"atrasoMs"`
}

// MotivoNaoImprime explica por que um trabalho recebido agora não sairia no
// papel. Vazio quando a impressora imprime normalmente.
func (e Estado) MotivoNaoImprime(l Linguagem) string {
	var m []string
	if e.TampaAberta {
		if l == ZPL {
			m = append(m, "cabeça aberta")
		} else {
			m = append(m, "tampa aberta")
		}
	}
	if e.SemPapel {
		m = append(m, "sem papel")
	}
	if e.SemRibbon && l == ZPL {
		m = append(m, "sem ribbon")
	}
	if e.ErroGuilhotina {
		m = append(m, "erro na guilhotina")
	}
	if e.Offline {
		if l == ZPL {
			m = append(m, "pausada")
		} else {
			m = append(m, "offline")
		}
	}
	return strings.Join(m, ", ")
}

// Trabalho é um cupom ou etiqueta recebido.
type Trabalho struct {
	ID           int64     `json:"id"`
	Numero       int       `json:"numero"` // sequência da impressora, como a senha de um guichê
	Impressora   string    `json:"impressora"`
	Recebido     time.Time `json:"recebido"`
	Origem       string    `json:"origem"`
	Bytes        int       `json:"bytes"`
	Impresso     bool      `json:"impresso"`
	Motivo       string    `json:"motivo,omitempty"`
	Visual       string    `json:"visual"` // HTML do cupom ou SVG da etiqueta
	Texto        string    `json:"texto"`  // o que foi impresso, em texto puro
	Avisos       []string  `json:"avisos"`
	Quantidade   int       `json:"quantidade,omitempty"`
	Linguagem    Linguagem `json:"linguagem"`
	ModoComandos string    `json:"modoComandos,omitempty"` // ESC/POS ou ESC/Bematech, no início do trabalho
	LarguraMM    float64   `json:"larguraMM,omitempty"`
	AlturaMM     float64   `json:"alturaMM,omitempty"`
	bruto        []byte
}

// Bruto devolve os bytes exatamente como chegaram.
func (t *Trabalho) Bruto() []byte { return t.bruto }

// Evento é uma linha da atividade: conexão, pedido de status, trabalho...
type Evento struct {
	ID         int64     `json:"id"`
	Impressora string    `json:"impressora"`
	Hora       time.Time `json:"hora"`
	Tipo       string    `json:"tipo"` // conexao, status, trabalho, estado, aviso, erro
	Texto      string    `json:"texto"`
	Detalhe    string    `json:"detalhe,omitempty"`
	Trabalho   int64     `json:"trabalho,omitempty"`
}

// Hex formata bytes como "10 04 01" para mostrar ao usuário.
func Hex(b []byte) string {
	var s []string
	for _, c := range b {
		s = append(s, fmt.Sprintf("%02X", c))
	}
	return strings.Join(s, " ")
}

// modoInicial é o conjunto de comandos com que a impressora começa.
func (c Config) modoInicial() escpos.Modo {
	if c.ModoComandos == "bematech" {
		return escpos.ModoBematech
	}
	return escpos.ModoESCPOS
}

func nomeModoConfig(m escpos.Modo) string {
	if m == escpos.ModoBematech {
		return "bematech"
	}
	return "escpos"
}
