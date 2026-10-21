package impressora

import (
	"fmt"
	"strings"

	"bobina/internal/escpos"
)

// Resposta é o que a impressora devolve a um pedido de status, com a
// explicação em português que vai para a atividade.
type Resposta struct {
	Bytes       []byte
	Explicacao  string
	AtivaASB    bool // GS a ligou o envio automático de status
	DesativaASB bool
}

// online diz se nada impede a impressora de imprimir.
func (e Estado) online() bool {
	return !e.Offline && !e.SemPapel && !e.TampaAberta && !e.ErroGuilhotina
}

// ---------------------------------------------------------------- ESC/POS

// RespostaESCPOS calcula a resposta a um comando de status ESC/POS. Devolve
// ok=false quando a impressora não responde (o sistema vai esperar à toa).
func RespostaESCPOS(c escpos.Comando, e Estado) (Resposta, bool) {
	if !e.Ligada || e.Respostas == RespondeNenhum {
		return Resposta{}, false
	}
	padrao := e.Respostas != RespondeBematech
	switch {
	case c.Nome == "ENQ":
		if e.Respostas == RespondeESCPOS {
			return Resposta{}, false
		}
		return respostaENQ(e), true
	case strings.HasPrefix(c.Nome, "DLE EOT") && padrao:
		return respostaDLEEOT(c.Parametro(2), e), true
	case c.Nome == "GS r" && padrao:
		return respostaGSr(c.Parametro(2), e)
	case c.Nome == "GS I" && padrao:
		return respostaGSI(c.Parametro(2))
	case c.Nome == "GS a" && padrao:
		if c.Parametro(2) == 0 {
			return Resposta{DesativaASB: true, Explicacao: "status automático desligado"}, true
		}
		return Resposta{Bytes: ASB(e), Explicacao: "status automático ligado; " + explicarESCPOS(e), AtivaASB: true}, true
	}
	return Resposta{}, false
}

// Bits fixos de toda resposta do DLE EOT: bit 1 e bit 4 ligados (0x12).
// É por eles que o driver confere se a resposta é válida.
const fixoDLEEOT = 0x12

func respostaDLEEOT(n int, e Estado) Resposta {
	b := byte(fixoDLEEOT)
	switch n {
	case 1: // status da impressora
		if e.GavetaAberta {
			b |= 0x04 // bit 2: sinal do conector da gaveta
		}
		if !e.online() {
			b |= 0x08 // bit 3: offline
		}
		return Resposta{[]byte{b}, "status da impressora: " + explicarESCPOS(e), false, false}
	case 2: // causa de estar offline
		if e.TampaAberta {
			b |= 0x04 // bit 2: tampa aberta
		}
		if e.SemPapel {
			b |= 0x20 // bit 5: parou por falta de papel
		}
		if e.ErroGuilhotina || e.SemPapel {
			b |= 0x40 // bit 6: há um erro
		}
		return Resposta{[]byte{b}, "causa offline: " + causa(e.TampaAberta, "tampa aberta", e.SemPapel, "sem papel", e.ErroGuilhotina, "erro"), false, false}
	case 3: // causa do erro
		if e.ErroGuilhotina {
			b |= 0x08 // bit 3: erro na guilhotina
		}
		return Resposta{[]byte{b}, "causa do erro: " + causa(e.ErroGuilhotina, "guilhotina"), false, false}
	case 4: // sensor de papel
		if e.PoucoPapel || e.SemPapel {
			b |= 0x0C // bits 2 e 3: papel perto do fim
		}
		if e.SemPapel {
			b |= 0x60 // bits 5 e 6: sem papel
		}
		return Resposta{[]byte{b}, "sensor de papel: " + papel(e), false, false}
	}
	return Resposta{[]byte{b}, fmt.Sprintf("DLE EOT %d: pedido desconhecido, respondido sem erros", n), false, false}
}

func respostaGSr(n int, e Estado) (Resposta, bool) {
	switch n {
	case 1, 49:
		var b byte
		if e.PoucoPapel || e.SemPapel {
			b |= 0x03 // bits 0 e 1: papel perto do fim
		}
		if e.SemPapel {
			b |= 0x0C // bits 2 e 3: sem papel
		}
		return Resposta{Bytes: []byte{b}, Explicacao: "sensor de papel: " + papel(e)}, true
	case 2, 50:
		var b byte
		if e.GavetaAberta {
			b = 0x01
		}
		return Resposta{Bytes: []byte{b}, Explicacao: "gaveta " + map[bool]string{true: "aberta", false: "fechada"}[e.GavetaAberta]}, true
	}
	return Resposta{}, false
}

