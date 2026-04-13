package marketplace

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/nextlevelbuilder/goclaw/internal/neterr"
)

const (
	uploadMaxAttempts = 3
	uploadRetryPause  = 2 * time.Second
	uploadChunkSize   = 4 << 20
)

// UploadPackageHTTP POSTs a zip to POST {baseURL}/v1/marketplace/packages/upload with multipart metadata.
// Retries on transport errors classified as network/timeout by neterr, and on HTTP 502/503/504.
// Non-retry failures return a wrapped error that includes classification for logging.
func UploadPackageHTTP(ctx context.Context, client *http.Client, baseURL, authBearer string, pkg Package, force bool, archive []byte, extraMetadata map[string]any) (map[string]any, error) {
	if client == nil {
		client = http.DefaultClient
	}
	if len(archive) > uploadChunkSize {
		return uploadPackageHTTPResumable(ctx, client, baseURL, authBearer, pkg, force, archive, extraMetadata)
	}
	var lastDiag neterr.Diagnosis
	var lastHTTPCode int
	var lastErr error

	for attempt := 1; attempt <= uploadMaxAttempts; attempt++ {
		out, code, err := uploadPackageHTTPOnce(ctx, client, baseURL, authBearer, pkg, force, archive, extraMetadata)
		if err == nil {
			return out, nil
		}
		lastErr = err
		lastHTTPCode = code

		lastDiag = neterr.Classify(err)
		retry := neterr.ShouldRetry(lastDiag)
		if code == http.StatusBadGateway || code == http.StatusServiceUnavailable || code == http.StatusGatewayTimeout {
			retry = attempt < uploadMaxAttempts
			lastDiag = neterr.Diagnosis{Kind: neterr.KindHTTPServer, Summary: fmt.Sprintf("HTTP %d from gateway", code)}
		}

		if !retry || attempt >= uploadMaxAttempts {
			msg := neterr.FormatSlowOrFailed("marketplace upload", lastErr, attempt, lastDiag)
			if lastHTTPCode > 0 {
				return nil, fmt.Errorf("%s (http=%d)", msg, lastHTTPCode)
			}
			return nil, fmt.Errorf("%s", msg)
		}

		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(uploadRetryPause * time.Duration(attempt)):
		}
	}
	return nil, fmt.Errorf("marketplace upload: internal retry loop exited unexpectedly (last=%v)", lastErr)
}

func uploadPackageHTTPOnce(ctx context.Context, client *http.Client, baseURL, authBearer string, pkg Package, force bool, archive []byte, extraMetadata map[string]any) (map[string]any, int, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", pkg.Name+"-"+pkg.Version+".zip")
	if err != nil {
		return nil, 0, err
	}
	if _, err := part.Write(archive); err != nil {
		return nil, 0, err
	}
	meta := map[string]any{
		"package": pkg,
		"force":   force,
	}
	for k, v := range extraMetadata {
		meta[k] = v
	}
	metaRaw, err := json.Marshal(meta)
	if err != nil {
		return nil, 0, err
	}
	if err := writer.WriteField("metadata", string(metaRaw)); err != nil {
		return nil, 0, err
	}
	if err := writer.Close(); err != nil {
		return nil, 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/marketplace/packages/upload", &body)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if authBearer != "" {
		req.Header.Set("Authorization", "Bearer "+authBearer)
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))

	if resp.StatusCode >= 400 {
		// Parse JSON error if present
		var msg string
		var er struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(raw, &er) == nil && er.Error != "" {
			msg = er.Error
		} else {
			msg = string(raw)
		}
		return nil, resp.StatusCode, fmt.Errorf("gateway HTTP %d: %s", resp.StatusCode, msg)
	}

	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, resp.StatusCode, err
	}
	return out, resp.StatusCode, nil
}

