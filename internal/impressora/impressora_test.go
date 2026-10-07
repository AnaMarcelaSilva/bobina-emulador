package impressora

import (
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

// novoGerenciador cria um gerenciador com as impressoras dadas, sem arquivo.
func novoGerenciador(t *testing.T, cfgs ...Config) *Gerenciador {
	t.Helper()
	g := &Gerenciador{assinantes: map[chan Notificacao]bool{}}
	for _, c := range cfgs {
		c = completar(c)
		c.Estado.Ligada = true
		g.impressoras = append(g.impressoras, &Impressora{g: g, cfg: c})
	}
	for _, i := range g.impressoras {
		i.iniciar()
		if r := i.Resumo(); r.Erro != "" {
			t.Fatalf("%s não iniciou: %s", c(i).Nome, r.Erro)
		}
	}
	t.Cleanup(g.Encerrar)
	return g
}

func c(i *Impressora) Config { return i.Config() }

func portaLivre(t *testing.T) int {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

// pedir envia bytes e lê a resposta (ou nada, se a impressora ficar calada).
func pedir(t *testing.T, con net.Conn, envio []byte, espera time.Duration) []byte {
	t.Helper()
	if _, err := con.Write(envio); err != nil {
		t.Fatal(err)
	}
	_ = con.SetReadDeadline(time.Now().Add(espera))
	buf := make([]byte, 256)
	n, _ := con.Read(buf)
	return buf[:n]
}

func esperarTrabalhos(t *testing.T, i *Impressora, n int) []*Trabalho {
	t.Helper()
	limite := time.Now().Add(3 * time.Second)
	for time.Now().Before(limite) {
		if tr := i.Trabalhos(); len(tr) >= n {
			return tr
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("esperava %d trabalho(s), vieram %d", n, len(i.Trabalhos()))
	return nil
}

func TestRedeESCPOS(t *testing.T) {
	porta := portaLivre(t)
	g := novoGerenciador(t, Config{ID: "cupom", Nome: "Cupom", Linguagem: ESCPOS, Conexao: Rede, Host: "127.0.0.1", Porta: porta})
	imp := g.Buscar("cupom")

	con, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(porta)))
	if err != nil {
		t.Fatal(err)
	}
	defer con.Close()

	// Status antes de imprimir, na mesma conexão do cupom.
	if r := pedir(t, con, []byte{0x10, 0x04, 0x01}, time.Second); len(r) != 1 || r[0] != 0x12 {
		t.Fatalf("DLE EOT 1 online deveria responder 12, veio % X", r)
	}
	if r := pedir(t, con, []byte{0x10, 0x04, 0x02}, time.Second); len(r) != 1 || r[0] != 18 {
		t.Fatalf("o driver Elgin espera 18 no DLE EOT 2, veio % X", r)
	}
	if r := pedir(t, con, []byte{0x05}, time.Second); len(r) != 1 || r[0] != 17 {
		t.Fatalf("o driver Bematech espera 17 (0x11) no ENQ, veio % X", r)
	}
	// Dois cupons separados por corte na mesma conexão.
	_, _ = con.Write([]byte("\x1b@PRIMEIRO\n\x1dV\x42\x28\x1b@SEGUNDO\n\x1dV\x42\x28"))
	tr := esperarTrabalhos(t, imp, 2)
	if !strings.Contains(tr[1].Texto, "PRIMEIRO") || !strings.Contains(tr[0].Texto, "SEGUNDO") {
		t.Errorf("cupons separados errado: %q / %q", tr[1].Texto, tr[0].Texto)
	}

	// Sem papel: o status muda e o cupom chega, mas não é impresso.
	imp.AlterarEstado(func(e *Estado) { e.SemPapel = true }, "")
	if r := pedir(t, con, []byte{0x10, 0x04, 0x04}, time.Second); len(r) != 1 || r[0] != 0x7E {
		t.Fatalf("DLE EOT 4 sem papel deveria responder 7E, veio % X", r)
	}
	if r := pedir(t, con, []byte{0x10, 0x04, 0x01}, time.Second); len(r) != 1 || r[0]&0x08 == 0 {
		t.Fatalf("DLE EOT 1 sem papel deveria vir offline, veio % X", r)
	}
	if r := pedir(t, con, []byte{0x05}, time.Second); len(r) != 1 || r[0] != 0x12 {
		t.Fatalf("ENQ sem papel deveria responder 12 (offline + sem papel), veio % X", r)
	}
	_, _ = con.Write([]byte("TERCEIRO\n\x1dV\x00"))
	tr = esperarTrabalhos(t, imp, 3)
	if tr[0].Impresso || tr[0].Motivo != "sem papel" {
		t.Errorf("com papel acabado o cupom não deveria sair: impresso=%v motivo=%q", tr[0].Impresso, tr[0].Motivo)
	}

	// Bematech no modo ESC/POS: ignora o ENQ, mas responde ao DLE EOT.
	imp.AlterarEstado(func(e *Estado) { e.SemPapel = false; e.Respostas = RespondeESCPOS }, "")
	if r := pedir(t, con, []byte{0x05}, 400*time.Millisecond); len(r) != 0 {
		t.Fatalf("no modo só ESC/POS o ENQ não deveria ter resposta, veio % X", r)
	}
	if r := pedir(t, con, []byte{0x10, 0x04, 0x01}, time.Second); len(r) != 1 || r[0] != 0x12 {
		t.Fatalf("no modo só ESC/POS o DLE EOT deveria responder, veio % X", r)
	}
}

func TestDesligadaRecusaConexao(t *testing.T) {
	porta := portaLivre(t)
	g := novoGerenciador(t, Config{ID: "cupom", Nome: "Cupom", Conexao: Rede, Host: "127.0.0.1", Porta: porta})
	imp := g.Buscar("cupom")
	imp.AlterarEstado(func(e *Estado) { e.Ligada = false }, "")
	if con, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(porta)), 500*time.Millisecond); err == nil {
		con.Close()
		t.Fatal("desligada, a impressora de rede não deveria aceitar conexão")
	}
	imp.AlterarEstado(func(e *Estado) { e.Ligada = true }, "")
	con, err := net.DialTimeout("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(porta)), time.Second)
	if err != nil {
		t.Fatalf("religada, deveria aceitar conexão: %v", err)
	}
	con.Close()
}

func TestRedeZPL(t *testing.T) {
	porta := portaLivre(t)
	g := novoGerenciador(t, Config{ID: "etq", Nome: "Etiqueta", Linguagem: ZPL, Conexao: Rede, Host: "127.0.0.1", Porta: porta,
		Dpmm: 8, LarguraMM: 60, AlturaMM: 40})
	imp := g.Buscar("etq")
	con, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(porta)))
	if err != nil {
		t.Fatal(err)
	}
	defer con.Close()

	imp.AlterarEstado(func(e *Estado) { e.TampaAberta = true }, "")
	r := string(pedir(t, con, []byte("~HQES"), time.Second))
	if !strings.Contains(r, "ERRORS:         1 00000000 00000004") {
		t.Fatalf("~HQES com cabeça aberta deveria ter o erro 4: %q", r)
	}
	imp.AlterarEstado(func(e *Estado) { e.TampaAberta = false }, "")

	// Etiqueta com ~HS no meio: o status responde e a etiqueta sai inteira.
	r = string(pedir(t, con, []byte("^XA^FO10,10^A0N,30,30^FDOI^FS~HS^XZ"), time.Second))
	if !strings.HasPrefix(r, "\x02030,0,0,0320") {
		t.Fatalf("~HS no meio da etiqueta respondeu errado: %q", r)
	}
	tr := esperarTrabalhos(t, imp, 1)
	if tr[0].Texto != "OI" || len(tr[0].Avisos) != 0 {
		t.Errorf("etiqueta errada: %q %v", tr[0].Texto, tr[0].Avisos)
	}

	// ~PP pausa e ~PS retoma, mudando o painel.
	_, _ = con.Write([]byte("~PP"))
	time.Sleep(100 * time.Millisecond)
	if !imp.Config().Estado.Offline {
		t.Error("~PP deveria pausar a impressora")
	}
	_, _ = con.Write([]byte("~PS"))
	time.Sleep(100 * time.Millisecond)
	if imp.Config().Estado.Offline {
		t.Error("~PS deveria tirar da pausa")
	}
}

