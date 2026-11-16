package snapshots

import (
	"cloudforge/core/internal/persistence"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/zalando/go-keyring"
)

const driveScope = "https://www.googleapis.com/auth/drive.file"

type account struct {
	ID    string `json:"id"`
	Email string `json:"email"`
}

type driveState struct {
	Configured bool     `json:"configured"`
	Managed    bool     `json:"managed"`
	Pending    bool     `json:"pending"`
	Account    *account `json:"account,omitempty"`
	Error      string   `json:"error,omitempty"`
}

type credentials struct {
	Access  string `json:"access_token"`
	Refresh string `json:"refresh_token"`
	Seconds int    `json:"expires_in"`
	Error   string `json:"error"`
}

type tokenVault interface {
	Get() (string, error)
	Set(string) error
	Delete() error
}

type osVault struct{ service, entry string }

func (v osVault) Get() (string, error)   { return keyring.Get(v.service, v.entry) }
func (v osVault) Set(value string) error { return keyring.Set(v.service, v.entry, value) }
func (v osVault) Delete() error          { return keyring.Delete(v.service, v.entry) }

type OAuthClient struct {
	ID     string `json:"clientId"`
	Secret string `json:"clientSecret"`
}

var googleClientID = regexp.MustCompile("^[A-Za-z0-9_-]+\\.apps\\.googleusercontent\\.com$")
var googleClientSecret = regexp.MustCompile("^[A-Za-z0-9_-]+$")

func (c OAuthClient) valid() bool {
	return len(c.ID) <= 256 && len(c.Secret) <= 256 &&
		googleClientID.MatchString(c.ID) && googleClientSecret.MatchString(c.Secret)
}

type drive struct {
	store                               *persistence.Store
	vault                               tokenVault
	clientVault                         tokenVault
	client                              *http.Client
	clientID, clientSecret              string
	tokenURL, revokeURL, api, uploadAPI string
	mu                                  sync.Mutex
	state                               driveState
	access                              string
	expires                             time.Time
	disconnecting                       bool
}

func newDrive(store *persistence.Store, installation string) *drive {
	service := "Cloudforge-" + installation
	d := &drive{
		store: store, vault: osVault{service, "google-drive"},
		clientVault:  osVault{service, "google-drive-client"},
		client:       &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		clientID:     strings.TrimSpace(os.Getenv("CLOUDFORGE_GOOGLE_CLIENT_ID")),
		clientSecret: strings.TrimSpace(os.Getenv("CLOUDFORGE_GOOGLE_CLIENT_SECRET")),
		tokenURL:     "https://oauth2.googleapis.com/token", revokeURL: "https://oauth2.googleapis.com/revoke",
		api: "https://www.googleapis.com/drive/v3", uploadAPI: "https://www.googleapis.com/upload/drive/v3",
	}
	d.state.Managed = d.clientID != "" && d.clientSecret != ""
	d.loadClient()
	var existing account
	if store.Setting("snapshotDriveAccount", &existing) == nil && existing.ID != "" {
		d.state.Account = &existing
	}
	return d
}

func (d *drive) loadClient() {
	if d.state.Managed {
		d.state.Configured = true
		return
	}
	raw, err := d.clientVault.Get()
	var client OAuthClient
	if err == nil && json.Unmarshal([]byte(raw), &client) == nil && client.valid() {
		d.clientID, d.clientSecret = client.ID, client.Secret
		d.state.Configured = true
	}
}

func (d *drive) configure(client OAuthClient) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.state.Managed {
		return errors.New("o cliente OAuth é definido no ambiente do Core")
	}
	if d.state.Pending || d.state.Account != nil || d.disconnecting {
		return errors.New("desconecte a conta Google antes de alterar o cliente OAuth")
	}
	client.ID, client.Secret = strings.TrimSpace(client.ID), strings.TrimSpace(client.Secret)
	if !client.valid() {
		return errors.New("informe o ID e o segredo de um cliente OAuth do Google do tipo App para computador")
	}
	raw, err := json.Marshal(client)
	if err != nil {
		return err
	}
	if err := d.clientVault.Set(string(raw)); err != nil {
		return errors.New("o cofre do sistema está indisponível para salvar o cliente OAuth")
	}
	d.clientID, d.clientSecret = client.ID, client.Secret
	d.state.Configured, d.state.Error = true, ""
	return nil
}

func (d *drive) status() driveState {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.state
}

