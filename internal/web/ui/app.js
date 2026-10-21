// Tela do Bobina: um balcão de guichês. Cada impressora visível é uma coluna
// com o número do último trabalho em LED e o papel saindo embaixo. Controles e
// diagnóstico ficam atrás de um clique (popover ou painel lateral).
"use strict";

const $ = (s, el = document) => el.querySelector(s);
const $$ = (s, el = document) => [...el.querySelectorAll(s)];
const esc = (s) => String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c]));
const icone = (id, classe = "") => `<svg class="${classe}"><use href="#${id}"/></svg>`;
const hora = (iso) => new Date(iso).toLocaleTimeString("pt-BR");

const app = {
  impressoras: [],
  visiveis: null, // Set com os ids no balcão; null = ainda não carregado
  trabalhos: {}, // id da impressora -> lista, mais novo primeiro
  eventos: {},
  zoom: Number(lerLocal("zoom")) || 1,
  lateral: null, // { tipo: "detalhes", id, aba } ou { tipo: "inspetor", trabalho, aba }
  editando: null,
  imprimirEm: null, // impressora que vai receber o arquivo escolhido
};

function lerLocal(chave) { try { return localStorage.getItem("bobina." + chave); } catch { return null; } }
function gravarLocal(chave, v) { try { localStorage.setItem("bobina." + chave, v); } catch { /* sem armazenamento */ } }

async function api(metodo, url, corpo, cru = false) {
  const opcoes = { method: metodo, headers: {} };
  if (corpo !== undefined) {
    if (cru) opcoes.body = corpo;
    else { opcoes.body = JSON.stringify(corpo); opcoes.headers["Content-Type"] = "application/json"; }
  }
  const r = await fetch(url, opcoes);
  if (r.status === 204) return null;
  const dados = await r.json().catch(() => null);
  if (!r.ok) throw new Error(dados?.erro || `Erro ${r.status}`);
  return dados;
}

function toast(msg, erro = false) {
  const el = document.createElement("div");
  el.className = "toast" + (erro ? " erro" : "");
  el.textContent = msg;
  $("#toasts").append(el);
  setTimeout(() => el.remove(), erro ? 6000 : 2400);
}

// A janela de desktop pode recusar a área de transferência moderna.
async function copiar(texto, aviso = "Copiado") {
  try {
    await navigator.clipboard.writeText(texto);
  } catch {
    const area = document.createElement("textarea");
    area.value = texto;
    document.body.append(area);
    area.select();
    document.execCommand("copy");
    area.remove();
  }
  toast(aviso);
}

const buscar = (id) => app.impressoras.find((i) => i.id === id);

// ------------------------------------------------------------------ estado

const CHAVES = {
  escpos: [
    { campo: "offline", rotulo: "Offline" },
    { campo: "semPapel", rotulo: "Sem papel" },
    { campo: "poucoPapel", rotulo: "Papel acabando", aviso: true },
    { campo: "tampaAberta", rotulo: "Tampa aberta" },
    { campo: "erroGuilhotina", rotulo: "Erro na guilhotina" },
    { campo: "gavetaAberta", rotulo: "Gaveta aberta", neutro: true },
  ],
  zpl: [
    { campo: "offline", rotulo: "Pausada" },
    { campo: "semPapel", rotulo: "Sem etiqueta" },
    { campo: "poucoPapel", rotulo: "Etiquetas acabando", aviso: true },
    { campo: "tampaAberta", rotulo: "Cabeça aberta" },
    { campo: "semRibbon", rotulo: "Sem ribbon" },
    { campo: "erroGuilhotina", rotulo: "Erro no cortador" },
  ],
};

const RESPOSTAS = {
  escpos: [["todas", "Todos os pedidos"], ["escpos", "Só ESC/POS padrão"], ["bematech", "Só Bematech (ENQ)"], ["nenhuma", "Não responde"]],
  zpl: [["todas", "Todos os pedidos"], ["nenhuma", "Não responde"]],
};

const ATRASOS = [[0, "Nenhum"], [200, "200 ms"], [500, "500 ms"], [1000, "1 s"], [3000, "3 s"], [10000, "10 s"]];

// saude resume o estado para o ponto colorido e a faixa de alerta.
function saude(r) {
  const e = r.estado;
  if (!e.ligada) return { classe: "", texto: "Desligada", faixa: null };
  if (r.erro) return { classe: "erro", texto: "Não iniciou", faixa: { classe: "", texto: r.erro } };
  const chaves = CHAVES[r.linguagem];
  const erros = chaves.filter((c) => !c.aviso && !c.neutro && e[c.campo]).map((c) => c.rotulo);
  if (erros.length) return { classe: "erro", texto: erros.join(", "), faixa: { classe: "", texto: erros.join(" · "), normalizar: true } };
  const avisos = chaves.filter((c) => c.aviso && e[c.campo]).map((c) => c.rotulo);
  if (avisos.length) return { classe: "alerta", texto: avisos.join(", "), faixa: { classe: "alerta", texto: avisos.join(" · "), normalizar: true } };
  return { classe: "ok", texto: "Pronta", faixa: null };
}

// fora do padrão: algo no estado que muda as respostas, mesmo sem erro.
const estadoAlterado = (e) => e.respostas !== "todas" || e.atrasoMs > 0 || Object.keys(e).some((k) => !["ligada", "respostas", "atrasoMs"].includes(k) && e[k]);

const NORMAL = { offline: false, semPapel: false, poucoPapel: false, tampaAberta: false, erroGuilhotina: false, gavetaAberta: false, semRibbon: false, respostas: "todas", atrasoMs: 0 };

async function mudarEstado(id, mudanca) {
  try {
    atualizarResumo(await api("PATCH", `/api/impressoras/${id}/estado`, mudanca));
  } catch (err) { toast(err.message, true); }
}

function nomeConexao(r) {
  const ling = r.linguagem === "zpl" ? "ZPL" : r.modoComandos === "bematech" ? "ESC/Bematech" : "ESC/POS";
  if (r.conexao === "rede") return ling;
  if (r.conexao === "serial") return `${ling} · serial`;
  return `${ling} · pasta`;
}

