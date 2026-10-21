//go:build linux

package impressora

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

// abrirComoApp abre a porta virtual do jeito da biblioteca serial do app:
// leitura e escrita, e com modo exclusivo (TIOCEXCL).
func abrirComoApp(t *testing.T, caminho string) *os.File {
	t.Helper()
	f, err := os.OpenFile(caminho, os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		t.Fatalf("não abriu %s: %v", caminho, err)
	}
	if err := unix.IoctlSetInt(int(f.Fd()), unix.TIOCEXCL, 0); err != nil {
		t.Fatalf("TIOCEXCL: %v", err)
	}
	return f
}

func pedirStatus(t *testing.T, f *os.File) []byte {
	t.Helper()
	if _, err := f.Write([]byte{0x10, 0x04, 0x01}); err != nil {
		t.Fatal(err)
	}
	r := make(chan []byte, 1)
	go func() {
		b := make([]byte, 8)
		n, _ := f.Read(b)
		r <- b[:n]
	}()
	select {
	case b := <-r:
		return b
	case <-time.After(2 * time.Second):
		t.Fatal("a porta virtual não respondeu ao status")
		return nil
	}
}

// Quando o sistema fecha a porta (ou morre sem fechar), a marca de exclusivo
// tem que sumir, como numa serial de verdade; senão a próxima abertura dá
// "dispositivo ocupado" para sempre.
func TestPortaVirtualLiberaExclusivoAoFechar(t *testing.T) {
	g := novoGerenciador(t, Config{ID: "excl", Nome: "Excl", Conexao: Serial})
	_ = g
	caminho := filepath.Join(os.TempDir(), "bobina-excl")

	primeira := abrirComoApp(t, caminho)
	if r := pedirStatus(t, primeira); len(r) != 1 || r[0] != 0x12 {
		t.Fatalf("primeira abertura: status veio % X", r)
	}
	primeira.Close()

	// A segunda abertura do app precisa funcionar.
	var segunda *os.File
	var err error
	limite := time.Now().Add(2 * time.Second)
	for time.Now().Before(limite) {
		if segunda, err = os.OpenFile(caminho, os.O_RDWR|unix.O_NOCTTY, 0); err == nil {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("depois de fechar, a porta deveria abrir de novo, mas deu: %v", err)
	}
	defer segunda.Close()
	if r := pedirStatus(t, segunda); len(r) != 1 || r[0] != 0x12 {
		t.Fatalf("segunda abertura: status veio % X", r)
	}
}

// TestAjudanteAppSerial não é um teste: é o "app" que o teste abaixo roda
// como outro processo. Abre a porta com exclusividade e fica esperando.
func TestAjudanteAppSerial(t *testing.T) {
	caminho := os.Getenv("BOBINA_AJUDANTE_PORTA")
	if caminho == "" {
		t.Skip("só roda chamado pelo teste de exclusividade")
	}
	f := abrirComoApp(t, caminho)
	defer f.Close()
	os.Stdout.WriteString("pronto\n")
	time.Sleep(time.Minute)
}

// O caso do app real: ele abre a porta com exclusividade e é fechado sem
// soltar a porta. Enquanto está vivo, ninguém mais abre (como numa serial de
// verdade); quando morre, a porta tem que voltar a abrir.
func TestExclusivoValeEnquantoOAppVive(t *testing.T) {
	novoGerenciador(t, Config{ID: "excl2", Nome: "Excl2", Conexao: Serial})
	caminho := filepath.Join(os.TempDir(), "bobina-excl2")

	app := exec.Command(os.Args[0], "-test.run=^TestAjudanteAppSerial$")
	app.Env = append(os.Environ(), "BOBINA_AJUDANTE_PORTA="+caminho)
	saida, _ := app.StdoutPipe()
	if err := app.Start(); err != nil {
		t.Fatal(err)
	}
	defer app.Process.Kill()
	linha := make([]byte, 7)
	if _, err := io.ReadFull(saida, linha); err != nil || string(linha) != "pronto\n" {
		t.Fatalf("o app de teste não abriu a porta: %q %v", linha, err)
	}

	// Com o app vivo, a exclusividade vale (o vigia não pode tirar).
	time.Sleep(1500 * time.Millisecond)
	if f, err := os.OpenFile(caminho, os.O_RDWR|unix.O_NOCTTY, 0); err == nil {
		f.Close()
		t.Fatal("com o app conectado, outro programa não deveria conseguir abrir a porta")
	}

	// O app morre sem fechar a porta.
	_ = app.Process.Kill()
	_ = app.Wait()

	limite := time.Now().Add(3 * time.Second)
	for {
		f, err := os.OpenFile(caminho, os.O_RDWR|unix.O_NOCTTY, 0)
		if err == nil {
			defer f.Close()
			if r := pedirStatus(t, f); len(r) != 1 || r[0] != 0x12 {
				t.Fatalf("depois de reabrir, o status veio % X", r)
			}
			return
		}
		if time.Now().After(limite) {
			t.Fatalf("depois que o app morreu, a porta continua ocupada: %v", err)
		}
		time.Sleep(100 * time.Millisecond)
	}
}
