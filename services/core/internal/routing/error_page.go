package routing

import (
	"embed"
	"encoding/base64"
	"html/template"
	"net/http"
)

//go:embed fonts/*.woff2 mascots/*.webp
var mascotFiles embed.FS

type errorPage struct {
	Title      string
	Message    string
	Guidance   string
	Status     int
	Host       string
	RequestURI string
	Mascot     template.URL
	MascotAlt  string
	BrandMark  template.URL
	Poppins600 template.URL
	Poppins700 template.URL
	Sans400    template.URL
	Sans600    template.URL
	Mono400    template.URL
}

type routeError struct {
	status    int
	title     string
	message   string
	guidance  string
	mascot    string
	mascotAlt string
}

var routeErrors = map[string]routeError{
	"offline": {
		status: http.StatusServiceUnavailable, title: "Esta aplicação está offline",
		message:  "O domínio continua configurado. A aplicação volta a responder quando o projeto estiver online.",
		guidance: "Tente novamente em instantes ou confira o estado do projeto no Cloudforge.",
		mascot:   "offline.webp", mascotAlt: "Pingo está sentado, com expressão triste.",
	},
	"port": {
		status: http.StatusServiceUnavailable, title: "A porta configurada não respondeu",
		message:  "O domínio foi encontrado, mas o Cloudforge não conseguiu alcançar a aplicação na porta local configurada.",
		guidance: "Confira se o projeto está em execução e se a porta publicada corresponde à porta em que ele escuta.",
		mascot:   "porta-indisponivel.webp", mascotAlt: "Pingo está preocupado com a conexão.",
	},
	"service": {
		status: http.StatusServiceUnavailable, title: "Serviço temporariamente indisponível",
		message:  "O Cloudforge não conseguiu consultar as configurações deste endereço agora.",
		guidance: "Aguarde um pouco e tente carregar a página novamente.",
		mascot:   "indisponivel.webp", mascotAlt: "Pingo está sem saber o que aconteceu.",
	},
	"not-found": {
		status: http.StatusNotFound, title: "Este domínio não foi encontrado",
		message:  "Não há uma aplicação do Cloudforge configurada para este endereço.",
		guidance: "Confira se o endereço está correto ou fale com quem compartilhou o link.",
		mascot:   "nao-encontrado.webp", mascotAlt: "Pingo procura uma resposta, com pontos de interrogação.",
	},
	"forbidden": {
		status: http.StatusForbidden, title: "Acesso negado",
		message:  "Você não tem autorização para acessar este recurso.",
		guidance: "Confira se está usando a conta correta ou peça acesso a quem administra este serviço.",
		mascot:   "sem-autorizacao.webp", mascotAlt: "Pingo cruza os braços, irritado.",
	},
	"internal": {
		status: http.StatusInternalServerError, title: "Não foi possível concluir esta solicitação",
		message:  "O Cloudforge encontrou um problema inesperado ao preparar esta página.",
		guidance: "Tente novamente em instantes.",
		mascot:   "base.webp", mascotAlt: "Pingo está sorrindo e pronto para ajudar.",
	},
}