// O que copiar e mostrar como "onde ligar o sistema".
function enderecoUtil(r) {
  if (r.conexao === "rede") return `${r.host === "0.0.0.0" ? "127.0.0.1" : r.host}:${r.porta}`;
  if (r.conexao === "serial") return (r.endereco || r.serial || "").split(" ")[0];
  return r.endereco || r.pasta;
}

// ------------------------------------------------------------------ dígitos de LED

// Sete segmentos por dígito (a b c d e f g), desenhados como polígonos.
const SEGMENTOS = { 0: "abcdef", 1: "bc", 2: "abged", 3: "abgcd", 4: "fgbc", 5: "afgcd", 6: "afgedc", 7: "abc", 8: "abcdefg", 9: "abcdfg", "-": "g" };

function poligono(x, y, horizontal, c) {
  const p = horizontal
    ? [[x, y + 1.5], [x + 1.5, y], [x + c - 1.5, y], [x + c, y + 1.5], [x + c - 1.5, y + 3], [x + 1.5, y + 3]]
    : [[x + 1.5, y], [x + 3, y + 1.5], [x + 3, y + c - 1.5], [x + 1.5, y + c], [x, y + c - 1.5], [x, y + 1.5]];
  return p.map((q) => q.join(",")).join(" ");
}

function ledSVG(numero) {
  const texto = numero ? String(numero % 10000).padStart(4, "0") : "    ";
  let s = "";
  [...texto].forEach((ch, i) => {
    const x = 4 + i * 18;
    const acesos = SEGMENTOS[ch] || "";
    const seg = {
      a: poligono(x + 1.5, 0, true, 11), g: poligono(x + 1.5, 11.5, true, 11), d: poligono(x + 1.5, 23, true, 11),
      f: poligono(x, 1.5, false, 11), b: poligono(x + 11, 1.5, false, 11), e: poligono(x, 13, false, 11), c: poligono(x + 11, 13, false, 11),
    };
    for (const [nome, pts] of Object.entries(seg)) s += `<polygon class="seg${acesos.includes(nome) ? " aceso" : ""}" points="${pts}"/>`;
  });
  return `<svg class="senha" viewBox="0 0 78 26" style="width:${24 * 78 / 26}px" role="img" aria-label="${numero ? "Último trabalho " + numero : "Nenhum trabalho"}"><g transform="skewX(-6) translate(2 0)">${s}</g></svg>`;
}

// ------------------------------------------------------------------ largura das colunas

// Mede o caractere na fonte que o cupom usa de verdade (a do sistema).
let largCaractere = 0;
function medirCaractere() {
  const sonda = document.createElement("span");
  sonda.className = "cupom";
  sonda.style.cssText = "position:absolute;visibility:hidden;width:auto;--zoom:1";
  sonda.textContent = "0".repeat(100);
  document.body.append(sonda);
  largCaractere = sonda.getBoundingClientRect().width / 100;
  sonda.remove();
}

// Abaixo disso a linha de ações não cabe.
const MIN_COLUNA = 384;

// A coluna tem a largura exata da bobina (ou da etiqueta) mais a moldura.
function larguraGuiche(r) {
  const papel = r.linguagem === "zpl"
    ? r.larguraMM * 5.2 * app.zoom + 16
    : (r.colunas + 0.5) * largCaractere * app.zoom + 32;
  if (!largCaractere) medirCaractere();
  return Math.max(MIN_COLUNA, Math.round(papel + 28));
}

// ------------------------------------------------------------------ topo: guichês

function renderGuiches() {
  $("#guiches").innerHTML = app.impressoras.map((r) => {
    const s = saude(r);
    const visivel = app.visiveis.has(r.id);
    return `<button class="ctl" data-id="${esc(r.id)}" aria-pressed="${visivel}"
      title="${visivel ? "Tirar do balcão" : "Mostrar no balcão"}">
      <span class="ponto ${s.classe}" aria-hidden="true"></span>${esc(r.nome)}<span class="so-leitor">, ${esc(s.texto)}</span></button>`;
  }).join("");
}

function alternarVisivel(id) {
  if (app.visiveis.has(id)) app.visiveis.delete(id); else app.visiveis.add(id);
  gravarLocal("visiveis", JSON.stringify([...app.visiveis]));
  renderGuiches();
  renderBalcao();
}

// ------------------------------------------------------------------ balcão

function renderBalcao() {
  const balcao = $("#balcao");
  const visiveis = app.impressoras.filter((r) => app.visiveis.has(r.id));
  const vazio = $("#vazio-geral");
  vazio.hidden = visiveis.length > 0;
  if (!visiveis.length) {
    $("#vazio-geral-texto").textContent = app.impressoras.length
      ? "Escolha na barra de cima quais impressoras ficam no balcão."
      : "Crie uma impressora de cupom ou etiqueta para começar.";
  }
  // Mantém as colunas que continuam (e o papel delas) e cria só as novas.
  const existentes = new Map($$(".guiche", balcao).map((el) => [el.dataset.id, el]));
  const ordem = visiveis.map((r) => {
    let el = existentes.get(r.id);
    existentes.delete(r.id);
    if (!el) {
      el = criarColuna(r);
      renderPapel(r.id, el);
    }
    return el;
  });
  existentes.forEach((el) => el.remove());
  ordem.forEach((el) => balcao.append(el));
  visiveis.forEach((r) => atualizarColuna(r));
}

function criarColuna(r) {
  const el = document.createElement("section");
  el.className = "guiche";
  el.dataset.id = r.id;
  el.innerHTML = `
    <header class="guiche-cab">
      <div class="guiche-linha">
        <div class="guiche-id">
          <h2><span class="ponto" aria-hidden="true"></span><span class="nome"></span></h2>
          <div class="guiche-con"><span class="ling"></span><button data-acao="copiar" title="Copiar endereço"><span></span>${icone("i-copiar")}</button></div>
        </div>
        <div class="visor"></div>
      </div>
      <div class="guiche-acoes">
        <label class="energia" title="Ligar ou desligar">
          <input type="checkbox" data-acao="energia" aria-label="Ligada"><span class="energia-trilho"></span><span class="energia-txt"></span>
        </label>
        <button class="ctl" data-acao="estado" aria-haspopup="true">${icone("i-ajustes")}Estado<span class="marcador" hidden></span>${icone("i-seta", "seta")}</button>
        <button class="ctl" data-acao="imprimir" aria-haspopup="true">${icone("i-impressora")}Imprimir${icone("i-seta", "seta")}</button>
        <span class="espaco"></span>
        <button class="ctl" data-acao="detalhes" title="Atividade e respostas de status">${icone("i-atividade")}<span class="rotulo-opcional">Atividade</span></button>
        <button class="ctl icone" data-acao="mais" aria-haspopup="true" title="Opções da impressora" aria-label="Opções da impressora">${icone("i-engrenagem")}</button>
      </div>
    </header>
    <div class="guiche-alerta" role="alert" hidden>${icone("i-alerta")}<span></span><button class="ctl" data-acao="normalizar">Normalizar</button></div>
    <div class="guiche-papel"></div>`;
  return el;
}

