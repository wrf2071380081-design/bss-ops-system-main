package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"model_bss/internal/api/middleware"
	"model_bss/pkg/apperr"
	"model_bss/pkg/perm"
	"model_bss/pkg/response"
)

func employeeSessionSnapshot(c *gin.Context) {
	snapshot := middleware.SnapshotFrom(c)
	if snapshot == nil || snapshot.PortalType != "INTERNAL" || snapshot.OperatorType != "STAFF" {
		response.Error(c, apperr.ErrForbidden)
		return
	}
	c.Header("Cache-Control", "no-store")
	view := *snapshot
	if view.Perms == nil {
		view.Perms = []string{}
	}
	response.Success(c, gin.H{"snapshot": view})
}

var agentEmployeeToken = regexp.MustCompile("^[a-f0-9]{64}$")

// Forward only the catalog routes, using the authenticated employee's own token.
func registerAgentCatalog(internal *gin.RouterGroup, origin string) {
	upstream, err := url.Parse(origin)
	if err != nil || (upstream.Scheme != "http" && upstream.Scheme != "https") ||
		upstream.Host == "" || upstream.User != nil || (upstream.Path != "" && upstream.Path != "/") ||
		upstream.RawQuery != "" || upstream.Fragment != "" {
		return
	}
	client := &http.Client{
		Timeout:       55 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	group := internal.Group("/agent-catalog")
	for name, path := range map[string]string{
		"/tasks": "/employee/api/workbench", "/review": "/employee/api/catalog-review", "/schedules": "/employee/schedules",
	} {
		forward := func(c *gin.Context) {
			token := bearerFromHeader(c)
			op := middleware.OperatorFrom(c)
			if op == nil || op.PortalType != "INTERNAL" || op.OperatorType != "STAFF" || !agentEmployeeToken.MatchString(token) {
				response.Error(c, apperr.ErrForbidden)
				return
			}
			target := *upstream
			target.Path = path
			target.RawQuery = c.Request.URL.RawQuery
			if name == "/schedules" {
				target.RawQuery = "view=catalog"
			}
			var body io.Reader
			if c.Request.Method == http.MethodPost {
				input, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, 128<<10))
				if err != nil {
					response.Error(c, apperr.ErrInvalidParams)
					return
				}
				body = strings.NewReader(string(input))
			}
			request, err := http.NewRequestWithContext(c.Request.Context(), c.Request.Method, target.String(), body)
			if err != nil {
				c.Status(http.StatusBadGateway)
				return
			}
			request.AddCookie(&http.Cookie{Name: "bss_employee_session", Value: token})
			request.Header.Set("Origin", upstream.Scheme+"://"+upstream.Host)
			request.Header.Set("Content-Type", c.GetHeader("Content-Type"))
			result, err := client.Do(request)
			if err != nil {
				c.Status(http.StatusBadGateway)
				return
			}
			defer result.Body.Close()
			c.Header("Cache-Control", "no-store")
			if result.StatusCode == http.StatusNoContent {
				response.Success(c, gin.H{})
				return
			}
			if result.StatusCode >= 300 {
				status := result.StatusCode
				if status < 400 {
					status = http.StatusBadGateway
				}
				c.Status(status)
				return
			}
			const limit = 4 << 20
			payload, err := io.ReadAll(io.LimitReader(result.Body, limit+1))
			if err != nil || len(payload) > limit || !json.Valid(payload) {
				c.Status(http.StatusBadGateway)
				return
			}
			response.Success(c, json.RawMessage(payload))
		}
		group.GET(name, middleware.RequirePerm(perm.M1View), forward)
		if name == "/schedules" {
			// The authenticated DSH gateway checks AGENT_SCHEDULE_ADMIN; editing a plan is separate from editing a model.
			group.POST(name, forward)
		} else {
			group.POST(name, middleware.RequirePerm(perm.M1Edit), forward)
		}
	}
}
