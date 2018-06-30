package rpc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/rs/cors"
)

const (
	contentType             = "application/json"
	maxRequestContentLength = 1024 * 128
)

func DialHTTP(endpoint string) (*Client, error) {
	return DialHTTPWithClient(endpoint, new(http.Client))
}

func DialHTTPWithClient(endpoint string, client *http.Client) (*Client, error) {
	req, err := http.NewRequest(http.MethodPost, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", contentType)

	initctx := context.Background()
	return newClient(initctx, func(context.Context) (net.Conn, error) {
		return &httpConn{client: client, req: req, closed: make(chan struct{})}, nil
	})
}

func NewHTTPServer(cors []string, vhosts []string, srv *Server) *http.Server {
	handler := newCorsHandler(srv, cors)
	handler = newVHostHandler(vhosts, handler)
	return &http.Server{
		Handler:      handler,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 10 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
}

func newCorsHandler(srv *Server, allowedOrigins []string) http.Handler {
	if len(allowedOrigins) == 0 {
		return srv
	}

	c := cors.New(cors.Options{
		AllowedOrigins: allowedOrigins,
		AllowedMethods: []string{http.MethodPost, http.MethodGet},
		MaxAge:         600,
		AllowedHeaders: []string{"*"},
	})
	return c.Handler(srv)
}

func newVHostHandler(vhosts []string, next http.Handler) http.Handler {
	vhostMap := make(map[string]struct{})
	for _, allowedHost := range vhosts {
		vhostMap[strings.ToLower(allowedHost)] = struct{}{}
	}
	return &virtualHostHandler{vhostMap, next}
}

type virtualHostHandler struct {
	vhosts map[string]struct{}
	next   http.Handler
}

func (h *virtualHostHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Host == "" {
		h.next.ServeHTTP(w, r)
		return
	}

	host, _, err := net.SplitHostPort(r.Host)
	if err != nil {
		host = r.Host
	}
	if ipAddr := net.ParseIP(host); ipAddr != nil {
		h.next.ServeHTTP(w, r)
		return
	}

	if _, exist := h.vhosts["*"]; exist {
		h.next.ServeHTTP(w, r)
		return
	}
	if _, exist := h.vhosts[host]; exist {
		h.next.ServeHTTP(w, r)
		return
	}

	http.Error(w, "invalid host specified", http.StatusForbidden)
}

func (srv *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && r.ContentLength == 0 && r.URL.RawQuery == "" {
		return
	}

	// 校验 request
	if code, err := validateRequest(r); err != nil {
		http.Error(w, err.Error(), code)
		return
	}

	// 设置 Context
	ctx := r.Context()
	ctx = context.WithValue(ctx, "remote", r.RemoteAddr)
	ctx = context.WithValue(ctx, "scheme", r.Proto)
	ctx = context.WithValue(ctx, "local", r.Host)

	// 构造 body@response, codec@Server
	body := io.LimitReader(r.Body, maxRequestContentLength)
	codec := NewJSONCodec(&httpReadWriteNopCloser{body, w})
	defer codec.Close()

	w.Header().Set("content-type", contentType)
	srv.ServeSingleRequest(ctx, codec, OptionMethodInvocation)
}

func validateRequest(r *http.Request) (int, error) {
	if r.Method == http.MethodPut || r.Method == http.MethodDelete {
		return http.StatusMethodNotAllowed, errors.New("method not allowed")
	}
	if r.ContentLength > maxRequestContentLength {
		err := fmt.Errorf("content length too large (%d > %d)", r.ContentLength, maxRequestContentLength)
		return http.StatusRequestEntityTooLarge, err
	}
	mt, _, err := mime.ParseMediaType(r.Header.Get("content-type"))
	if r.Method != http.MethodOptions && (err != nil || mt != contentType) {
		err := fmt.Errorf("invalid content type, only %s is supported", contentType)
		return http.StatusUnsupportedMediaType, err
	}
	return 0, nil
}

func (s *Server) ServeSingleRequest(ctx context.Context, codec ServerCodec, options CodecOption) {
	s.serveRequest(ctx, codec, true, options)
}

type httpReadWriteNopCloser struct {
	io.Reader
	io.Writer
}

func (t *httpReadWriteNopCloser) Close() error {
	return nil
}
