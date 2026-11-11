package cloudflare

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/zalando/go-keyring"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Vault interface {
	Get(string) (string, error)
	Set(string, string) error
}
type OSVault struct{ Installation string }

func (v OSVault) Get(key string) (string, error) {
	return keyring.Get("Cloudforge-"+v.Installation, key)
}
func (v OSVault) Set(key, value string) error {
	return keyring.Set("Cloudforge-"+v.Installation, key, value)
}

type Client struct {
	BaseURL string
	HTTP    *http.Client
	Vault   Vault
}

func NewClient(v Vault) *Client {
	return &Client{"https://api.cloudflare.com/client/v4", &http.Client{Timeout: 30 * time.Second}, v}
}
func (c *Client) Call(ctx context.Context, method, path string, body, out any) error {
	token, e := c.Vault.Get("api-token")
	if e != nil || token == "" {
		return errors.New("configure um API Token Cloudflare no armazenamento seguro do sistema")
	}
	return c.callToken(ctx, token, method, path, body, out)
}
func (c *Client) callToken(ctx context.Context, token, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		data, e := json.Marshal(body)
		if e != nil {
			return e
		}
		reader = bytes.NewReader(data)
	}
	req, e := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reader)
	if e != nil {
		return e
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, e := c.HTTP.Do(req)
	if e != nil {
		return errors.New("não foi possível conectar à API Cloudflare")
	}
	defer resp.Body.Close()
	var envelope struct {
		Success bool            `json:"success"`
		Result  json.RawMessage `json:"result"`
		Errors  []struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"errors"`
	}
	if e = json.NewDecoder(io.LimitReader(resp.Body, 4*1024*1024)).Decode(&envelope); e != nil {
		return errors.New("resposta Cloudflare inválida")
	}
	if !envelope.Success || resp.StatusCode >= 400 {
		if len(envelope.Errors) > 0 {
			return fmt.Errorf("Cloudflare %d: %s", envelope.Errors[0].Code, strings.ReplaceAll(envelope.Errors[0].Message, token, "[redigido]"))
		}
		return fmt.Errorf("Cloudflare HTTP %d", resp.StatusCode)
	}
	if out != nil {
		return json.Unmarshal(envelope.Result, out)
	}
	return nil
}
func (c *Client) Connect(ctx context.Context, token string) error {
	if len(token) < 20 || len(token) > 512 {
		return errors.New("API Token inválido")
	}
	var verify struct {
		Status string `json:"status"`
	}
	if e := c.callToken(ctx, token, "GET", "/user/tokens/verify", nil, &verify); e != nil {
		return e
	}
	if verify.Status != "active" {
		return errors.New("API Token não está ativo")
	}
	if e := c.Vault.Set("api-token", token); e != nil {
		return fmt.Errorf("armazenamento seguro indisponível: %w", e)
	}
	return nil
}

type Zone struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Account struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	} `json:"account"`
}

func (c *Client) Zones(ctx context.Context) ([]Zone, error) {
	out := []Zone{}
	for page := 1; page <= 100; page++ {
		var zones []Zone
		if e := c.Call(ctx, "GET", fmt.Sprintf("/zones?per_page=50&page=%d", page), nil, &zones); e != nil {
			return nil, e
		}
		out = append(out, zones...)
		if len(zones) < 50 {
			break
		}
	}
	return out, nil
}

type DNS struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Content string `json:"content"`
	Comment string `json:"comment"`
}

func (c *Client) Records(ctx context.Context, zone, host string) ([]DNS, error) {
	var records []DNS
	e := c.Call(ctx, "GET", "/zones/"+url.PathEscape(zone)+"/dns_records?name="+url.QueryEscape(host), nil, &records)
	return records, e
}
