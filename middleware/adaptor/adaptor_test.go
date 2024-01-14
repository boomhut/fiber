//nolint:bodyclose, contextcheck, revive // Much easier to just ignore memory leaks in tests
package adaptor

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/boomhut/fiber/v3"
	"github.com/stretchr/testify/require"
	"github.com/valyala/fasthttp"
)

func Test_HTTPHandler(t *testing.T) {
	expectedMethod := fiber.MethodPost
	expectedProto := "HTTP/1.1"
	expectedProtoMajor := 1
	expectedProtoMinor := 1
	expectedRequestURI := "/foo/bar?baz=123"
	expectedBody := "body 123 foo bar baz"
	expectedContentLength := len(expectedBody)
	expectedHost := "foobar.com"
	expectedRemoteAddr := "1.2.3.4:6789"
	expectedHeader := map[string]string{
		"Foo-Bar":         "baz",
		"Abc":             "defg",
		"XXX-Remote-Addr": "123.43.4543.345",
	}
	expectedURL, err := url.ParseRequestURI(expectedRequestURI)
	require.NoError(t, err)

	expectedContextKey := "contextKey"
	expectedContextValue := "contextValue"

	callsCount := 0
	nethttpH := func(w http.ResponseWriter, r *http.Request) {
		callsCount++
		require.Equal(t, expectedMethod, r.Method, "Method")
		require.Equal(t, expectedProto, r.Proto, "Proto")
		require.Equal(t, expectedProtoMajor, r.ProtoMajor, "ProtoMajor")
		require.Equal(t, expectedProtoMinor, r.ProtoMinor, "ProtoMinor")
		require.Equal(t, expectedRequestURI, r.RequestURI, "RequestURI")
		require.Equal(t, expectedContentLength, int(r.ContentLength), "ContentLength")
		require.Equal(t, 0, len(r.TransferEncoding), "TransferEncoding")
		require.Equal(t, expectedHost, r.Host, "Host")
		require.Equal(t, expectedRemoteAddr, r.RemoteAddr, "RemoteAddr")

		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		require.Equal(t, expectedBody, string(body), "Body")
		require.Equal(t, expectedURL, r.URL, "URL")
		require.Equal(t, expectedContextValue, r.Context().Value(expectedContextKey), "Context")

		for k, expectedV := range expectedHeader {
			v := r.Header.Get(k)
			require.Equal(t, expectedV, v, "Header")
		}

		w.Header().Set("Header1", "value1")
		w.Header().Set("Header2", "value2")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, "request body is %q", body)
	}
	fiberH := HTTPHandlerFunc(http.HandlerFunc(nethttpH))
	fiberH = setFiberContextValueMiddleware(fiberH, expectedContextKey, expectedContextValue)

	var fctx fasthttp.RequestCtx
	var req fasthttp.Request

	req.Header.SetMethod(expectedMethod)
	req.SetRequestURI(expectedRequestURI)
	req.Header.SetHost(expectedHost)
	req.BodyWriter().Write([]byte(expectedBody)) //nolint:errcheck, gosec // not needed
	for k, v := range expectedHeader {
		req.Header.Set(k, v)
	}

	remoteAddr, err := net.ResolveTCPAddr("tcp", expectedRemoteAddr)
	require.NoError(t, err)

	fctx.Init(&req, remoteAddr, nil)
	app := fiber.New()
	ctx := app.NewCtx(&fctx)
	defer app.ReleaseCtx(ctx)

	err = fiberH(ctx)
	require.NoError(t, err)
	require.Equal(t, 1, callsCount, "callsCount")

	resp := &fctx.Response
	require.Equal(t, http.StatusBadRequest, resp.StatusCode(), "StatusCode")
	require.Equal(t, "value1", string(resp.Header.Peek("Header1")), "Header1")
	require.Equal(t, "value2", string(resp.Header.Peek("Header2")), "Header2")

	expectedResponseBody := fmt.Sprintf("request body is %q", expectedBody)
	require.Equal(t, expectedResponseBody, string(resp.Body()), "Body")
}

type contextKey string

func (c contextKey) String() string {
	return "test-" + string(c)
}

var (
	TestContextKey       = contextKey("TestContextKey")
	TestContextSecondKey = contextKey("TestContextSecondKey")
)