func respostaGSI(n int) (Resposta, bool) {
	texto := map[int]string{65: "1.0", 66: "BOBINA", 67: "EMULADOR ESC/POS", 68: "00000001"}
	switch {
	case n == 1 || n == 49:
		return Resposta{Bytes: []byte{0x20}, Explicacao: "modelo"}, true
	case n == 2 || n == 50:
		return Resposta{Bytes: []byte{0x02}, Explicacao: "tipo: com guilhotina"}, true
	case n == 3 || n == 51:
		return Resposta{Bytes: []byte{0x10}, Explicacao: "versão"}, true
	case texto[n] != "":
		return Resposta{Bytes: append([]byte("_"+texto[n]), 0), Explicacao: texto[n]}, true
	}
	return Resposta{}, false
}

// respostaENQ segue o que o driver Bematech confere: bit 0 = online, bit 1 =
// sem papel. Com tudo certo a resposta é 0x11 (17).
func respostaENQ(e Estado) Resposta {
	b := byte(0x10)
	if e.online() {
		b |= 0x01
	}
	if e.SemPapel {
		b |= 0x02
	}
	return Resposta{Bytes: []byte{b}, Explicacao: "status Bematech: " + explicarESCPOS(e)}
}

// ASB monta os 4 bytes do status automático (GS a).
func ASB(e Estado) []byte {
	b := []byte{0x10, 0x00, 0x00, 0x00}
	if e.GavetaAberta {
		b[0] |= 0x04
	}
	if !e.online() {
		b[0] |= 0x08
	}
	if e.TampaAberta {
		b[0] |= 0x20
	}
	if e.ErroGuilhotina {
		b[1] |= 0x08
	}
	if e.PoucoPapel || e.SemPapel {
		b[2] |= 0x03
	}
	if e.SemPapel {
		b[2] |= 0x0C
	}
	return b
}

func explicarESCPOS(e Estado) string {
	if m := e.MotivoNaoImprime(ESCPOS); m != "" {
		return "offline (" + m + ")"
	}
	if e.PoucoPapel {
		return "online, papel acabando"
	}
	return "online"
}

func papel(e Estado) string {
	switch {
	case e.SemPapel:
		return "sem papel"
	case e.PoucoPapel:
		return "papel acabando"
	}
	return "papel ok"
}

// causa lista os nomes cujos sinais estão ligados: causa(true, "a", false, "b").
func causa(pares ...any) string {
	var r []string
	for i := 0; i+1 < len(pares); i += 2 {
		if pares[i].(bool) {
			r = append(r, pares[i+1].(string))
		}
	}
	if len(r) == 0 {
		return "nenhuma"
	}
	return strings.Join(r, ", ")
}

// ---------------------------------------------------------------- ZPL

// RespostaZPL responde aos comandos de status da Zebra (~HS, ~HQES, ~HI) e às
// consultas SGD (! U1 getvar). cmd vem sem o "~".
func RespostaZPL(cmd string, e Estado, cfg Config) (Resposta, bool) {
	if !e.Ligada || e.Respostas == RespondeNenhum {
		return Resposta{}, false
	}
	b := func(v bool) int {
		if v {
			return 1
		}
		return 0
	}
	switch cmd {
	case "HS":
		comprimento := int(cfg.AlturaMM * float64(cfg.Dpmm))
		s1 := fmt.Sprintf("030,%d,%d,%04d,000,0,0,0,000,0,0,0", b(e.SemPapel), b(e.Offline), comprimento)
		s2 := fmt.Sprintf("001,0,%d,%d,1,2,6,0,00000000,1,000", b(e.TampaAberta), b(e.SemRibbon))
		s3 := "1234,0"
		r := "\x02" + s1 + "\x03\r\n\x02" + s2 + "\x03\r\n\x02" + s3 + "\x03\r\n"
		return Resposta{Bytes: []byte(r), Explicacao: "~HS: " + explicarZPL(e)}, true
	case "HQES":
		var erros, avisos uint32
		if e.SemPapel {
			erros |= 0x01
		}
		if e.SemRibbon {
			erros |= 0x02
		}
		if e.TampaAberta {
			erros |= 0x04
		}
		if e.ErroGuilhotina {
			erros |= 0x08
		}
		if e.PoucoPapel {
			avisos |= 0x08
		}
		r := fmt.Sprintf("\x02\r\n  PRINTER STATUS\r\n   ERRORS:         %d 00000000 %08X\r\n   WARNINGS:       %d 00000000 %08X\r\n\x03\r\n",
			b(erros != 0), erros, b(avisos != 0), avisos)
		return Resposta{Bytes: []byte(r), Explicacao: fmt.Sprintf("~HQES: erros %08X, avisos %08X (%s)", erros, avisos, explicarZPL(e))}, true
	case "HI":
		r := fmt.Sprintf("\x02BOBINA ZPL,V1.0,%d,8192KB\x03\r\n", cfg.Dpmm)
		return Resposta{Bytes: []byte(r), Explicacao: "~HI: identificação"}, true
	}
	return Resposta{}, false
}