func TestSerialVirtual(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("porta virtual só no Linux")
	}
	g := novoGerenciador(t, Config{ID: "serial-teste", Nome: "Serial", Conexao: Serial})
	imp := g.Buscar("serial-teste")
	caminho := filepath.Join(os.TempDir(), "bobina-serial-teste")
	porta, err := os.OpenFile(caminho, os.O_RDWR, 0)
	if err != nil {
		t.Fatalf("a porta virtual %s deveria existir: %v (%s)", caminho, err, imp.Resumo().Endereco)
	}
	defer porta.Close()
	if _, err := porta.Write([]byte{0x10, 0x04, 0x01}); err != nil {
		t.Fatal(err)
	}
	resposta := make(chan []byte, 1)
	go func() {
		b := make([]byte, 8)
		n, _ := porta.Read(b)
		resposta <- b[:n]
	}()
	select {
	case r := <-resposta:
		if len(r) != 1 || r[0] != 0x12 {
			t.Fatalf("status pela serial veio % X", r)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("a serial virtual não respondeu ao status")
	}
	_, _ = porta.Write([]byte("PELA SERIAL\n"))
	tr := esperarTrabalhos(t, imp, 1)
	if !strings.Contains(tr[0].Texto, "PELA SERIAL") {
		t.Errorf("cupom pela serial: %q", tr[0].Texto)
	}
}

