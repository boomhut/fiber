//nolint:bodyclose // Much easier to just ignore memory leaks in tests
package envvar

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"github.com/boomhut/fiber/v3"
	"github.com/stretchr/testify/require"
)

func TestEnvVarStructWithExportVarsExcludeVars(t *testing.T) {
	t.Setenv("testKey", "testEnvValue")
	t.Setenv("anotherEnvKey", "anotherEnvVal")
	t.Setenv("excludeKey", "excludeEnvValue")

	vars := newEnvVar(Config{
		ExportVars:  map[string]string{"testKey": "", "testDefaultKey": "testDefaultVal"},
		ExcludeVars: map[string]string{"excludeKey": ""},
	})

	require.Equal(t, vars.Vars["testKey"], "testEnvValue")
	require.Equal(t, vars.Vars["testDefaultKey"], "testDefaultVal")
	require.Equal(t, vars.Vars["excludeKey"], "")
	require.Equal(t, vars.Vars["anotherEnvKey"], "")
}

func TestEnvVarHandler(t *testing.T) {
	t.Setenv("testKey", "testVal")

	expectedEnvVarResponse, err := json.Marshal(
		struct {
			Vars map[string]string `json:"vars"`
		}{
			map[string]string{"testKey": "testVal"},
		})
	require.NoError(t, err)

	app := fiber.New()
	app.Use("/envvars", New(Config{
		ExportVars: map[string]string{"testKey": ""},
	}))

	req, err := http.NewRequestWithContext(context.Background(), fiber.MethodGet, "http://localhost/envvars", nil)
	require.NoError(t, err)
	resp, err := app.Test(req)
	require.NoError(t, err)

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	require.Equal(t, expectedEnvVarResponse, respBody)
}

func TestEnvVarHandlerNotMatched(t *testing.T) {
	app := fiber.New()
	app.Use("/envvars", New(Config{
		ExportVars: map[string]string{"testKey": ""},
	}))

	app.Get("/another-path", func(ctx fiber.Ctx) error {
		require.NoError(t, ctx.SendString("OK"))
		return nil
	})

	req, err := http.NewRequestWithContext(context.Background(), fiber.MethodGet, "http://localhost/another-path", nil)
	require.NoError(t, err)
	resp, err := app.Test(req)
	require.NoError(t, err)

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	require.Equal(t, []byte("OK"), respBody)
}

func TestEnvVarHandlerDefaultConfig(t *testing.T) {
	t.Setenv("testEnvKey", "testEnvVal")

	app := fiber.New()
	app.Use("/envvars", New())

	req, err := http.NewRequestWithContext(context.Background(), fiber.MethodGet, "http://localhost/envvars", nil)
	require.NoError(t, err)
	resp, err := app.Test(req)
	require.NoError(t, err)

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var envVars EnvVar
	require.Equal(t, nil, json.Unmarshal(respBody, &envVars))
	val := envVars.Vars["testEnvKey"]
	require.Equal(t, "testEnvVal", val)
}

func TestEnvVarHandlerMethod(t *testing.T) {
	app := fiber.New()
	app.Use("/envvars", New())

	req, err := http.NewRequestWithContext(context.Background(), fiber.MethodPost, "http://localhost/envvars", nil)
	require.NoError(t, err)
	resp, err := app.Test(req)
	require.NoError(t, err)
	require.Equal(t, fiber.StatusMethodNotAllowed, resp.StatusCode)
}

func TestEnvVarHandlerSpecialValue(t *testing.T) {
	testEnvKey := "testEnvKey"
	fakeBase64 := "testBase64:TQ=="
	t.Setenv(testEnvKey, fakeBase64)

	app := fiber.New()
	app.Use("/envvars", New())
	app.Use("/envvars/export", New(Config{ExportVars: map[string]string{testEnvKey: ""}}))

	req, err := http.NewRequestWithContext(context.Background(), fiber.MethodGet, "http://localhost/envvars", nil)
	require.NoError(t, err)
	resp, err := app.Test(req)
	require.NoError(t, err)

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)

	var envVars EnvVar
	require.Equal(t, nil, json.Unmarshal(respBody, &envVars))
	val := envVars.Vars[testEnvKey]
	require.Equal(t, fakeBase64, val)

	req, err = http.NewRequestWithContext(context.Background(), fiber.MethodGet, "http://localhost/envvars/export", nil)
	require.NoError(t, err)
	resp, err = app.Test(req)
	require.NoError(t, err)

	respBody, err = io.ReadAll(resp.Body)
	require.NoError(t, err)

	var envVarsExport EnvVar
	require.Equal(t, nil, json.Unmarshal(respBody, &envVarsExport))
	val = envVarsExport.Vars[testEnvKey]
	require.Equal(t, fakeBase64, val)
}
