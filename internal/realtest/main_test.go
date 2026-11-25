package realtest_test

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tenancyadminpb "git.authwise.com/authwise/apis/authwise/tenancy/admin/v1alpha1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// TestMain turns Guard on for the stack's tenant before any test runs, as
// an operator does for a customer: kit's EnableProduct, which guard-control
// requires of a tenant before it holds any of its networks. Idempotent, so
// a rerun against the same stack is fine. Without a stack the tests skip
// and this does nothing.
func TestMain(m *testing.M) {
	if os.Getenv("REALSERVER_CREDENTIAL") != "" && os.Getenv("TF_ACC") != "" {
		if err := enableGuard(); err != nil {
			fmt.Fprintln(os.Stderr, "realtest: turning Guard on for "+realTenant+":", err)
			os.Exit(1)
		}
	}
	os.Exit(m.Run())
}

type credential struct {
	TokenURL     string `json:"token_url"`
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	Audience     string `json:"audience"`
}

func readCredential() (credential, error) {
	var c credential
	raw, err := os.ReadFile(os.Getenv("REALSERVER_CREDENTIAL"))
	if err != nil {
		return c, err
	}
	return c, json.Unmarshal(raw, &c)
}

// token is a client_credentials grant for the stack's test client.
func token(ctx context.Context, c credential) (string, error) {
	form := url.Values{
		"grant_type":    {"client_credentials"},
		"client_id":     {c.ClientID},
		"client_secret": {c.ClientSecret},
		"audience":      {c.Audience},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK || out.AccessToken == "" {
		return "", fmt.Errorf("token endpoint answered %s", resp.Status)
	}
	return out.AccessToken, nil
}

// tenancyAdminTLS is the stack's per-run client certificate and CA
// (realserver/certs.sh): kit's tenancy listener requires both a client
// certificate and a bearer.
func tenancyAdminTLS() (credentials.TransportCredentials, error) {
	dir := env("REALSERVER_CERTS", "")
	if dir == "" {
		dir = filepath.Join(filepath.Dir(os.Getenv("REALSERVER_CREDENTIAL")), "certs")
	}
	cert, err := tls.LoadX509KeyPair(filepath.Join(dir, "client.pem"), filepath.Join(dir, "client.key"))
	if err != nil {
		return nil, err
	}
	ca, err := os.ReadFile(filepath.Join(dir, "ca.pem"))
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca) {
		return nil, fmt.Errorf("no CA certificate in %s", filepath.Join(dir, "ca.pem"))
	}
	return credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      pool,
		ServerName:   "localhost",
		MinVersion:   tls.VersionTLS12,
	}), nil
}

func enableGuard() error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	c, err := readCredential()
	if err != nil {
		return fmt.Errorf("reading the stack's credential: %w", err)
	}
	bearer, err := token(ctx, c)
	if err != nil {
		return err
	}
	creds, err := tenancyAdminTLS()
	if err != nil {
		return err
	}
	conn, err := grpc.NewClient(env("REALSERVER_TENANCY_ADMIN", "127.0.0.1:9443"), grpc.WithTransportCredentials(creds))
	if err != nil {
		return err
	}
	defer conn.Close()

	ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+bearer)
	_, err = tenancyadminpb.NewAuthwiseTenancyAdminServiceClient(conn).EnableProduct(ctx, &tenancyadminpb.EnableProductRequest{
		Name:     "tenants/" + realTenant,
		Product:  "guard",
		IssuerId: "i-01",
	})
	// FailedPrecondition is Guard already on through another issuer: a
	// stack someone set up by hand. Anything else is a broken stack.
	if status.Code(err) == codes.FailedPrecondition {
		return nil
	}
	return err
}