var errorPageTemplate = template.Must(template.New("error-page").Parse(`<!doctype html>
<html lang="pt-BR">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <meta name="theme-color" content="#0b0d11">
  <title>{{.Title}} · Cloudforge</title>
  <style>
    @font-face { font-family: Poppins; font-style: normal; font-weight: 600; font-display: swap; src: url("{{.Poppins600}}") format("woff2"); }
    @font-face { font-family: Poppins; font-style: normal; font-weight: 700; font-display: swap; src: url("{{.Poppins700}}") format("woff2"); }
    @font-face { font-family: "IBM Plex Sans"; font-style: normal; font-weight: 400; font-display: swap; src: url("{{.Sans400}}") format("woff2"); }
    @font-face { font-family: "IBM Plex Sans"; font-style: normal; font-weight: 600; font-display: swap; src: url("{{.Sans600}}") format("woff2"); }
    @font-face { font-family: "IBM Plex Mono"; font-style: normal; font-weight: 400; font-display: swap; src: url("{{.Mono400}}") format("woff2"); }
    :root { color-scheme: dark; font-family: "IBM Plex Sans", sans-serif; background: #0b0d11; color: #f1f3f7; font-synthesis: none; }
    * { box-sizing: border-box; }
    body { margin: 0; min-height: 100vh; background: #0b0d11; }
    a { color: inherit; }
    ::selection { background: #a94b0d; color: #fff; }
    .shell { width: min(1120px, calc(100% - 64px)); min-height: 100vh; margin: 0 auto; display: flex; flex-direction: column; }
    .topbar { min-height: 76px; display: flex; align-items: center; justify-content: space-between; gap: 20px; border-bottom: 1px solid #232834; }
    .brand { display: inline-flex; align-items: center; gap: 9px; color: #fff; font-family: Poppins, "IBM Plex Sans", sans-serif; font-size: 22px; font-weight: 700; letter-spacing: -.025em; line-height: 1; text-decoration: none; white-space: nowrap; }
    .brand img { width: 42px; height: 42px; object-fit: contain; flex-shrink: 0; }
    .brand-wordmark { display: inline-flex; align-items: baseline; }
    .brand-forge { color: #fc7800; }
    .host { max-width: 55%; overflow: hidden; color: #949eae; font: 12px/1.4 "IBM Plex Mono", monospace; text-overflow: ellipsis; white-space: nowrap; }
    .content { flex: 1; display: grid; grid-template-columns: minmax(0, 1fr) minmax(280px, 360px); align-items: center; gap: clamp(48px, 7vw, 104px); padding: clamp(64px, 9vh, 112px) 0; }
    .copy { max-width: 620px; }
    h1 { max-width: 17ch; margin: 0 0 24px; font-family: Poppins, "IBM Plex Sans", sans-serif; font-size: 42px; font-weight: 600; letter-spacing: -.03em; line-height: 1.14; text-wrap: balance; }
    .message { max-width: 56ch; margin: 0; color: #ebedf2; font-size: 16px; line-height: 1.6; }
    .guidance { max-width: 54ch; margin: 28px 0 0; padding-top: 22px; border-top: 1px solid #232834; color: #949eae; font-size: 14px; line-height: 1.6; }
    .action { display: inline-flex; align-items: center; gap: 10px; min-height: 48px; margin-top: 28px; padding: 0 18px; border: 1px solid #ff7a1a; border-radius: 7px; background: #ff7a1a; color: #140800; font-size: 14px; font-weight: 600; text-decoration: none; transition: background 140ms ease, border-color 140ms ease; }
    .action:hover { border-color: #ff9138; background: #ff9138; }
    .action:focus-visible { outline: 2px solid #f1f3f7; outline-offset: 3px; }
    .action svg { width: 16px; height: 16px; }
    .mascot { width: 100%; max-width: 360px; margin: 0; justify-self: center; }
    .mascot img { display: block; width: 100%; height: auto; }
    .footer { min-height: 64px; display: flex; align-items: center; justify-content: space-between; gap: 20px; border-top: 1px solid #232834; color: #949eae; font: 12px/1.5 "IBM Plex Mono", monospace; }
    .status { color: #f87a74; }
    @media (max-width: 900px) {
      .content { grid-template-columns: minmax(0, 1fr) minmax(220px, 300px); gap: 36px; }
      h1 { font-size: 36px; }
    }
    @media (max-width: 720px) {
      .shell { width: min(100% - 36px, 520px); }
      .topbar { min-height: 70px; }
      .host { max-width: 58%; font-size: 12px; }
      .content { grid-template-columns: 1fr; gap: 12px; padding: 56px 0 48px; }
      .mascot { grid-row: 1; width: min(50vw, 190px); }
      .copy { grid-row: 2; }
      h1 { max-width: 18ch; font-size: 32px; }
      .footer { min-height: 58px; font-size: 11px; }
    }
    @media (prefers-reduced-motion: reduce) { *, *::before, *::after { scroll-behavior: auto !important; transition-duration: .01ms !important; } }
  </style>
</head>
<body>
  <div class="shell">
    <header class="topbar">
      <a class="brand" href="/" aria-label="Cloudforge"><img src="{{.BrandMark}}" alt=""><span class="brand-wordmark">Cloud<span class="brand-forge">forge</span></span></a>
      <span class="host">{{.Host}}</span>
    </header>
    <main class="content" aria-labelledby="error-title">
      <section class="copy">
        <h1 id="error-title">{{.Title}}</h1>
        <p class="message">{{.Message}}</p>
        <p class="guidance">{{.Guidance}}</p>
        <a class="action" href="{{.RequestURI}}">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M20 7v5h-5"/><path d="M20 12a8 8 0 1 0-2.3 5.7"/></svg>
          Tentar novamente
        </a>
      </section>
      <figure class="mascot"><img src="{{.Mascot}}" alt="{{.MascotAlt}}" width="640" height="640"></figure>
    </main>
    <footer class="footer"><span class="status">HTTP {{.Status}}</span><span>Cloudforge · Controle de aplicações</span></footer>
  </div>
</body>
</html>`))

func (r *Router) showError(w http.ResponseWriter, req *http.Request, kind string) {
	page, ok := routeErrors[kind]
	if !ok {
		page = routeErrors["internal"]
	}
	assetName := page.mascot
	if _, err := mascotFiles.ReadFile("mascots/" + assetName); err != nil {
		page = routeErrors["internal"]
		assetName = page.mascot
	}
	asset, err := mascotFiles.ReadFile("mascots/" + assetName)
	if err != nil {
		http.Error(w, "Serviço temporariamente indisponível", http.StatusServiceUnavailable)
		return
	}
	brandMark, err := mascotFiles.ReadFile("mascots/cloudforge-mark.webp")
	if err != nil {
		http.Error(w, "Serviço temporariamente indisponível", http.StatusServiceUnavailable)
		return
	}
	fontURL := func(name string) template.URL {
		font, err := mascotFiles.ReadFile("fonts/" + name)
		if err != nil {
			return ""
		}
		return template.URL("data:font/woff2;base64," + base64.StdEncoding.EncodeToString(font))
	}
	content := errorPage{
		Title: page.title, Message: page.message, Guidance: page.guidance,
		Status: page.status, Host: req.Host, RequestURI: req.URL.RequestURI(),
		Mascot: template.URL("data:image/webp;base64," + base64.StdEncoding.EncodeToString(asset)), MascotAlt: page.mascotAlt,
		BrandMark:  template.URL("data:image/webp;base64," + base64.StdEncoding.EncodeToString(brandMark)),
		Poppins600: fontURL("poppins-600.woff2"), Poppins700: fontURL("poppins-700.woff2"),
		Sans400: fontURL("ibm-plex-sans-400.woff2"), Sans600: fontURL("ibm-plex-sans-600.woff2"),
		Mono400: fontURL("ibm-plex-mono-400.woff2"),
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; img-src data:; font-src data:; base-uri 'none'; form-action 'none'; frame-ancestors 'none'")
	w.WriteHeader(page.status)
	_ = errorPageTemplate.Execute(w, content)
}