func TestPasta(t *testing.T) {
	pasta := t.TempDir()
	g := novoGerenciador(t, Config{ID: "prn", Nome: "PRN", Conexao: Pasta, Pasta: pasta})
	imp := g.Buscar("prn")
	if err := os.WriteFile(filepath.Join(pasta, "cupom.prn"), []byte("\x1b@CUPOM DA PASTA\n\x1dV\x00"), 0o644); err != nil {
		t.Fatal(err)
	}
	tr := esperarTrabalhos(t, imp, 1)
	if !strings.Contains(tr[0].Texto, "CUPOM DA PASTA") || tr[0].Origem != "arquivo cupom.prn" {
		t.Errorf("trabalho da pasta errado: %q de %q", tr[0].Texto, tr[0].Origem)
	}
	if _, err := os.Stat(filepath.Join(pasta, "cupom.prn")); !os.IsNotExist(err) {
		t.Error("o arquivo impresso deveria ter ido para a subpasta impressos")
	}
}

func TestValidacaoPortaRepetida(t *testing.T) {
	porta := portaLivre(t)
	g := novoGerenciador(t, Config{ID: "a", Nome: "A", Conexao: Rede, Host: "127.0.0.1", Porta: porta})
	_, err := g.Criar(Config{Nome: "B", Conexao: Rede, Host: "127.0.0.1", Porta: porta})
	if err == nil || !strings.Contains(err.Error(), "já é usada") {
		t.Fatalf("deveria recusar porta repetida: %v", err)
	}
}

func TestTentaDeNovoQuandoAPortaLibera(t *testing.T) {
	ocupada, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	porta := ocupada.Addr().(*net.TCPAddr).Port
	g := &Gerenciador{assinantes: map[chan Notificacao]bool{}}
	t.Cleanup(g.Encerrar)
	r, err := g.Criar(Config{Nome: "Cupom", Conexao: Rede, Host: "127.0.0.1", Porta: porta, Estado: Estado{Ligada: true}})
	if err != nil {
		t.Fatal(err)
	}
	if r.Erro == "" {
		t.Fatal("com a porta ocupada deveria mostrar erro")
	}
	ocupada.Close()
	limite := time.Now().Add(5 * time.Second)
	for time.Now().Before(limite) {
		if g.Buscar(r.ID).Resumo().Erro == "" {
			con, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(porta)))
			if err != nil {
				t.Fatalf("diz que está escutando, mas recusou: %v", err)
			}
			con.Close()
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("a impressora não voltou depois que a porta foi liberada")
}

func TestLimparZeraAContagem(t *testing.T) {
	g := novoGerenciador(t, Config{ID: "prn", Nome: "PRN", Conexao: Pasta, Pasta: t.TempDir()})
	imp := g.Buscar("prn")
	imp.Imprimir([]byte("UM\n"), "teste")
	imp.Imprimir([]byte("DOIS\n"), "teste")
	if n := imp.Trabalhos()[0].Numero; n != 2 {
		t.Fatalf("antes de limpar o último deveria ser 2, veio %d", n)
	}
	imp.Limpar()
	novos := imp.Imprimir([]byte("DE NOVO\n"), "teste")
	if len(novos) != 1 || novos[0].Numero != 1 {
		t.Fatalf("depois de limpar a contagem deveria voltar a 1: %+v", novos)
	}
}

func TestGSF9GravaOModoBematech(t *testing.T) {
	g := novoGerenciador(t, Config{ID: "bema", Nome: "Bema", Conexao: Pasta, Pasta: t.TempDir()})
	imp := g.Buscar("bema")
	// GS F9 20 00 grava o ESC/Bematech; ESC E sem parâmetro liga o negrito.
	novos := imp.Imprimir([]byte("\x1d\xf9\x20\x00\x1bEComanda: 7\n\x1bF"), "driver")
	if len(novos) != 1 || novos[0].Texto != "Comanda: 7" || novos[0].ModoComandos != "escpos" {
		t.Fatalf("trabalho errado: %+v", novos)
	}
	if imp.Config().ModoComandos != "bematech" {
		t.Fatal("o GS F9 20 deveria gravar o modo Bematech na impressora")
	}
	// A próxima impressão já começa no modo gravado, mesmo sem o GS F9.
	novos = imp.Imprimir([]byte("\x1bEMesa 3\n"), "driver")
	if len(novos) != 1 || novos[0].Texto != "Mesa 3" || novos[0].ModoComandos != "bematech" {
		t.Fatalf("depois de gravado, o ESC E não deveria comer letras: %+v", novos)
	}
}

func TestPastaSoMexeEmArquivosDeImpressao(t *testing.T) {
	pasta := t.TempDir()
	documento := filepath.Join(pasta, "contrato.docx")
	if err := os.WriteFile(documento, []byte("não é para imprimir"), 0o644); err != nil {
		t.Fatal(err)
	}
	g := novoGerenciador(t, Config{ID: "prn", Nome: "PRN", Conexao: Pasta, Pasta: pasta})
	if err := os.WriteFile(filepath.Join(pasta, "cupom.prn"), []byte("OI\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	esperarTrabalhos(t, g.Buscar("prn"), 1)
	if _, err := os.Stat(documento); err != nil {
		t.Fatalf("o .docx não poderia ter saído do lugar: %v", err)
	}
}
