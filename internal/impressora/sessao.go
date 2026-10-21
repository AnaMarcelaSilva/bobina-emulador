package impressora

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"bobina/internal/escpos"
)

// Tempo sem chegar nada para considerar o trabalho terminado. O ESC/POS não
// tem um "fim de cupom": o corte fecha o trabalho, e quando não há corte é o
// silêncio que fecha.
const tempoOcioso = 700 * time.Millisecond

// Sessao acompanha uma conexão (um socket TCP, a porta serial ou um arquivo).
// Ela lê o fluxo comando a comando, responde aos pedidos de status na hora e
// junta o resto em trabalhos.
type Sessao struct {
	imp      *Impressora
	origem   string
	escrever func([]byte) error // nil quando não há volta (pasta)

	mu     sync.Mutex
	buf    []byte // bytes do trabalho atual
	lido   int    // até onde o buf já foi interpretado (ESC/POS)
	ocioso *time.Timer
	asb    bool
	fim    bool

	modo       escpos.Modo // conjunto de comandos em uso agora (ESC/POS ou Bematech)
	modoInicio escpos.Modo // conjunto em uso quando o trabalho atual começou
}

func novaSessao(imp *Impressora, origem string, escrever func([]byte) error) *Sessao {
	s := &Sessao{imp: imp, origem: origem, escrever: escrever}
	s.modo = imp.Config().modoInicial()
	s.modoInicio = s.modo
	imp.registrarSessao(s, true)
	return s
}

// Receber trata um pedaço de dados que acabou de chegar.
func (s *Sessao) Receber(dados []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fim {
		return
	}
	cfg := s.imp.Config()
	if !cfg.Estado.Ligada {
		s.imp.evento("aviso", fmt.Sprintf("%d bytes chegaram com a impressora desligada e foram descartados", len(dados)), s.origem, 0)
		return
	}
	if len(s.buf) == 0 {
		s.modoInicio = s.modo
	}
	s.buf = append(s.buf, dados...)
	if cfg.Linguagem == ZPL {
		s.processarZPL(cfg)
	} else {
		s.processarESCPOS(cfg)
	}
	s.reiniciarOcioso()
}

// Encerrar fecha a sessão; o que sobrou no buffer vira um último trabalho.
func (s *Sessao) Encerrar() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.fim {
		return
	}
	s.fim = true
	if s.ocioso != nil {
		s.ocioso.Stop()
	}
	s.fecharTrabalho()
	s.imp.registrarSessao(s, false)
}

func (s *Sessao) reiniciarOcioso() {
	if s.ocioso != nil {
		s.ocioso.Stop()
	}
	if len(s.buf) == 0 {
		return
	}
	s.ocioso = time.AfterFunc(tempoOcioso, func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		if !s.fim {
			s.fecharTrabalho()
		}
	})
}

// responder manda a resposta de volta, esperando o atraso configurado.
func (s *Sessao) responder(pedido string, enviado []byte, r Resposta, atraso int) {
	detalhe := Hex(enviado) + " → " + Hex(r.Bytes)
	if s.escrever == nil {
		s.imp.evento("status", pedido+" sem caminho de volta (arquivo): "+r.Explicacao, detalhe, 0)
		return
	}
	if r.AtivaASB {
		s.asb = true
	}
	if r.DesativaASB {
		s.asb = false
	}
	if len(r.Bytes) == 0 {
		s.imp.evento("status", pedido+": "+r.Explicacao, Hex(enviado), 0)
		return
	}
	texto := fmt.Sprintf("%s respondido: %s", pedido, r.Explicacao)
	if atraso > 0 {
		texto += fmt.Sprintf(" (depois de %d ms)", atraso)
	}
	s.imp.evento("status", texto, detalhe, 0)
	enviar := func() {
		if err := s.escrever(r.Bytes); err != nil {
			s.imp.evento("erro", "Não foi possível enviar a resposta: "+err.Error(), "", 0)
		}
	}
	if atraso > 0 {
		time.AfterFunc(time.Duration(atraso)*time.Millisecond, enviar)
	} else {
		enviar()
	}
}

func (s *Sessao) semResposta(pedido string, enviado []byte, e Estado) {
	motivo := "a impressora está configurada para não responder"
	switch e.Respostas {
	case RespondeESCPOS:
		motivo = "configurada para responder só ao ESC/POS padrão, como uma Bematech no modo ESC/POS"
	case RespondeBematech:
		motivo = "configurada para responder só ao ENQ da Bematech"
	}
	s.imp.evento("status", pedido+" ignorado: "+motivo, Hex(enviado), 0)
}

// ---------------------------------------------------------------- ESC/POS