func opaqueID() (string, error) {
	var data [32]byte
	if _, err := rand.Read(data[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(data[:]), nil
}

func (d *drive) begin(ctx context.Context, accountGate *sync.RWMutex) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if !d.state.Configured {
		return "", errors.New("configure o cliente OAuth em Configurações → Google Drive")
	}
	if d.state.Pending || d.state.Account != nil {
		return "", errors.New("conclua a conexão atual ou desconecte a conta primeiro")
	}
	state, err := opaqueID()
	if err != nil {
		return "", err
	}
	verifier, err := opaqueID()
	if err != nil {
		return "", err
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return "", errors.New("não foi possível abrir o retorno local do Google")
	}
	redirect := "http://" + listener.Addr().String() + "/oauth/google"
	hash := sha256.Sum256([]byte(verifier))
	parameters := url.Values{
		"client_id": {d.clientID}, "redirect_uri": {redirect}, "response_type": {"code"},
		"state": {state}, "code_challenge": {base64.RawURLEncoding.EncodeToString(hash[:])},
		"code_challenge_method": {"S256"}, "scope": {driveScope}, "access_type": {"offline"},
		"prompt": {"consent"},
	}
	d.state.Pending, d.state.Error = true, ""
	go d.awaitAuthorization(ctx, listener, state, verifier, redirect, accountGate)
	return "https://accounts.google.com/o/oauth2/v2/auth?" + parameters.Encode(), nil
}

func (d *drive) awaitAuthorization(parent context.Context, listener net.Listener, state, verifier, redirect string, accountGate *sync.RWMutex) {
	ctx, cancel := context.WithTimeout(parent, 5*time.Minute)
	defer cancel()
	result := make(chan string, 1)
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second}
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.Host != listener.Addr().String() ||
			r.URL.Path != "/oauth/google" || r.URL.Query().Get("state") != state {
			http.Error(w, "Retorno OAuth inválido", http.StatusBadRequest)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		_, _ = io.WriteString(w, "Retorno recebido. Volte ao Cloudforge para consultar a conexão.")
		select {
		case result <- r.URL.Query().Get("code"):
		default:
		}
	})
	go func() { _ = server.Serve(listener) }()
	var finishErr error
	defer func() {
		d.mu.Lock()
		defer d.mu.Unlock()
		d.state.Pending = false
		if finishErr != nil {
			d.state.Error = finishErr.Error()
		}
	}()
	select {
	case code := <-result:
		_ = server.Shutdown(ctx)
		if code == "" {
			finishErr = errors.New("autorização do Google cancelada ou recusada")
			return
		}
		accountGate.Lock()
		defer accountGate.Unlock()
		finishErr = d.connect(ctx, code, redirect, verifier)
	case <-ctx.Done():
		_ = server.Close()
		finishErr = errors.New("a autorização do Google expirou")
	}
}

var errRevoked = errors.New("autorização do Google expirada ou revogada. Reconecte sua conta")
var errDriveNetwork = errors.New("não foi possível se comunicar com o Google Drive")

func (d *drive) exchange(ctx context.Context, form url.Values) (credentials, error) {
	var token credentials
	form.Set("client_id", d.clientID)
	form.Set("client_secret", d.clientSecret)
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", d.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return token, errDriveNetwork
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := d.client.Do(req)
	if err != nil {
		return token, errDriveNetwork
	}
	defer resp.Body.Close()
	if json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&token) != nil {
		return token, errDriveNetwork
	}
	if token.Error == "invalid_grant" {
		return token, errRevoked
	}
	if resp.StatusCode != 200 || token.Access == "" || token.Seconds <= 0 {
		return token, errors.New("o Google rejeitou as credenciais OAuth do aplicativo")
	}
	return token, nil
}

func (d *drive) connect(ctx context.Context, code, redirect, verifier string) error {
	token, err := d.exchange(ctx, url.Values{"grant_type": {"authorization_code"}, "code": {code},
		"redirect_uri": {redirect}, "code_verifier": {verifier}})
	if err != nil {
		return err
	}
	if token.Refresh == "" {
		return errors.New("o Google não concedeu acesso em segundo plano")
	}
	req, err := http.NewRequestWithContext(ctx, "GET", d.api+"/about?fields=user(permissionId,emailAddress)", nil)
	if err != nil {
		return errDriveNetwork
	}
	req.Header.Set("Authorization", "Bearer "+token.Access)
	resp, err := d.client.Do(req)
	if err != nil {
		return errDriveNetwork
	}
	defer resp.Body.Close()
	var about struct {
		User struct {
			ID    string `json:"permissionId"`
			Email string `json:"emailAddress"`
		} `json:"user"`
	}
	if resp.StatusCode != 200 || json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&about) != nil || about.User.ID == "" {
		return errors.New("não foi possível identificar a conta do Google Drive")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	a := account{about.User.ID, about.User.Email}
	if err = d.vault.Set(token.Refresh); err != nil {
		return errors.New("o cofre do sistema está indisponível para salvar a conexão do Google")
	}
	if err = d.store.Set("snapshotDriveAccount", a); err != nil {
		_ = d.vault.Delete()
		return errors.New("falha ao salvar a identificação da conta do Google")
	}
	d.state.Account, d.state.Error = &a, ""
	d.access, d.expires = token.Access, time.Now().Add(time.Duration(token.Seconds)*time.Second)
	return nil
}