func Test_HTTPMiddleware(t *testing.T) {
	tests := []struct {
		name       string
		url        string
		method     string
		statusCode int
	}{
		{
			name:       "Should return 200",
			url:        "/",
			method:     "POST",
			statusCode: 200,
		},
		{
			name:       "Should return 405",
			url:        "/",
			method:     "GET",
			statusCode: 405,
		},
		{
			name:       "Should return 400",
			url:        "/unknown",
			method:     "POST",
			statusCode: 404,
		},
	}

	nethttpMW := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodPost {
				w.WriteHeader(http.StatusMethodNotAllowed)
				return
			}
			r = r.WithContext(context.WithValue(r.Context(), TestContextKey, "okay"))
			r = r.WithContext(context.WithValue(r.Context(), TestContextSecondKey, "not_okay"))
			r = r.WithContext(context.WithValue(r.Context(), TestContextSecondKey, "okay"))

			next.ServeHTTP(w, r)
		})
	}

	app := fiber.New()
	app.Use(HTTPMiddleware(nethttpMW))
	app.Post("/", func(c fiber.Ctx) error {
		value := c.Context().Value(TestContextKey)
		val, ok := value.(string)
		if !ok {
			t.Error("unexpected error on type-assertion")
		}
		if value != nil {
			c.Set("context_okay", val)
		}
		value = c.Context().Value(TestContextSecondKey)
		if value != nil {
			val, ok := value.(string)
			if !ok {
				t.Error("unexpected error on type-assertion")
			}
			c.Set("context_second_okay", val)
		}
		return c.SendStatus(fiber.StatusOK)
	})

	for _, tt := range tests {
		req, err := http.NewRequestWithContext(context.Background(), tt.method, tt.url, nil)
		require.NoError(t, err)

		resp, err := app.Test(req)
		require.NoError(t, err)
		require.Equal(t, tt.statusCode, resp.StatusCode, "StatusCode")
	}

	req, err := http.NewRequestWithContext(context.Background(), fiber.MethodPost, "/", nil)
	require.NoError(t, err)

	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, resp.Header.Get("context_okay"), "okay")
	require.Equal(t, resp.Header.Get("context_second_okay"), "okay")
}

func Test_FiberHandler(t *testing.T) {
	testFiberToHandlerFunc(t, false)
}

func Test_FiberApp(t *testing.T) {
	testFiberToHandlerFunc(t, false, fiber.New())
}

func Test_FiberHandlerDefaultPort(t *testing.T) {
	testFiberToHandlerFunc(t, true)
}

func Test_FiberAppDefaultPort(t *testing.T) {
	testFiberToHandlerFunc(t, true, fiber.New())
}

func testFiberToHandlerFunc(t *testing.T, checkDefaultPort bool, app ...*fiber.App) {
	t.Helper()

	expectedMethod := fiber.MethodPost
	expectedRequestURI := "/foo/bar?baz=123"
	expectedBody := "body 123 foo bar baz"
	expectedContentLength := len(expectedBody)
	expectedHost := "foobar.com"
	expectedRemoteAddr := "1.2.3.4:6789"
	if checkDefaultPort {
		expectedRemoteAddr = "1.2.3.4:80"
	}
	expectedHeader := map[string]string{
		"Foo-Bar":         "baz",
		"Abc":             "defg",
		"XXX-Remote-Addr": "123.43.4543.345",
	}
	expectedURL, err := url.ParseRequestURI(expectedRequestURI)
	require.NoError(t, err)

	callsCount := 0
	fiberH := func(c fiber.Ctx) error {
		callsCount++
		require.Equal(t, expectedMethod, c.Method(), "Method")
		require.Equal(t, expectedRequestURI, string(c.Context().RequestURI()), "RequestURI")
		require.Equal(t, expectedContentLength, c.Context().Request.Header.ContentLength(), "ContentLength")
		require.Equal(t, expectedHost, c.Hostname(), "Host")
		require.Equal(t, expectedRemoteAddr, c.Context().RemoteAddr().String(), "RemoteAddr")

		body := string(c.Body())
		require.Equal(t, expectedBody, body, "Body")
		require.Equal(t, expectedURL.String(), c.OriginalURL(), "URL")

		for k, expectedV := range expectedHeader {
			v := c.Get(k)
			require.Equal(t, expectedV, v, "Header")
		}

		c.Set("Header1", "value1")
		c.Set("Header2", "value2")
		c.Status(fiber.StatusBadRequest)
		_, err := c.Write([]byte(fmt.Sprintf("request body is %q", body)))
		return err
	}

	var handlerFunc http.HandlerFunc
	if len(app) > 0 {
		app[0].Post("/foo/bar", fiberH)
		handlerFunc = FiberApp(app[0])
	} else {
		handlerFunc = FiberHandlerFunc(fiberH)
	}

	var r http.Request

	r.Method = expectedMethod
	r.Body = &netHTTPBody{[]byte(expectedBody)}
	r.RequestURI = expectedRequestURI
	r.ContentLength = int64(expectedContentLength)
	r.Host = expectedHost
	r.RemoteAddr = expectedRemoteAddr
	if checkDefaultPort {
		r.RemoteAddr = "1.2.3.4"
	}

	hdr := make(http.Header)
	for k, v := range expectedHeader {
		hdr.Set(k, v)
	}
	r.Header = hdr

	var w netHTTPResponseWriter
	handlerFunc.ServeHTTP(&w, &r)

	require.Equal(t, http.StatusBadRequest, w.StatusCode(), "StatusCode")
	require.Equal(t, "value1", w.Header().Get("Header1"), "Header1")
	require.Equal(t, "value2", w.Header().Get("Header2"), "Header2")

	expectedResponseBody := fmt.Sprintf("request body is %q", expectedBody)
	require.Equal(t, expectedResponseBody, string(w.body), "Body")
}

