package databases

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"testing"
)

func TestCertificateOwnershipAndPersistence(t *testing.T) {
	m := testManager(t)
	i, err := m.Create(context.Background(), "owner", Input{Name: "TLS", Engine: "redis"})
	if err != nil {
		t.Fatal(err)
	}
	r, err := m.get("owner", i.ID)
	if err != nil {
		t.Fatal(err)
	}
	first, err := m.issueTLS(r)
	if err != nil {
		t.Fatal(err)
	}
	var raw []byte
	if err = m.db.QueryRow("SELECT material FROM database_tls_authority").Scan(&raw); err != nil || bytes.Contains(raw, []byte("PRIVATE KEY")) {
		t.Fatal("CA não foi armazenada com segurança", err)
	}
	if err = m.saveSecrets(r, secret{TLS: first}); err != nil {
		t.Fatal(err)
	}
	if _, _, err = m.Certificate("owner", i.ID, "pem"); !errors.Is(err, ErrConflict) {
		t.Fatal("certificado disponibilizado antes da validação", err)
	}
	r.State, r.Action, r.TLSValidated = "running", "", true
	if err = m.save(r); err != nil {
		t.Fatal(err)
	}
	m.ready[i.ID] = "validated"
	if _, _, err = m.Certificate("owner", i.ID, "server.key"); !errors.Is(err, ErrNotFound) {
		t.Fatal("formato de download inesperado", err)
	}
	files := map[string][]byte{}
	for format, filename := range map[string]string{"pem": "client.pem", "crt": "ca.crt", "key": "client.key"} {
		got, _, err := m.Certificate("other", i.ID, format)
		if !errors.Is(err, ErrNotFound) || got != nil {
			t.Fatal("material TLS disponibilizado a outra conta", err)
		}
		got, name, err := m.Certificate("owner", i.ID, format)
		if err != nil || name != filename || len(got) == 0 || (format != "key" && bytes.Contains(got, []byte("PRIVATE KEY"))) {
			t.Fatal("download inválido", format, err)
		}
		files[format] = got
	}
	client, err := tls.X509KeyPair(files["pem"], files["key"])
	if err != nil {
		t.Fatal("os downloads não formam um par", err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(files["crt"]) {
		t.Fatal("CA inválida")
	}
	if _, err = client.Leaf.Verify(x509.VerifyOptions{Roots: roots, KeyUsages: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}}); err != nil {
		t.Fatal("cliente não assinado pela CA baixada", err)
	}
	if _, err = client.Leaf.Verify(x509.VerifyOptions{Roots: roots}); err == nil {
		t.Fatal("certificado de cliente aceito como certificado de servidor")
	}
	ca, err := m.authority()
	if err != nil || bytes.Equal(files["key"], first.Key) || bytes.Equal(files["key"], ca.Key) {
		t.Fatal("download expôs uma chave do servidor ou da CA", err)
	}
	reopened := &Manager{db: m.db, localKey: m.localKey}
	second, err := reopened.issueTLS(r)
	if err != nil || !bytes.Equal(second.CA, first.CA) || bytes.Equal(second.Key, first.Key) || bytes.Equal(second.Client.Key, first.Client.Key) {
		t.Fatal("reabertura não preservou CA ou emitiu uma chave repetida", err)
	}
	if _, _, err = reopened.Certificate("owner", i.ID, "pem"); !errors.Is(err, ErrConflict) {
		t.Fatal("Core precisa revalidar o endpoint após reiniciar", err)
	}
	stored, err := reopened.secrets(r)
	if err != nil || !bytes.Equal(stored.TLS.Key, first.Key) || !bytes.Equal(stored.TLS.Client.Key, files["key"]) {
		t.Fatal("chaves não persistiram", err)
	}

	pair, err := tls.X509KeyPair(first.PEM, first.Key)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := tls.Listen("tcp4", "127.0.0.1:0", &tls.Config{
		Certificates: []tls.Certificate{pair}, MinVersion: tls.VersionTLS12,
		ClientCAs: roots, ClientAuth: tls.VerifyClientCertIfGiven,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 3 {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			_ = conn.(*tls.Conn).Handshake()
			_ = conn.Close()
		}
	}()
	port := listener.Addr().(*net.TCPAddr).Port
	conn, err := dialTLS(context.Background(), r, first, port)
	if err != nil {
		t.Fatal(err)
	}
	_ = conn.Close()
	conn, err = dialDatabaseTLS(context.Background(), r, first, port, &client)
	if err != nil {
		t.Fatal("handshake com o material baixado falhou", err)
	}
	_ = conn.Close()
	if conn, err = dialTLS(context.Background(), r, second, port); err == nil {
		_ = conn.Close()
		t.Fatal("certificado diferente aceito porque a CA é a mesma")
	}
	<-done
}

func TestBackfillClientTLS(t *testing.T) {
	m := testManager(t)
	i, err := m.Create(context.Background(), "owner", Input{Name: "TLS", Engine: "postgresql"})
	if err != nil {
		t.Fatal(err)
	}
	r, err := m.get("owner", i.ID)
	if err != nil {
		t.Fatal(err)
	}
	material, err := m.issueTLS(r)
	if err != nil {
		t.Fatal(err)
	}
	material.Client = nil
	legacy := secret{TLS: material, Admin: "admin-value", Password: "password-value", Reveal: true}
	if err = m.saveSecrets(r, legacy); err != nil {
		t.Fatal(err)
	}
	updated, err := m.backfillClientTLS(r, legacy)
	if err != nil {
		t.Fatal(err)
	}
	if err = updated.TLS.validate(r); err != nil {
		t.Fatal(err)
	}
	if updated.Password != legacy.Password || updated.Admin != legacy.Admin || updated.Reveal != legacy.Reveal ||
		!bytes.Equal(updated.TLS.CA, material.CA) || !bytes.Equal(updated.TLS.PEM, material.PEM) || !bytes.Equal(updated.TLS.Key, material.Key) {
		t.Fatal("backfill alterou o TLS do servidor ou as credenciais")
	}
	persisted, err := m.secrets(r)
	if err != nil {
		t.Fatal(err)
	}
	again, err := m.backfillClientTLS(r, persisted)
	if err != nil || again.TLS.Client == nil || !bytes.Equal(again.TLS.Client.Key, updated.TLS.Client.Key) {
		t.Fatal("backfill perdeu ou substituiu um par já emitido", err)
	}
}

func TestPostgresTLSAddsLANAddress(t *testing.T) {
	m := testManager(t)
	r := record{Instance: Instance{ID: "postgres-lan", Engine: "postgresql"}}
	ca, issuer, signer, err := m.tlsSigner()
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := issueServerTLS(r, issuer, signer, nil)
	if err != nil {
		t.Fatal(err)
	}
	client, err := issueClientTLS(r, issuer, signer)
	if err != nil {
		t.Fatal(err)
	}
	legacy := &serverTLS{certificate: leaf, CA: ca, Client: client}
	addresses := []net.IP{net.ParseIP("192.168.20.5")}
	updated, err := m.postgresTLSForLAN(r, legacy, addresses)
	if err != nil {
		t.Fatal(err)
	}
	pair, err := tls.X509KeyPair(updated.PEM, updated.Key)
	if err != nil {
		t.Fatal(err)
	}
	if err = pair.Leaf.VerifyHostname(addresses[0].String()); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(updated.CA, legacy.CA) || updated.Client != legacy.Client {
		t.Fatal("a atualização do certificado do servidor alterou a CA ou o par de cliente")
	}
	again, err := m.postgresTLSForLAN(r, updated, addresses)
	if err != nil || again != updated {
		t.Fatal("o mesmo endereço não deveria gerar outro certificado", err)
	}
}