// RespostaSGD responde ao "! U1 getvar" usado pelos SDKs da Zebra.
func RespostaSGD(variavel string, e Estado) (Resposta, bool) {
	if !e.Ligada || e.Respostas == RespondeNenhum {
		return Resposta{}, false
	}
	ok := func(problema bool, sim string) string {
		if problema {
			return sim
		}
		return "ok"
	}
	valores := map[string]string{
		"device.languages":    "zpl",
		"device.product_name": "BOBINA ZPL",
		"appl.name":           "V1.0",
		"media.status":        ok(e.SemPapel, "out"),
		"head.latch":          ok(e.TampaAberta, "open"),
		"ribbon.status":       ok(e.SemRibbon, "out"),
		"device.pause":        map[bool]string{true: "on", false: "off"}[e.Offline],
	}
	v, existe := valores[variavel]
	if !existe {
		v = "?"
	}
	return Resposta{Bytes: []byte(`"` + v + `"`), Explicacao: fmt.Sprintf("getvar %s = %s", variavel, v)}, true
}

func explicarZPL(e Estado) string {
	if m := e.MotivoNaoImprime(ZPL); m != "" {
		return m
	}
	if e.PoucoPapel {
		return "pronta, etiquetas acabando"
	}
	return "pronta"
}

// ---------------------------------------------------------------- tabela

// LinhaResposta é uma linha do painel "o que ela responde agora".
type LinhaResposta struct {
	Pedido     string `json:"pedido"`
	Bytes      string `json:"bytes"`
	Resposta   string `json:"resposta"`
	Explicacao string `json:"explicacao"`
}

// TabelaRespostas calcula a resposta atual a cada pedido de status, para o
// usuário ver o efeito de cada chave do painel.
func TabelaRespostas(cfg Config) []LinhaResposta {
	e := cfg.Estado
	var r []LinhaResposta
	add := func(pedido string, enviado []byte, resp Resposta, ok bool) {
		l := LinhaResposta{Pedido: pedido, Bytes: Hex(enviado)}
		if ok && resp.Bytes != nil {
			l.Resposta = Hex(resp.Bytes)
			l.Explicacao = resp.Explicacao
		} else {
			l.Resposta = "—"
			l.Explicacao = "não responde (o sistema espera até dar tempo esgotado)"
		}
		r = append(r, l)
	}
	if cfg.Linguagem == ZPL {
		for _, c := range []string{"HS", "HQES"} {
			resp, ok := RespostaZPL(c, e, cfg)
			if ok {
				resp.Bytes = []byte(strings.NewReplacer("\x02", "<STX>", "\x03", "<ETX>", "\r\n", " ").Replace(string(resp.Bytes)))
				l := LinhaResposta{Pedido: "~" + c, Bytes: Hex([]byte("~" + c)), Resposta: strings.TrimSpace(string(resp.Bytes)), Explicacao: resp.Explicacao}
				r = append(r, l)
			} else {
				add("~"+c, []byte("~"+c), resp, false)
			}
		}
		resp, ok := RespostaSGD("media.status", e)
		add(`! U1 getvar "media.status"`, nil, resp, ok)
		r[len(r)-1].Bytes = ""
		if ok {
			r[len(r)-1].Resposta = string(resp.Bytes)
		}
		return r
	}
	for _, n := range []byte{1, 2, 3, 4} {
		cmd := []byte{0x10, 0x04, n}
		c, _ := escpos.Proximo(cmd)
		resp, ok := RespostaESCPOS(c, e)
		add(fmt.Sprintf("DLE EOT %d", n), cmd, resp, ok)
	}
	cmd := []byte{0x1D, 'r', 1}
	c, _ := escpos.Proximo(cmd)
	resp, ok := RespostaESCPOS(c, e)
	add("GS r 1", cmd, resp, ok)
	c, _ = escpos.Proximo([]byte{0x05})
	resp, ok = RespostaESCPOS(c, e)
	add("ENQ (Bematech)", []byte{0x05}, resp, ok)
	return r
}
