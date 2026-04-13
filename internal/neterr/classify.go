// Package neterr classifies errors from HTTP clients and network I/O for retries and user-facing reports.
package neterr

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"syscall"
)

// Kind describes a coarse failure category.
type Kind string

const (
	KindNone             Kind = ""
	KindTimeout          Kind = "timeout"
	KindNetworkTransient Kind = "network_transient" // connection reset, broken pipe, temporary failure
	KindDNS              Kind = "dns"
	KindTLS              Kind = "tls"
	KindCancelled        Kind = "cancelled"
	KindHTTPClient       Kind = "http_client" // 4xx
	KindHTTPServer       Kind = "http_server" // 5xx
	KindOther            Kind = "other"
)

// Diagnosis is a structured explanation for logging and CLI output.
type Diagnosis struct {
	Kind    Kind
	Summary string // one line, safe for users
	Detail  string // underlying error text, may duplicate Summary
}

func (d Diagnosis) Error() string {
	if d.Detail != "" && d.Detail != d.Summary {
		return d.Summary + ": " + d.Detail
	}
	return d.Summary
}

// Classify inspects err and returns a diagnosis. Always returns non-zero Summary.
func Classify(err error) Diagnosis {
	if err == nil {
		return Diagnosis{Kind: KindNone, Summary: "no error"}
	}

	if errors.Is(err, context.Canceled) {
		return Diagnosis{Kind: KindCancelled, Summary: "request cancelled", Detail: err.Error()}
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return Diagnosis{Kind: KindTimeout, Summary: "deadline exceeded (timeout)", Detail: err.Error()}
	}

	var ne net.Error
	if errors.As(err, &ne) {
		if ne.Timeout() {
			return Diagnosis{Kind: KindTimeout, Summary: "network timeout", Detail: err.Error()}
		}
	}

	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return Diagnosis{Kind: KindDNS, Summary: "DNS resolution failed", Detail: err.Error()}
	}

	var opErr *net.OpError
	if errors.As(err, &opErr) {
		if opErr.Timeout() {
			return Diagnosis{Kind: KindTimeout, Summary: "network operation timed out", Detail: err.Error()}
		}
		if isConnReset(opErr.Err) {
			return Diagnosis{Kind: KindNetworkTransient, Summary: "connection reset by peer", Detail: err.Error()}
		}
		if isTemporaryNetErr(opErr.Err) {
			return Diagnosis{Kind: KindNetworkTransient, Summary: "temporary network failure", Detail: err.Error()}
		}
	}

	if isConnReset(err) {
		return Diagnosis{Kind: KindNetworkTransient, Summary: "connection reset", Detail: err.Error()}
	}

	var urlErr *os.PathError
	if errors.As(err, &urlErr) {
		// rare for HTTP; keep as other
		_ = urlErr
	}

	msg := err.Error()
	if strings.Contains(msg, "tls:") || strings.Contains(msg, "x509") || strings.Contains(msg, "certificate") {
		return Diagnosis{Kind: KindTLS, Summary: "TLS/certificate error", Detail: err.Error()}
	}

	return Diagnosis{Kind: KindOther, Summary: "request failed", Detail: err.Error()}
}

func isConnReset(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.ECONNRESET) {
		return true
	}
	if errors.Is(err, syscall.EPIPE) {
		return true
	}
	// Windows WSAECONNRESET etc. surface as syscall.Errno in some paths
	if errno, ok := err.(syscall.Errno); ok {
		switch errno {
		case syscall.ECONNRESET, syscall.EPIPE, syscall.ETIMEDOUT:
			return true
		}
	}
	s := err.Error()
	return strings.Contains(s, "connection reset") || strings.Contains(s, "broken pipe")
}

func isTemporaryNetErr(err error) bool {
	var te interface{ Temporary() bool }
	if errors.As(err, &te) && te.Temporary() {
		return true
	}
	return false
}

// ShouldRetry reports whether a quick retry may help (transient network or timeout).
func ShouldRetry(d Diagnosis) bool {
	switch d.Kind {
	case KindNetworkTransient, KindTimeout:
		return true
	default:
		return false
	}
}

// FormatSlowOrFailed builds a user-facing message when an operation exceeded a deadline or failed after retries.
func FormatSlowOrFailed(operation string, lastErr error, attempts int, d Diagnosis) string {
	if lastErr == nil {
		return ""
	}
	if ShouldRetry(d) && attempts > 1 {
		return fmt.Sprintf("%s: still failing after %d attempt(s) — classified as %s (often network instability; check VPN/firewall/DNS). Last: %v",
			operation, attempts, d.Kind, lastErr)
	}
	if ShouldRetry(d) {
		return fmt.Sprintf("%s: %s — if this persists, check network/VPN/firewall and gateway reachability. Detail: %v",
			operation, d.Summary, lastErr)
	}
	return fmt.Sprintf("%s: %s (not retried as non-network). Detail: %v", operation, d.Summary, lastErr)
}

// ClassifyHTTPResponse maps status codes when the transport succeeded.
func ClassifyHTTPResponse(code int) Diagnosis {
	switch {
	case code >= 500:
		return Diagnosis{Kind: KindHTTPServer, Summary: fmt.Sprintf("server error HTTP %d", code)}
	case code >= 400:
		return Diagnosis{Kind: KindHTTPClient, Summary: fmt.Sprintf("client/request error HTTP %d", code)}
	default:
		return Diagnosis{Kind: KindNone, Summary: fmt.Sprintf("HTTP %d", code)}
	}
}

// IsHTTPSuccess is true for 2xx.
func IsHTTPSuccess(code int) bool {
	return code >= 200 && code < 300
}
