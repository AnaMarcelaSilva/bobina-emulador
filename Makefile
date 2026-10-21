VERSAO ?= dev
LDFLAGS = -s -w
# Precisa do Go 1.26 ou mais novo (veja go.mod). Para usar outro: make GO=/caminho/go
GO ?= go

.PHONY: todos linux windows windows-console mac testes limpar instalar desinstalar

todos: linux windows mac

linux:
	GOOS=linux GOARCH=amd64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/bobina-linux .

# Sem janela de console: abre direto a tela, como um aplicativo.
windows:
	GOOS=windows GOARCH=amd64 $(GO) build -trimpath -ldflags "$(LDFLAGS) -H windowsgui" -o dist/bobina.exe .

# Com console, para usar "bobina enviar" e "bobina status" no Windows.
windows-console:
	GOOS=windows GOARCH=amd64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/bobina-console.exe .

mac:
	GOOS=darwin GOARCH=arm64 $(GO) build -trimpath -ldflags "$(LDFLAGS)" -o dist/bobina-mac .

testes:
	$(GO) test -race ./...

limpar:
	rm -rf dist

# Instala para o usuário atual no Linux (sem sudo), com o mesmo comando que
# quem baixa o executável usa: "bobina instalar" (veja instalar.go).
instalar: linux
	./dist/bobina-linux instalar

desinstalar: linux
	./dist/bobina-linux desinstalar
