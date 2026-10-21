//go:build linux

package impressora

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/creack/pty"
	"golang.org/x/sys/unix"
)

// criarPortaVirtual abre um pseudo-terminal: o lado "escravo" (/dev/pts/N)
// funciona como uma porta serial para o sistema testado, e o emulador fica
// com o lado "mestre". Um atalho com nome fixo evita que o caminho mude a
// cada execução.
func criarPortaVirtual(id string) (portaVirtual, error) {
	mestre, escravo, err := pty.Open()
	if err != nil {
		return portaVirtual{}, fmt.Errorf("não foi possível criar a porta virtual: %v", err)
	}
	// Modo cru: sem eco e sem trocar \n por \r\n, como uma serial de verdade.
	if t, err := unix.IoctlGetTermios(int(escravo.Fd()), unix.TCGETS); err == nil {
		t.Iflag &^= unix.IGNBRK | unix.BRKINT | unix.PARMRK | unix.ISTRIP | unix.INLCR | unix.IGNCR | unix.ICRNL | unix.IXON
		t.Oflag &^= unix.OPOST
		t.Lflag &^= unix.ECHO | unix.ECHONL | unix.ICANON | unix.ISIG | unix.IEXTEN
		t.Cflag &^= unix.CSIZE | unix.PARENB
		t.Cflag |= unix.CS8
		_ = unix.IoctlSetTermios(int(escravo.Fd()), unix.TCSETS, t)
	}

	atalho := filepath.Join(os.TempDir(), "bobina-"+id)
	_ = os.Remove(atalho)
	descricao := escravo.Name()
	if os.Symlink(escravo.Name(), atalho) == nil {
		descricao = fmt.Sprintf("%s (atalho %s)", atalho, escravo.Name())
	} else {
		atalho = ""
	}

	// O emulador mantém o lado escravo aberto, para a leitura do mestre não
	// dar erro quando o sistema testado fecha a porta. Por causa disso, a
	// marca de exclusivo (TIOCEXCL) que a biblioteca serial pede nunca sairia
	// sozinha: o vigia abaixo tira a marca quando ninguém mais está com a
	// porta aberta, como acontece numa serial de verdade.
	parada := make(chan struct{})
	go vigiarExclusivo(escravo, parada)
	limpar := func() {
		close(parada)
		escravo.Close()
		if atalho != "" {
			os.Remove(atalho)
		}
	}
	return portaVirtual{rw: mestre, limpar: limpar, descricao: descricao}, nil
}

// vigiarExclusivo tira o modo exclusivo da porta quando o programa que o
// pediu já fechou a porta (ou morreu sem fechar). Enquanto ele estiver
// conectado, a exclusividade continua valendo.
func vigiarExclusivo(escravo *os.File, parada <-chan struct{}) {
	fd := int(escravo.Fd())
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-parada:
			return
		case <-t.C:
		}
		exclusivo, err := unix.IoctlGetInt(fd, unix.TIOCGEXCL)
		if err != nil || exclusivo == 0 || alguemComAPorta(escravo.Name()) {
			continue
		}
		_ = unix.IoctlSetInt(fd, unix.TIOCNXCL, 0)
	}
}

// alguemComAPorta procura, em /proc, outro processo com a porta aberta.
// Só vê os processos do mesmo usuário, que é o caso do sistema testado.
func alguemComAPorta(caminho string) bool {
	eu := os.Getpid()
	processos, _ := filepath.Glob("/proc/[0-9]*/fd")
	for _, dir := range processos {
		var pid int
		if _, err := fmt.Sscanf(dir, "/proc/%d/fd", &pid); err != nil || pid == eu {
			continue
		}
		fds, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, f := range fds {
			if alvo, err := os.Readlink(filepath.Join(dir, f.Name())); err == nil && alvo == caminho {
				return true
			}
		}
	}
	return false
}
