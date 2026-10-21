package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Instalação no Linux só para o usuário atual (sem sudo), seguindo o padrão
// XDG: o executável em ~/.local/bin, o ícone no tema hicolor e o atalho em
// ~/.local/share/applications, onde a busca de aplicativos encontra.

type locaisInstalacao struct {
	executavel, icone, atalho string
	dirAtalhos, dirIcones     string
}

func locais() (locaisInstalacao, error) {
	casa, err := os.UserHomeDir()
	if err != nil {
		return locaisInstalacao{}, err
	}
	dados := os.Getenv("XDG_DATA_HOME")
	if dados == "" {
		dados = filepath.Join(casa, ".local", "share")
	}
	l := locaisInstalacao{
		executavel: filepath.Join(casa, ".local", "bin", "bobina"),
		dirIcones:  filepath.Join(dados, "icons", "hicolor"),
		dirAtalhos: filepath.Join(dados, "applications"),
	}
	l.icone = filepath.Join(l.dirIcones, "scalable", "apps", "bobina.svg")
	l.atalho = filepath.Join(l.dirAtalhos, "bobina.desktop")
	return l, nil
}

func comandoInstalar() error {
	if runtime.GOOS != "linux" {
		fmt.Println("No Windows não precisa instalar: é só abrir o bobina-windows-amd64.exe.")
		fmt.Println("Para fixar, clique com o botão direito na janela aberta (barra de tarefas) e escolha \"Fixar\".")
		return nil
	}
	l, err := locais()
	if err != nil {
		return err
	}
	origem, err := os.Executable()
	if err != nil {
		return err
	}
	if origem, err = filepath.EvalSymlinks(origem); err != nil {
		return err
	}

	// 1. O executável. Grava ao lado e renomeia: funciona mesmo com o
	// Bobina instalado aberto, e nunca deixa um arquivo pela metade.
	if origem != l.executavel {
		if err := copiarExecutavel(origem, l.executavel); err != nil {
			return fmt.Errorf("não foi possível copiar o programa para %s: %w", l.executavel, err)
		}
	}
	// 2. O ícone (já vem embutido no programa).
	if err := gravar(l.icone, icone, 0o644); err != nil {
		return err
	}
	// 3. O atalho do menu de aplicativos.
	atalho := strings.Join([]string{
		"[Desktop Entry]",
		"Type=Application",
		"Name=Bobina",
		"GenericName=Emulador de impressoras",
		"Comment=Emula impressoras de cupom (ESC/POS) e etiqueta (ZPL) para testar sistemas",
		"Keywords=impressora;emulador;escpos;zpl;cupom;etiqueta;printer;",
		"Exec=" + l.executavel,
		"Icon=bobina",
		"Terminal=false",
		"Categories=Development;",
		"StartupWMClass=bobina",
		"",
	}, "\n")
	if err := gravar(l.atalho, []byte(atalho), 0o644); err != nil {
		return err
	}
	atualizarCaches(l)

	fmt.Println("Bobina instalado:")
	fmt.Println("  programa:", l.executavel)
	fmt.Println("  atalho:  ", l.atalho)
	fmt.Println("Procure por \"Bobina\" nos aplicativos. Pode apagar o arquivo baixado.")
	if !noPath(filepath.Dir(l.executavel)) {
		fmt.Println("Para abrir pelo terminal com \"bobina\", inclua ~/.local/bin no PATH (o menu de aplicativos funciona sem isso).")
	}
	return nil
}

func comandoDesinstalar() error {
	if runtime.GOOS != "linux" {
		fmt.Println("No Windows não há instalação: é só apagar o bobina-windows-amd64.exe.")
		return nil
	}
	l, err := locais()
	if err != nil {
		return err
	}
	removidos := 0
	for _, arq := range []string{l.atalho, l.icone, l.executavel} {
		switch err := os.Remove(arq); {
		case err == nil:
			removidos++
		case !errors.Is(err, os.ErrNotExist):
			return err
		}
	}
	atualizarCaches(l)
	if removidos == 0 {
		fmt.Println("O Bobina não estava instalado.")
		return nil
	}
	fmt.Println("Bobina removido do menu de aplicativos.")
	if cfg, err := os.UserConfigDir(); err == nil {
		fmt.Println("As impressoras configuradas continuam em", filepath.Join(cfg, "bobina"), "(apague a pasta se não quiser mais).")
	}
	return nil
}

func copiarExecutavel(origem, destino string) error {
	if err := os.MkdirAll(filepath.Dir(destino), 0o755); err != nil {
		return err
	}
	de, err := os.Open(origem)
	if err != nil {
		return err
	}
	defer de.Close()
	tmp := destino + ".novo"
	para, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(para, de); err != nil {
		para.Close()
		os.Remove(tmp)
		return err
	}
	if err := para.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, destino)
}

func gravar(caminho string, dados []byte, modo os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(caminho), 0o755); err != nil {
		return err
	}
	return os.WriteFile(caminho, dados, modo)
}

// atualizarCaches avisa o sistema do atalho e do ícone novos. Sem o cache de
// ícones atualizado, quando outro programa já criou um, o Bobina aparece com
// o ícone genérico.
func atualizarCaches(l locaisInstalacao) {
	if caminho, err := exec.LookPath("update-desktop-database"); err == nil {
		_ = exec.Command(caminho, l.dirAtalhos).Run()
	}
	if caminho, err := exec.LookPath("gtk-update-icon-cache"); err == nil {
		_ = exec.Command(caminho, "-f", "-t", "-q", l.dirIcones).Run()
	}
}

func noPath(dir string) bool {
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		if filepath.Clean(p) == filepath.Clean(dir) {
			return true
		}
	}
	return false
}