// atualizarColuna muda só o cabeçalho e as ações; o papel fica como está.
function atualizarColuna(r) {
  const el = $(`.guiche[data-id="${CSS.escape(r.id)}"]`);
  if (!el) return;
  const s = saude(r);
  el.style.setProperty("--largura", larguraGuiche(r) + "px");
  const desligadoAntes = el.classList.contains("desligado");
  el.classList.toggle("desligado", !r.estado.ligada);
  $(".guiche-id .ponto", el).className = "ponto " + s.classe;
  $(".guiche-id .nome", el).textContent = r.nome;
  $(".guiche-id h2", el).title = s.texto;
  $(".ling", el).textContent = nomeConexao(r) + " ·";
  $(".guiche-con button span", el).textContent = enderecoUtil(r);
  const ultimo = app.trabalhos[r.id]?.[0]?.numero || 0;
  const visor = $(".visor", el);
  if (visor.dataset.n !== String(ultimo)) {
    visor.innerHTML = ledSVG(ultimo);
    visor.dataset.n = ultimo;
  }
  const energia = $("[data-acao=energia]", el);
  energia.checked = r.estado.ligada;
  energia.setAttribute("aria-label", r.estado.ligada ? "Ligada" : "Desligada");
  $(".energia-txt", el).textContent = r.estado.ligada ? "Ligada" : "Desligada";
  // Desligada não recebe nada: imprimir e simular estado não fazem sentido.
  $("[data-acao=estado]", el).disabled = !r.estado.ligada;
  $("[data-acao=imprimir]", el).disabled = !r.estado.ligada;
  const marcador = $(".marcador", el);
  const alteradas = CHAVES[r.linguagem].filter((c) => r.estado[c.campo]).length + (r.estado.respostas !== "todas") + (r.estado.atrasoMs > 0);
  marcador.hidden = !r.estado.ligada || alteradas === 0;
  marcador.textContent = alteradas;
  marcador.title = `${alteradas} simulação(ões) ativa(s)`;
  const faixa = $(".guiche-alerta", el);
  faixa.hidden = !s.faixa;
  if (s.faixa) {
    faixa.className = "guiche-alerta " + s.faixa.classe;
    $("span", faixa).textContent = s.faixa.texto;
    $("button", faixa).hidden = !s.faixa.normalizar;
  }
  if (desligadoAntes !== !r.estado.ligada && !(app.trabalhos[r.id] || []).length) renderPapel(r.id, el);
}

function renderPapel(id, coluna = $(`.guiche[data-id="${CSS.escape(id)}"]`)) {
  if (!coluna) return;
  const r = buscar(id);
  const lista = app.trabalhos[id] || [];
  const papel = $(".guiche-papel", coluna);
  if (!lista.length) {
    papel.innerHTML = textoVazio(r);
    return;
  }
  papel.replaceChildren(...lista.map((t) => cartaoTrabalho(t)));
}

function textoVazio(r) {
  if (!r.estado.ligada) {
    return `<div class="vazio-guiche">
      <p>Desligada: nada é recebido nem respondido.</p>
      <button class="ctl" data-acao="ligar">${icone("i-energia")}Ligar</button>
    </div>`;
  }
  const oQue = r.linguagem === "zpl" ? "etiquetas" : "cupons";
  const onde = r.conexao === "pasta" ? `Esperando arquivos .prn em` : `Esperando ${oQue} em`;
  return `<div class="vazio-guiche">
    <p>${onde} <button class="endereco" data-acao="copiar" title="Copiar"><span>${esc(enderecoUtil(r))}</span>${icone("i-copiar")}</button></p>
    <small>Ou arraste um arquivo para cá.</small>
    <button class="ctl" data-acao="exemplo">${icone("i-impressora")}Imprimir exemplo</button>
  </div>`;
}

function cartaoTrabalho(t, novo = false) {
  const el = document.createElement("article");
  el.className = "trabalho" + (t.impresso ? "" : " nao-impresso") + (novo ? " novo" : "");
  el.dataset.trabalho = t.id;
  const selos = [];
  if (!t.impresso) selos.push(`<span class="selo falha">${icone("i-alerta")}Não impresso: ${esc(t.motivo)}</span>`);
  if (t.avisos?.length) selos.push(`<span class="selo aviso">${icone("i-alerta")}${t.avisos.length} aviso${t.avisos.length > 1 ? "s" : ""}</span>`);
  if (t.quantidade > 1) selos.push(`<span class="selo qtd">${t.quantidade} cópias</span>`);
  const visual = t.linguagem === "zpl"
    ? `<div class="papel etiqueta-papel" role="button" tabindex="0" style="--mm:${t.larguraMM}" aria-label="Inspecionar etiqueta ${t.numero}">${t.visual}</div>`
    : `<div class="papel" role="button" tabindex="0" aria-label="Inspecionar cupom ${t.numero}">${t.visual}</div>`;
  el.innerHTML = `<div class="trabalho-meta"><b>${String(t.numero).padStart(4, "0")}</b><span>${hora(t.recebido)}</span><span>${esc(t.origem)}</span>${selos.join("")}</div>${visual}`;
  return el;
}

