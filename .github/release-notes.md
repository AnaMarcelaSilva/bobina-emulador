## Como baixar e usar

Escolha o seu sistema e siga só a parte dele. Os arquivos estão em **Assets**, no fim desta página. Não precisa instalar Go nem nada de desenvolvimento.

### Windows 10 ou 11

1. Baixe **`bobina-windows-amd64.exe`**.
2. Abra o arquivo baixado com dois cliques.
3. Se aparecer "O Windows protegeu o computador", clique em **Mais informações** e depois em **Executar assim mesmo**. O aviso aparece porque o programa não tem assinatura digital.

### Linux (Ubuntu 22.04 ou mais novo)

1. Baixe **`bobina-linux-amd64`**.
2. No terminal, na pasta do download, instale:
   ```bash
   cd ~/Downloads
   chmod +x bobina-linux-amd64
   ./bobina-linux-amd64 instalar
   ```
3. Procure por **Bobina** nos aplicativos e abra. O arquivo baixado pode ser apagado.

Se aparecer um erro com `libwebkit2gtk-4.1`, instale a biblioteca da janela e repita o passo 2:
```bash
sudo apt install libwebkit2gtk-4.1-0
```

Para atualizar, baixe a versão nova e rode `instalar` de novo. Para remover: `~/.local/bin/bobina desinstalar` (as impressoras configuradas são mantidas).

---

### Conferir o download (opcional)

Serve para ter certeza de que o arquivo não foi alterado. Baixe também **`SHA256SUMS`** para a mesma pasta.

- **Linux:**
  ```bash
  sha256sum -c SHA256SUMS --ignore-missing
  ```
  Deve aparecer `SUCESSO` (ou `OK`).
- **Windows** (PowerShell, na pasta do download):
  ```powershell
  Get-FileHash .\bobina-windows-amd64.exe
  ```
  O código mostrado deve ser igual ao da linha do `.exe` no arquivo `SHA256SUMS`.