func setFiberContextValueMiddleware(next fiber.Handler, key string, value any) fiber.Handler {
	return func(c fiber.Ctx) error {
		c.Locals(key, value)
		return next(c)
	}
}

func Test_FiberHandler_RequestNilBody(t *testing.T) {
	expectedMethod := fiber.MethodGet
	expectedRequestURI := "/foo/bar"
	expectedContentLength := 0

	callsCount := 0
	fiberH := func(c fiber.Ctx) error {
		callsCount++
		require.Equal(t, expectedMethod, c.Method(), "Method")
		require.Equal(t, expectedRequestURI, string(c.Context().RequestURI()), "RequestURI")
		require.Equal(t, expectedContentLength, c.Context().Request.Header.ContentLength(), "ContentLength")

		_, err := c.Write([]byte("request body is nil"))
		return err
	}
	nethttpH := FiberHandler(fiberH)

	var r http.Request

	r.Method = expectedMethod
	r.RequestURI = expectedRequestURI

	var w netHTTPResponseWriter
	nethttpH.ServeHTTP(&w, &r)

	expectedResponseBody := "request body is nil"
	require.Equal(t, expectedResponseBody, string(w.body), "Body")
}

type netHTTPBody struct {
	b []byte
}

func (r *netHTTPBody) Read(p []byte) (int, error) {
	if len(r.b) == 0 {
		return 0, io.EOF
	}
	n := copy(p, r.b)
	r.b = r.b[n:]
	return n, nil
}

func (r *netHTTPBody) Close() error {
	r.b = r.b[:0]
	return nil
}

type netHTTPResponseWriter struct {
	statusCode int
	h          http.Header
	body       []byte
}

func (w *netHTTPResponseWriter) StatusCode() int {
	if w.statusCode == 0 {
		return http.StatusOK
	}
	return w.statusCode
}

func (w *netHTTPResponseWriter) Header() http.Header {
	if w.h == nil {
		w.h = make(http.Header)
	}
	return w.h
}

func (w *netHTTPResponseWriter) WriteHeader(statusCode int) {
	w.statusCode = statusCode
}

func (w *netHTTPResponseWriter) Write(p []byte) (int, error) {
	w.body = append(w.body, p...)
	return len(p), nil
}

func Test_ConvertRequest(t *testing.T) {
	t.Parallel()

	app := fiber.New()

	app.Get("/test", func(c fiber.Ctx) error {
		httpReq, err := ConvertRequest(c, false)
		if err != nil {
			return err
		}

		return c.SendString("Request URL: " + httpReq.URL.String())
	})

	resp, err := app.Test(httptest.NewRequest(fiber.MethodGet, "/test?hello=world&another=test", http.NoBody))
	require.Equal(t, nil, err, "app.Test(req)")
	require.Equal(t, http.StatusOK, resp.StatusCode, "Status code")

	body, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, "Request URL: /test?hello=world&another=test", string(body))
}

// Benchmark for FiberHandlerFunc
func Benchmark_FiberHandlerFunc_1MB(b *testing.B) {
	fiberH := func(c fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	}
	handlerFunc := FiberHandlerFunc(fiberH)

	// Create body content
	bodyContent := make([]byte, 1*1024*1024)
	bodyBuffer := bytes.NewBuffer(bodyContent)

	r := http.Request{
		Method: http.MethodPost,
		Body:   http.NoBody,
	}

	// Replace the empty Body with our buffer
	r.Body = io.NopCloser(bodyBuffer)
	defer r.Body.Close() //nolint:errcheck // not needed

	// Create recorder
	w := httptest.NewRecorder()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		handlerFunc.ServeHTTP(w, &r)
	}
}

func Benchmark_FiberHandlerFunc_10MB(b *testing.B) {
	fiberH := func(c fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	}
	handlerFunc := FiberHandlerFunc(fiberH)

	// Create body content
	bodyContent := make([]byte, 10*1024*1024)
	bodyBuffer := bytes.NewBuffer(bodyContent)

	r := http.Request{
		Method: http.MethodPost,
		Body:   http.NoBody,
	}

	// Replace the empty Body with our buffer
	r.Body = io.NopCloser(bodyBuffer)
	defer r.Body.Close() //nolint:errcheck // not needed

	// Create recorder
	w := httptest.NewRecorder()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		handlerFunc.ServeHTTP(w, &r)
	}
}

func Benchmark_FiberHandlerFunc_50MB(b *testing.B) {
	fiberH := func(c fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	}
	handlerFunc := FiberHandlerFunc(fiberH)

	// Create body content
	bodyContent := make([]byte, 50*1024*1024)
	bodyBuffer := bytes.NewBuffer(bodyContent)

	r := http.Request{
		Method: http.MethodPost,
		Body:   http.NoBody,
	}

	// Replace the empty Body with our buffer
	r.Body = io.NopCloser(bodyBuffer)
	defer r.Body.Close() //nolint:errcheck // not needed

	// Create recorder
	w := httptest.NewRecorder()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		handlerFunc.ServeHTTP(w, &r)
	}
}