function adicionarTrabalho(t) {
  const lista = (app.trabalhos[t.impressora] ||= []);
  if (lista.some((x) => x.id === t.id)) return;
  lista.unshift(t);
  if (lista.length > 200) lista.length = 200;
  const coluna = $(`.guiche[data-id="${CSS.escape(t.impressora)}"]`);
  if (!coluna) return;
  const papel = $(".guiche-papel", coluna);
  papel.querySelector(".vazio-guiche")?.remove();
  papel.prepend(cartaoTrabalho(t, true));
  while (papel.children.length > 200) papel.lastChild.remove();
  papel.scrollTo({ top: 0, behavior: "smooth" });
  // O sinal do painel: o número troca e o cabeçalho pisca uma vez.
  const r = buscar(t.impressora);
  if (r) atualizarColuna(r);
  coluna.classList.remove("chamou");
  void coluna.offsetWidth;
  coluna.classList.add("chamou");
}

// ------------------------------------------------------------------ popovers

// Quem abriu o popover com o mouse não precisa do anel de foco ao fechar.
let ultimoFoiTeclado = false;
document.addEventListener("keydown", () => { ultimoFoiTeclado = true; }, true);
document.addEventListener("pointerdown", () => { ultimoFoiTeclado = false; }, true);

function abrirPop(ancora, html, aoClicar) {
  const pop = $("#pop");
  if (pop.matches(":popover-open") && pop.dataset.ancora === ancora.dataset.chave) {
    pop.hidePopover();
    return;
  }
  pop.innerHTML = html;
  pop.dataset.ancora = ancora.dataset.chave || "";
  pop.onclick = (e) => aoClicar(e, pop);
  pop.onchange = (e) => aoClicar(e, pop);
  pop.showPopover();
  const a = ancora.getBoundingClientRect();
  const p = pop.getBoundingClientRect();
  const x = Math.min(Math.max(8, a.left), innerWidth - p.width - 8);
  const y = a.bottom + 6 + p.height > innerHeight ? Math.max(8, a.top - p.height - 6) : a.bottom + 6;
  pop.style.left = x + "px";
  pop.style.top = y + "px";
  ancora.setAttribute("aria-expanded", "true");
  pop.addEventListener("toggle", function fechou(ev) {
    if (ev.newState !== "closed") return;
    ancora.setAttribute("aria-expanded", "false");
    pop.removeEventListener("toggle", fechou);
    if (!ultimoFoiTeclado) requestAnimationFrame(() => ancora.blur());
  });
  pop.querySelector("input, select, button")?.focus();
}

function popEstado(r, ancora) {
  const e = r.estado;
  const linhas = CHAVES[r.linguagem].map((c) => `
    <label class="linha-chave" style="--tom:${c.aviso ? "var(--alerta)" : c.neutro ? "var(--foco)" : "var(--erro)"}">
      <span>${c.rotulo}</span><input type="checkbox" data-campo="${c.campo}" ${e[c.campo] ? "checked" : ""}><span class="trilho"></span>
    </label>`).join("");
  const opcoes = (lista, atual) => lista.map(([v, t]) => `<option value="${v}" ${String(v) === String(atual) ? "selected" : ""}>${t}</option>`).join("");
  const html = `<div class="pop-titulo">Simular</div>${linhas}
    <div class="pop-sep"></div>
    <div class="linha-campo"><span>Responde ao status</span><select data-campo="respostas">${opcoes(RESPOSTAS[r.linguagem], e.respostas)}</select></div>
    <div class="linha-campo"><span>Atraso da resposta</span><select data-campo="atrasoMs">${opcoes(ATRASOS, e.atrasoMs)}</select></div>
    ${estadoAlterado(e) ? `<div class="pop-sep"></div><button class="pop-item" data-normalizar>Voltar ao normal</button>` : ""}`;
  abrirPop(ancora, html, (ev, pop) => {
    if (ev.type === "click" && ev.target.closest("[data-normalizar]")) {
      mudarEstado(r.id, NORMAL);
      pop.hidePopover();
      return;
    }
    if (ev.type !== "change") return;
    const alvo = ev.target;
    const campo = alvo.dataset.campo;
    if (!campo) return;
    const valor = alvo.type === "checkbox" ? alvo.checked : campo === "atrasoMs" ? Number(alvo.value) : alvo.value;
    mudarEstado(r.id, { [campo]: valor });
  });
}

function popImprimir(r, ancora) {
  const exemplo = r.linguagem === "zpl" ? "Etiqueta de comanda com código de barras e QR" : "Cupom com acentos, QR Code e código de barras";
  const html = `
    <button class="pop-item" data-amostra="exemplo">${icone("i-impressora")}<span>Exemplo completo<small>${exemplo}</small></span></button>
    <button class="pop-item" data-amostra="problemas">${icone("i-alerta")}<span>Com erros comuns<small>UTF-8, linha longa, código inválido</small></span></button>
    <div class="pop-sep"></div>
    <button class="pop-item" data-amostra="arquivo">${icone("i-arquivo")}<span>Enviar arquivo…<small>.prn, .txt, .bin, .zpl</small></span></button>`;
  abrirPop(ancora, html, (ev, pop) => {
    const b = ev.target.closest("[data-amostra]");
    if (ev.type !== "click" || !b) return;
    pop.hidePopover();
    imprimirAmostra(r.id, b.dataset.amostra);
  });
}

function popMais(r, ancora) {
  const html = `
    <button class="pop-item" data-m="configurar">${icone("i-engrenagem")}Configurar…</button>
    <button class="pop-item" data-m="limpar">${icone("i-lixo")}Limpar o papel</button>
    <div class="pop-sep"></div>
    <button class="pop-item" data-m="ocultar">${icone("i-olho-fechado")}Tirar do balcão</button>`;
  abrirPop(ancora, html, async (ev, pop) => {
    const b = ev.target.closest("[data-m]");
    if (ev.type !== "click" || !b) return;
    pop.hidePopover();
    switch (b.dataset.m) {
      case "configurar": abrirConfig(r); break;
      case "limpar":
        await api("DELETE", `/api/impressoras/${r.id}/trabalhos`).catch((err) => toast(err.message, true));
        app.trabalhos[r.id] = [];
        app.eventos[r.id] = [];
        renderPapel(r.id);
        atualizarColuna(r); // o visor volta a ficar apagado
        if (app.lateral?.id === r.id) renderLateral();
        break;
      case "ocultar": alternarVisivel(r.id); break;
    }
  });
}

