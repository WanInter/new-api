package router

import (
	"testing"

	"github.com/gin-gonic/gin"
)

func hasRoute(routes []gin.RouteInfo, method string, path string) bool {
	for _, route := range routes {
		if route.Method == method && route.Path == path {
			return true
		}
	}
	return false
}

func TestSetApiRouterRegistersWaffoPancakeRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	engine := gin.New()

	SetApiRouter(engine)
	routes := engine.Routes()

	requiredRoutes := []struct {
		method string
		path   string
	}{
		{method: "POST", path: "/api/waffo-pancake/webhook"},
		{method: "POST", path: "/api/user/waffo-pancake/amount"},
		{method: "POST", path: "/api/user/waffo-pancake/pay"},
	}

	for _, route := range requiredRoutes {
		if !hasRoute(routes, route.method, route.path) {
			t.Fatalf("expected route %s %s to be registered", route.method, route.path)
		}
	}
}
