package scgi

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Client executes XML-RPC requests through a Unix SCGI socket.
type Client struct {
	SocketPath string
	Timeout    time.Duration
}

func (c *Client) Do(ctx context.Context, payload []byte) ([]byte, error) {
	t := c.Timeout
	if t == 0 {
		t = 15 * time.Second
	}

	dialer := net.Dialer{Timeout: t}
	conn, err := dialer.DialContext(ctx, "unix", c.SocketPath)
	if err != nil {
		return nil, fmt.Errorf("dial unix socket: %w", err)
	}
	defer conn.Close()

	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	} else {
		_ = conn.SetDeadline(time.Now().Add(t))
	}

	headers := buildSCGIHeaders(len(payload))
	request := make([]byte, 0, len(headers)+len(payload)+64)
	request = append(request, []byte(strconv.Itoa(len(headers)))...)
	request = append(request, ':')
	request = append(request, headers...)
	request = append(request, ',')
	request = append(request, payload...)

	if _, err := conn.Write(request); err != nil {
		return nil, fmt.Errorf("write scgi request: %w", err)
	}

	raw, err := io.ReadAll(conn)
	if err != nil {
		return nil, fmt.Errorf("read scgi response: %w", err)
	}
	if len(raw) == 0 {
		return nil, errors.New("empty scgi response")
	}

	body, status, err := parseSCGIResponse(raw)
	if err != nil {
		return nil, err
	}
	if status >= 400 {
		if len(body) > 240 {
			body = body[:240]
		}
		return nil, fmt.Errorf("scgi xml-rpc status %d: %s", status, string(body))
	}
	return body, nil
}

func buildSCGIHeaders(contentLength int) []byte {
	pairs := [][2]string{
		{"CONTENT_LENGTH", strconv.Itoa(contentLength)},
		{"SCGI", "1"},
		{"REQUEST_METHOD", "POST"},
		{"REQUEST_URI", "/RPC2"},
		{"CONTENT_TYPE", "text/xml"},
	}

	buf := make([]byte, 0, 128)
	for _, pair := range pairs {
		buf = append(buf, pair[0]...)
		buf = append(buf, 0)
		buf = append(buf, pair[1]...)
		buf = append(buf, 0)
	}
	return buf
}

func parseSCGIResponse(raw []byte) ([]byte, int, error) {
	if bytes.HasPrefix(raw, []byte("HTTP/")) {
		resp, err := http.ReadResponse(bufio.NewReader(bytes.NewReader(raw)), &http.Request{Method: http.MethodPost})
		if err != nil {
			return nil, 0, fmt.Errorf("parse http response: %w", err)
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, 0, fmt.Errorf("read response body: %w", err)
		}
		return body, resp.StatusCode, nil
	}

	headers, body, found := splitHeadersBody(raw)
	if !found {
		return nil, 0, errors.New("invalid scgi response: no header/body separator")
	}

	status := 200
	for _, line := range strings.Split(string(headers), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(strings.ToLower(line), "status:") {
			s := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, "Status:"), "status:"))
			fields := strings.Fields(s)
			if len(fields) > 0 {
				if code, err := strconv.Atoi(fields[0]); err == nil {
					status = code
				}
			}
		}
	}

	return body, status, nil
}

func splitHeadersBody(raw []byte) ([]byte, []byte, bool) {
	if idx := bytes.Index(raw, []byte("\r\n\r\n")); idx >= 0 {
		return raw[:idx], raw[idx+4:], true
	}
	if idx := bytes.Index(raw, []byte("\n\n")); idx >= 0 {
		return raw[:idx], raw[idx+2:], true
	}
	return nil, nil, false
}