func (s *Sessao) processarESCPOS(cfg Config) {
	for s.lido < len(s.buf) {
		c, ok := escpos.ProximoNoModo(s.buf[s.lido:], s.modo)
		if !ok {
			return // comando pela metade: espera o resto
		}
		s.lido += c.Tamanho
		if c.NovoModo != 0 && (c.NovoModo != s.modo || c.Grava) {
			s.modo = c.NovoModo
			s.imp.trocouModo(c.NovoModo, c.Grava)
		}
		switch c.Tipo {
		case escpos.Status:
			if r, ok := RespostaESCPOS(c, cfg.Estado); ok {
				s.responder(c.Nome, c.Bytes, r, cfg.Estado.AtrasoMs)
			} else {
				s.semResposta(c.Nome, c.Bytes, cfg.Estado)
			}
		case escpos.Corte:
			// O corte separa um cupom do próximo, como no papel: o que vem
			// depois dele já é o cupom seguinte.
			resto := append([]byte{}, s.buf[s.lido:]...)
			s.buf = s.buf[:s.lido]
			s.fecharTrabalho()
			s.buf = resto
			s.modoInicio = s.modo
		}
	}
}

// ---------------------------------------------------------------- ZPL

var (
	// Comandos de status que podem vir no meio de qualquer coisa.
	statusZPL = regexp.MustCompile(`(?i)~(HQES|HS|HI|JA|PS|PP|WC)`)
	sgd       = regexp.MustCompile(`(?i)! *U1 +getvar +"([^"]+)"\s*`)
	fimZPL    = regexp.MustCompile(`(?i)\^XZ`)
)

func (s *Sessao) processarZPL(cfg Config) {
	// 1. Responde e tira do fluxo os pedidos de status.
	for {
		loc := statusZPL.FindSubmatchIndex(s.buf)
		if loc == nil {
			break
		}
		cmd := strings.ToUpper(string(s.buf[loc[2]:loc[3]]))
		s.comandoTilZPL(cmd, cfg)
		s.buf = append(s.buf[:loc[0]], s.buf[loc[1]:]...)
	}
	for {
		loc := sgd.FindSubmatchIndex(s.buf)
		if loc == nil {
			break
		}
		variavel := string(s.buf[loc[2]:loc[3]])
		pedido := fmt.Sprintf(`getvar "%s"`, variavel)
		if r, ok := RespostaSGD(variavel, cfg.Estado); ok {
			s.responder(pedido, s.buf[loc[0]:loc[1]], r, cfg.Estado.AtrasoMs)
		} else {
			s.semResposta(pedido, nil, cfg.Estado)
		}
		s.buf = append(s.buf[:loc[0]], s.buf[loc[1]:]...)
	}
	// 2. Cada ^XZ fecha uma etiqueta.
	for {
		loc := fimZPL.FindIndex(s.buf)
		if loc == nil {
			break
		}
		etiqueta := s.buf[:loc[1]]
		resto := append([]byte{}, s.buf[loc[1]:]...)
		s.buf = etiqueta
		s.fecharTrabalho()
		s.buf = resto
	}
	if len(bytes.TrimSpace(s.buf)) == 0 {
		s.buf = nil
	}
}

func (s *Sessao) comandoTilZPL(cmd string, cfg Config) {
	switch cmd {
	case "HS", "HQES", "HI":
		if r, ok := RespostaZPL(cmd, cfg.Estado, cfg); ok {
			s.responder("~"+cmd, []byte("~"+cmd), r, cfg.Estado.AtrasoMs)
		} else {
			s.semResposta("~"+cmd, []byte("~"+cmd), cfg.Estado)
		}
	case "PS":
		s.imp.AlterarEstado(func(e *Estado) { e.Offline = false }, "~PS: o sistema tirou a impressora da pausa")
	case "PP":
		s.imp.AlterarEstado(func(e *Estado) { e.Offline = true }, "~PP: o sistema pausou a impressora")
	case "JA":
		s.imp.evento("estado", "~JA: o sistema cancelou tudo o que estava na fila", "", 0)
	case "WC":
		s.imp.evento("aviso", "~WC: pedido de etiqueta de configuração (o emulador não imprime)", "", 0)
	}
}

// ---------------------------------------------------------------- trabalho

// fecharTrabalho transforma o buffer num trabalho, se houver algo que a
// impressora imprimiria (pedidos de status sozinhos não viram trabalho).
func (s *Sessao) fecharTrabalho() {
	dados := s.buf
	s.buf, s.lido = nil, 0
	if len(dados) == 0 || !temConteudo(dados, s.imp.Config().Linguagem, s.modoInicio) {
		return
	}
	s.imp.novoTrabalho(dados, s.origem, s.modoInicio)
}

func temConteudo(dados []byte, l Linguagem, modo escpos.Modo) bool {
	if l == ZPL {
		return bytes.Contains(bytes.ToUpper(dados), []byte("^XA"))
	}
	for _, c := range escpos.LerNoModo(dados, modo) {
		switch c.Tipo {
		case escpos.Texto, escpos.Imagem, escpos.CodigoBarras, escpos.Corte, escpos.Gaveta, escpos.Avanco:
			return true
		}
	}
	return false
}

// enviarASB manda o status automático para quem pediu com GS a.
func (s *Sessao) enviarASB(e Estado) {
	s.mu.Lock()
	ativo := s.asb && s.escrever != nil && !s.fim
	s.mu.Unlock()
	if ativo && e.Ligada {
		if err := s.escrever(ASB(e)); err == nil {
			s.imp.evento("status", "Status automático enviado: "+explicarESCPOS(e), Hex(ASB(e)), 0)
		}
	}
}
