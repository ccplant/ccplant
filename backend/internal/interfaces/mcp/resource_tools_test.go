package mcp

import (
	"context"
	"net/http"
	"net/url"
	"testing"

	"github.com/labstack/echo/v4"
	mcpsdk "github.com/modelcontextprotocol/go-sdk/mcp"
)

type stubResourceRequester struct{}

func (stubResourceRequester) Do(context.Context, string, string, url.Values, any) (any, error) {
	return map[string]any{"success": true}, nil
}

func TestRegisterResourceTools(t *testing.T) {
	server := NewMCPServer(nil, nil, nil, "user-1", nil, "", "", &mcpsdk.ServerOptions{})
	server.resourceRequester = stubResourceRequester{}
	server.RegisterTools()
}

func TestEchoResourceRequesterDispatchesJSONRequest(t *testing.T) {
	e := echo.New()
	e.PUT("/widgets/:id", func(c echo.Context) error {
		if got := c.Request().Header.Get("Authorization"); got != "Bearer secret" {
			t.Fatalf("authorization header = %q", got)
		}
		if got := c.QueryParam("scope"); got != "team" {
			t.Fatalf("scope = %q", got)
		}
		var body map[string]any
		if err := c.Bind(&body); err != nil {
			return err
		}
		return c.JSON(http.StatusOK, map[string]any{"id": c.Param("id"), "name": body["name"]})
	})

	headers := make(http.Header)
	headers.Set("Authorization", "Bearer secret")
	requester := newEchoResourceRequester(e, headers, nil)
	result, err := requester.Do(context.Background(), http.MethodPut, "/widgets/widget-1", url.Values{"scope": {"team"}}, map[string]any{"name": "updated"})
	if err != nil {
		t.Fatal(err)
	}
	got, ok := result.(map[string]any)
	if !ok || got["id"] != "widget-1" || got["name"] != "updated" {
		t.Fatalf("result = %#v", result)
	}
}

func TestEchoResourceRequesterReturnsAPIError(t *testing.T) {
	e := echo.New()
	e.GET("/widgets/:id", func(c echo.Context) error {
		return echo.NewHTTPError(http.StatusNotFound, "widget not found")
	})

	requester := newEchoResourceRequester(e, nil, nil)
	_, err := requester.Do(context.Background(), http.MethodGet, "/widgets/missing", nil, nil)
	if err == nil || err.Error() != "API returned status 404: {\"message\":\"widget not found\"}" {
		t.Fatalf("error = %v", err)
	}
}

func TestEchoResourceRequesterHandlesEmptySuccess(t *testing.T) {
	e := echo.New()
	e.DELETE("/widgets/:id", func(c echo.Context) error { return c.NoContent(http.StatusNoContent) })

	requester := newEchoResourceRequester(e, nil, nil)
	result, err := requester.Do(context.Background(), http.MethodDelete, "/widgets/widget-1", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, ok := result.(map[string]any)
	if !ok || got["success"] != true {
		t.Fatalf("result = %#v", result)
	}
}
