//go:build linux

package impressora

import (
	"fmt"
	"os"
	"path/filepath"

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

	// O emulador mantém o lado escravo aberto: assim, quando o sistema
	// testado fecha a porta, a leitura do mestre não dá erro.
	limpar := func() {
		escravo.Close()
		if atalho != "" {
			os.Remove(atalho)
		}
	}
	return portaVirtual{rw: mestre, limpar: limpar, descricao: descricao}, nil
}