// ------------------------------------------------------------------ imprimir

async function imprimirAmostra(id, amostra) {
  if (amostra === "arquivo") {
    app.imprimirEm = id;
    $("#arquivo").click();
    return;
  }
  try { await api("POST", `/api/impressoras/${id}/teste/${amostra}`); }
  catch (err) { toast(err.message, true); }
}

async function imprimirArquivo(id, arquivo) {
  try {
    const dados = await arquivo.arrayBuffer();
    const novos = await api("POST", `/api/impressoras/${id}/imprimir?origem=${encodeURIComponent("arquivo " + arquivo.name)}`, dados, true);
    if (!novos.length) toast(`${arquivo.name}: nada para imprimir, só comandos sem conteúdo`);
  } catch (err) { toast(err.message, true); }
}

// ------------------------------------------------------------------ painel lateral

async function carregarEventos(id) {
  if (!app.eventos[id]) app.eventos[id] = await api("GET", `/api/impressoras/${id}/eventos`).catch(() => []);
}

async function abrirDetalhes(id, aba = "atividade") {
  await carregarEventos(id);
  app.lateral = { tipo: "detalhes", id, aba };
  renderLateral();
}

async function abrirInspetor(trabalhoId) {
  try {
    const t = await api("GET", `/api/trabalhos/${trabalhoId}`);
    app.lateral = { tipo: "inspetor", trabalho: t, aba: "comandos" };
    renderLateral();
  } catch (err) { toast(err.message, true); }
}

function fecharLateral() {
  app.lateral = null;
  $("#lateral").hidden = true;
  $("#veu").hidden = true;
}

function renderLateral() {
  const l = app.lateral;
  if (!l) return;
  $("#lateral").hidden = false;
  $("#veu").hidden = false;
  if (l.tipo === "detalhes") {
    const r = buscar(l.id);
    if (!r) { fecharLateral(); return; }
    $("#lat-titulo").textContent = r.nome;
    $("#lat-sub").innerHTML = `<span>${esc(nomeConexao(r))}</span><span><b>${esc(r.endereco || enderecoUtil(r))}</b></span><span>${esc(saude(r).texto)}</span>`;
    $("#lat-avisos").innerHTML = "";
    abas([["atividade", "Atividade"], ["respostas", "Respostas de status"], ["api", "Usar em testes"]], l.aba);
    $("#lat-rodape").innerHTML = "";
    if (l.aba === "atividade") renderAtividade(r);
    else if (l.aba === "respostas") renderRespostas(r);
    else renderAPI(r);
    return;
  }
  const t = l.trabalho;
  const tipo = t.linguagem === "zpl" ? "Etiqueta" : "Cupom";
  const r = buscar(t.impressora);
  $("#lat-titulo").innerHTML = `${tipo} ${String(t.numero).padStart(4, "0")} ${t.impresso ? "" : `<span class="selo falha">não impresso</span>`}`;
  $("#lat-sub").innerHTML = [
    ["Impressora", r?.nome || t.impressora], ["Recebido", new Date(t.recebido).toLocaleString("pt-BR")], ["Origem", t.origem], ["Tamanho", `${t.bytes} bytes`],
  ].map(([k, v]) => `<span>${k}: <b>${esc(v)}</b></span>`).join("");
  const cartoes = [];
  if (!t.impresso) cartoes.push(`<div class="aviso falha">${icone("i-alerta")}<div>A impressora estava com <b>${esc(t.motivo)}</b>: numa impressora de verdade nada sairia no papel.</div></div>`);
  for (const a of t.avisos || []) cartoes.push(`<div class="aviso">${icone("i-alerta")}<div>${esc(a)}</div></div>`);
  $("#lat-avisos").innerHTML = cartoes.join("");
  abas([["comandos", `Comandos (${t.comandos.length})`], ["bytes", "Bytes"], ["texto", "Texto"]], l.aba);
  $("#lat-rodape").innerHTML = `<button class="ctl" data-l="reimprimir">${icone("i-impressora")}Reimprimir</button>
    <button class="ctl" data-l="salvar">${icone("i-baixar")}Salvar .prn</button>`;
  renderInspetorAba(t, l.aba);
}

function abas(lista, ativa) {
  $("#lat-abas").innerHTML = lista.map(([id, rotulo]) =>
    `<button class="aba" role="tab" data-aba="${id}" aria-selected="${id === ativa}">${rotulo}</button>`).join("");
}

function itemEvento(e, novo = false) {
  const li = document.createElement("li");
  li.className = `t-${e.tipo}` + (novo ? " novo" : "");
  const tipos = { conexao: "conexão", status: "status", trabalho: "impressão", estado: "estado", aviso: "aviso", erro: "erro" };
  li.innerHTML = `<span class="hora">${hora(e.hora)}</span>
    <span><span class="tipo">${tipos[e.tipo] || e.tipo}</span>${esc(e.texto)}${e.trabalho ? ` · <a data-trabalho="${e.trabalho}">ver</a>` : ""}${e.detalhe ? `<span class="det">${esc(e.detalhe)}</span>` : ""}</span>`;
  return li;
}

function renderAtividade(r) {
  const lista = app.eventos[r.id] || [];
  const corpo = $("#lat-corpo");
  if (!lista.length) {
    corpo.innerHTML = `<ol class="atividade"><li class="vazio">Conexões, pedidos de status e impressões aparecem aqui.</li></ol>`;
    return;
  }
  const ol = document.createElement("ol");
  ol.className = "atividade";
  ol.replaceChildren(...lista.slice(0, 300).map((e) => itemEvento(e)));
  corpo.replaceChildren(ol);
}

function renderRespostas(r) {
  $("#lat-corpo").innerHTML = `<p class="dica">O que esta impressora responde agora a cada pedido de status. Mude o estado e a resposta muda junto.</p>
    <table class="tabela"><thead><tr><th>Pedido</th><th>Resposta</th></tr></thead><tbody>${
    (r.respostas || []).map((l) => `<tr><td><div class="mono">${esc(l.pedido)}</div><div class="expl mono">${esc(l.bytes)}</div></td>
      <td><span class="resp">${esc(l.resposta)}</span><span class="expl">${esc(l.explicacao)}</span></td></tr>`).join("")
  }</tbody></table>`;
}