func uploadPackageHTTPResumable(ctx context.Context, client *http.Client, baseURL, authBearer string, pkg Package, force bool, archive []byte, extraMetadata map[string]any) (map[string]any, error) {
	totalParts := (len(archive) + uploadChunkSize - 1) / uploadChunkSize
	uploadID := fmt.Sprintf("%s-%d", sanitizeUploadID(pkg.ID), time.Now().UnixNano())
	if uploadID == "" {
		uploadID = fmt.Sprintf("upload-%d", time.Now().UnixNano())
	}

	meta := map[string]any{
		"package": pkg,
		"force":   force,
	}
	for k, v := range extraMetadata {
		meta[k] = v
	}
	if _, ok := meta["file_name"]; !ok {
		meta["file_name"] = pkg.Name + "-" + pkg.Version + ".zip"
	}
	metaRaw, err := json.Marshal(meta)
	if err != nil {
		return nil, err
	}

	for part := 0; part < totalParts; part++ {
		start := part * uploadChunkSize
		end := start + uploadChunkSize
		if end > len(archive) {
			end = len(archive)
		}
		chunk := archive[start:end]
		var lastErr error
		var lastDiag neterr.Diagnosis
		var lastCode int
		for attempt := 1; attempt <= uploadMaxAttempts; attempt++ {
			code, err := uploadChunkHTTPOnce(ctx, client, baseURL, authBearer, uploadID, part+1, totalParts, chunk, func() []byte {
				if part == 0 {
					return metaRaw
				}
				return nil
			}())
			if err == nil {
				lastErr = nil
				break
			}
			lastErr = err
			lastCode = code
			lastDiag = neterr.Classify(err)
			retry := neterr.ShouldRetry(lastDiag)
			if code == http.StatusBadGateway || code == http.StatusServiceUnavailable || code == http.StatusGatewayTimeout {
				retry = attempt < uploadMaxAttempts
			}
			if !retry || attempt >= uploadMaxAttempts {
				break
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(uploadRetryPause * time.Duration(attempt)):
			}
		}
		if lastErr != nil {
			msg := neterr.FormatSlowOrFailed("marketplace resumable chunk upload", lastErr, uploadMaxAttempts, lastDiag)
			if lastCode > 0 {
				return nil, fmt.Errorf("%s (http=%d upload_id=%s part=%d)", msg, lastCode, uploadID, part+1)
			}
			return nil, fmt.Errorf("%s (upload_id=%s part=%d)", msg, uploadID, part+1)
		}
	}

	return uploadCompleteHTTPOnce(ctx, client, baseURL, authBearer, uploadID)
}

func uploadChunkHTTPOnce(ctx context.Context, client *http.Client, baseURL, authBearer, uploadID string, partNumber, totalParts int, chunk []byte, metadataRaw []byte) (int, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("file", fmt.Sprintf("part-%06d.bin", partNumber))
	if err != nil {
		return 0, err
	}
	if _, err := part.Write(chunk); err != nil {
		return 0, err
	}
	_ = writer.WriteField("upload_id", uploadID)
	_ = writer.WriteField("part_number", strconv.Itoa(partNumber))
	_ = writer.WriteField("total_parts", strconv.Itoa(totalParts))
	if len(metadataRaw) > 0 {
		_ = writer.WriteField("metadata", string(metadataRaw))
	}
	if err := writer.Close(); err != nil {
		return 0, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/marketplace/packages/upload/chunk", &body)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if authBearer != "" {
		req.Header.Set("Authorization", "Bearer "+authBearer)
	}
	resp, err := client.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return resp.StatusCode, fmt.Errorf("chunk upload HTTP %d: %s", resp.StatusCode, string(raw))
	}
	return resp.StatusCode, nil
}

func uploadCompleteHTTPOnce(ctx context.Context, client *http.Client, baseURL, authBearer, uploadID string) (map[string]any, error) {
	body, _ := json.Marshal(map[string]any{"upload_id": uploadID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/v1/marketplace/packages/upload/complete", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if authBearer != "" {
		req.Header.Set("Authorization", "Bearer "+authBearer)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("upload complete HTTP %d: %s", resp.StatusCode, string(raw))
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func sanitizeUploadID(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var b strings.Builder
	for _, r := range raw {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r == '.' {
			b.WriteRune(r)
			continue
		}
		b.WriteByte('-')
	}
	return strings.Trim(b.String(), "-")
}
