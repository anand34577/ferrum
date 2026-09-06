package pve

import (
	"context"
	"fmt"
	"net/url"
)

// NodeCertificate is one TLS certificate installed on a node
// (/nodes/{node}/certificates/info) — the API endpoint itself (pveproxy) or
// a service certificate (SMTP, ...).
type NodeCertificate struct {
	Filename    string   `json:"filename"`
	Subject     string   `json:"subject,omitempty"`
	Issuer      string   `json:"issuer,omitempty"`
	NotBefore   int64    `json:"notbefore,omitempty"`
	NotAfter    int64    `json:"notafter,omitempty"`
	Fingerprint string   `json:"fingerprint,omitempty"`
	SAN         []string `json:"san,omitempty"`
}

func (c *Client) NodeCertificates(ctx context.Context, node string) ([]NodeCertificate, error) {
	var out struct {
		Data []NodeCertificate `json:"data"`
	}
	if err := c.get(ctx, fmt.Sprintf("/nodes/%s/certificates/info", PathEscape(node)), &out); err != nil {
		return nil, err
	}
	return out.Data, nil
}

// UploadCustomCertificate installs a custom (non-ACME) certificate+key pair
// for the node's pveproxy. force overwrites an existing custom certificate.
func (c *Client) UploadCustomCertificate(ctx context.Context, node, certificate, key string, force bool) error {
	form := url.Values{"certificates": {certificate}}
	if key != "" {
		form.Set("key", key)
	}
	if force {
		form.Set("force", "1")
	}
	return c.post(ctx, fmt.Sprintf("/nodes/%s/certificates/custom", PathEscape(node)), form, nil)
}

// DeleteCustomCertificate removes the node's custom certificate, reverting
// to the self-signed one PVE generates for itself.
func (c *Client) DeleteCustomCertificate(ctx context.Context, node string) error {
	return c.delete(ctx, fmt.Sprintf("/nodes/%s/certificates/custom", PathEscape(node)), nil, nil)
}

// ---- ACME (Let's Encrypt) ----

// AcmeOrderCertificate requests/renews an ACME certificate for the node
// using its already-configured ACME account/domains. Async — returns a UPID.
func (c *Client) AcmeOrderCertificate(ctx context.Context, node string) (string, error) {
	var out struct {
		Data string `json:"data"`
	}
	if err := c.post(ctx, fmt.Sprintf("/nodes/%s/certificates/acme/certificate", PathEscape(node)), url.Values{}, &out); err != nil {
		return "", err
	}
	return out.Data, nil
}

func (c *Client) RevokeAcmeCertificate(ctx context.Context, node string) (string, error) {
	var out struct {
		Data string `json:"data"`
	}
	if err := c.delete(ctx, fmt.Sprintf("/nodes/%s/certificates/acme/certificate", PathEscape(node)), nil, &out); err != nil {
		return "", err
	}
	return out.Data, nil
}