function renderAPI(r) {
  const url = `${location.origin}/api/impressoras/${r.id}`;
  const ultimo = app.trabalhos[r.id]?.[0]?.id || 0;
  const end = enderecoUtil(r);
  const blocos = [
    ["Mudar o estado", "As mesmas chaves do botão Estado. Mande só o que muda.", `curl -X PATCH ${url}/estado -d '{"semPapel": true}'`],
    ["Esperar o próximo trabalho", "Espera até chegar algo depois do trabalho indicado. A resposta traz o texto impresso e os avisos.", `curl "${url}/trabalhos?desde=${ultimo}&aguardar=10s"`],
    ["Imprimir um arquivo pela API", "Os bytes passam pelo mesmo caminho de uma conexão de verdade.", `curl --data-binary @cupom.prn ${url}/imprimir`],
  ];
  if (r.conexao === "rede") {
    blocos.push(r.linguagem === "zpl"
      ? ["Perguntar o status direto na porta", "Como o sistema faria.", `printf '~HQES' | nc -q1 ${end.replace(":", " ")}`]
      : ["Perguntar o status direto na porta", "Como o sistema faria: DLE EOT 1.", `printf '\\x10\\x04\\x01' | nc -q1 ${end.replace(":", " ")} | xxd`]);
  }
  blocos.push(["Voltar ao normal", "Útil no fim de cada teste.", `curl -X PATCH ${url}/estado -d '${JSON.stringify({ ligada: true, ...NORMAL })}'`]);
  $("#lat-corpo").innerHTML = blocos.map(([t, d, c]) =>
    `<div class="bloco-api"><h3>${esc(t)}</h3><p>${esc(d)}</p><pre>${esc(c)}<button data-copiar>copiar</button></pre></div>`).join("");
}

async function renderInspetorAba(t, aba) {
  const corpo = $("#lat-corpo");
  if (aba === "comandos") {
    corpo.innerHTML = `<table class="cmd-lista"><tbody>${t.comandos.map((c) => `<tr class="tipo-${esc(c.tipo)}">
      <td>${c.offset}</td><td><div class="nome">${esc(c.nome)}</div></td>
      <td>${esc(c.descricao)}${c.texto ? `<div><span class="txt">${esc(c.texto)}</span></div>` : ""}<div class="hex">${esc(c.hex)}</div></td>
    </tr>`).join("")}</tbody></table>`;
  } else if (aba === "bytes") {
    corpo.innerHTML = `<pre class="hexdump">Carregando…</pre>`;
    const bytes = new Uint8Array(await (await fetch(`/api/trabalhos/${t.id}/bruto`)).arrayBuffer());
    corpo.innerHTML = `<pre class="hexdump">${hexdump(bytes)}</pre>`;
  } else {
    corpo.innerHTML = `<pre class="texto-puro">${esc(t.texto || "(sem texto)")}</pre>`;
  }
}

function hexdump(b) {
  const linhas = [];
  for (let i = 0; i < b.length; i += 16) {
    const fatia = [...b.slice(i, i + 16)];
    const hex = fatia.map((x) => x.toString(16).padStart(2, "0")).join(" ").padEnd(47, " ");
    const asc = fatia.map((x) => (x >= 32 && x < 127 ? String.fromCharCode(x) : ".")).join("");
    linhas.push(`<span class="off">${i.toString(16).padStart(6, "0")}</span>  ${hex}  <span class="asc">${esc(asc)}</span>`);
  }
  return linhas.join("\n") || "(vazio)";
}

// ------------------------------------------------------------------ dados ao vivo

function atualizarResumo(r) {
  const i = app.impressoras.findIndex((x) => x.id === r.id);
  if (i >= 0) app.impressoras[i] = r; else app.impressoras.push(r);
  renderGuiches();
  atualizarColuna(r);
  const l = app.lateral;
  if (l?.tipo === "detalhes" && l.id === r.id && l.aba === "respostas") renderRespostas(r);
}

function adicionarEvento(e) {
  const lista = app.eventos[e.impressora];
  if (!lista) return; // ainda não carregado; vem completo quando abrir
  lista.unshift(e);
  if (lista.length > 400) lista.length = 400;
  const l = app.lateral;
  if (l?.tipo !== "detalhes" || l.id !== e.impressora || l.aba !== "atividade") return;
  const ol = $("#lat-corpo .atividade");
  if (!ol) return;
  ol.querySelector(".vazio")?.remove();
  ol.prepend(itemEvento(e, true));
}

async function carregar() {
  app.impressoras = await api("GET", "/api/impressoras");
  if (!app.visiveis) {
    let salvos = null;
    try { salvos = JSON.parse(lerLocal("visiveis")); } catch { /* nada salvo */ }
    app.visiveis = new Set(Array.isArray(salvos) ? salvos : app.impressoras.map((r) => r.id));
  }
  const listas = await Promise.all(app.impressoras.map((r) => api("GET", `/api/impressoras/${r.id}/trabalhos`).catch(() => [])));
  app.impressoras.forEach((r, i) => { app.trabalhos[r.id] = listas[i]; });
  app.eventos = {};
  renderGuiches();
  $("#balcao").replaceChildren();
  renderBalcao();
}

function conectarAoVivo() {
  const fonte = new EventSource("/api/notificacoes");
  let caiu = false;
  fonte.onopen = () => {
    $("#ao-vivo").classList.remove("caiu");
    $("#ao-vivo").title = "Ligado ao emulador";
    if (caiu) { caiu = false; carregar().catch(() => {}); }
  };
  fonte.onerror = () => {
    caiu = true;
    $("#ao-vivo").classList.add("caiu");
    $("#ao-vivo").title = "Sem ligação com o emulador";
  };
  fonte.onmessage = (m) => {
    const n = JSON.parse(m.data);
    switch (n.tipo) {
      case "impressora": atualizarResumo(n.dados); break;
      case "trabalho": adicionarTrabalho(n.dados); break;
      case "evento": adicionarEvento(n.dados); break;
      case "removida":
        app.impressoras = app.impressoras.filter((i) => i.id !== n.impressora);
        app.visiveis.delete(n.impressora);
        if (app.lateral?.id === n.impressora) fecharLateral();
        renderGuiches();
        renderBalcao();
        break;
    }
  };
}

