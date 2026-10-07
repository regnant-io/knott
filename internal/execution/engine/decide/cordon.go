// Copyright 2026 Regnant
// SPDX-License-Identifier: Apache-2.0

package decide

import (
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// Cordon is Regnant's confidential inference engine. As a KNOTT provider it is
// a local model with an accountable way in: each request is admitted under a
// client identity and the node's policy, written to Cordon's hash-chained
// audit log before it runs, filtered on the way out, and answered with an
// Ed25519 signature a third party can verify offline.
//
// KNOTT calls Cordon's OpenAI-compatible route and keeps the evidence Cordon
// returns — the request ID that keys Cordon's audit log and the response
// signature — with the decision. A KNOTT audit entry then points at the
// matching Cordon entry, so "which model said this, and can you prove it"
// has an answer on both sides.
//
// Identity follows Cordon's deployment mode. A Light node (development, or a
// single machine) takes the client ID from a header; every other mode needs
// mutual TLS, so KNOTT presents the client certificate Cordon enrolled.

// DefaultCordonURL is where `cordon run` serves its API on the same machine.
const DefaultCordonURL = "http://127.0.0.1:8443"

// cordonEvidence is what Cordon returns about a response, under "cordon".
type cordonEvidence struct {
	RequestID     string          `json:"request_id"`
	ClientID      string          `json:"client_id"`
	OutputHash    string          `json:"output_hash"`
	Mrenclave     string          `json:"mrenclave"`
	Timestamp     string          `json:"timestamp"`
	ContentPolicy json.RawMessage `json:"content_policy"`
	Signature     struct {
		KeyID      string `json:"enclave_key_id"`
		Algorithm  string `json:"algorithm"`
		Value      string `json:"value"`
		Provenance string `json:"key_provenance"`
	} `json:"signature"`
}

// receipt flattens Cordon's evidence into what a KNOTT decision keeps.
func (c cordonEvidence) receipt(node string) map[string]any {
	r := map[string]any{
		"provider":       "cordon",
		"node":           node,
		"request_id":     c.RequestID,
		"client_id":      c.ClientID,
		"output_hash":    c.OutputHash,
		"mrenclave":      c.Mrenclave,
		"timestamp":      c.Timestamp,
		"signature":      c.Signature.Value,
		"algorithm":      c.Signature.Algorithm,
		"key_id":         c.Signature.KeyID,
		"key_provenance": c.Signature.Provenance,
		"verify":         "cordon-verify-log, or CORDON_RESPONSE_v1|request_id|output_hash|model|timestamp_ms|mrenclave against the CMK-derived enclave key",
	}
	if len(c.ContentPolicy) > 0 {
		var cp any
		if json.Unmarshal(c.ContentPolicy, &cp) == nil {
			r["content_policy"] = cp
		}
	}
	return r
}

// CordonURL is the Cordon address in effect, or "" when Cordon is not set up.
func (e *Engine) CordonURL() string {
	return strings.TrimRight(strings.TrimSpace(e.Config().CordonURL), "/")
}

// cordonHTTP returns a client carrying the configured TLS identity, built once
// per configuration. A certificate that cannot be loaded is an error on every
// call rather than a silent fall back to an anonymous connection.
func (e *Engine) cordonHTTP() (*http.Client, error) {
	cfg := e.Config()
	key := cfg.CordonCertFile + "|" + cfg.CordonKeyFile + "|" + cfg.CordonCAFile
	e.cacheMu.Lock()
	defer e.cacheMu.Unlock()
	if e.cordonClient != nil && e.cordonKey == key {
		return e.cordonClient, nil
	}
	client := &http.Client{Timeout: e.Client.Timeout}
	if cfg.CordonCertFile != "" || cfg.CordonCAFile != "" {
		tlsCfg := &tls.Config{MinVersion: tls.VersionTLS13}
		if cfg.CordonCertFile != "" {
			cert, err := tls.LoadX509KeyPair(cfg.CordonCertFile, cfg.CordonKeyFile)
			if err != nil {
				return nil, fmt.Errorf("cannot load the Cordon client certificate: %w", err)
			}
			tlsCfg.Certificates = []tls.Certificate{cert}
		}
		if cfg.CordonCAFile != "" {
			pem, err := os.ReadFile(cfg.CordonCAFile)
			if err != nil {
				return nil, fmt.Errorf("cannot read the Cordon CA certificate: %w", err)
			}
			pool := x509.NewCertPool()
			if !pool.AppendCertsFromPEM(pem) {
				return nil, errors.New("the Cordon CA file holds no PEM certificate")
			}
			tlsCfg.RootCAs = pool
		}
		client.Transport = &http.Transport{TLSClientConfig: tlsCfg, Proxy: nil}
	}
	e.cordonClient, e.cordonKey = client, key
	return client, nil
}

// cordonModelFor maps a workflow's model profile onto Cordon. Cordon serves
// the model its operator loaded; "default" asks for exactly that, and a
// configured model name is passed through for nodes that admit several.
func (e *Engine) cordonModelFor(profile string) string {
	if m := strings.TrimSpace(e.Config().CordonModel); m != "" {
		return m
	}
	return "default"
}

func (e *Engine) cordonChat(model, system, user string, wantJSON bool, temp *float64, maxTokens int) (reply, error) {
	base := e.CordonURL()
	if base == "" {
		return reply{}, errors.New("Cordon is not configured — set its address in Settings → AI")
	}
	client, err := e.cordonHTTP()
	if err != nil {
		return reply{}, err
	}
	messages := []any{}
	if system != "" {
		messages = append(messages, map[string]any{"role": "system", "content": system})
	}
	messages = append(messages, map[string]any{"role": "user", "content": user})
	body := map[string]any{"model": model, "messages": messages, "max_tokens": maxTokens, "stream": false}
	if temp != nil {
		body["temperature"] = *temp
	} else if wantJSON {
		// A decision should not change because the sampler rolled differently.
		body["temperature"] = 0
	}
	if wantJSON {
		// Cordon passes this to the runtime, which constrains decoding to a
		// JSON object — the difference between a small local model that can
		// make decisions and one that writes prose around them.
		body["response_format"] = map[string]any{"type": "json_object"}
	}
	headers := map[string]string{}
	if id := strings.TrimSpace(e.Config().CordonClientID); id != "" {
		headers["x-client-id"] = id
	}
	raw, err := e.postWith(client, base+"/openai/v1/chat/completions", headers, body)
	if err != nil {
		return reply{}, explainCordonError(err, base)
	}
	var resp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			Total int `json:"total_tokens"`
		} `json:"usage"`
		Cordon *cordonEvidence `json:"cordon"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return reply{}, fmt.Errorf("could not read the Cordon response: %w", err)
	}
	if len(resp.Choices) == 0 {
		return reply{}, errors.New("Cordon returned no choices")
	}
	out := reply{text: resp.Choices[0].Message.Content, tokens: resp.Usage.Total}
	if resp.Cordon != nil {
		out.evidence = resp.Cordon.receipt(base)
	}
	return out, nil
}

// cordonReachable checks the unauthenticated liveness route.
func (e *Engine) cordonReachable() (bool, string) {
	base := e.CordonURL()
	if base == "" {
		return false, ""
	}
	client, err := e.cordonHTTP()
	if err != nil {
		return false, err.Error()
	}
	probe := *client
	probe.Timeout = 3 * time.Second
	resp, err := probe.Get(base + "/v1/health")
	if err != nil {
		return false, explainCordonError(err, base).Error()
	}
	defer resp.Body.Close()
	var h struct {
		Status  string `json:"status"`
		Serving bool   `json:"serving"`
	}
	_ = json.NewDecoder(resp.Body).Decode(&h)
	if resp.StatusCode >= 400 || !h.Serving {
		return false, fmt.Sprintf("Cordon at %s is up but not serving (status %q)", base, h.Status)
	}
	return true, ""
}

func explainCordonError(err error, base string) error {
	msg := err.Error()
	var he *httpError
	switch {
	case errors.As(err, &he) && (he.status == 401 || he.status == 403):
		return fmt.Errorf("Cordon at %s refused this client — enrol KNOTT's client ID (or certificate) in Cordon's clients.json and admit the model (%v)", base, err)
	case errors.As(err, &he) && he.status == 404:
		return fmt.Errorf("Cordon at %s has no OpenAI-compatible route — upgrade Cordon to 2.2 or later (%v)", base, err)
	case strings.Contains(msg, "connection refused"), strings.Contains(msg, "actively refused"),
		strings.Contains(msg, "no such host"), strings.Contains(msg, "dial tcp"):
		return fmt.Errorf("cannot reach Cordon at %s — is `cordon run` running? (%v)", base, err)
	case strings.Contains(msg, "certificate"):
		return fmt.Errorf("TLS with Cordon at %s failed — check the client certificate and CA in Settings → AI (%v)", base, err)
	}
	return err
}
