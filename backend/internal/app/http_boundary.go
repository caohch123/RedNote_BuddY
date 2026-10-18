package app

import (
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

// ConfigureFrontendOrigins must be called before Router starts serving requests.
func (a *App) ConfigureFrontendOrigins(origins []string) error {
	allowed := map[string]bool{}
	for _, raw := range origins {
		origin := strings.TrimSpace(raw)
		if origin == "" {
			continue
		}
		u, err := url.Parse(origin)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.Path != "" || u.RawQuery != "" || u.Fragment != "" || strings.ContainsAny(u.Host, "*\\") {
			return fmt.Errorf("invalid frontend origin: %q (use an exact http(s) origin without a path)", origin)
		}
		allowed[origin] = true
	}
	a.frontendOrigins = allowed
	return nil
}

func (a *App) httpBoundary() gin.HandlerFunc {
	return a.boundary(true)
}

func (a *App) boundary(checkCSRF bool) gin.HandlerFunc {
	_, port, _ := net.SplitHostPort(a.Addr)
	return func(c *gin.Context) {
		if c.Request.Host != a.Addr && c.Request.Host != "localhost:"+port {
			fail(c, 403, "仅允许本机工作台访问")
			return
		}
		origin := c.GetHeader("Origin")
		if origin != "" {
			if origin != "http://"+a.Addr && origin != "http://localhost:"+port && !a.frontendOrigins[origin] {
				fail(c, 403, "来源不允许")
				return
			}
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Credentials", "true")
			c.Header("Vary", "Origin")
			c.Header("Access-Control-Allow-Methods", "GET, HEAD, POST, PATCH, PUT, DELETE, OPTIONS")
			c.Header("Access-Control-Allow-Headers", "Content-Type, X-CSRF-Token, Range")
			c.Header("Access-Control-Expose-Headers", "Content-Disposition, Content-Range, Accept-Ranges")
		}
		if c.Request.Method == http.MethodOptions && origin != "" && c.GetHeader("Access-Control-Request-Method") != "" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		if checkCSRF && strings.HasPrefix(c.Request.URL.Path, "/api/") && c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead {
			if c.GetHeader("X-CSRF-Token") != a.CSRF {
				fail(c, 403, "请刷新页面后重试")
				return
			}
		}
		if !strings.HasPrefix(c.Request.URL.Path, "/api/v1/assets") {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 5<<20)
		}
		c.Next()
	}
}