// ------------------------------------------------------------------ configuração

const CORES = ["#0ea5e9", "#a855f7", "#f59e0b", "#10b981", "#ef4444", "#ec4899", "#6366f1", "#64748b"];

async function abrirConfig(r) {
  app.editando = r || null;
  const f = $("#form-impressora");
  f.reset();
  $("#erro-form").hidden = true;
  $("#dlg-titulo").textContent = r ? `Configurar ${r.nome}` : "Nova impressora";
  $("#btn-remover").hidden = !r;

  const [portas, paginas] = await Promise.all([
    api("GET", "/api/portas-seriais").catch(() => []),
    api("GET", "/api/paginas-codigo").catch(() => ({})),
  ]);
  $("#sel-serial").innerHTML = `<option value="">Criar porta virtual (Linux)</option>` + (portas || []).map((p) => `<option>${esc(p)}</option>`).join("");
  $("#sel-pagina").innerHTML = Object.entries(paginas).sort((a, b) => a[0] - b[0]).map(([n, nome]) => `<option value="${n}">${n} · ${esc(nome)}</option>`).join("");

  const c = r || { linguagem: "escpos", conexao: "rede", host: "0.0.0.0", porta: proximaPorta(), baud: 9600, larguraPapel: 80, colunas: 48, paginaCodigo: 2, dpmm: 8, larguraMM: 100, alturaMM: 150, cor: CORES[app.impressoras.length % CORES.length] };
  f.nome.value = c.nome || "";
  f.linguagem.value = c.linguagem;
  f.conexao.value = c.conexao;
  f.host.value = c.host || "0.0.0.0";
  f.porta.value = c.porta || proximaPorta();
  if (c.serial && !(portas || []).includes(c.serial)) $("#sel-serial").insertAdjacentHTML("beforeend", `<option>${esc(c.serial)}</option>`);
  f.serial.value = c.serial || "";
  f.baud.value = String(c.baud || 9600);
  f.pasta.value = c.pasta || "";
  f.bobina.value = `${c.larguraPapel || 80}-${c.colunas || 48}`;
  if (!f.bobina.value) f.bobina.value = "80-48";
  f.paginaCodigo.value = String(c.paginaCodigo ?? 2);
  f.modoComandos.value = c.modoComandos === "bematech" ? "bematech" : "escpos";
  f.dpmm.value = String(c.dpmm || 8);
  f.larguraMM.value = c.larguraMM || 100;
  f.alturaMM.value = c.alturaMM || 150;
  mostrarCamposConfig();
  $("#dlg-impressora").showModal();
  f.nome.focus();
}

function proximaPorta() {
  const usadas = new Set(app.impressoras.filter((i) => i.conexao === "rede").map((i) => i.porta));
  let p = 9100;
  while (usadas.has(p)) p++;
  return p;
}

function mostrarCamposConfig() {
  const f = $("#form-impressora");
  const ativos = [f.conexao.value, f.linguagem.value];
  $$("[data-se]", f).forEach((el) => { el.hidden = !ativos.includes(el.dataset.se); });
}

async function salvarConfig(ev) {
  ev.preventDefault();
  const f = $("#form-impressora");
  const [larguraPapel, colunas] = f.bobina.value.split("-").map(Number);
  const cfg = {
    nome: f.nome.value.trim(), linguagem: f.linguagem.value, conexao: f.conexao.value,
    host: f.host.value, porta: Number(f.porta.value), serial: f.serial.value, baud: Number(f.baud.value),
    pasta: f.pasta.value.trim(), larguraPapel, colunas, paginaCodigo: Number(f.paginaCodigo.value || 2),
    modoComandos: f.modoComandos.value,
    dpmm: Number(f.dpmm.value), larguraMM: Number(f.larguraMM.value), alturaMM: Number(f.alturaMM.value),
    cor: app.editando?.cor || CORES[app.impressoras.length % CORES.length],
  };
  const editando = app.editando;
  try {
    const r = editando
      ? await api("PUT", `/api/impressoras/${editando.id}`, cfg)
      : await api("POST", "/api/impressoras", cfg);
    $("#dlg-impressora").close();
    if (!editando) {
      app.visiveis.add(r.id);
      gravarLocal("visiveis", JSON.stringify([...app.visiveis]));
      app.trabalhos[r.id] = [];
    }
    atualizarResumo(r);
    renderBalcao();
    renderPapel(r.id);
    toast(editando ? "Configuração salva" : `${r.nome} criada${r.erro ? ", mas não iniciou: " + r.erro : ""}`, !!r.erro);
  } catch (err) {
    $("#erro-form").textContent = err.message;
    $("#erro-form").hidden = false;
  }
}

function confirmar(titulo, texto) {
  return new Promise((ok) => {
    const d = $("#dlg-confirmar");
    $("#conf-titulo").textContent = titulo;
    $("#conf-texto").textContent = texto;
    d.returnValue = "";
    d.onclose = () => ok(d.returnValue === "sim");
    d.showModal();
  });
}

// ------------------------------------------------------------------ tema e zoom

const TEMAS = [["", "automático"], ["claro", "claro"], ["escuro", "escuro"]];
function aplicarTema(t) {
  if (t) document.documentElement.dataset.tema = t; else delete document.documentElement.dataset.tema;
  $("#btn-tema").title = "Tema: " + TEMAS.find(([v]) => v === t)[1];
}
function aplicarZoom() {
  document.documentElement.style.setProperty("--zoom", app.zoom);
  gravarLocal("zoom", app.zoom);
  app.impressoras.forEach((r) => atualizarColuna(r));
}

// ------------------------------------------------------------------ eventos da tela

