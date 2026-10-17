package app

import (
	"context"
	"github.com/gin-gonic/gin"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Opt-in real Gin fixture server. Its database and files live in t.TempDir;
// it never writes synthetic examples into the personal workbench.
func TestBrowserFixtureServer(t *testing.T) {
	if os.Getenv("XHS_BROWSER_SMOKE") != "1" {
		t.Skip("set XHS_BROWSER_SMOKE=1 for browser integration")
	}
	root, e := filepath.Abs("../..")
	if e != nil {
		t.Fatal(e)
	}
	a, e := New(root, t.TempDir(), "127.0.0.1:8081")
	if e != nil {
		t.Fatal(e)
	}
	defer a.Close()
	r := a.Router()
	seedSource(t, apiFixture{app: a, router: r})
	finished := make(chan struct{}, 1)
	r.POST("/_test/finish", func(c *gin.Context) {
		if c.GetHeader("X-CSRF-Token") != a.CSRF {
			c.AbortWithStatus(403)
			return
		}
		c.Status(204)
		select {
		case finished <- struct{}{}:
		default:
		}
	})
	listener, e := net.Listen("tcp", a.Addr)
	if e != nil {
		t.Fatal(e)
	}
	srv := &http.Server{Handler: r, ReadHeaderTimeout: 10 * time.Second}
	a.StartWorkers()
	go srv.Serve(listener)
	t.Log("Real browser fixture at http://127.0.0.1:8081; POST /_test/finish ends this isolated server")
	select {
	case <-finished:
	case <-time.After(5 * time.Minute):
		t.Error("browser integration timed out")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	srv.Shutdown(ctx)
}
