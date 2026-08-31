// Package webshell implements the existing SimpleWebShell HTTP API.
package webshell

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const maxResponseBytes = 2 << 20

// Client is a non-invasive HTTP client for an existing SimpleWebShell.
type Client struct {
	baseURL string
	key     string
	http    *http.Client
	session string
}

// New creates a client.
func New(baseURL, key string, timeout time.Duration, insecureTLS bool) *Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if insecureTLS {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} // #nosec G402 -- explicit operator option
	}
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		key:     key,
		http:    &http.Client{Timeout: timeout, Transport: transport},
	}
}

// Probe verifies endpoint reachability and key validity without changing remote state.
func (c *Client) Probe(ctx context.Context) error {
	q := url.Values{"key": {c.key}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/get_current_path?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("连接 SimpleWebShell 失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("SimpleWebShell 探测失败 HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// CreateSession creates a remote session and starts using it.
func (c *Client) CreateSession(ctx context.Context) (string, error) {
	q := url.Values{"key": {c.key}}
	body, err := c.simpleGET(ctx, "/session_create", q)
	if err != nil {
		return "", err
	}
	id := strings.TrimSpace(body)
	if id == "" {
		return "", fmt.Errorf("SimpleWebShell 返回空 session")
	}
	c.session = id
	return id, nil
}

// DeleteSession removes the active session.
func (c *Client) DeleteSession(ctx context.Context) error {
	if c.session == "" {
		return nil
	}
	q := url.Values{"key": {c.key}, "session": {c.session}}
	_, err := c.simpleGET(ctx, "/session_delete", q)
	c.session = ""
	return err
}

// Exec executes one shell command using POST /post.
func (c *Client) Exec(ctx context.Context, command string) (string, error) {
	payload := map[string]string{"key": c.key, "cmd": command}
	if c.session != "" {
		payload["session"] = c.session
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/post", bytes.NewReader(b))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("远程命令请求失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	out := strings.TrimSpace(string(body))
	if resp.StatusCode != http.StatusOK {
		return out, fmt.Errorf("远程命令失败 HTTP %d: %s", resp.StatusCode, out)
	}
	return out, nil
}

// Upload streams a local file to an absolute remote path.
func (c *Client) Upload(ctx context.Context, localPath, remotePath string) error {
	f, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer f.Close()

	pr, pw := io.Pipe()
	mw := multipart.NewWriter(pw)
	go func() {
		var writeErr error
		defer func() { _ = pw.CloseWithError(writeErr) }()
		if err := mw.WriteField("path", remotePath); err != nil {
			writeErr = err
			return
		}
		part, err := mw.CreateFormFile("file", filepath.Base(localPath))
		if err != nil {
			writeErr = err
			return
		}
		if _, err := io.Copy(part, f); err != nil {
			writeErr = err
			return
		}
		writeErr = mw.Close()
	}()

	q := url.Values{"key": {c.key}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/file_send?"+q.Encode(), pr)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("上传失败: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("上传失败 HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

func (c *Client) simpleGET(ctx context.Context, path string, q url.Values) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path+"?"+q.Encode(), nil)
	if err != nil {
		return "", err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	out := strings.TrimSpace(string(body))
	if resp.StatusCode != http.StatusOK {
		return out, fmt.Errorf("HTTP %d: %s", resp.StatusCode, out)
	}
	return out, nil
}