function ligarEventos() {
  $("#guiches").addEventListener("click", (e) => {
    const b = e.target.closest("[data-id]");
    if (b) alternarVisivel(b.dataset.id);
  });
  $("#btn-nova").onclick = () => abrirConfig(null);

  // Ações das colunas, por delegação.
  $("#balcao").addEventListener("click", (e) => {
    const coluna = e.target.closest(".guiche");
    if (!coluna) return;
    const r = buscar(coluna.dataset.id);
    const alvo = e.target.closest("[data-acao]");
    const papel = e.target.closest(".papel");
    if (papel) { abrirInspetor(papel.closest(".trabalho").dataset.trabalho); return; }
    if (!alvo || !r) return;
    alvo.dataset.chave = `${r.id}:${alvo.dataset.acao}`;
    switch (alvo.dataset.acao) {
      case "copiar": copiar(enderecoUtil(r), `Copiado: ${enderecoUtil(r)}`); break;
      case "estado": popEstado(r, alvo); break;
      case "imprimir": popImprimir(r, alvo); break;
      case "mais": popMais(r, alvo); break;
      case "detalhes": abrirDetalhes(r.id); break;
      case "normalizar": mudarEstado(r.id, NORMAL); break;
      case "exemplo": imprimirAmostra(r.id, "exemplo"); break;
      case "ligar": mudarEstado(r.id, { ligada: true }); break;
    }
  });
  $("#balcao").addEventListener("change", (e) => {
    if (e.target.dataset.acao !== "energia") return;
    mudarEstado(e.target.closest(".guiche").dataset.id, { ligada: e.target.checked });
  });
  $("#balcao").addEventListener("keydown", (e) => {
    const papel = e.target.closest?.(".papel");
    if (papel && (e.key === "Enter" || e.key === " ")) { e.preventDefault(); abrirInspetor(papel.closest(".trabalho").dataset.trabalho); }
  });

  // Arrastar arquivo para uma coluna imprime nela.
  let alvoArraste = null;
  $("#balcao").addEventListener("dragover", (e) => {
    const coluna = e.target.closest(".guiche");
    if (!coluna) return;
    e.preventDefault();
    if (alvoArraste !== coluna) { alvoArraste?.classList.remove("arrastando"); alvoArraste = coluna; coluna.classList.add("arrastando"); }
  });
  $("#balcao").addEventListener("dragleave", (e) => {
    if (alvoArraste && !alvoArraste.contains(e.relatedTarget)) { alvoArraste.classList.remove("arrastando"); alvoArraste = null; }
  });
  $("#balcao").addEventListener("drop", (e) => {
    const coluna = e.target.closest(".guiche");
    alvoArraste?.classList.remove("arrastando");
    alvoArraste = null;
    if (!coluna) return;
    e.preventDefault();
    for (const a of e.dataTransfer.files) imprimirArquivo(coluna.dataset.id, a);
  });
  $("#arquivo").onchange = (e) => {
    for (const a of e.target.files) imprimirArquivo(app.imprimirEm, a);
    e.target.value = "";
  };

  // Painel lateral
  $("#lat-fechar").onclick = fecharLateral;
  $("#veu").onclick = fecharLateral;
  $("#lat-abas").addEventListener("click", async (e) => {
    const b = e.target.closest("[data-aba]");
    if (!b || !app.lateral) return;
    app.lateral.aba = b.dataset.aba;
    renderLateral();
  });
  $("#lateral").addEventListener("click", async (e) => {
    const ver = e.target.closest("[data-trabalho]");
    if (ver) { abrirInspetor(ver.dataset.trabalho); return; }
    const botaoCopiar = e.target.closest("[data-copiar]");
    if (botaoCopiar) { copiar(botaoCopiar.parentElement.firstChild.textContent); return; }
    const acao = e.target.closest("[data-l]")?.dataset.l;
    const t = app.lateral?.trabalho;
    if (!acao || !t) return;
    if (acao === "salvar") {
      try { toast(`Salvo em ${(await api("POST", `/api/trabalhos/${t.id}/salvar`)).caminho}`); }
      catch (err) { toast(err.message, true); }
    } else if (acao === "reimprimir") {
      const dados = await (await fetch(`/api/trabalhos/${t.id}/bruto`)).arrayBuffer();
      fecharLateral();
      await api("POST", `/api/impressoras/${t.impressora}/imprimir?origem=${encodeURIComponent("reimpressão do " + String(t.numero).padStart(4, "0"))}`, dados, true)
        .catch((err) => toast(err.message, true));
    }
  });
  document.addEventListener("keydown", (e) => {
    if (e.key === "Escape" && app.lateral && !$("#pop").matches(":popover-open") && !document.querySelector("dialog[open]")) fecharLateral();
  });

  // Configuração
  $("#form-impressora").addEventListener("change", (e) => {
    if (e.target.name === "conexao" || e.target.name === "linguagem") mostrarCamposConfig();
  });
  $("#form-impressora").addEventListener("submit", salvarConfig);
  // Ao fechar um diálogo o foco volta ao botão que o abriu; sem teclado, não precisa do anel.
  $$("dialog").forEach((d) => d.addEventListener("close", () => requestAnimationFrame(() => document.activeElement?.blur())));
  $("#btn-cancelar").onclick = () => $("#dlg-impressora").close();
  $("#btn-remover").onclick = async () => {
    const r = app.editando;
    $("#dlg-impressora").close();
    if (!(await confirmar(`Remover ${r.nome}?`, "A impressora para de escutar e os trabalhos guardados somem."))) return;
    try { await api("DELETE", `/api/impressoras/${r.id}`); }
    catch (err) { toast(err.message, true); }
  };
  document.addEventListener("click", (e) => {
    if (e.target.closest("[data-acao=nova]")) abrirConfig(null);
  });

  // Tema e zoom
  $("#btn-tema").onclick = () => {
    const i = TEMAS.findIndex(([v]) => v === (lerLocal("tema") || ""));
    const novo = TEMAS[(i + 1) % TEMAS.length][0];
    gravarLocal("tema", novo);
    aplicarTema(novo);
  };
  $("#zoom-mais").onclick = () => { app.zoom = Math.min(2.2, +(app.zoom + 0.1).toFixed(2)); aplicarZoom(); };
  $("#zoom-menos").onclick = () => { app.zoom = Math.max(0.6, +(app.zoom - 0.1).toFixed(2)); aplicarZoom(); };
}

aplicarTema(lerLocal("tema") || "");
document.documentElement.style.setProperty("--zoom", app.zoom);
ligarEventos();
carregar().then(conectarAoVivo).catch((err) => toast("Não foi possível falar com o emulador: " + err.message, true));