func (d *drive) accessToken(ctx context.Context, accountID, rejected string) (string, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.disconnecting || !d.state.Configured || d.state.Account == nil || d.state.Account.ID != accountID {
		return "", errors.New("conecte uma conta Google Drive")
	}
	if d.access != "" && d.access != rejected && time.Until(d.expires) > time.Minute {
		return d.access, nil
	}
	refresh, err := d.vault.Get()
	if errors.Is(err, keyring.ErrNotFound) {
		d.state.Error = errRevoked.Error()
		return "", errRevoked
	}
	if err != nil {
		return "", errors.New("não foi possível acessar o cofre do sistema")
	}
	token, err := d.exchange(ctx, url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}})
	if err != nil {
		if errors.Is(err, errRevoked) {
			d.state.Error = err.Error()
		}
		return "", err
	}
	if token.Refresh != "" && token.Refresh != refresh {
		if err := d.vault.Set(token.Refresh); err != nil {
			return "", errors.New("falha ao atualizar o cofre do sistema")
		}
	}
	d.access, d.expires = token.Access, time.Now().Add(time.Duration(token.Seconds)*time.Second)
	d.state.Error = ""
	return d.access, nil
}

func (d *drive) disconnect(ctx context.Context) error {
	d.mu.Lock()
	if d.state.Pending {
		d.mu.Unlock()
		return errors.New("aguarde a tentativa de conexão em andamento")
	}
	if d.state.Account == nil {
		d.mu.Unlock()
		return nil
	}
	d.disconnecting = true
	d.mu.Unlock()
	defer func() { d.mu.Lock(); d.disconnecting = false; d.mu.Unlock() }()
	token, err := d.vault.Get()
	if err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return errors.New("não foi possível acessar o cofre do sistema")
	}
	var revokeErr error
	if token != "" {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		form := url.Values{"token": {token}}.Encode()
		req, err := http.NewRequestWithContext(ctx, "POST", d.revokeURL, strings.NewReader(form))
		if err == nil {
			req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			resp, requestErr := d.client.Do(req)
			if requestErr != nil || (resp.StatusCode != 200 && resp.StatusCode != 400) {
				revokeErr = errors.New("conexão local removida. O Google não confirmou a revogação")
			}
			if resp != nil {
				_ = resp.Body.Close()
			}
		}
	}
	if err := d.vault.Delete(); err != nil && !errors.Is(err, keyring.ErrNotFound) {
		return errors.New("o cofre do sistema recusou a remoção da conexão")
	}
	d.mu.Lock()
	d.state.Account, d.state.Error, d.access = nil, "", ""
	if revokeErr != nil {
		d.state.Error = revokeErr.Error()
	}
	d.mu.Unlock()
	if err := d.store.Set("snapshotDriveAccount", account{}); err != nil {
		return errors.New("falha ao atualizar o cadastro local da conta")
	}
	return revokeErr
}

func googleHTTPError(resp *http.Response) error {
	var payload struct {
		Error struct {
			Errors []struct {
				Reason string `json:"reason"`
			} `json:"errors"`
		} `json:"error"`
	}
	_ = json.NewDecoder(io.LimitReader(resp.Body, 64*1024)).Decode(&payload)
	for _, item := range payload.Error.Errors {
		if item.Reason == "storageQuotaExceeded" {
			return errors.New("espaço insuficiente no Google Drive")
		}
	}
	if resp.StatusCode == 401 {
		return errRevoked
	}
	if resp.StatusCode == 403 {
		return errors.New("o Google Drive recusou o acesso ou atingiu o limite de requisições")
	}
	if resp.StatusCode == 404 {
		return errors.New("arquivo indisponível para esta conta e este cliente OAuth")
	}
	return fmt.Errorf("o Google Drive respondeu com HTTP %d", resp.StatusCode)
}
