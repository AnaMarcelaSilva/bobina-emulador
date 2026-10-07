VERSAO ?= dev
LDFLAGS = -s -w
# Precisa do Go 1.26 ou mais novo (veja go.mod). Para usar outro: make GO=/caminho/go
GO ?= go

.PHONY: todos linux windows windows-console mac testes limpar instalar desinstalar

todos: linux windows mac

linux:
	GOOS=linux GOARCH=amd64 $(GO) build -ldflags "$(LDFLAGS)" -o dist/bobina-linux .

# Sem janela de console: abre direto a tela, como um aplicativo.
windows:
	GOOS=windows GOARCH=amd64 $(GO) build -ldflags "$(LDFLAGS) -H windowsgui" -o dist/bobina.exe .

# Com console, para usar "bobina enviar" e "bobina status" no Windows.
windows-console:
	GOOS=windows GOARCH=amd64 $(GO) build -ldflags "$(LDFLAGS)" -o dist/bobina-console.exe .

mac:
	GOOS=darwin GOARCH=arm64 $(GO) build -ldflags "$(LDFLAGS)" -o dist/bobina-mac .

testes:
	$(GO) test -race ./...

limpar:
	rm -rf dist

# Instala para o usuário atual no Linux (sem sudo): o executável vai para
# ~/.local/bin e o atalho para o menu de aplicativos, onde a busca encontra.
BIN_DIR = $(HOME)/.local/bin
APP_DIR = $(HOME)/.local/share/applications
ICONE_DIR = $(HOME)/.local/share/icons/hicolor/scalable/apps

instalar: linux
	install -Dm755 dist/bobina-linux $(BIN_DIR)/bobina
	install -Dm644 internal/web/ui/icone.svg $(ICONE_DIR)/bobina.svg
	mkdir -p $(APP_DIR)
	printf '%s\n' \
		'[Desktop Entry]' \
		'Type=Application' \
		'Name=Bobina' \
		'GenericName=Emulador de impressoras' \
		'Comment=Emula impressoras de cupom (ESC/POS) e etiqueta (ZPL) para testar sistemas' \
		'Keywords=impressora;emulador;escpos;zpl;cupom;etiqueta;printer;' \
		'Exec=$(BIN_DIR)/bobina' \
		'Icon=bobina' \
		'Terminal=false' \
		'Categories=Development;' \
		'StartupWMClass=bobina' \
		> $(APP_DIR)/bobina.desktop
	-update-desktop-database $(APP_DIR) >/dev/null 2>&1
	@# Se outro programa criou um cache de ícones aqui, o sistema só procura
	@# nele: sem atualizar, o Bobina aparece com o ícone genérico.
	-gtk-update-icon-cache -f -t $(HOME)/.local/share/icons/hicolor >/dev/null 2>&1
	@echo "Instalado: procure por Bobina nos aplicativos."

desinstalar:
	rm -f $(BIN_DIR)/bobina $(APP_DIR)/bobina.desktop $(ICONE_DIR)/bobina.svg
	-update-desktop-database $(APP_DIR) >/dev/null 2>&1
