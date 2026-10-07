package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"bobina/internal/impressora"
)

// comandoEnviar manda um arquivo (ou a entrada padrão) para uma impressora de
// rede, emulada ou de verdade: bobina enviar 127.0.0.1:9100 cupom.prn
func comandoEnviar(args []string) error {
	if len(args) < 1 {
		return errors.New("uso: bobina enviar HOST:PORTA [arquivo]  (sem arquivo, lê da entrada padrão)")
	}
	var dados []byte
	var err error
	if len(args) > 1 {
		dados, err = os.ReadFile(args[1])
	} else {
		dados, err = io.ReadAll(os.Stdin)
	}
	if err != nil {
		return err
	}
	con, err := net.DialTimeout("tcp", args[0], 3*time.Second)
	if err != nil {
		return err
	}
	defer con.Close()
	if _, err := con.Write(dados); err != nil {
		return err
	}
	fmt.Printf("%d bytes enviados para %s\n", len(dados), args[0])
	return nil
}

// comandoStatus pergunta o status como um sistema faria e explica a resposta.
func comandoStatus(args []string) error {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	zpl := fs.Bool("zpl", false, "pergunta com ~HS e ~HQES (impressora de etiqueta)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return errors.New("uso: bobina status [-zpl] HOST:PORTA")
	}
	con, err := net.DialTimeout("tcp", fs.Arg(0), 3*time.Second)
	if err != nil {
		return err
	}
	defer con.Close()

	perguntar := func(nome string, pedido []byte) []byte {
		_, _ = con.Write(pedido)
		_ = con.SetReadDeadline(time.Now().Add(1500 * time.Millisecond))
		var resp []byte
		buf := make([]byte, 512)
		for {
			n, err := con.Read(buf)
			resp = append(resp, buf[:n]...)
			if err != nil || (!*zpl && len(resp) > 0) {
				break
			}
			_ = con.SetReadDeadline(time.Now().Add(150 * time.Millisecond))
		}
		if len(resp) == 0 {
			fmt.Printf("%-12s sem resposta\n", nome)
		}
		return resp
	}

	if *zpl {
		for _, c := range []string{"~HS", "~HQES"} {
			if r := perguntar(c, []byte(c)); len(r) > 0 {
				legivel := strings.NewReplacer("\x02", "<STX>", "\x03", "<ETX>", "\r", "").Replace(string(r))
				fmt.Printf("%s\n%s\n", c, legivel)
			}
		}
		return nil
	}
	nomes := map[byte]string{1: "impressora", 2: "offline", 3: "erro", 4: "papel"}
	for n := byte(1); n <= 4; n++ {
		r := perguntar(fmt.Sprintf("DLE EOT %d", n), []byte{0x10, 0x04, n})
		if len(r) == 0 {
			continue
		}
		b := r[0]
		var sinais []string
		switch n {
		case 1:
			sinais = bits(b, map[byte]string{0x04: "gaveta", 0x08: "OFFLINE"})
		case 2:
			sinais = bits(b, map[byte]string{0x04: "tampa aberta", 0x08: "avanço pelo botão", 0x20: "parou sem papel", 0x40: "erro"})
		case 3:
			sinais = bits(b, map[byte]string{0x08: "guilhotina", 0x20: "erro irrecuperável", 0x40: "erro recuperável"})
		case 4:
			sinais = bits(b, map[byte]string{0x0C: "papel acabando", 0x60: "SEM PAPEL"})
		}
		if len(sinais) == 0 {
			sinais = []string{"ok"}
		}
		fmt.Printf("DLE EOT %d  %-10s %s  %s\n", n, "("+nomes[n]+")", impressora.Hex(r), strings.Join(sinais, ", "))
	}
	return nil
}

func bits(b byte, nomes map[byte]string) []string {
	var r []string
	for _, mascara := range []byte{0x04, 0x08, 0x0C, 0x20, 0x40, 0x60} {
		if nome, ok := nomes[mascara]; ok && b&mascara == mascara {
			r = append(r, nome)
		}
	}
	return r
}
