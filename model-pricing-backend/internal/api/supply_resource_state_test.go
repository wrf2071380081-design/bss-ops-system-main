package api

import "testing"

// DEC-010: all visible state changes must enter the plan lifecycle. The old
// direct route must not remain callable merely because its button was removed.
func TestDirectResourceStateHTTPRouteIsNotExposed(t *testing.T) {
	r := supplyHTTPRouter(supplyHTTPStaff(), nil, newSupplyHTTPIdemStore())
	rec := supplyHTTPCall(r, "POST", "/api/internal/resources/11/state", `{"expected_revision":"2","action":"OFFLINE","external_confirmed":true,"completion_note":"done","reason":"maintenance"}`, "direct-key", "trace")
	if rec.Code != 404 {
		t.Fatalf("direct state route still exposed: %d %s", rec.Code, rec.Body)
	}
}
